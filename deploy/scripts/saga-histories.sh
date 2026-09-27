#!/usr/bin/env bash
# Dumps the history of every CreateOrder saga started in [FROM, TO), for
# deploy/loadtest/saga_latency.py. Temporal keeps a history for 24h.
#   deploy/scripts/saga-histories.sh 2026-09-27T06:43:00Z 2026-09-27T06:47:00Z out.txt
set -euo pipefail
FROM=${1:?usage: saga-histories.sh FROM TO OUT  (RFC 3339, UTC)}
TO=${2:?usage: saga-histories.sh FROM TO OUT}
OUT=${3:?usage: saga-histories.sh FROM TO OUT}
NS=ticketbottle
QUERY="WorkflowType=\"CreateOrder\" AND StartTime >= \"$FROM\" AND StartTime < \"$TO\""

# One exec for the whole loop: a kubectl round trip per saga costs seconds on the tunnel.
kubectl -n $NS exec deploy/temporal -- sh -c "
  temporal workflow list --query '$QUERY' --limit 100000 -o jsonl \
    | sed -n 's/.*\"workflowId\":\"\([^\"]*\)\".*/\1/p' > /tmp/saga-ids
  for id in \$(cat /tmp/saga-ids); do echo \"=== \$id\"; temporal workflow show -w \"\$id\" -o jsonl; done > /tmp/saga-histories"
kubectl -n $NS exec deploy/temporal -- cat /tmp/saga-histories > "$OUT"
echo "$(grep -c '^=== ' "$OUT") sagas -> $OUT"
