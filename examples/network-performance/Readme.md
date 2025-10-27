# Network Performance Test

Deploy namespace in all clusters:

```yaml
apiVersion: v1
kind: Namespace
metadata:
  name: loadtest
```

We consider two clusters deployed in different regions and create fortio and iperf server in both clusters. For simplicity let's call clusters as cluster A and B.

We run workload job in different scenarios:

- Job is run in cluster A and reaches to servers in B. The input of the Job is identified by the following cases:
  - Base (direct): expose services as LoadBalancer and use external address.
  - Tunnels (no sidecar): a service with unique name (iperf-svc-b) is used.
  - Tunnels (with sidecar): service of same name are created and the selection are controlled using DestinationRule objects.


Case 1 files are with `case1` folder.