# Readme

## Develop

```bash
NAME=redis-app-policies-f5bpw
kubectl patch skyxrd $NAME -n redis-app --type merge -p '{"spec":{"approve":true}}'




kubectl patch ILPTask redis-app-policies \
  --type=json \
  -p='[{"op":"remove","path":"/status/optimization/status"}]' \
  --subresource=status
  
kubectl patch SkyXRD redis-app-policiesxh229 \
  --type=json \
  -p='[{"op":"remove","path":"/status/manifests"}]' \
  --subresource=status

kubectl patch ILPTask redis-app-policies \
  --type=json \
  -p='[{"op":"remove","path":"/status/optimization/result"}]' \
  --subresource=status

aws ec2 describe-availability-zones --region us-west-1 --query "AvailabilityZones[].ZoneName" --output text


```
