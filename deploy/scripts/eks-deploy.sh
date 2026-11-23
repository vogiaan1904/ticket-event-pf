#!/usr/bin/env bash
# Deploy the TicketBottle chart to EKS from ECR, then wait until the ALB serves.
# Account-specific values (registry, IRSA role ARN, subnets) come from terraform
# outputs so they never live in git; the ALB allows the IP you deploy from (0041).
set -euo pipefail
HERE="$(cd "$(dirname "$0")" && pwd)"
EKS_ENV="$HERE/../terraform/envs/eks"
. "$HERE/eks-context.sh"
CHART="$HERE/../helm/ticketbottle"
NS=ticketbottle
GEN=/tmp/tb-eks-values.yaml

# The build ECR's :$REF points at, as k3s-deploy does; IMAGE_TAG overrides it.
if [ -z "${IMAGE_TAG:-}" ]; then
  IMAGE_TAG=$("$HERE/image-tag.sh" "${REF:-dev}")
fi

REGISTRY=$(cd "$EKS_ENV" && terraform output -raw ecr_registry)
# JSON is YAML, so the maps go in as flow mappings.
PG_HOSTS=$(cd "$EKS_ENV" && terraform output -json postgres_hosts)
PG_ARNS=$(cd "$EKS_ENV" && terraform output -json postgres_password_arns)
# service_role_arns is keyed by ServiceAccount name; the chart keys by short name.
SA_ROLES=$(cd "$EKS_ENV" && terraform output -json service_role_arns \
  | jq -r 'to_entries[] | "  \(.key | sub("-service$"; "")): { roleArn: \"\(.value)\" }"')
SUBNETS=$(cd "$EKS_ENV" && terraform output -raw alb_subnets)
MYIP=$(curl -fsS --max-time 5 https://checkip.amazonaws.com | tr -d '[:space:]')
echo "$MYIP" | grep -Eq '^([0-9]{1,3}\.){3}[0-9]{1,3}$' ||
  { echo "REFUSING: '$MYIP' is not an IPv4; the ALB would allow nobody"; exit 1; }
MYIP="$MYIP/32"

cat >"$GEN" <<EOF
image:
  registry: "${REGISTRY}/"
  tag: "${IMAGE_TAG}"
serviceAccount:
${SA_ROLES}
postgres:
  hosts: ${PG_HOSTS}
  passwordArns: ${PG_ARNS}
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
