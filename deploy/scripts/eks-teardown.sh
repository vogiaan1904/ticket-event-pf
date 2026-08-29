#!/usr/bin/env bash
# Ordered EKS teardown. ORDER MATTERS — three of these resources are NOT in
# Terraform state and would survive (and bill after) a bare `terraform destroy`:
#   1. the ALB + target groups + security group created by the LB controller
#      from the Ingress object
#   2. the EBS volumes behind StatefulSet PVCs (volumeClaimTemplate PVCs are never
#      garbage-collected — not by `helm uninstall`, not by deleting the namespace)
#   3. the controller's own webhook/SA if the cluster is deleted underneath it
set -euo pipefail
HERE="$(cd "$(dirname "$0")" && pwd)"
EKS_ENV="$HERE/../terraform/envs/eks"
NS=ticketbottle
REGION=us-east-1

VPC=$(cd "$EKS_ENV" && terraform output -raw vpc_id)

echo "== 0. sanity: any LoadBalancer-type Services? (this chart has none) =="
kubectl get svc -A --field-selector spec.type=LoadBalancer --no-headers 2>/dev/null || true

echo "== 1. delete the Ingress so the controller deletes its ALB =="
kubectl delete ingress --all -n "$NS" --ignore-not-found

echo "   waiting for the ALB to disappear from the VPC..."
for i in $(seq 1 40); do
  N=$(aws elbv2 describe-load-balancers --region "$REGION" \
        --query "length(LoadBalancers[?VpcId=='$VPC'])" --output text)
  echo "   [$i] load balancers in $VPC: $N"
  [ "$N" = "0" ] && break
  sleep 15
done

echo "== 2. uninstall the app release =="
helm uninstall tb -n "$NS" 2>/dev/null || true

echo "== 3. delete PVCs — helm uninstall does NOT remove volumeClaimTemplate PVCs =="
kubectl delete pvc --all -n "$NS" --ignore-not-found
echo "   waiting for the PersistentVolumes (and their EBS volumes) to go away..."
for i in $(seq 1 24); do
  N=$(kubectl get pv --no-headers 2>/dev/null | wc -l | tr -d ' ')
  echo "   [$i] PVs remaining: $N"
  [ "$N" = "0" ] && break
  sleep 10
done

echo "== 4. uninstall the load balancer controller =="
helm uninstall aws-load-balancer-controller -n kube-system 2>/dev/null || true

echo "== 5. terraform destroy =="
cd "$EKS_ENV" && terraform destroy

echo
echo "== teardown finished. Now prove it: make -C deploy eks-leak-check =="