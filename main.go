package main

import (
	"flag"
	"log"
	"net/http"
	"os"
	"path/filepath"

	"bitbucket.org/linkernetworks/aurora/src/env"
	"bitbucket.org/linkernetworks/aurora/src/env/names"
	"bitbucket.org/linkernetworks/aurora/src/kubeconfig"
	"bitbucket.org/linkernetworks/aurora/src/kubemon"

	core_v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	// Uncomment the following line to load the gcp plugin (only required to authenticate against GKE clusters).
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
)

var buildNumber string

func main() {
	var home = env.HomeDir()
	var kconfig string = ""
	var namespace string = "default"
	var podName string = ""
	var targetName string = ""

	var defaultKubeConfigPath = filepath.Join(home, ".kube", "config")
	log.Println("Current BuildNumber: ", buildNumber)
	flag.StringVar(&kconfig, "kubeconfig", defaultKubeConfigPath, "(optional) absolute path to the kubeconfig file")
	flag.StringVar(&namespace, "namespace", "default", "kubernetes namespace")
	flag.StringVar(&podName, "podName", "", "pod name for tracking container")
	flag.StringVar(&targetName, "targetName", "", "pod image for tracking container")
	flag.Parse()

	if podName == "" {
		log.Fatal("The terminator need the Pod name.")
	}
	if targetName == "" {
		log.Fatal("The terminator need the target container image.")
	}

	var fluentdPort string = env.DefaultFluentdPort
	var fluentdStopEndpointUrl string
	if portstr, ok := os.LookupEnv(names.FluentdPort); ok {
		fluentdPort = portstr
	}
	fluentdStopEndpointUrl = "http://127.0.0.1:" + fluentdPort + "/api/processes.interruptWorkers"

	config, err := kubeconfig.Load("", kconfig)
	if err != nil {
		log.Fatal(err.Error())
	}

	// Create the clientset
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		log.Fatal(err.Error())
	}

	log.Printf("Start tracking target namespace=%s pod=%s image=%s\n", namespace, podName, targetName)
	o, stop := trackPodContainers(clientset, namespace, targetName, podName)
Watch:
	for {
		statuses := <-o
		for _, v := range statuses {
			log.Printf("Check pod status")
			log.Printf("PodName:%s, PodImage:%s", v.Name, v.Image)
			if isTargetContainerCompleted(v, targetName) {
				log.Printf("found target container completed: %+v\n", v)
				close(o)
				break Watch
			}
		}
	}

	log.Println("Sending stop watch signal..")
	var e struct{}
	stop <- e
	close(stop)

	log.Println("Sending terminate signal to fluentd: ", fluentdStopEndpointUrl)
	_, err = http.Get(fluentdStopEndpointUrl)
	if err != nil {
		log.Fatalf("Failed to close log-collector %v", err)
	}

	log.Println("Exiting...")
}

func isTargetContainerCompleted(containerStatus core_v1.ContainerStatus, targetName string) bool {
	if containerStatus.Name == targetName {
		terminateStatus := containerStatus.State.Terminated
		log.Printf("Checking container status: %+v", terminateStatus)
		log.Printf("The LastTerminationState of target container: %+v", containerStatus.LastTerminationState)
		if terminateStatus != nil {
			log.Printf("targetName %s termination detected.", targetName)
			return true
		}

	}
	return false
}

func trackPodContainers(clientset *kubernetes.Clientset, namespace, targetName, podName string) (chan []core_v1.ContainerStatus, chan struct{}) {
	o := make(chan []core_v1.ContainerStatus)
	stop := make(chan struct{})
	_, controller := kubemon.WatchPods(clientset, namespace, fields.Everything(), cache.ResourceEventHandlerFuncs{
		UpdateFunc: func(oldObj, newObj interface{}) {
			log.Println("Received UpdateFunc Event")
			var pod *core_v1.Pod
			var ok bool
			if pod, ok = newObj.(*core_v1.Pod); !ok {
				return
			}
			if podName != pod.ObjectMeta.Name {
				return
			}
			log.Println("Received Object with PodName", podName)

			o <- pod.Status.ContainerStatuses
		},
	})
	// _ = store
	go controller.Run(stop)
	return o, stop
}
