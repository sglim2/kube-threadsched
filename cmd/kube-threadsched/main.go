package main

import (
	"context"
	"flag"
	"fmt"
	"strconv"
	"time"

	// Kubernetes client libraries necessary to communicate with the cluster. 
	// these allow us to list, watch, and bind pods and nodes.
	v1 "k8s.io/api/core/v1" // definitions for core Kubernetes API objects (like Pods, Nodes)
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1" // metadata types for Kubernetes objects
	"k8s.io/client-go/kubernetes" // client library to interact with the Kubernetes API server
	"k8s.io/client-go/tools/clientcmd" // Helps build Kubernetes client configuration from kubeconfig files
)

// The name of our scheduler.
// This is the name that we will use to identify pods that should be scheduled by our custom scheduler.
// i.e. the value of the field spec.schedulerName in the PodSpec.
const schedulerName = "namespacedThreadSpreadSched"

func main() {
	// Parse the 'kubeconfig' flag to get the path to the kubeconfig file.
	// This is only necessary if the scheduler is running outside the cluster.
	// If no '-kubeconfig' flag is provided, the value defaults to an empty string.
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

	fmt.Println("Starting Thread Ratio Scheduler...")

	// Start polling
	for {
		pods, err := clientset.CoreV1().Pods("").List(context.TODO(), metav1.ListOptions{})
		if err != nil {
			fmt.Printf("Error listing pods: %v\n", err)
			time.Sleep(5 * time.Second)
			continue
		}

		// Iterate through each pod in the cluster.
		for _, pod := range pods.Items {
			// Skip pods that are already scheduled or not intended for our scheduler.
			if pod.Spec.SchedulerName != schedulerName || pod.Spec.NodeName != "" {
				continue
			}

            // Collect the namespace of the pod
			// Our scheduler will spread the pods in the same namespace (or more precisely their 
			//   defined cpu limits) across the nodes in the cluster.
		    //namespace := pod.Namespace

			// Select a node for the pod.
			fmt.Printf("Attempting to schedule pod: %s/%s\n", pod.Namespace, pod.Name)
			nodeName, err := selectNode(clientset, &pod)
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


func selectNode(clientset *kubernetes.Clientset, pod *v1.Pod) (string, error) {

    //First get the list of pods in the target namespace, and sum the CPU limit
    pods_namespaced, err := clientset.CoreV1().Pods(pod.Namespace).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		fmt.Printf("[SelectNode] Error listing pods in Namespace %s: %v\n", pod.Namespce, err)
		time.Sleep(5 * time.Second)
		continue
	}


	// get the total nunmber of nodes, and create a map to store the total CPU capacity of each node
	nodes, err := clientset.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		fmt.Printf("[SelectNode] Error listing nodes: %v\n", err)
		time.Sleep(5 * time.Second)
		continue
	}

	// cycle through the nodes, and calculate the ratio of the total CPU limit of all 
	// containers of all the pods in the namespace, for that node.
	node_cpu_capacity := make(map[string]int64)
    for _, node := range nodes.Items {
		// Obtain the total CPU capacity from the node’s status.
		cpuCapacityQuantity, err := node.Status.Capacity[v1.ResourceCPU]
		if err != nil {
			Printf("[SelectNode] Error getting CPU capacity of node %s: %v\n", node.Name, err)
			// If the node doesn’t report CPU capacity, set to zero 
			// which will be used to set the node as not suitable
			cpuCapacityQuantity = 0
			continue
		}

		// get the total 





    // Calculate the total CPU limit required by the new pod.
    // (Assumes each container in the pod has a CPU limit defined.)
    newPodLimit := int64(0) 
    for _, container := range pod.Spec.Containers {
        if limit, ok := container.Resources.Limits[v1.ResourceCPU]; ok {
            newPodLimit += limit.Value()
        }
    }
    if newPodLimit == 0 {
        return "", fmt.Errorf("new pod has no CPU limit defined")
    }

    // List all nodes in the cluster.
    nodes, err := clientset.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{})
    if err != nil {
        return "", err
    }

    var selectedNode string
    // Start with a high "best" ratio so that any valid node will replace it.
    bestRatio := 1e9 // arbitrarily high number

    // Iterate through each node.
    for _, node := range nodes.Items {
        // Obtain the total CPU capacity from the node’s status.
        cpuCapacityQuantity, ok := node.Status.Capacity[v1.ResourceCPU]
        if !ok {
            // If the node doesn’t report CPU capacity, skip it.
            continue
        }
        totalCores := cpuCapacityQuantity.Value() // e.g. 128 or 192

        // Sum the CPU limits of all pods currently scheduled on this node.
        allocated := int64(0)
        podsOnNode, err := clientset.CoreV1().Pods("").List(context.TODO(), metav1.ListOptions{
            FieldSelector: "spec.nodeName=" + node.Name,
        })
        if err != nil {
            // If there’s an error listing pods for this node, skip it.
            continue
        }
        for _, p := range podsOnNode.Items {
            for _, container := range p.Spec.Containers {
                if limit, ok := container.Resources.Limits[v1.ResourceCPU]; ok {
                    allocated += limit.Value()
                }
            }
        }

        // Check if the node can accommodate the new pod.
        if allocated+newPodLimit > totalCores {
            continue // not enough capacity, skip this node
        }

        // Compute the ratio after scheduling the new pod.
        ratio := float64(allocated+newPodLimit) / float64(totalCores)
        // Choose the node with the lowest ratio (i.e. most spare capacity relative to its size).
        if ratio < bestRatio {
            bestRatio = ratio
            selectedNode = node.Name
        }
    }

    if selectedNode == "" {
        return "", fmt.Errorf("no suitable node found")
    }
    return selectedNode, nil
}


func selectNode_basic(clientset *kubernetes.Clientset) (string, error) {
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

