#!/bin/bash

# Change these values to match your clusters
AWS=xk-aws-us-east--4z74l
GCP=xk-gcp-us-east1-thams-zhsn7
GCP_LOC=us-east1-b

skycluster xkube config -k $AWS > /tmp/aws1
if [ $? -eq 0 ]; then
  AWS_CONTEXT=$(KUBECONFIG=/tmp/aws1 kubectl config get-contexts -o name)
  echo "AWS Context: $AWS_CONTEXT"
  KUBECONFIG=/tmp/aws1 kubectl config rename-context $AWS_CONTEXT aws
  echo "Renamed AWS context to 'aws'"
else
  echo "Failed to get AWS kubeconfig. Please ensure skycluster is installed and configured."
fi

echo "Fetching GCP cluster credentials..."
KUBECONFIG=/tmp/gcp1 kubectl config delete-context gke || true
KUBECONFIG=/tmp/gcp1 gcloud container clusters get-credentials "$GCP" --location "$GCP_LOC"
if [ $? -eq 0 ]; then
  echo "Fetched GCP cluster credentials. Renaming context..."
  GCP_CONTEXT=$(KUBECONFIG=/tmp/gcp1 kubectl config get-contexts -o name)
  echo "GCP Context: $GCP_CONTEXT"
  KUBECONFIG=/tmp/gcp1 kubectl config rename-context $GCP_CONTEXT gke
  echo "Renamed GCP context to 'gke'"
else
  echo "Failed to get GCP cluster credentials. Please ensure gcloud is installed and configured."
fi

export KUBECONFIG=~/.kube/config:/tmp/gcp1:/tmp/aws1

