#!/bin/bash
set -euo pipefail

# collect lists into arrays
readarray -t LIST < <(skycluster xkube list | awk '{print $1}' | tail -n +2)
readarray -t PLATFORMS < <(skycluster xkube list | awk '{print $2}' | tail -n +2)
readarray -t LOCs < <(skycluster xkube list | awk '{print $5}' | tail -n +2)

if [ "${#LIST[@]}" -ne "${#LOCs[@]}" ]; then
  echo "Error: Length of LIST and LOCs do not match." >&2
  exit 1
fi

mkdir -p /tmp/kubeconfigs

# arrays for GCP clusters and their locations
GCP_NAMES=()
GCP_LOCS=()

index=1
for i in "${!LIST[@]}"; do
  cluster="${LIST[$i]}"
  loc="${LOCs[$i]}"
  pltf="${PLATFORMS[$i]}"
  cfg="/tmp/kubeconfigs/$cluster"

  if [ "$pltf" != "gcp" ]; then
    skycluster xkube config -k "$cluster" > "$cfg"

    # workaround for openstack same cluster name as "default"
    if [ "$pltf" == "openstack" ]; then
      cntx_name="op${index}"
      sed -i "s/: default/: $cntx_name/g" "$cfg"
    fi

    CONTEXT=$(KUBECONFIG="$cfg" kubectl config current-context)
    echo "Context for $cluster: $CONTEXT"
    if [ "$pltf" == "baremetal" ]; then
      cntx_name="br${index}"
    elif [ "$pltf" == "openstack" ]; then
      cntx_name="op${index}"
    else
      cntx_name="${pltf}${index}"
    fi
    if [ "$CONTEXT" != "$cntx_name" ]; then
      KUBECONFIG="$cfg" kubectl config rename-context "$CONTEXT" "$cntx_name"
    fi
  elif [ "$pltf" == "gcp" ]; then
    GCP_NAMES+=("$cluster")
    GCP_LOCS+=("$loc")
  fi
  index=$((index + 1))
done

# Fetch credentials for GCP clusters and rename their contexts
index=1
for i in "${!GCP_NAMES[@]}"; do
  gcp_cluster="${GCP_NAMES[$i]}"
  gcp_loc="${GCP_LOCS[$i]}"
  cfg="/tmp/kubeconfigs/$gcp_cluster"

  ext_name=$(skycluster xkube list | grep "$gcp_cluster" | awk '{print $6}')
  echo "Fetching GCP for cluster: $ext_name in $gcp_loc"

  # gcloud will write to KUBECONFIG if set
  if KUBECONFIG="$cfg" gcloud container clusters get-credentials "$ext_name" --location "$gcp_loc"; then
    GCP_CONTEXT=$(KUBECONFIG="$cfg" kubectl config current-context)
    echo "GCP Context for $gcp_cluster: $GCP_CONTEXT"
    cntx_name="${pltf}${index}"
    KUBECONFIG="$cfg" kubectl config rename-context "$GCP_CONTEXT" "$cntx_name"
    echo "Renamed GCP context to '$cntx_name' for $gcp_cluster"
  else
    echo "Failed to get GCP cluster credentials for $gcp_cluster. Ensure gcloud is installed and configured." >&2
  fi
  index=$((index + 1))
done

echo "Reload your shell."


