#!/usr/bin/env bash
# Recovery tool: remove EKS resources that outlived their cluster.
#
# When the cluster is destroyed while the AWS Load Balancer Controller still owns
# an ALB — the classic cause is expired credentials making kubectl look "down" —
# the ALB, its target groups, its k8s-* security groups and the PVC-backed EBS
# volumes are stranded. Nothing left in the cluster can clean them up, and
# Terraform never knew about them, so they bill until deleted by hand.
#
# Scope is deliberately narrow: only `k8s-*`-named ELB resources and only
# `available` (unattached) volumes tagged as Kubernetes PVCs.
#
# Usage: ./eks-sweep-orphans.sh [--yes]
set -euo pipefail

REGION="${AWS_REGION:-us-east-1}"
ASSUME_YES=0
case "${1:-}" in --yes|-y) ASSUME_YES=1 ;; esac

aws sts get-caller-identity >/dev/null 2>&1 || { echo "AWS credentials invalid — re-authenticate first."; exit 1; }

step() { printf '\n== %s ==\n' "$1"; }

step "survey"
LB_ARNS=$(aws --region "$REGION" elbv2 describe-load-balancers \
  --query 'LoadBalancers[?starts_with(LoadBalancerName,`k8s-`)].LoadBalancerArn' --output text)
VOLS=$(aws --region "$REGION" ec2 describe-volumes \
  --filters Name=status,Values=available Name=tag-key,Values=kubernetes.io/created-for/pvc/name \
  --query 'Volumes[].VolumeId' --output text)

echo "load balancers : ${LB_ARNS:-none}"
echo "PVC volumes    : ${VOLS:-none}"

if [ -z "$LB_ARNS" ] && [ -z "$VOLS" ]; then
  echo; echo "nothing to sweep."; exit 0
fi

if [ "$ASSUME_YES" -eq 0 ]; then
  printf '\nPermanently delete the above (volume DATA IS LOST)? Type "sweep": '
  read -r C; [ "$C" = "sweep" ] || { echo "aborted"; exit 1; }
fi

if [ -n "$LB_ARNS" ]; then
  step "delete load balancers"
  for arn in $LB_ARNS; do echo "  $arn"; aws --region "$REGION" elbv2 delete-load-balancer --load-balancer-arn "$arn"; done
  echo "  waiting for ENIs/EIPs to release"
  for i in $(seq 1 30); do
    LEFT=$(aws --region "$REGION" elbv2 describe-load-balancers \
      --query 'LoadBalancers[?starts_with(LoadBalancerName,`k8s-`)].LoadBalancerName' --output text)
    [ -z "$LEFT" ] && { echo "  gone"; break; }
    sleep 10
  done
fi

# Target groups only delete once no listener references them, and deleting a load
# balancer releases its listeners ASYNCHRONOUSLY — so a single attempt right after
# the LB disappears reliably loses the race with ResourceInUse. Retry.
step "delete orphan target groups"
for attempt in 1 2 3 4 5 6; do
  REMAINING=""
  for tg in $(aws --region "$REGION" elbv2 describe-target-groups \
               --query 'TargetGroups[?starts_with(TargetGroupName,`k8s-`)].TargetGroupArn' --output text); do
    aws --region "$REGION" elbv2 delete-target-group --target-group-arn "$tg" 2>/dev/null \
      && echo "  deleted $tg" || REMAINING="$REMAINING $tg"
  done
  [ -z "$REMAINING" ] && break
  echo "  listeners still releasing, retrying in 20s:$REMAINING"
  sleep 20
done

if [ -n "$VOLS" ]; then
  step "delete unattached PVC volumes"
  for v in $VOLS; do echo "  $v"; aws --region "$REGION" ec2 delete-volume --volume-id "$v" || true; done
fi

# SGs can only go once the ENIs that used them are released, which is why this
# runs last and retries — deletion races the ELB teardown.
step "delete k8s-* security groups"
for attempt in 1 2 3 4 5 6; do
  REMAINING=""
  for sg in $(aws --region "$REGION" ec2 describe-security-groups \
               --filters 'Name=group-name,Values=k8s-*' --query 'SecurityGroups[].GroupId' --output text); do
    aws --region "$REGION" ec2 delete-security-group --group-id "$sg" 2>/dev/null \
      && echo "  deleted $sg" || REMAINING="$REMAINING $sg"
  done
  [ -z "$REMAINING" ] && break
  echo "  still in use (ENIs releasing), retrying in 20s:$REMAINING"
  sleep 20
done

step "re-checking"
exec "$(cd "$(dirname "$0")" && pwd)/eks-leak-check.sh"
