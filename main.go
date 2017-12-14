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

func main() {
	var home = env.HomeDir()
	var kconfig string = ""
	var namespace string = "default"
	var podName string = ""
	var podImage string = ""

	var defaultKubeConfigPath = filepath.Join(home, ".kube", "config")
	flag.StringVar(&kconfig, "kubeconfig", defaultKubeConfigPath, "(optional) absolute path to the kubeconfig file")
	flag.StringVar(&namespace, "namespace", "default", "kubernetes namespace")
	flag.StringVar(&podName, "podName", "", "pod name for tracking container")
	flag.StringVar(&podImage, "podImage", "", "pod image for tracking container")
	flag.Parse()

	if podName == "" {
		log.Fatal("The terminator need the Pod name.")
	}
	if podImage == "" {
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

	log.Printf("Start tracking target namespace=%s pod=%s image=%s\n", namespace, podName, podImage)
	o, stop := trackPodContainers(clientset, namespace, podImage, podName)
Watch:
	for {
		statuses := <-o
		for _, v := range statuses {
			if isTargetContainerCompleted(v, podImage) {
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

func isTargetContainerCompleted(containerStatus core_v1.ContainerStatus, podImage string) bool {
	if containerStatus.Image == podImage {
		terminateStatus := containerStatus.State.Terminated
		log.Printf("Checking container status: %+v", terminateStatus)
		if terminateStatus != nil {
			// && terminateStatus.Reason == "Completed"
			return true
		}

	}
	return false
}

func trackPodContainers(clientset *kubernetes.Clientset, namespace, podImage, podName string) (chan []core_v1.ContainerStatus, chan struct{}) {
	o := make(chan []core_v1.ContainerStatus)
	stop := make(chan struct{})
	_, controller := kubemon.WatchPods(clientset, namespace, fields.Everything(), cache.ResourceEventHandlerFuncs{
		UpdateFunc: func(oldObj, newObj interface{}) {
			var pod *core_v1.Pod
			var ok bool
			if pod, ok = newObj.(*core_v1.Pod); !ok {
				return
			}
			if podName != pod.ObjectMeta.Name {
				return
			}

			o <- pod.Status.ContainerStatuses
		},
	})
	// _ = store
	go controller.Run(stop)
	return o, stop
}
