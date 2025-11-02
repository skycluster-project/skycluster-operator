# echo-app (example)

Brief example showing an Istio-based traffic routing / failover setup for a simple "echo" service.

**Note:** This example must be deployed manually using `kubectl` across two fully deployed Kubernetes cluster with inter-cluster connectivity.

## Purpose

- Demonstrate how to route traffic based on the source pod (`VirtualService` sourceLabels).
- Demonstrate destination subsets, locality-based failover preferences and outlier detection (DestinationRule).
- Provide simple test clients (netshoot pods) and two echo backends (`echo1`, `echo2`) that return their IP and hostname.

## Components

- Namespace: `echo-app`
- Service
  - `echo-svc` (ClusterIP) exposing port 8080
- Backends (in us-east-1)
  - Deployments: `echo1`, `echo2` — each runs a tiny HTTP responder (nc)
- Test clients
  - `netshoot1` (label `name: net1`) — used to exercise routing to subset `net1`
  - `netshoot2` (label `name: net2`) — used to exercise routing to subset `net2`
- Istio configuration
  - `VirtualService echo-svc` — two HTTP routes that match by sourceLabels (`name: net1` or `name: net2`) and route to subsets `net1` / `net2`
  - `DestinationRule dst-rule` — defines subsets `net1` and `net2`, per-subset trafficPolicy with locality failover preferences and outlierDetection

## How it works

1. Clients (netshoot pods) call `echo-svc:8080`.
2. Istio VirtualService inspects the source pod labels:
   - requests from `name=net1` are routed to subset `net1`
   - requests from `name=net2` are routed to subset `net2`
3. DestinationRule subsets control LB and failover behavior:
   - each subset configures locality-based failover preferences (intended to prefer pods with a given label, e.g. `app=echo1` or `app=echo2`)
   - outlier detection ejects unhealthy endpoints automatically
4. Each echo backend replies with a simple one-line payload including its IP and hostname, so you can see which backend served the request.

## Expected behabior
- From netshoot1 you must get a response from echo-app [VERSION 1]
- From netshoot2 you must get a response from echo-app [VERSION 2]
