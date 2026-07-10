#!/usr/bin/env bash
set -euo pipefail
NS=ticketbottle
echo "== all app pods Ready =="
kubectl -n $NS wait --for=condition=Ready pod \
  -l 'app in (user-service,event-service,inventory-service,waitroom-service,payment-service,order-service,order-consumer,app-gateway)' \
  --timeout=180s
echo "== gateway signup (gateway -> user-svc -> postgres) =="
EMAIL="smoke+$(date +%s)@example.com"
CODE=$(curl -s -o /tmp/signup.json -w '%{http_code}' -X POST http://localhost:3000/api/auth/signup \
  -H 'Content-Type: application/json' \
  -d "{\"email\":\"$EMAIL\",\"password\":\"Password123!\",\"firstName\":\"Smoke\",\"lastName\":\"Test\"}")
echo "  HTTP $CODE"
grep -qiE "accessToken|access_token" /tmp/signup.json && echo "  OK signup returned tokens" || { echo "  FAIL: $(cat /tmp/signup.json)"; exit 1; }
echo "ALL APP SMOKE CHECKS PASSED"
