package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	_ "net/http"
	"os"
	"path/filepath"

	"bitbucket.org/linkernetworks/cv-tracker/src/env"
	"bitbucket.org/linkernetworks/cv-tracker/src/kubeconfig"
	"bitbucket.org/linkernetworks/cv-tracker/src/kubemon"

	core_v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	// Uncomment the following line to load the gcp plugin (only required to authenticate against GKE clusters).
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
)

const (
	DefaultFluentdPort = "24444"
)

func main() {
	var home = env.HomeDir()
	var kconfig string = ""
	var namespace string = ""
	var image string = ""
	var podName string = ""
	var ok bool
	flag.StringVar(&kconfig, "kubeconfig", filepath.Join(home, ".kube", "config"), "(optional) absolute path to the kubeconfig file")
	flag.Parse()

	if namespace, ok = os.LookupEnv(env.KUBE_NAMESPACE); ok {
		namespace = "default"
	}

	if podName, ok = os.LookupEnv(env.POD_NAME); ok {
		log.Fatal(errors.New("The terminator need the Pod name."))
	}
	if image, ok = os.LookupEnv(env.JOB_IMAGE); ok {
		log.Fatal(errors.New("The terminator need the target container image."))
	}

	var fluentdPort string = DefaultFluentdPort
	var fluentdStopEndpointUrl string
	if portstr, ok := os.LookupEnv(env.FLUENTD_PORT); ok {
		fluentdPort = portstr
	}
	fluentdStopEndpointUrl = fmt.Sprintf("http://127.0.0.1:%s/api/processes.interruptWorkers", fluentdPort)
	_ = fluentdStopEndpointUrl

	config, err := kubeconfig.Load("", kconfig)
	if err != nil {
		log.Fatal(err.Error())
	}

	// create the clientset
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		log.Fatal(err.Error())
	}

	log.Printf("Target pod name %s, klube namespace %s, image %s", namespace, podName, image)
	trackPodContainer(clientset, namespace, image, podName)
	// TODO: use fluentdStopEndpointUrl to stop fluentd
}

func isTargetContainerCompleted(containerStatus core_v1.ContainerStatus, image string) bool {
	if containerStatus.Image == image {
		terminateStatus := containerStatus.State.Terminated
		if terminateStatus != nil && terminateStatus.Reason == "Completed" {
			return true
		}

	}
	return false
}

func trackPodContainer(clientset *kubernetes.Clientset, namespace, image, podName string) {
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
			for _, v := range pod.Status.ContainerStatuses {
				if isTargetContainerCompleted(v, image) {
					/*
						uri := getLogCollectionURI()
						log.Printf("Target Container is terminated, ready to send HTTP RPC to %s to stop it", uri)
						_, err := http.Get(uri)
						if err != nil {
							log.Fatalf("Failed to close log-collector %v", err)
							return
						}

						//Stop the Daemon here
						close(stop)
						return
					*/
				}

			}
		},
	})
	// _ = store
	go controller.Run(stop)
	<-stop
}
