# SkyCluster

## Setup

For development environments use `kind` to create a local cluster to run `skycluster-operator`:

```bash
kind create cluster --name skycluster --config skycluster-kind.yaml
```

```yaml
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
networking:
  apiServerAddress: "0.0.0.0"
  apiServerPort: 6443
kubeadmConfigPatches:
  - |
    kind: ClusterConfiguration
    apiServer:
      certSANs:
        - "127.0.0.1"
        - "skycluster.local"
        - "X.X.X.X"  # Replace with your cluster internal IP
        - "X.X.X.X"  # Replace with your cluster public IP
nodes:
  - role: control-plane
```


## Develop

```bash
mkdir -p /var/log/skycluster
go run cmd/main.go \
  --webhook-cert-path /home/ubuntu/keys/webhook-certs \
  --webhook-port 9445
  --log-dir /var/log/skycluster/operator.log
```