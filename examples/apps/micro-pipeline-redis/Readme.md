# Simple Micro-pipeline Application


Labels to add to the manifests in `deploy.yaml`:

```
labels:
    skycluster.io/app-id: testcase1
    skycluster.io/app-scope: distributed
```

Make sure policies come with default app-id label:

```
labels:
    skycluster.io/app-id: testcase1
```

Default settings to deploy application is set to `false`. Fetch the deployment plans and check it out first:

```bash
NS=micro-pipeline
kubectl -n $NS get atlass -o json | jq '.items[0]' > /tmp/xrd.json; 
for i in $(seq 0 $(( $(jq '.status.manifests|length' /tmp/xrd.json)-1 ))); do   
  jq -r ".status.manifests[$i].manifest" /tmp/xrd.json > manifest-$i.yaml; 
done
```

Once ready deploy the manifests:

```bash
NS=micro-pipeline
NAME=$(kubectl get atlass -n $NS -o jsonpath="{.items[0].metadata.name}")$
kubectl patch -n $NS atlass $NAME -p '{"spec":{"approve":true}}' --type=merge
```

Then get the modified deployment plan ready to be deployed across different domains:

```bash
# get the names
NS=micro-pipeline
kubectl -n $NS get skynets -o json | jq '.items[0]' > /tmp/app.json; 
for i in $(seq 0 $(( $(jq '.status.objects|length' /tmp/app.json)-1 ))); do 
  jq -r ".status.objects[$i].name" /tmp/app.json; 
done

# Fetch their manifest
for i in $(seq 0 $(( $(jq '.status.objects|length' /tmp/app.json)-1 ))); do 
  jq -r ".status.objects[$i].manifest" /tmp/app.json > manifest-app-$i.yaml; 
done
```

Once ready approve the changes to be applied:

```bash
NS=micro-pipeline
NAME=$(kubectl get skynets -n $NS -o jsonpath="{.items[0].metadata.name}")
kubectl patch -n $NS skynets $NAME -p '{"spec":{"approve":false}}' --type=merge
```