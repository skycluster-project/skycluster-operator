# Readme


## Development

### Commands

```bash
NN=echo-app-policies-0mk5u
NS=echo-app

kubectl get skyxrds $NN -n $NS -o json > /tmp/xrd.json; for i in $(seq 0 $(( $(jq '.status.manifests|length' /tmp/xrd.json)-1 ))); do   jq -r ".status.manifests[$i].manifest" /tmp/xrd.json > manifest-$i.yaml; done

## ## ##

NN=echo-app-policies-0mk5u
kubectl patch skynet $NN -n echo-app -p '{"spec":{"approve":true}}' --type=merge

kubectl get skynets $NN -n $NS -o json > /tmp/xrd.json; for i in $(seq 0 $(( $(jq '.status.objects|length' /tmp/xrd.json)-1 ))); do   jq -r ".status.objects[$i].name" /tmp/xrd.json; done
```