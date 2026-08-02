#!/usr/bin/env bash
# Rung 3 bootstrap: cluster-level pieces the app chart assumes exist.
#   1. the gp3 StorageClass (EBS CSI driver is installed by Terraform as an addon)
#   2. the AWS Load Balancer Controller, wearing its IRSA role
# Idempotent — safe to re-run.
set -euo pipefail
HERE="$(cd "$(dirname "$0")" && pwd)"
EKS_ENV="$HERE/../terraform/envs/eks"

CLUSTER=$(cd "$EKS_ENV" && terraform output -raw cluster_name)
LBC_ROLE=$(cd "$EKS_ENV" && terraform output -raw lbc_role_arn)
VPC=$(cd "$EKS_ENV" && terraform output -raw vpc_id)
REGION=us-east-1

echo "== 1. gp3 StorageClass =="
kubectl apply -f "$HERE/../k8s/eks/storageclass-gp3.yaml"

echo "== 2. AWS Load Balancer Controller (cluster=$CLUSTER vpc=$VPC) =="
helm repo add eks https://aws.github.io/eks-charts >/dev/null 2>&1 || true
helm repo update eks >/dev/null

helm upgrade --install aws-load-balancer-controller eks/aws-load-balancer-controller \
  -n kube-system \
  --set clusterName="$CLUSTER" \
  --set region="$REGION" \
  --set vpcId="$VPC" \
  --set serviceAccount.create=true \
  --set serviceAccount.name=aws-load-balancer-controller \
  --set "serviceAccount.annotations.eks\.amazonaws\.com/role-arn=$LBC_ROLE" \
  --set createIngressClassResource=true \
  --set ingressClass=alb \
  --wait --timeout 5m

kubectl -n kube-system rollout status deploy/aws-load-balancer-controller --timeout=5m
echo "bootstrap complete"