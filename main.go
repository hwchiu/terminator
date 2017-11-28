package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"

	"bitbucket.org/linkernetworks/cv-tracker/src/env"
	"bitbucket.org/linkernetworks/cv-tracker/src/env/names"
	"bitbucket.org/linkernetworks/cv-tracker/src/kubeconfig"
	"bitbucket.org/linkernetworks/cv-tracker/src/kubemon"

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
	var image string = ""
	var podName string = ""
	var ok bool
	flag.StringVar(&kconfig, "kubeconfig", filepath.Join(home, ".kube", "config"), "(optional) absolute path to the kubeconfig file")
	flag.Parse()

	if val, ok := os.LookupEnv(names.KubeNamespace); ok {
		namespace = val
	}

	if podName, ok = os.LookupEnv(names.PodName); !ok {
		log.Fatal(errors.New("The terminator need the Pod name."))
	}
	if image, ok = os.LookupEnv(names.JobImage); !ok {
		log.Fatal(errors.New("The terminator need the target container image."))
	}

	var fluentdPort string = env.DefaultFluentdPort
	var fluentdStopEndpointUrl string
	if portstr, ok := os.LookupEnv(names.FluentdPort); ok {
		fluentdPort = portstr
	}
	fluentdStopEndpointUrl = fmt.Sprintf("http://127.0.0.1:%s/api/processes.interruptWorkers", fluentdPort)

	config, err := kubeconfig.Load("", kconfig)
	if err != nil {
		log.Fatal(err.Error())
	}

	// Create the clientset
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		log.Fatal(err.Error())
	}

	log.Printf("Start tracking target namespace=%s pod=%s image=%s\n", namespace, podName, image)
	o, stop := trackPodContainers(clientset, namespace, image, podName)
Watch:
	for {
		statuses := <-o
		for _, v := range statuses {
			if isTargetContainerCompleted(v, image) {
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

func isTargetContainerCompleted(containerStatus core_v1.ContainerStatus, image string) bool {
	if containerStatus.Image == image {
		terminateStatus := containerStatus.State.Terminated
		log.Printf("Checking container status: %+v", terminateStatus)
		if terminateStatus != nil && terminateStatus.Reason == "Completed" {
			return true
		}

	}
	return false
}

func trackPodContainers(clientset *kubernetes.Clientset, namespace, image, podName string) (chan []core_v1.ContainerStatus, chan struct{}) {
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
