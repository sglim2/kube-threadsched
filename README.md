# namespacedThreadSpread-scheduler
A custom Kubernetes scheduler written in Go that schedules pods based on the CPU limits of pods within the same namespace relative to each node’s CPU capacity. This scheduler is designed to evenly spread pods (by their defined CPU limits) across nodes, even in clusters with heterogeneous node capacities (in fact, only really useful for cluster with varying amounts of cpu across cluster worker nodes).

## Overview
In many environments, especially when pods in one namespace are expected to run simultaneously while others are idle, it’s important to distribute workloads evenly. This scheduler selects a node for a pod by computing a ratio:

```
ratio = (sum of CPU limits for pods in the namespace on the node + new pod's CPU limit) / (total CPU capacity of the node)
```

The scheduler only considers pods in the same namespace and ensures that a node has enough free resources based on CPU requests. In cases of a tie in ratio scores, the node with the fewest pods is chosen.

## How It Works

 1. Polling:
The scheduler continuously polls the Kubernetes API for pods that are pending scheduling and have their spec.schedulerName set to namespacedThreadSpread-scheduler.
 2. Node Selection:
For each pod, the scheduler:
   * Lists all nodes and retrieves their CPU capacity from node.Status.Capacity.
   * Aggregates CPU limits and requests for pods in the same namespace running on each node.
   * Adds the new pod's resource values to simulate scheduling.
   * Computes a ratio (score) based on the CPU limit sum divided by the node's capacity.
   * Selects the node with the lowest score. If two nodes have the same score, the one with fewer pods is chosen.
3. Binding:
Once a node is selected, the scheduler creates a binding between the pod and the node using the Kubernetes API.
 4. Logging:
Detailed logs provide visibility into the scheduling decision process, including per-node resource data and scores, with an asterisk marking the chosen node.

## Testng

```
for i in `seq 1 50`
do 
  kubectl run rocky$i  --image=rockylinux/rockylinux:latest --overrides='{"spec": {"schedulerName": "namespacedThreadSpread-scheduler", "containers": [{"name": "rocky", "image": "rockylinux/rockylinux:latest", "args": ["bash", "-c", "sleep infinity"], "resources": {"requests": {"cpu": "1"}, "limits": {"cpu": "8"}}}]}}' --override-type merge
done 
```

