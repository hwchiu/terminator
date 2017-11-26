package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"

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

func main() {
	var home = env.HomeDir()
	var kconfig string = ""
	var namespace string = ""
	var image string = ""
	var podName string = ""
	flag.StringVar(&kconfig, "kubeconfig", filepath.Join(home, ".kube", "config"), "(optional) absolute path to the kubeconfig file")
	flag.Parse()

	if namespace = os.Getenv(env.KUBE_NAMESPACE); namespace == "" {
		namespace = "default"
	}

	if podName = os.Getenv(env.POD_NAME); podName == "" {
		log.Fatal(errors.New("The terminator need the Pod name."))
	}
	if image = os.Getenv(env.JOB_IMAGE); image == "" {
		log.Fatal(errors.New("The terminator need the target container image."))
	}

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
	watchPods(clientset, namespace, image, podName)
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

func getLogCollectionURI() string {
	portStr := os.Getenv(env.FLUENTD_PORT)
	pod, err := strconv.Atoi(portStr)
	if err != nil {
		pod = env.VAL_FLUENTD_PORT
	}

	return fmt.Sprintf("http://127.0.0.1:%d/api/processes.interruptWorkers", pod)
}

func watchPods(clientset *kubernetes.Clientset, namespace, image, podName string) {
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
				}

			}
		},
	})
	// _ = store
	go controller.Run(stop)
	<-stop
}
