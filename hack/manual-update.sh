#!/bin/bash

if [ "$#" -ne 1 ]; then
    echo "Usage: $0 <ProviderProfileName>"
    exit 1
fi
P=$1

kubectl patch -nskycluster-system providerprofiles $P --type='merge' -p '{
  "status": {
    "conditions": [
      {
        "type": "ResyncRequired",
        "status": "True",
        "reason": "ManualUpdate",
        "message": "Condition manually set to true",
        "lastTransitionTime": "'$(date -u +%Y-%m-%dT%H:%M:%SZ)'"
      }
    ]
  }
}' --subresource=status