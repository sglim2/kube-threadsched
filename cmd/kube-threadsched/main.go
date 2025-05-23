package main

import (
	"context"
	"flag"
	"fmt"
	"math"
	"time"

	// Kubernetes client libraries necessary to communicate with the cluster.
	// these allow us to list, watch, and bind pods and nodes.
	v1 "k8s.io/api/core/v1"                       // definitions for core Kubernetes API objects (like Pods, Nodes)
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1" // metadata types for Kubernetes objects
	"k8s.io/client-go/kubernetes"                 // client library to interact with the Kubernetes API server
	"k8s.io/client-go/tools/clientcmd"            // Helps build Kubernetes client configuration from kubeconfig files
	"k8s.io/apimachinery/pkg/api/resource"        // Helps work with Kubernetes resource quantities (helps define default quantities for, e.g. CPU and memory))
)

// The name of our scheduler.
// This is the name that we will use to identify pods that should be scheduled by our custom scheduler.
// i.e. the value of the field spec.schedulerName in the PodSpec.
const schedulerName = "namespacedThreadSpread-scheduler"

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

		// timestamp
	    fmt.Printf(time.Now().Format(time.RFC3339) + " [POLLING]\n")

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
				fmt.Printf(time.Now().Format(time.RFC3339) + " [IGNORE] pod/namespace/scheduler %s/%s/%s\n", pod.Name, pod.Namespace, pod.Spec.SchedulerName)
				continue
			}

			// log the pod being processed
            fmt.Printf(time.Now().Format(time.RFC3339) + " [PROCESS] Processing pod/namespace/scheduler %s/%s\n", pod.Name, pod.Namespace, pod.Spec.SchedulerName)

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


// Given a pod, select a node to schedule the pod to.
// The node is selected based on the ratio of 
//
//     (sum of all pod cpu *limits* on the node, with the same namespace) / (total cpus capacity of that node)
//
//  * Only pods living in the same namespace of the target pod are considered in the above calculation.
//  * The target node must also have enough resources avaiable (based on pods *requests*, and considering all 
//    workloads in all namespaces). 
//  * If a node does not have enough resources available, the next best node is considered.
//
// Goal:
//  * Spread the pods in the same namespace (or more precisely their defined cpu limits) across the nodes in the cluster.
//
// Use case:
//  * When many pods in a namespace are expected to be active simultaneously, while workloads in other namespaces 
//    are expected to be idle.
//  * When the cluster nodes are not expected to be homegeneous in terms of CPU capacity. (If the nodes are
//    homogenous, the basic scheduler should be sufficient, in particular with the use of topologySpreadConstraints.)
func selectNode(clientset *kubernetes.Clientset, pod *v1.Pod) (string, error) {

	// Define a struct to hold node-related information.
    type NodeInfo struct {
        CPUCapacity              int64   // total CPU capacity (e.g., from node.Status.Capacity)
        AssignedCPULimits        int64   // sum of CPU limits for pods in the same namespace
        AssignedCPULimitsPlus    int64   // sum of CPU limits for pods in the same namespace, plus the new pod
		AssignedCPURequests	     int64   // sum of CPU requests for pods in the same namespace, will determine if the node has enough resources available
		AssignedCPURequestsPlus  int64   // sum of CPU requests for pods in the same namespace, plus the new pod
        ScoreLimits              float64 // calculated score for primary scheduling decisions, based on CPU limits 
        ScorePods                int64   // calculated score for secondary scheduling decisions, based on number of pods
    }

    var bestNode string
	
	node_cpu_capacity := make(map[string]*NodeInfo)

    // Get the list of pods in the target namespace
    pods_namespaced, err := clientset.CoreV1().Pods(pod.Namespace).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		fmt.Printf("[SelectNode] Error listing pods in Namespace %s: %v\n", pod.Namespace, err)
		//time.Sleep(5 * time.Second)
		//continue
	}

	// get the total nunmber of nodes, and create a map to store the total CPU capacity of each node
	nodes, err := clientset.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		fmt.Printf("[SelectNode] Error listing nodes: %v\n", err)
		//time.Sleep(5 * time.Second)
		//continue
	}

	// Cycle through the nodes, populate the map to store the total CPU capacity for each node.
	// Calculate the ratio of the total CPU limit for all containers of all the pods in the namespace, for that node.
    for _, node := range nodes.Items {

		// Obtain the total CPU capacity from the node’s status.
		cpuCapacityQuantity, ok := node.Status.Capacity[v1.ResourceCPU]
		if !ok {
			fmt.Printf("[SelectNode] Error getting CPU capacity of node %s: %v\n", node.Name, err)
			// If the node doesn’t report CPU capacity, set to zero 
			cpuCapacityQuantity = *resource.NewQuantity(0, resource.DecimalSI) 
		}

		// Initialize the NodeInfo struct for the node.
        node_cpu_capacity[node.Name] = &NodeInfo{
			CPUCapacity:             cpuCapacityQuantity.Value(),  // A CPUCapacity of zero means the node is unschedulable.
    	    AssignedCPULimits:       0,    // This will be updated later.
    	    AssignedCPULimitsPlus:   0,    // This will be updated later.
			AssignedCPURequests:     0,    // This will be calculated later.
			AssignedCPURequestsPlus: 0,    // This will be calculated later.
        	ScoreLimits:             1e9,  // Start with an arbitrarily high score.
            ScorePods:               0,    // inititalize
        }

	}

	// Cycle through the pods in the namespace, and calculate the sum total CPU limits
	//   for all containers of all the pods in the namespace, append to each node.
	for _, pod := range pods_namespaced.Items {
		// Only consider pods that have been scheduled (i.e. NodeName is non-empty)
    	if pod.Spec.NodeName == "" {
        	continue
	    }
		// Cycle through the containers in the pod, and sum the CPU limits.
		for _, container := range pod.Spec.Containers {
			if limit, ok := container.Resources.Limits[v1.ResourceCPU]; ok {
				node_cpu_capacity[pod.Spec.NodeName].AssignedCPULimits += limit.Value()
			}
			if request, ok := container.Resources.Requests[v1.ResourceCPU]; ok {
				node_cpu_capacity[pod.Spec.NodeName].AssignedCPURequests += request.Value()
			}
		}
        // get the total number of pods in the namespace for each node 
		node_cpu_capacity[pod.Spec.NodeName].ScorePods += 1
	}

    // Add the new pod's CPU limits and requests to each node candidate.
	for _, node := range nodes.Items {
        info := node_cpu_capacity[node.Name]
        // Start with the current values.
   		info.AssignedCPULimitsPlus = info.AssignedCPULimits
        info.AssignedCPURequestsPlus = info.AssignedCPURequests
    
        // For each container in the new pod, add its CPU limit/request.
        for _, container := range pod.Spec.Containers {
            if limit, ok := container.Resources.Limits[v1.ResourceCPU]; ok {
                info.AssignedCPULimitsPlus += limit.Value()
            }
            if request, ok := container.Resources.Requests[v1.ResourceCPU]; ok {
                info.AssignedCPURequestsPlus += request.Value()
            }
        }
    }

	// Calculate the score for each Node
	for _, node := range nodes.Items {

        // Check if the node can accommodate the new pod.
        if node_cpu_capacity[node.Name].AssignedCPURequestsPlus > node_cpu_capacity[node.Name].CPUCapacity {
			node_cpu_capacity[node.Name].CPUCapacity = 0  // not enough capacity, assign as 'Unschedulable'
			continue 
        }

		// Calculate the scores for the node.
		if node_cpu_capacity[node.Name].CPUCapacity > 0 {
            // Compute the CPU Limit Score (including the new pod limits). Lowest score wins.
	        node_cpu_capacity[node.Name].ScoreLimits = float64(node_cpu_capacity[node.Name].AssignedCPULimitsPlus) / float64(node_cpu_capacity[node.Name].CPUCapacity)
		}
	}


	// choose the node with the lowest ScoreLimits
	// If there are multiple nodes with the same ScoreLimits, the node with the lowest ScorePods is selected.
	// If there are multiple nodes with the same ScoreLimits and ScorePods, the first node in the list is selected.
 
	bestNode = ""
	bestScoreLimits := math.MaxFloat64
	bestScorePods := int64(1e9)

	for nodeName, info := range node_cpu_capacity {
    	// Skip nodes that are unschedulable (CPUCapacity set to 0).
    	if info.CPUCapacity == 0 {
        	continue
    	}
    	// If this node has a lower ScoreLimits, or if ScoreLimits are equal and ScorePods is lower, choose it.
    	if info.ScoreLimits < bestScoreLimits || (info.ScoreLimits == bestScoreLimits && info.ScorePods < bestScorePods) {
	        bestScoreLimits = info.ScoreLimits
    	    bestScorePods = info.ScorePods
        	bestNode = nodeName
    	}
	}

	// Log detailed information about each node.
	fmt.Printf(time.Now().Format(time.RFC3339) + " [Scheduling Decision] Pod: %s\n", pod.Name)
	fmt.Printf("%4s%6s%6s%8s%9s%4s"," ", "Node", "CPU", "Limits", "Requests", " ")
	fmt.Println("-------------------------------------")
	for nodeName, info := range node_cpu_capacity {
		marker := ""
		if nodeName == bestNode {
			marker = " *" // Mark the selected node with an asterisk.
		}
	    fmt.Printf("%4s%6s%6d%8d%9d%4s\n",marker, nodeName, info.CPUCapacity, info.AssignedCPULimits, info.AssignedCPURequests, marker)
	}
	fmt.Println("-------------------------------------")

	if bestNode == "" {
    	return "", fmt.Errorf("no suitable node found")
	}

	return bestNode, nil
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

