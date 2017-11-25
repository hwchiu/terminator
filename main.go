package main

import (
	//"encoding/json"
	"flag"
	"log"
	"os"
	"path/filepath"

	"bitbucket.org/linkernetworks/cv-tracker/src/env"
	"bitbucket.org/linkernetworks/cv-tracker/src/kubeconfig"
	"bitbucket.org/linkernetworks/cv-tracker/src/kubemon"

	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	// Uncomment the following line to load the gcp plugin (only required to authenticate against GKE clusters).
	core_v1 "k8s.io/api/core/v1"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
)

func main() {
	var home = env.HomeDir()
	var kconfig string = ""
	var namespace string = ""
	var image = ""
	var podName = ""
	flag.StringVar(&kconfig, "kubeconfig", filepath.Join(home, ".kube", "config"), "(optional) absolute path to the kubeconfig file")
	flag.StringVar(&namespace, "namespace", "testing", "kubernetes namespace to watch")
	flag.Parse()

	podName = os.Getenv("POD_NAME")
	image = os.Getenv("JOB_IMAGE")
	config, err := kubeconfig.Load("", kconfig)
	if err != nil {
		log.Fatal(err.Error())
	}

	// create the clientset
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		log.Fatal(err.Error())
	}

	watchPods(clientset, namespace, image, podName)
}

func isTargetContainerCompleted(containerStatus core_v1.ContainerStatus, image string) bool {
	if containerStatus.Image == image {
		terminated := containerStatus.State.Terminated
		if terminated != nil && terminated.Reason == "Completed" {
			return true
		}

	}
	return false
}

func watchPods(clientset *kubernetes.Clientset, namespace, image, podName string) {
	_, controller := kubemon.WatchPods(clientset, namespace, fields.Everything(), cache.ResourceEventHandlerFuncs{
		UpdateFunc: func(oldObj, newObj interface{}) {
			if pod, ok := newObj.(*core_v1.Pod); ok {
				if podName == pod.ObjectMeta.Name {
					for _, v := range pod.Status.ContainerStatuses {
						if isTargetContainerCompleted(v, image) {
							log.Println("Send RPC")
						}

					}
				}
			}
		},
	})
	// _ = store
	stop := make(chan struct{})
	go controller.Run(stop)
	<-stop
}
