# kube-threadsched

A custom Kubernetes scheduler that distributes pods based on the ratio of available node threads to requested CPUs.

## Overview

This project implements a scheduler in Go that assigns pods to nodes by comparing the available threads (from a custom node label) against the number of pods already scheduled on that node.

## Getting Started

## Initialize the module and build the scheduler

```bash
go mod tidy
go build -o kube-threadsched ./cmd/ratio-scheduler
```
