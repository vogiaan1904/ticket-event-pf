#!/usr/bin/env bash
# Rollout drain measurement: docs/plans/2026-09-24-rollout-drain-measurement.md.
#   rollout-drain.sh control  <deploy>      delete the only pod; the probe MUST fail
#   rollout-drain.sh rollouts <deploy> [N]  N rollout restarts, GAP seconds apart
# Prints every probe failure, how long each old pod took to exit, and the
# gateway's UNAVAILABLE count over the run. Raw logs go to $OUT.
set -euo pipefail
HERE="$(cd "$(dirname "$0")" && pwd)"
NS=ticketbottle
MODE=${1:?control|rollouts}
TARGET=${2:?deployment name, e.g. app-gateway}
N=${3:-5}
GAP=${GAP:-60}
OUT=${OUT:-$(mktemp -d)}
EVENT_ID=${EVENT_ID:-$(kubectl -n $NS exec statefulset/postgres -- psql -U root -d ticketbottle_event \
  -tAc "select id from events where status='PUBLISHED' limit 1" | tr -d '[:space:]')}
[ -n "$EVENT_ID" ] || { echo "no PUBLISHED event to probe"; exit 1; }
# The access guard looks up the token's sub in user-svc on every request.
kubectl -n $NS exec statefulset/postgres -- psql -U root -d ticketbottle_user -c \
  "INSERT INTO users (id, email, \"firstName\", \"lastName\", password, \"updatedAt\")
   VALUES ('rollout-probe', 'rollout-probe@example.com', 'Rollout', 'Probe', 'seeded-no-signin', now())
   ON CONFLICT (id) DO NOTHING;" >/dev/null

case $MODE in
  control)  DURATION=90s ;;
  rollouts) DURATION=$(( N * (GAP + 45) + 30 ))s ;;   # 45: a stop that runs to SIGKILL
  *) echo "mode must be control or rollouts"; exit 1 ;;
esac

# Millisecond UTC, portable: BSD date has no %N.
now() { perl -MTime::HiRes=time -MPOSIX=strftime -e '$t=time; printf "%s.%03dZ", strftime("%FT%T", gmtime $t), ($t-int $t)*1000'; }
log() { echo "$(now) $*" | tee -a "$OUT/timeline.log"; }

# Read from the pod, not Prometheus: a scrape interval would blur a one-second burst.
unavailable() {
  local pod; pod=$(kubectl -n $NS get pod -l app=app-gateway -o jsonpath='{.items[0].metadata.name}')
  kubectl get --raw "/api/v1/namespaces/$NS/pods/$pod:2112/proxy/metrics" \
    | awk '/^tb_grpc_requests_total\{/ && /code="UNAVAILABLE"/ { s += $NF } END { print s + 0 }'
}

# Invariant: the old pod is gone before this returns, so its exit time is measured.
stop_pod() {
  local old t0; old=$(kubectl -n $NS get pod -l app="$TARGET" -o jsonpath='{.items[0].metadata.name}')
  t0=$(date +%s)
  if [ "$MODE" = control ]; then
    kubectl -n $NS delete pod "$old" --wait=false >/dev/null
    # Why: right after a delete, rollout status can still read the old pod's Ready.
    until kubectl -n $NS get pod -l app="$TARGET" -o jsonpath='{range .items[*]}{.metadata.name} {.status.conditions[?(@.type=="Ready")].status}{"\n"}{end}' \
      | grep -v "^$old " | grep -q ' True$'; do sleep 0.5; done
  else
    kubectl -n $NS rollout restart deploy/"$TARGET" >/dev/null
    kubectl -n $NS rollout status deploy/"$TARGET" --timeout=3m >/dev/null
  fi
  log "$TARGET ready ($old replaced)"
  while kubectl -n $NS get pod "$old" >/dev/null 2>&1; do sleep 0.5; done
  log "$old gone, $(( $(date +%s) - t0 ))s after the stop began"
}

kubectl -n $NS delete job/k6-rollout-probe --ignore-not-found --wait >/dev/null
PROBE_JS=$(sed 's/^/    /' "$HERE/../loadtest/rollout-probe.js") \
EVENT_ID=$EVENT_ID DURATION=$DURATION \
  envsubst < "$HERE/../loadtest/rollout-probe-job.yaml" | kubectl apply -f - >/dev/null
kubectl -n $NS wait --for=condition=ready pod -l app=k6-rollout-probe --timeout=2m >/dev/null
sleep 15
# A probe that fails with nothing stopped would pass the control while blind.
if kubectl -n $NS logs job/k6-rollout-probe | grep -q '^{"t"'; then
  kubectl -n $NS logs job/k6-rollout-probe | grep '^{"t"' | head -3
  echo "INVALID: the probe fails before any pod is stopped"; exit 1
fi
[ "$TARGET" = app-gateway ] || U0=$(unavailable)
log "probe running: $MODE $TARGET, event $EVENT_ID, $DURATION"

if [ "$MODE" = control ]; then
  log "delete pod"; stop_pod
else
  for i in $(seq 1 "$N"); do
    log "rollout $i/$N"; stop_pod
    [ "$i" = "$N" ] || sleep "$GAP"
  done
fi

kubectl -n $NS wait --for=condition=complete job/k6-rollout-probe --timeout=15m >/dev/null
kubectl -n $NS logs job/k6-rollout-probe > "$OUT/probe.log"
grep '^{"t"' "$OUT/probe.log" > "$OUT/failures.jsonl" || true

echo "== $MODE $TARGET =="
grep -E 'http_reqs|tb_probe_failures' "$OUT/probe.log" || true
echo "failures: $(wc -l < "$OUT/failures.jsonl" | tr -d ' ')  by status:"
sed -E 's/.*"status":([0-9]+).*/\1/' "$OUT/failures.jsonl" | sort | uniq -c
[ "$TARGET" = app-gateway ] || echo "gateway UNAVAILABLE over the run: $(( $(unavailable) - U0 ))"
echo "timeline + raw logs: $OUT"
