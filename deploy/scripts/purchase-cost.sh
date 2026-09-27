#!/usr/bin/env bash
# What one purchase costs the k3s box over a load run: CPU core-seconds per
# completed purchase, whole node and Temporal and Postgres, and history events
# per saga. Needs the Prometheus port-forward on :9090 and KUBECONFIG.
#   deploy/scripts/purchase-cost.sh FROM TO K6_LOG   (RFC 3339, UTC)
set -euo pipefail
FROM=${1:?usage: purchase-cost.sh FROM TO K6_LOG}
TO=${2:?usage: purchase-cost.sh FROM TO K6_LOG}
K6_LOG=${3:?usage: purchase-cost.sh FROM TO K6_LOG}
PROM=${PROM:-http://localhost:9090}

at() {
  curl -fsS "$PROM/api/v1/query" --data-urlencode "query=$1" --data-urlencode "time=$2" |
    python3 -c 'import sys, json; r = json.load(sys.stdin)["data"]["result"]; print(r[0]["value"][1] if r else "nan")'
}
# Raw counter deltas, not increase(): increase() extrapolates to the window's edges.
delta() { python3 -c "print(float('$(at "$1" "$TO")') - float('$(at "$1" "$FROM")'))"; }

NODE=$(delta 'sum(node_cpu_seconds_total{mode!="idle"})')
TEMPORAL=$(delta 'sum(container_cpu_usage_seconds_total{namespace="ticketbottle",pod=~"temporal-[a-z0-9]+-[a-z0-9]+",container!=""})')
POSTGRES=$(delta 'sum(container_cpu_usage_seconds_total{namespace="ticketbottle",pod="postgres-0",container!=""})')
PURCHASES=$(python3 -c "import re; s = open('$K6_LOG').read(); print(re.search(r'\"tb_orders_completed\":\s*\{\s*\"count\":\s*(\d+)', s).group(1))")

events() {
  kubectl -n ticketbottle exec deploy/temporal -- temporal workflow list --limit 100000 -o jsonl \
    --query "WorkflowType=\"$1\" AND StartTime >= \"$FROM\" AND StartTime < \"$TO\"" |
    python3 -c 'import sys, json; h = [int(json.loads(l)["historyLength"]) for l in sys.stdin if l.strip()]; print(f"{sum(h) / len(h):.1f} over {len(h)}" if h else "none")'
}

python3 - "$NODE" "$TEMPORAL" "$POSTGRES" "$PURCHASES" <<'EOF'
import sys
node, temporal, postgres, n = map(float, sys.argv[1:])
print(f"purchases {n:.0f}")
for name, cores in (("node", node), ("temporal", temporal), ("postgres", postgres)):
    print(f"  {name:<9} {cores:7.1f} core-s  {cores / n:6.3f} per purchase")
EOF
echo "  history events per saga: CreateOrder $(events CreateOrder), ConfirmOrder $(events ConfirmOrder)"
