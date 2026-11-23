#!/usr/bin/env bash
# Finds what bounds purchase throughput on the k3s box. Each argument is one run
# of that many buyers for $DURATION with the door open; capacity.py prints what
# it served and the limit it hit, also into $OUT/summary.txt. Deploy with
# deploy/loadtest/capacity-values.yaml first. Needs GW, KUBECONFIG, Prometheus on :9090.
#   deploy/scripts/capacity-sweep.sh 10 20 40 80
set -euo pipefail
HERE="$(cd "$(dirname "$0")" && pwd)"
. "$HERE/pg.sh"
NS=ticketbottle
PROM=${PROM:-http://localhost:9090}
OUT=${OUT:-/tmp/capacity-sweep}
DURATION=${DURATION:-3m}
: "${GW:?set GW, e.g. http://localhost:3000/api}"
[[ "$DURATION" =~ ^[0-9]+m$ ]] || { echo "DURATION must be whole minutes, e.g. 3m"; exit 1; }
MINS=${DURATION%m}

q() { curl -fsS "$PROM/api/v1/query" --data-urlencode "query=$1" --data-urlencode "time=$2" > "$3"; }
at() {
  curl -fsS "$PROM/api/v1/query" --data-urlencode "query=$1" --data-urlencode "time=$2" |
    python3 -c 'import sys, json; r = json.load(sys.stdin)["data"]["result"]; print(r[0]["value"][1] if r else "nan")'
}
temporal_idle() {
  local q='sum(rate(container_cpu_usage_seconds_total{namespace="ticketbottle",pod=~"temporal-[a-z0-9]+-[a-z0-9]+",container!=""}[1m]))'
  until python3 -c "import sys; sys.exit(0 if float('$(at "$q" "$(date -u +%FT%TZ)")') < 0.05 else 1)"; do sleep 10; done
}
restarts() {
  kubectl -n $NS get pods -o jsonpath='{range .items[*]}{range .status.containerStatuses[*]}{.restartCount}{"\n"}{end}{end}' |
    awk '{ s += $1 } END { print s + 0 }'
}
plus() { python3 -c 'import sys; from datetime import datetime, timedelta; t = datetime.fromisoformat(sys.argv[1].replace("Z", "+00:00")); print((t + timedelta(seconds=int(sys.argv[2]))).strftime("%Y-%m-%dT%H:%M:%SZ"))' "$1" "$2"; }

# A run measures the design only on the capacity overlay: say which part is missing.
url=$(kubectl -n $NS exec deploy/payment-service -- printenv ZALOPAY_CREATE_URL || true)
[ "$url" = "http://payment-webhook:8080/v2/create" ] || { echo "payment-service calls '${url:-the ZaloPay sandbox}', not the simulated provider"; exit 1; }
limited=$(kubectl -n $NS get pods --field-selector=status.phase=Running \
  -o jsonpath='{range .items[*]}{range .spec.containers[*]}{.resources.limits.cpu}{"\n"}{end}{end}' | grep -c . || true)
[ "$limited" = 0 ] || { echo "$limited running containers still have a CPU limit"; exit 1; }
conns=$(pg postgres -tAc 'SHOW max_connections')
[ "$conns" = 500 ] || { echo "Postgres allows $conns connections, not 500"; exit 1; }
VCPU=$(kubectl get nodes -o jsonpath='{.items[0].status.capacity.cpu}')
echo "box: $VCPU vCPU; runs of $DURATION; raw files under $OUT"

n=0
for VUS in "$@"; do
  n=$((n + 1)); L="v$VUS-$n"; D="$OUT/$VCPU-vcpu/$L"; mkdir -p "$D"
  temporal_idle
  before=$(restarts)
  # One exec samples Postgres every 2s from inside its pod, through seeding and the run.
  kubectl -n $NS exec -i statefulset/postgres -- sh -c \
    "cat > /tmp/pg-sample.sql; for i in \$(seq 1 $(( (MINS * 60 + 180) / 2 ))); do psql -U root -d postgres -AtF'|' -f /tmp/pg-sample.sql; sleep 2; done" \
    < "$HERE/../loadtest/pg-sample.sql" > "$D/pg.txt" 2>&1 &
  PG=$!
  TOTAL=100000 VUS=$VUS DURATION=$DURATION K6_CPU=2 K6_MEMORY=3Gi "$HERE/gate4a-load.sh" > "$D/run.txt" 2>&1 ||
    { kill $PG 2>/dev/null; echo "$L: load run failed: $(tail -1 "$D/run.txt")"; exit 1; }
  kill $PG 2>/dev/null || true
  after=$(restarts)

  kubectl -n $NS logs job/k6-load > "$D/k6.log"
  read -r K6POD FROM TO <<<"$(kubectl -n $NS get pod -l job-name=k6-load -o jsonpath='{.items[0].metadata.name} {.items[0].status.startTime} {.items[0].status.containerStatuses[0].state.terminated.finishedAt}')"
  for edge in from to; do
    T=$FROM; [ $edge = to ] && T=$TO
    q 'sum by (pod) (container_cpu_usage_seconds_total{namespace="ticketbottle",container!=""})' "$T" "$D/pods-$edge.json"
    q 'sum by (mode) (node_cpu_seconds_total)' "$T" "$D/node-$edge.json"
    q 'sum(node_disk_reads_completed_total{device=~"nvme[0-9]+n[0-9]+"}) + sum(node_disk_writes_completed_total{device=~"nvme[0-9]+n[0-9]+"})' "$T" "$D/disk-$edge.json"
  done
  for m in throttled_periods periods; do
    q "max_over_time(container_cpu_cfs_${m}_total{namespace=\"ticketbottle\",pod=\"$K6POD\",container=\"k6\"}[$((MINS * 60 + 60))s])" "$TO" "$D/k6-${m%_periods}.json"
  done
  # Sagas started 60-90s in, past the opening burst: a whole run is too many to fetch.
  "$HERE/saga-histories.sh" "$(plus "$FROM" 60)" "$(plus "$FROM" 90)" "$D/histories.txt" > /dev/null

  printf '{"label": "%s", "vcpu": %s, "vus": %s, "from": "%s", "to": "%s", "restarts_before": %s, "restarts_after": %s}\n' \
    "$L" "$VCPU" "$VUS" "$FROM" "$TO" "$before" "$after" > "$D/meta.json"
  python3 "$HERE/../loadtest/capacity.py" "$D" | tee -a "$OUT/summary.txt"
done
