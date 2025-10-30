#!/bin/bash

# Change these values to match your clusters
AWS=xk-aws-us-east--78uib
GCP=xk-gcp-us-east1-rhdru-q4rdv
GCP_LOC=us-east1-b

skycluster xkube config -k $AWS > /tmp/aws1
AWS_CONTEXT=$(KUBECONFIG=/tmp/aws1 kubectl config get-contexts -o name)
KUBECONFIG=/tmp/aws1 kubectl config rename-context $AWS_CONTEXT aws

KUBECONFIG=/tmp/gcp1 gcloud container clusters get-credentials "$GCP" --location "$GCP_LOC"
GCP_CONTEXT=$(KUBECONFIG=/tmp/gcp1 kubectl config get-contexts -o name)
KUBECONFIG=/tmp/gcp1 kubectl config rename-context $GCP_CONTEXT gke

export KUBECONFIG=~/.kube/config:/tmp/gcp1:/tmp/aws1

