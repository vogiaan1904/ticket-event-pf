#!/usr/bin/env bash
# Gate 4b — node loss under load.
#
# Terminates a node while purchases are in flight and asserts nothing is lost.
#
# It REFUSES to kill a node hosting ANY PVC-backed pod. Every EBS-backed volume
# carries node affinity to one availability zone, so a stranded stateful pod sits
# Pending until a replacement node appears in that same zone. That failure is
# real and worth seeing — but it is Lab 05's drain exercise, not this gate, and
# stranding redpanda in particular fails §6 for reasons unrelated to the saga.
#
# It also REFUSES to terminate anything until the sold counter proves buyers are
# genuinely mid-purchase: a chaos test that does not overlap the failure with
# real work proves nothing, however green it looks.
set -euo pipefail
HERE="$(cd "$(dirname "$0")" && pwd)"
NS=ticketbottle
REGION=us-east-1
GW=${GW:?set GW to the ALB url}
# The load must OUTLAST the kill, so the window is set here rather than left to
# the burst's natural length. TOTAL is sized so inventory cannot sell out first.
DURATION=${DURATION:-4m}
VUS=${VUS:-20}
TOTAL=${TOTAL:-2000}
TCID_FILE=${TCID_FILE:-/tmp/gate4a-ticketclass}

psql() { kubectl -n $NS exec statefulset/postgres -- psql -U root -d "$1" -tAc "$2" | tr -d '[:space:]'; }
fail() { echo "GATE 4b FAILED: $1"; exit 1; }

echo "== 0. choose a victim node =="
PG_NODE=$(kubectl -n $NS get pod -l app=postgres -o jsonpath='{.items[0].spec.nodeName}')
ORDER_NODE=$(kubectl -n $NS get pod -l app=order-service -o jsonpath='{.items[0].spec.nodeName}')
echo "  postgres is on : $PG_NODE"
echo "  order-service on: $ORDER_NODE"

# Refuse if the victim hosts ANY PVC-backed pod, not just postgres. Every
# EBS-backed PersistentVolume carries node affinity to one availability zone, so
# whatever stateful pod lives here cannot follow the workload to the survivor —
# it sits Pending until a replacement node happens to appear in the same zone.
# postgres is only the most obvious case: stranding redpanda breaks the outbox
# leg and fails §6 for a reason that has nothing to do with the saga.
#
# On a two-node cluster EVERY node holds one of these, so this correctly refuses
# both and tells you to add a third. That is the honest answer, and much clearer
# than discovering it through a mystifying assertion failure ten minutes later.
VICTIM_PVC_PODS=$(kubectl -n $NS get pods --field-selector "spec.nodeName=$ORDER_NODE" \
  -o jsonpath='{range .items[*]}{.metadata.name}{"\t"}{.spec.volumes[*].persistentVolumeClaim.claimName}{"\n"}{end}' \
  | awk -F'\t' '$2!=""{printf "%s ", $1}')

[ -z "$VICTIM_PVC_PODS" ] || fail "order-service shares node $ORDER_NODE with AZ-pinned volumes: $VICTIM_PVC_PODS
  Terminating it would strand those EBS volumes in this node's zone, and they
  cannot be attached from the other one. Add a third node so order-service can
  land somewhere carrying no PVC:
    aws eks update-nodegroup-config --cluster-name ticketbottle-eks --nodegroup-name spot \\
      --region $REGION --scaling-config minSize=2,maxSize=3,desiredSize=3
    aws eks wait nodegroup-active --cluster-name ticketbottle-eks --nodegroup-name spot --region $REGION
    kubectl -n $NS delete pod -l app=order-service
  The new node is empty, so it scores as least-allocated and wins the pod."

VICTIM_ID=$(kubectl get node "$ORDER_NODE" -o jsonpath='{.spec.providerID}' | awk -F/ '{print $NF}')
echo "  victim: $ORDER_NODE ($VICTIM_ID)"

echo "== 1. start a SUSTAINED purchase load in the background =="
# DURATION switches purchase.js to constant-vus. Without it the default
# per-vu-iterations scenario has every VU buy once and exit, which drains in
# ~10s — so a fixed sleep almost always terminates an already-idle node and the
# gate passes having proved nothing. Low rate is still right: this is about
# recovery, not throughput.
rm -f "$TCID_FILE"
VUS=$VUS TOTAL=$TOTAL DURATION=$DURATION TCID_FILE=$TCID_FILE \
  "$HERE/gate4a-load.sh" > /tmp/gate4b-load.log 2>&1 &
LOAD_PID=$!

echo "== 2. wait until orders are genuinely in flight =="
# Kill on a signal, not a stopwatch. The seed phase (signup, event, ticket class,
# buyers, Job apply) takes a variable slice of any fixed sleep, so the only
# honest trigger is the sold counter actually moving. A chaos test that does not
# overlap the failure with real work is theatre.
SOLD_AT_KILL=0
for i in $(seq 1 120); do
  if [ -s "$TCID_FILE" ]; then
    TCID=$(cat "$TCID_FILE")
    SOLD_AT_KILL=$(psql ticketbottle_inventory "SELECT sold FROM ticket_class WHERE id='$TCID';" 2>/dev/null || echo 0)
    [ -n "$SOLD_AT_KILL" ] || SOLD_AT_KILL=0
    echo "   [$i] sold=$SOLD_AT_KILL"
    [ "$SOLD_AT_KILL" -ge 5 ] && break
  else
    echo "   [$i] waiting for the load to seed its ticket class..."
  fi
  sleep 2
done
[ "$SOLD_AT_KILL" -ge 5 ] || fail "load never got going (sold=$SOLD_AT_KILL) — nothing was in flight to disrupt.
  Check /tmp/gate4b-load.log; the node was NOT terminated."

echo "== 3. terminate $VICTIM_ID =="
# terminate-instances, not AWS FIS: FIS bills per action and this is free.
aws ec2 terminate-instances --region "$REGION" --instance-ids "$VICTIM_ID" >/dev/null
KILL_TS=$(date +%s)
# UTC, because that is what `tctl workflow list` prints. Cross-reference this
# against the START/END TIME columns: the proof of saga resumption is a workflow
# that STARTED before this instant and ENDED after it.
echo "  terminated at $(date -u +%H:%M:%S) UTC, with sold=$SOLD_AT_KILL in flight"

echo "== 4. wait for order-service to be Ready again =="
for i in $(seq 1 60); do
  READY=$(kubectl -n $NS get deploy order-service -o jsonpath='{.status.readyReplicas}' 2>/dev/null || echo 0)
  echo "   [$i] order-service ready=${READY:-0}"
  [ "${READY:-0}" -ge 1 ] && break
  sleep 10
done
[ "${READY:-0}" -ge 1 ] || fail "order-service never became Ready after node loss"
echo "  recovered in $(( $(date +%s) - KILL_TS ))s"

wait $LOAD_PID || true   # the load script's own gate may fail; assertions below are the verdict
tail -20 /tmp/gate4b-load.log

echo "== 5. assert no order is stranded mid-saga =="
STUCK=$(psql ticketbottle_inventory "SELECT count(*) FROM reservation WHERE status='ACTIVE' AND expires_at < now();")
[ "$STUCK" -eq 0 ] || fail "$STUCK reservations expired while still ACTIVE — the saga did not compensate"

echo "== 6. assert every outbox row was published =="
# NOTE the double quotes around the column names. payment-svc is Prisma, and
# Prisma maps the TABLE (@@map("outbox")) but not the COLUMNS — they are created
# as "publishedAt" / "createdAt", so unquoted snake_case does not exist and
# unquoted camelCase gets folded to lowercase by Postgres. Both fail.
UNPUB=$(psql ticketbottle_payment "SELECT count(*) FROM outbox WHERE \"publishedAt\" IS NULL AND \"createdAt\" < now() - interval '2 minutes';")
[ "$UNPUB" -eq 0 ] || fail "$UNPUB outbox rows older than 2 minutes are still unpublished"

echo "== 7. assert the system kept selling ACROSS the node loss =="
# The point of the whole exercise. If sold never moved after the kill, the load
# had already drained and the gate measured an idle cluster — exactly the
# failure mode the §2 poll exists to prevent, caught here from the other side.
SOLD_FINAL=$(psql ticketbottle_inventory "SELECT sold FROM ticket_class WHERE id='$(cat "$TCID_FILE")';")
echo "  sold at kill=$SOLD_AT_KILL, final=$SOLD_FINAL"
[ "$SOLD_FINAL" -gt "$SOLD_AT_KILL" ] || fail "no order completed after the node died (sold stayed at $SOLD_AT_KILL).
  The load drained before the kill landed; the recovery was never exercised."

echo "== 8. record what did NOT come back =="
echo "  pods not Running:"
kubectl -n $NS get pods --field-selector=status.phase!=Running --no-headers || echo "    (none)"

echo
echo "GATE 4b PASSED: node $VICTIM_ID terminated with $SOLD_AT_KILL sales in flight,
  order-service recovered, sold advanced $SOLD_AT_KILL -> $SOLD_FINAL across the
  failure, no stranded reservations and no unpublished outbox rows."
