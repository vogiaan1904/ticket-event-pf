#!/usr/bin/env bash
# Finds the k3s box's door speed. Each argument is one run: the waitroom's
# release rate is deployed at that rate, 40 buyers queue for 5 minutes, and one
# line of checkout latency, throughput, cost and queue is printed. The rule for
# choosing a rate: docs/design/admission-sizing.md. Needs REF, GW, KUBECONFIG and
# the Prometheus port-forward on :9090.
#   REF=sha-<build> deploy/scripts/door-sweep.sh 1 2 3 4 4 3 2 1
set -euo pipefail
HERE="$(cd "$(dirname "$0")" && pwd)"
NS=ticketbottle
PROM=${PROM:-http://localhost:9090}
OUT=${OUT:-/tmp/door-sweep}
: "${GW:?set GW, e.g. http://localhost:3000/api}"
export REF=${REF:?set REF to the sha- tag being measured; :dev can move mid-sweep}
mkdir -p "$OUT"

at() {
  curl -fsS "$PROM/api/v1/query" --data-urlencode "query=$1" --data-urlencode "time=$2" |
    python3 -c 'import sys, json; r = json.load(sys.stdin)["data"]["result"]; print(r[0]["value"][1] if r else "nan")'
}
temporal_idle() {
  local q='sum(rate(container_cpu_usage_seconds_total{namespace="ticketbottle",pod=~"temporal-[a-z0-9]+-[a-z0-9]+",container!=""}[1m]))'
  until python3 -c "import sys; sys.exit(0 if float('$(at "$q" "$(date -u +%FT%TZ)")') < 0.05 else 1)"; do sleep 10; done
}

n=0
for RATE in "$@"; do
  n=$((n + 1)); L="r$RATE-$n"
  make -C "$HERE/.." k3s-deploy HELM_EXTRA="--set waitroom.releaseRate=$RATE" > "$OUT/deploy-$L.txt" 2>&1 ||
    { echo "$L: deploy failed: $(tail -1 "$OUT/deploy-$L.txt")"; exit 1; }
  kubectl -n $NS rollout status deploy/waitroom-service --timeout=5m > /dev/null
  # The ConfigMap can say one rate while the pod still runs the last: ask the pod.
  started=$(kubectl -n $NS logs deploy/waitroom-service | grep 'Starting queue processor' | tail -1)
  grep -qE "batch_size: $RATE([^0-9]|$)" <<<"$started" || { echo "$L: waitroom runs '$started', not rate $RATE"; exit 1; }
  temporal_idle

  TOTAL=100000 VUS=40 DURATION=5m "$HERE/gate4a-load.sh" > "$OUT/run-$L.txt" 2>&1
  kubectl -n $NS logs job/k6-load > "$OUT/k6-$L.log"
  read -r FROM TO <<<"$(kubectl -n $NS get pod -l job-name=k6-load \
    -o jsonpath='{.items[0].status.startTime} {.items[0].status.containerStatuses[0].state.terminated.finishedAt}')"

  "$HERE/purchase-cost.sh" "$FROM" "$TO" "$OUT/k6-$L.log" > "$OUT/cost-$L.txt"
  QMIN=$(at 'min_over_time(sum(tb_waitroom_queue_depth)[4m:15s])' "$TO")
  SMAX=$(at 'max_over_time(sum(tb_waitroom_slots_in_use)[4m:15s])' "$TO")
  "$HERE/saga-histories.sh" "$FROM" "$TO" "$OUT/h-$L.txt" > /dev/null
  OPEN=$(python3 "$HERE/../loadtest/saga_latency.py" "$OUT/h-$L.txt" --burst-first 20 |
    awk '/first workflow task queued/ { print $5 "/" $8; exit }')

  python3 - "$L" "$RATE" "$FROM" "$TO" "$OUT" "$QMIN" "$SMAX" "$OPEN" "$HERE" <<'PY'
import os, re, sys
from datetime import datetime
L, rate, frm, to, out, qmin, smax, opening, here = sys.argv[1:]
sys.path.insert(0, os.path.join(here, "..", "loadtest"))
import saga_latency
log = open(f"{out}/k6-{L}.log").read()
ok = sorted(float(ms) / 1000 for ms, st in re.findall(r'msg="CHECKOUT \S+ ([\d.]+) (\d+)"', log) if int(st) < 400)
q = lambda p: ok[min(len(ok) - 1, round(p / 100 * (len(ok) - 1)))]
secs = (datetime.fromisoformat(to.replace("Z", "+00:00")) - datetime.fromisoformat(frm.replace("Z", "+00:00"))).total_seconds()
temporal = re.search(r"temporal\s+\S+ core-s\s+(\S+)", open(f"{out}/cost-{L}.txt").read()).group(1)
calm, stalled = saga_latency.without_stalls(saga_latency.load_histories(f"{out}/h-{L}.txt"), saga_latency.load_client(f"{out}/k6-{L}.log"))
print(f"{L:<7} rate {rate}/s | {len(ok)} purchases, {len(ok) / secs:.2f}/s | checkout p50 {q(50):.2f}s p99 {q(99):.2f}s,"
      f" {100 * sum(x <= 2 for x in ok) / len(ok):.1f}% under 2s | without payment stalls p99 {saga_latency.pct(calm, 99):.2f}s, {stalled} stalled"
      f" | temporal {temporal} core-s | queue min {float(qmin):.0f},"
      f" slots max {float(smax):.0f} | opening first-task wait p50/max {opening}s")
PY
done
