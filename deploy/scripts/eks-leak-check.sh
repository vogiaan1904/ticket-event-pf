#!/usr/bin/env bash
# Gate 3b: assert `terraform destroy` left nothing billable behind.
# Reads the VPC id from the FOUNDATION state (which survives; envs/eks state is
# empty after destroy).
set -uo pipefail
HERE="$(cd "$(dirname "$0")" && pwd)"
REGION=us-east-1
CLUSTER=${CLUSTER:-ticketbottle-eks}
VPC=$(cd "$HERE/../terraform/envs/foundation" && terraform output -raw vpc_id)
FAIL=0

check() { # check <label> <output>
  local label="$1" out="$2"
  if [ -z "$(echo "$out" | tr -d '[:space:]')" ] || [ "$out" = "None" ]; then
    echo "  OK    $label"
  else
    echo "  LEAK  $label"
    echo "$out" | sed 's/^/          /'
    FAIL=1
  fi
}

echo "== Gate 3b leak check (vpc=$VPC cluster=$CLUSTER) =="

check "EKS clusters" \
  "$(aws eks list-clusters --region "$REGION" --query "clusters[?@=='$CLUSTER']" --output text)"

check "node-group instances" \
  "$(aws ec2 describe-instances --region "$REGION" \
      --filters "Name=tag:eks:cluster-name,Values=$CLUSTER" \
                "Name=instance-state-name,Values=pending,running,stopping,stopped" \
      --query "Reservations[].Instances[].[InstanceId,InstanceType,State.Name]" --output text)"

check "load balancers in the VPC" \
  "$(aws elbv2 describe-load-balancers --region "$REGION" \
      --query "LoadBalancers[?VpcId=='$VPC'].[LoadBalancerName,Type,State.Code]" --output text)"

check "target groups in the VPC" \
  "$(aws elbv2 describe-target-groups --region "$REGION" \
      --query "TargetGroups[?VpcId=='$VPC'].TargetGroupName" --output text)"

check "EBS volumes tagged for the cluster" \
  "$(aws ec2 describe-volumes --region "$REGION" \
      --filters "Name=tag-key,Values=kubernetes.io/cluster/$CLUSTER" \
      --query "Volumes[].[VolumeId,Size,State]" --output text)"

check "unattached EBS volumes (any)" \
  "$(aws ec2 describe-volumes --region "$REGION" --filters Name=status,Values=available \
      --query "Volumes[].[VolumeId,Size]" --output text)"

check "Elastic IPs" \
  "$(aws ec2 describe-addresses --region "$REGION" \
      --query "Addresses[].[PublicIp,AllocationId]" --output text)"

check "NAT gateways in the VPC" \
  "$(aws ec2 describe-nat-gateways --region "$REGION" --filter "Name=vpc-id,Values=$VPC" \
      --query "NatGateways[?State!='deleted'].NatGatewayId" --output text)"

check "controller-created security groups (k8s-*)" \
  "$(aws ec2 describe-security-groups --region "$REGION" --filters "Name=vpc-id,Values=$VPC" \
      --query "SecurityGroups[?starts_with(GroupName,'k8s-')].GroupName" --output text)"

check "CloudWatch log groups for the cluster" \
  "$(aws logs describe-log-groups --region "$REGION" --log-group-name-prefix "/aws/eks/$CLUSTER" \
      --query "logGroups[].logGroupName" --output text)"

echo
if [ "$FAIL" = "0" ]; then
  echo "GATE 3b PASSED: no leaked billable resources."
else
  echo "GATE 3b FAILED: delete the resources listed above, then re-run."
  exit 1
fi