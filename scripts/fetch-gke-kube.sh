#!/bin/bash
# Usage: ./rename_gke_context.sh <cluster> <loc> <new_context_name>

CLUSTER=$1
LOC=$2
NEW_NAME=$3

# Get list of contexts before
before=$(kubectl config get-contexts -o name)

# Fetch credentials
gcloud container clusters get-credentials "$CLUSTER" --location "$LOC"

# Get list of contexts after
after=$(kubectl config get-contexts -o name)

# Find the new context
new_context=$(comm -13 <(echo "$before" | sort) <(echo "$after" | sort))

# Rename the new context
if [ -n "$new_context" ]; then
  kubectl config rename-context "$new_context" "$NEW_NAME"
  echo "Renamed context '$new_context' to '$NEW_NAME'"
else
  echo "No new context found."
fi
