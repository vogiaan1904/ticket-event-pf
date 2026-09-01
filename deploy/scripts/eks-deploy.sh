#!/usr/bin/env bash
# Deploy the TicketBottle chart to EKS from ECR, then wait until the ALB serves.
# Account-specific values (registry, IRSA role ARN, subnets, your IP) are generated
# from terraform outputs so they never live in git.
set -euo pipefail
HERE="$(cd "$(dirname "$0")" && pwd)"
EKS_ENV="$HERE/../terraform/envs/eks"
CHART="$HERE/../helm/ticketbottle"
NS=ticketbottle
GEN=/tmp/tb-eks-values.yaml

# Deploy an immutable sha- tag, not :latest. A moving tag leaves the pod template
# unchanged, so `helm upgrade` rolls nothing and the running build is unknowable;
# a later HPA scale-out pulls a newer image and serves two versions at once.
#
# Resolve to the newest origin/main commit that actually has an image: CI only
# builds on changes under services/ and deploy/adapters/, so a chart- or
# script-only commit advances main without producing one.
REGION=${REGION:-us-east-1}
git -C "$HERE/.." fetch origin main --quiet 2>/dev/null || true

resolve_tag() {
  local sha
  for sha in $(git -C "$HERE/.." rev-list -n 30 origin/main); do
    if aws ecr describe-images --region "$REGION" --repository-name ticketbottle/gateway \
         --image-ids imageTag="sha-$sha" >/dev/null 2>&1; then
      echo "sha-$sha"
      return 0
    fi
  done
  return 1
}

if [ -z "${IMAGE_TAG:-}" ] && ! IMAGE_TAG=$(resolve_tag); then
  echo "no built image for any of the last 30 commits on origin/main"
  echo "  origin/main is $(git -C "$HERE/.." rev-parse --short origin/main)"
  echo "  gh run list --branch main --limit 3"
  exit 1
fi

REGISTRY=$(cd "$EKS_ENV" && terraform output -raw ecr_registry)
ORDER_ROLE=$(cd "$EKS_ENV" && terraform output -raw order_role_arn)
SUBNETS=$(cd "$EKS_ENV" && terraform output -raw alb_subnets)
MYIP=$(cd "$EKS_ENV" && terraform output -raw my_ip_cidr)

cat >"$GEN" <<EOF
image:
  registry: "${REGISTRY}/"
  tag: "${IMAGE_TAG}"
serviceAccount:
  order:
    roleArn: "${ORDER_ROLE}"
ingress:
  subnets: "${SUBNETS}"
  inboundCidrs: "${MYIP}"
EOF
echo "== generated $GEN =="; cat "$GEN"

echo "== deploying $IMAGE_TAG =="
SECRETS="$HERE/../secrets.values.yaml"
if [ ! -f "$SECRETS" ]; then
  echo "missing $SECRETS — run: make -C deploy secrets-init"
  exit 1
fi

helm upgrade --install tb "$CHART" -n "$NS" --create-namespace \
  -f "$CHART/values-eks.yaml" -f "$GEN" -f "$SECRETS" --wait --timeout 15m

echo "== waiting for the controller to publish an ALB hostname =="
HOST=""
for i in $(seq 1 40); do
  HOST=$(kubectl -n "$NS" get ingress app-gateway \
           -o jsonpath='{.status.loadBalancer.ingress[0].hostname}' 2>/dev/null || true)
  [ -n "$HOST" ] && break
  echo "  [$i] no hostname yet"
  sleep 15
done
if [ -z "$HOST" ]; then
  echo "no ALB hostname after 10m. Check:"
  echo "  kubectl -n kube-system logs deploy/aws-load-balancer-controller --tail=50"
  echo "  kubectl -n $NS describe ingress app-gateway"
  exit 1
fi
echo "ALB: http://$HOST"

echo "== waiting for target registration (~2 min) =="
for i in $(seq 1 40); do
  CODE=$(curl -s -o /dev/null -w '%{http_code}' --max-time 5 "http://$HOST/api" || true)
  echo "  [$i] GET /api -> ${CODE:-000}"
  if [ -n "$CODE" ] && [ "$CODE" != "000" ] && [ "$CODE" -lt 400 ]; then
    echo "GATEWAY REACHABLE: http://$HOST/api"
    exit 0
  fi
  sleep 15
done
echo "ALB never became healthy — inspect the target group health in the console"
exit 1
