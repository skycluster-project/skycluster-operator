# Readme

## Develop

```bash
NAME=redis-app-policies-f5bpw
kubectl patch skyxrd $NAME -n redis-app --type merge -p '{"spec":{"approve":true}}'




kubectl patch ILPTask redis-app-policies \
  --type=json \
  -p='[{"op":"remove","path":"/status/optimization/status"}]' \
  --subresource=status
  
kubectl patch SkyXRD redis-app-policies-w4gjf \
  --type=json \
  -p='[{"op":"remove","path":"/status/manifests"}]' \
  --subresource=status

kubectl patch ILPTask redis-app-policies \
  --type=json \
  -p='[{"op":"remove","path":"/status/optimization/result"}]' \
  --subresource=status

aws ec2 describe-availability-zones --region us-west-1 --query "AvailabilityZones[].ZoneName" --output text


mkdir /tmp/shared
N=netshoot-846db597fb-px7cc
kubectl cp skycluster-system/$N:/shared /tmp/shared


kubectl scale deployment -n crossplane-system crossplane --replicas=0

N=redis-app-policies-w4gjf
kubectl get skynet $NN -n redis-app -o json > /tmp/xrd.json; for i in $(seq 0 $(( $(jq '.status.manifests|length' /tmp/xrd.json)-1 ))); do   jq -r ".status.manifests[$i]" /tmp/xrd.json > manifest-net-$i.yaml; done

kubectl patch skynet $NN -n redis-app -p '{"spec":{"approve":true}}' --type=merge


```
