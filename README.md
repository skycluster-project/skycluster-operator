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

make manifests
make install
make run

# To build and push the image to repository:
make docker-build docker-push IMG=etesami/skycluster-operator:v0.1.1

# To deply the controller as pod in the cluster
make deploy IMG=etesami/skycluster:latest
make deploy IMG=etesami/testimg:latest

# To undeploy
make undeploy

mkdir -p /var/log/skycluster
go run cmd/main.go \
  --webhook-cert-path /home/ubuntu/keys/webhook-certs \
  --webhook-port 9445
  --log-dir /var/log/skycluster/operator.log
```

## Building the image

```bash
# for production, the deployment webhook checks all deployment
# limit the scope by adding
namespaceSelector:
  matchLabels:
    webhook-enabled: "true"
# to the webhook mutation definition manually.

make docker-build

# push the image

# Generte manifests
kubectl kustomize config/crd > config/output/crds.yaml
kubectl kustomize config/default > config/output/operator.yaml

# once ready 
# Remove namespace (it is created by other charts)
cp config/output/crds.yaml ~/skycluster-helm/crds/crds/operator.yaml
cp config/output/operator.yaml ~/skycluster-helm/crds/templates/operator.yaml
```