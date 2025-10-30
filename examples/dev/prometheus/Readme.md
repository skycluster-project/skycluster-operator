# Readme

Install Prometheus with Thanos across remote clusters to enable data collection across a centralized Prometheus.

## Remote Clusters

In remote clusters you can install Thanos sidecar along with Prometheus using its helm chart.

- Use `prometheous-thanos-values.yaml` to install prometheus:

```bash
helm repo add prometheus-community https://prometheus-community.github.io/helm-charts

# Use cluster specific Kubeconfig or its context
KUBECONFIG=/tmp/aws1
NAME=aws1
helm install $ prometheus-community/kube-prometheus-stack \
  -f prometheous-thanos-values.yaml -n monitoring \
  --create-namespace --kubeconfig $KUBECONFIG \
  --version v0.86.1
```

To change the grafana and prometheus service to use ClusterIP instead of LoadBalancer:

```bash
NAME=aws1
kubectl patch service $NAME -n monitoring -p '{"spec": {"type": "LoadBalancer"}}'
kubectl patch service $NAME -n monitoring -p '{"spec": {"type": "LoadBalancer"}}'
```

## Central Cluster

If you install Prometheus in remote clusters with Thanos sidecar, you can fetch the remote data in a centralized cluster using Thanos Query and Prometheus. Using `prometheous-values.yaml`:

```bash
# Install in local (centralized) cluster
helm install $ prometheus-community/kube-prometheus-stack \
  -f prometheous-values.yaml -n monitoring \
  --create-namespace --version v0.86.1


# Install Thanos Query separatly
helm repo add bitnami https://charts.bitnami.com/bitnami
helm search repo bitnami | grep thanos
# bitnami/thanos   17.3.1  0.39.2

helm install thanos --version="17.3.1" --install \
  --namespace="monitoring" \
  --values thanos-query-values.yaml bitnami/thanos
```

Introduce the Thanos remote cluster endpoints using `additional-scrape-configs` secret.


## Kiali Setup

Install Kiali Operator in central cluster using helm, then create Kiali CR:

```bash
helm install     
  --set cr.create=true \
  --set cr.namespace=monitoring \
  --set cr.spec.auth.strategy="anonymous" \
  --namespace kiali-operator \
  --create-namespace  kiali-operator kiali/kiali-operator

kubectl apply -f ./kiali.yaml -n monitoring

# Once installed, install the remote clusters:
# Download kiali-prepare-remote-cluster.sh from Kiali github

kiali-prepare-remote-cluster.sh \
  --remote-cluster-name aws \
  --process-remote-resources true \
  --process-kiali-secret true \
  --kiali-cluster-namespace monitoring \
  --kiali-cluster-context kind-skycluster --remote-cluster-context aws

kiali-prepare-remote-cluster.sh \
  --remote-cluster-name gke \
  --process-remote-resources true \
  --process-kiali-secret true \
  --kiali-cluster-namespace monitoring \
  --kiali-cluster-context kind-skycluster --remote-cluster-context gke
```