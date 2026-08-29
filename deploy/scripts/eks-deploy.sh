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

REGISTRY=$(cd "$EKS_ENV" && terraform output -raw ecr_registry)
ORDER_ROLE=$(cd "$EKS_ENV" && terraform output -raw order_role_arn)
SUBNETS=$(cd "$EKS_ENV" && terraform output -raw alb_subnets)
MYIP=$(cd "$EKS_ENV" && terraform output -raw my_ip_cidr)

cat >"$GEN" <<EOF
image:
  registry: "${REGISTRY}/"
serviceAccount:
  order:
    roleArn: "${ORDER_ROLE}"
ingress:
  subnets: "${SUBNETS}"
  inboundCidrs: "${MYIP}"
EOF
echo "== generated $GEN =="; cat "$GEN"

echo "== helm upgrade --install =="
helm upgrade --install tb "$CHART" -n "$NS" --create-namespace \
  -f "$CHART/values-eks.yaml" -f "$GEN" --wait --timeout 15m

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