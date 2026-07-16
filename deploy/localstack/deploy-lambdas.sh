#!/usr/bin/env bash
# Deploy the 3 payment Lambdas + API Gateway to the host LocalStack, using the
# Rung-1.5 env (DB/Kafka point at kind NodePorts: ticketbottle-control-plane:31432/31094).
# Reuses the lambdas' own deploy-to-dev-compose.sh (publishes layers + creates the
# functions + wires the API Gateway) with the rung env swapped in, then restored.
set -euo pipefail
HERE="$(cd "$(dirname "$0")" && pwd)"
LAMBDAS="$HERE/../../services/payment-svc/lambdas"
cp "$LAMBDAS/envs/env.localstack.json" "$LAMBDAS/envs/env.localstack.json.bak"
cp "$HERE/env.lambdas.json" "$LAMBDAS/envs/env.localstack.json"
trap 'mv -f "$LAMBDAS/envs/env.localstack.json.bak" "$LAMBDAS/envs/env.localstack.json"' EXIT
( cd "$LAMBDAS" && ./scripts/deploy-to-dev-compose.sh )
