# namespacedThreadSpread-scheduler

A custom Kubernetes scheduler written in Go that schedules pods based on the CPU limits of pods within the same namespace relative to each node’s CPU capacity. This scheduler is designed to evenly spread pods (by their defined CPU limits) across nodes, even in clusters with heterogeneous nodes (in fact, only really useful for cluster with varying amounts of cpu across cluster worker nodes).

## Overview

Balances pods within a namespace across a k8s cluster, based on cpu usage. This scheduler is only really useful for k8s clusters with heterogenous workers, and expecting to run many pods (compared to the number of worker nodes available) within a namespace. if either of these criteria are not met, then the standard k8s scheduler will be a better choice.

The scheduler only considers pods in the same namespace and ensures that a node has enough free resources based on CPU requests. In cases of a tie in ratio scores, the node with the fewest pods is chosen.

## How It Works

 1. Polling:
The scheduler continuously polls the api-server for pods that are pending and have their spec.schedulerName set to namespacedThreadSpread-scheduler.
 2. Node Selection: For each pod, the scheduler will:
   * collect node CPU capacity from node.Status.Capacity.
   * check CPU limits and requests for pods in the same namespace for each schedulable node.
   * Computes a ratio (score) based on the CPU limit and the node's total cpu.
   * Selects the node with the best score. If two nodes have the same score, the node with fewer pods is chosen.

## Testing

```
for i in `seq 1 50`
do 
  kubectl run rocky$i  --image=rockylinux/rockylinux:latest --overrides='{"spec": {"schedulerName": "namespacedThreadSpread-scheduler", "containers": [{"name": "rocky", "image": "rockylinux/rockylinux:latest", "args": ["bash", "-c", "sleep infinity"], "resources": {"requests": {"cpu": "1"}, "limits": {"cpu": "8"}}}]}}' --override-type merge
done 
```

