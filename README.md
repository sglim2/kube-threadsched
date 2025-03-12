# kube-threadsched

A custom Kubernetes scheduler that distributes pods based on the ratio of available node threads to requested CPUs.

## Overview

This project implements a scheduler in Go that assigns pods to nodes by comparing the available threads (from a custom node label) against the number of pods already scheduled on that node.


## Initialize the module and build the scheduler

```bash
go mod tidy
go build -o kube-threadsched ./cmd/ratio-scheduler
```


## Launch 

```
kubectl apply -f contrib/kube-threadsched-rbac.yaml
```


## Testng

```
for i in `seq 1 20` 
do 
  kubectl run rocky$i --image=rockylinux/rockylinux:latest --overrides='{"spec": {"schedulerName": "threadsched"}}' --overrides='{"spec":{"containers[0]":{"resources":{"limits":{"cpu":"8"}}}}}' -- bash -c "sleep infinity" 
done
```

