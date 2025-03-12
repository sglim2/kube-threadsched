package main

import (
	"context"
	"flag"
	"fmt"
	"strconv"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

const schedulerName = "ratio-scheduler"

func main() {
	kubeconfig := flag.String("kubeconfig", "", "Absolute path to the kubeconfig file")
	flag.Parse()

	config, err := clientcmd.BuildConfigFromFlags("", *kubeconfig)
	if err != nil {
		panic(fmt.Sprintf("Error building kubeconfig: %v", err))
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		panic(fmt.Sprintf("Error creating Kubernetes client: %v", err))
	}

	fmt.Println("Starting Ratio Scheduler...")

	for {
		pods, err := clientset.CoreV1().Pods("").List(context.TODO(), metav1.ListOptions{})
		if err != nil {
			fmt.Printf("Error listing pods: %v\n", err)
			time.Sleep(5 * time.Second)
			continue
		}

		for _, pod := range pods.Items {
			if pod.Spec.SchedulerName != schedulerName || pod.Spec.NodeName != "" {
				continue
			}

			fmt.Printf("Attempting to schedule pod: %s/%s\n", pod.Namespace, pod.Name)
			nodeName, err := selectNode(clientset)
			if err != nil {
				fmt.Printf("No suitable node found for pod %s/%s: %v\n", pod.Namespace, pod.Name, err)
				continue
			}

			if err := bindPod(clientset, &pod, nodeName); err != nil {
				fmt.Printf("Error binding pod %s/%s to node %s: %v\n", pod.Namespace, pod.Name, nodeName, err)
			} else {
				fmt.Printf("Pod %s/%s successfully scheduled on node %s\n", pod.Namespace, pod.Name, nodeName)
			}
		}

		time.Sleep(5 * time.Second)
	}
}

func selectNode(clientset *kubernetes.Clientset) (string, error) {
	nodes, err := clientset.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return "", err
	}

	var selectedNode string
	bestScore := -1

	for _, node := range nodes.Items {
		threadsStr, ok := node.Labels["threads"]
		if !ok {
			continue
		}
		totalThreads, err := strconv.Atoi(threadsStr)
		if err != nil {
			continue
		}

		podsOnNode, err := clientset.CoreV1().Pods("").List(context.TODO(), metav1.ListOptions{
			FieldSelector: "spec.nodeName=" + node.Name,
		})
		if err != nil {
			continue
		}
		allocated := len(podsOnNode.Items)
		available := totalThreads - allocated

		if available > bestScore && available > 0 {
			bestScore = available
			selectedNode = node.Name
		}
	}

	if selectedNode == "" {
		return "", fmt.Errorf("no node with available threads found")
	}
	return selectedNode, nil
}

func bindPod(clientset *kubernetes.Clientset, pod *v1.Pod, nodeName string) error {
	binding := &v1.Binding{
		ObjectMeta: metav1.ObjectMeta{
			Name: pod.Name,
		},
		Target: v1.ObjectReference{
			Kind: "Node",
			Name: nodeName,
		},
	}
	return clientset.CoreV1().Pods(pod.Namespace).Bind(context.TODO(), binding, metav1.CreateOptions{})
}

