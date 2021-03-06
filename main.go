package main

import (
	"flag"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/hwchiu/terminator/utils"
	"github.com/linkernetworks/kubeconfig"
	"github.com/linkernetworks/kubemon"

	core_v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	// Uncomment the following line to load the gcp plugin (only required to authenticate against GKE clusters).
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
)

const (
	DefaultStopURL = "http://127.0.0.1:24444/api/processes.interruptWorkers"
)

func main() {
	var home = utils.HomeDir()
	var kconfig string = ""
	var namespace string = "default"
	var podName string = ""
	var container string = ""
	var interval string = ""
	var retryTimes string = ""
	var defaultKubeConfigPath = filepath.Join(home, ".kube", "config")

	var version bool = false

	flag.BoolVar(&version, "version", false, "version")
	flag.StringVar(&kconfig, "kubeconfig", defaultKubeConfigPath, "(optional) absolute path to the kubeconfig file")
	flag.StringVar(&namespace, "namespace", "default", "kubernetes namespace")
	flag.StringVar(&podName, "podName", "", "pod name for tracking container")
	flag.StringVar(&container, "container", "", "contaienr name for tracking container")
	flag.StringVar(&interval, "interval", "5", "interval between each check (seconds)")
	flag.StringVar(&retryTimes, "retryTimes", "10", "the retry times for sending stop signal, $interval seconds for each retry")
	flag.Parse()

	if podName == "" {
		log.Fatal("The terminator need the Pod name.")
	}
	if container == "" {
		log.Fatal("The terminator need the target container image.")
	}

	var stopURL string = DefaultStopURL
	if env, ok := os.LookupEnv("STOP_URL"); ok {
		stopURL = env
	}

	config, err := kubeconfig.Load(kconfig)
	if err != nil {
		log.Fatal(err.Error())
	}

	// Create the clientset
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		log.Fatal(err.Error())
	}

	t, _ := strconv.Atoi(interval)
	ticker := time.NewTicker(time.Duration(t) * time.Second)
	log.Printf("Start tracking target namespace=%s pod=%s image=%s\n", namespace, podName, container)
	o, stop := trackPodContainers(clientset, namespace, container, podName)
Watch:
	for {
		select {
		case statuses := <-o:
			if findTargetContainer(statuses, container) {
				break Watch
			}
		case <-ticker.C:
			log.Println("Check the pod status from ticker")
			podList, err := kubemon.GetPods(clientset, namespace)
			if err != nil {
				log.Println("Get Pod List fail", err)
				continue
			}

			for _, pod := range podList.Items {
				if podName != pod.ObjectMeta.Name {
					continue
				}

				log.Println("Ready to check container statuses")
				if findTargetContainer(pod.Status.ContainerStatuses, container) {
					break Watch
				}
			}
		}
	}

	close(o)
	ticker.Stop()
	log.Println("Sending stop watch signal..")
	var e struct{}
	stop <- e
	close(stop)

	times, _ := strconv.Atoi(retryTimes)
	t, _ = strconv.Atoi(interval)
	for i := 1; i <= times; i++ {
		log.Printf("Sending terminate signal to fluentd:%s , times:%d", stopURL, i)

		_, err = http.Get(stopURL)
		if err == nil {
			log.Printf("Sending terminate signal to fluentd:%s success", stopURL)
			break
		}

		log.Println("Sending terminate signal to fluentd fails", err)
		time.Sleep(time.Duration(t) * time.Second)
	}

	log.Println("Exiting...")
}

func isTargetContainerCompleted(containerStatus core_v1.ContainerStatus, containerName string) bool {
	if containerStatus.Name == containerName {
		terminateStatus := containerStatus.State.Terminated
		log.Printf("Checking container status: %+v", terminateStatus)
		log.Printf("The LastTerminationState of target container: %+v", containerStatus.LastTerminationState)
		if terminateStatus != nil {
			log.Printf("container %s termination detected.", containerName)
			return true
		}
	}
	return false
}

func trackPodContainers(clientset *kubernetes.Clientset, namespace, container, podName string) (chan []core_v1.ContainerStatus, chan struct{}) {
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

func findTargetContainer(statuses []core_v1.ContainerStatus, containerName string) bool {
	for _, v := range statuses {
		log.Printf("Check pod status")
		log.Printf("PodName:%s, PodImage:%s", v.Name, v.Image)
		if isTargetContainerCompleted(v, containerName) {
			log.Printf("found target container completed: %+v\n", v)
			return true
		}
	}
	return false
}
