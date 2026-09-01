#!/usr/bin/env bash
# Gate 4a — correctness under concurrency.
#
# Seeds ONE ticket class with a fixed allotment, drives more concurrent buyers
# than there are tickets, then asserts:
#   1. no oversell            (sold + reserved <= TOTAL, always)
#   2. admission control held  (concurrent checkouts <= MAX_CONCURRENT)
#   3. every rejected buyer got a 409, never a 5xx or a stray 4xx
#   4. nothing leaked          (reserved == 0 and no ACTIVE reservation once holds expire)
#   5. the HPA scaled app-gateway out AND back in
#
# Deliberately not asserted: sold == TOTAL. Each VU buys once and never retries,
# so when demand outruns throughput some allotment goes unsold. That is the
# harness, not the system — selling out needs a retrying buyer.
set -euo pipefail
HERE="$(cd "$(dirname "$0")" && pwd)"
NS=ticketbottle
TOTAL=${TOTAL:-200}
VUS=${VUS:-300}
GW=${GW:?set GW to the ALB url, e.g. http://<alb>/api}
USER_PREFIX=gate4a-u
# Set by Gate 4b only. Switches purchase.js to a sustained constant-vus run and
# turns this script into a pure load generator — see the early exit after §3.
DURATION=${DURATION:-}
# Gate 4b needs the seeded ticket class id to tell when buyers are actually
# mid-purchase. A file is the least invasive channel for it; this script's
# stdout is the human-readable run log.
TCID_FILE=${TCID_FILE:-/tmp/gate4a-ticketclass}
MAX_CONCURRENT=${MAX_CONCURRENT:-$(kubectl -n $NS get cm waitroom-config -o jsonpath='{.data.QUEUE_DEFAULT_MAX_CONCURRENT}')}

psql() { kubectl -n $NS exec statefulset/postgres -- psql -U root -d ticketbottle_inventory -tAc "$1" | tr -d '[:space:]'; }
fail() { echo "GATE 4a FAILED: $1"; exit 1; }

echo "== 0. record the HPA's starting replica count =="
kubectl -n $NS get hpa app-gateway >/dev/null 2>&1 || fail "no app-gateway HPA — is values-eks.yaml applied?"
START_REPLICAS=$(kubectl -n $NS get deploy app-gateway -o jsonpath='{.spec.replicas}')
echo "  app-gateway starts at $START_REPLICAS replica(s)"

echo "== 1. seed an event and a fixed allotment of $TOTAL =="
# Reuses the exact setup path Gate 1 uses; see gate1-purchase-flow.sh for why
# the category and the PUBLISHED status are seeded directly.
EMAIL="gate4a+$(date +%s)@example.com"
TOK=$(curl -s -X POST "$GW/auth/signup" -H 'Content-Type: application/json' \
  -d "{\"firstName\":\"Gate\",\"lastName\":\"Four\",\"email\":\"$EMAIL\",\"password\":\"Password123!\"}" \
  | python3 -c 'import sys,json; print(json.load(sys.stdin)["data"]["accessToken"])')
[ -n "$TOK" ] || fail "signup returned no accessToken"
AUTH="Authorization: Bearer $TOK"

kubectl -n $NS exec statefulset/postgres -- psql -U root -d ticketbottle_event -c \
  "INSERT INTO categories (id, name, \"createdAt\", \"updatedAt\") VALUES ('gate4-category','Gate4',now(),now()) ON CONFLICT (id) DO NOTHING;" >/dev/null

EVENT_ID=$(curl -s -X POST "$GW/events" -H "$AUTH" -H 'Content-Type: application/json' -d "{
  \"name\":\"Gate4 Rush\",\"description\":\"load\",
  \"startDate\":\"2027-01-01T00:00:00Z\",\"endDate\":\"2027-01-02T00:00:00Z\",
  \"thumbnailUrl\":\"https://example.com/t.png\",\"venue\":\"Test Arena\",
  \"street\":\"1 St\",\"city\":\"HCMC\",\"country\":\"VN\",\"categoryIds\":[\"gate4-category\"],
  \"organizerName\":\"Gate Org\",\"organizerDescription\":\"load\",
  \"organizerLogoUrl\":\"https://example.com/logo.png\"}" \
  | python3 -c 'import sys,json; print(json.load(sys.stdin)["data"]["id"])')
[ -n "$EVENT_ID" ] || fail "event create failed"

curl -s -X POST "$GW/events/$EVENT_ID/config" -H "$AUTH" -H 'Content-Type: application/json' -d '{
  "ticketSaleStartDate":"2020-01-01T00:00:00Z","ticketSaleEndDate":"2030-01-01T00:00:00Z",
  "isFree":false,"maxAttendees":100000,"isPublic":true,"requiresApproval":false,
  "allowWaitRoom":true,"isNewTrending":false}' >/dev/null

kubectl -n $NS exec statefulset/postgres -- psql -U root -d ticketbottle_event -c \
  "UPDATE events SET status='PUBLISHED' WHERE id='$EVENT_ID';" >/dev/null

TCID=$("$HERE/seed-ticketclass.sh" "$EVENT_ID" "$TOTAL" 10000)
[ -n "$TCID" ] || fail "seed-ticketclass returned no id"
printf '%s' "$TCID" > "$TCID_FILE"
echo "  eventId=$EVENT_ID ticketClassId=$TCID total=$TOTAL"

# Seed one user per VU. Signing up in-band costs a bcrypt hash per buyer and
# saturates the gateway long before inventory is contended, so the run would
# measure password hashing rather than the purchase path. The password is a
# placeholder; purchase.js mints its access token directly.
echo "== 2. seed $VUS buyers =="
kubectl -n $NS exec statefulset/postgres -- psql -U root -d ticketbottle_user -c \
  "INSERT INTO users (id, email, \"firstName\", \"lastName\", password, \"updatedAt\")
   SELECT '$USER_PREFIX' || n, '$USER_PREFIX' || n || '@example.com', 'Load', 'VU' || n, 'seeded-no-signin', now()
   FROM generate_series(1, $VUS) AS n ON CONFLICT (id) DO NOTHING;" >/dev/null

echo "== 3. run $VUS concurrent buyers, in-cluster =="
kubectl -n $NS delete job k6-load --ignore-not-found >/dev/null
PURCHASE_JS=$(sed 's/^/    /' "$HERE/../loadtest/purchase.js") \
EVENT_ID="$EVENT_ID" TICKET_CLASS_ID="$TCID" VUS="$VUS" USER_PREFIX="$USER_PREFIX" \
DURATION="$DURATION" \
  envsubst < "$HERE/../loadtest/job.yaml" | kubectl apply -f -

# Watch both terminal conditions: --for=condition=complete alone never fires on
# a failed Job, so a crossed k6 threshold would stall here for the full timeout.
kubectl -n $NS wait --for=condition=complete job/k6-load --timeout=15m >/dev/null 2>&1 &
DONE_PID=$!
kubectl -n $NS wait --for=condition=failed job/k6-load --timeout=15m >/dev/null 2>&1 &
FAIL_PID=$!

echo "   watching app-gateway replicas and admitted checkouts while the load runs..."
PEAK=$START_REPLICAS
PEAK_ADMITTED=0
while kill -0 $DONE_PID 2>/dev/null && kill -0 $FAIL_PID 2>/dev/null; do
  R=$(kubectl -n $NS get deploy app-gateway -o jsonpath='{.status.replicas}' 2>/dev/null || echo 0)
  if [ "${R:-0}" -gt "$PEAK" ]; then PEAK=$R; echo "   peak replicas: $PEAK"; fi

  # The checkouts sorted set is the admission bound itself, so sampling it
  # measures what the queue actually allowed rather than what the logs report.
  A=$(kubectl -n $NS exec statefulset/redis -- redis-cli ZCARD "waitroom:$EVENT_ID:checkouts" 2>/dev/null | tr -d '[:space:]')
  if [ "${A:-0}" -gt "$PEAK_ADMITTED" ]; then PEAK_ADMITTED=$A; echo "   peak admitted: $PEAK_ADMITTED"; fi
  sleep 5
done
kill $DONE_PID $FAIL_PID 2>/dev/null || true
wait $DONE_PID $FAIL_PID 2>/dev/null || true

if [ "$(kubectl -n $NS get job k6-load -o jsonpath='{.status.conditions[?(@.type=="Failed")].status}')" = "True" ]; then
  kubectl -n $NS logs job/k6-load | tail -40
  fail "k6 job failed — see the summary above"
fi

kubectl -n $NS logs job/k6-load | tail -40

# With DURATION set this script is a load generator, not a gate, and the
# remaining assertions get in the way: §7 wants an HPA scale-out that 20 VUs
# will not produce, and §6 and §8 each poll for up to ten minutes. Stop here and
# let Gate 4b's own assertions decide.
if [ -n "$DURATION" ]; then
  echo
  echo "load generator finished (${DURATION}, ${VUS} VUs) — Gate 4b asserts the outcome."
  exit 0
fi

echo "== 4. assert no oversell =="
SOLD=$(psql "SELECT sold FROM ticket_class WHERE id='$TCID';")
RESERVED=$(psql "SELECT reserved FROM ticket_class WHERE id='$TCID';")
echo "  sold=$SOLD reserved=$RESERVED (total=$TOTAL)"
[ $((SOLD + RESERVED)) -le "$TOTAL" ] || fail "OVERSELL: sold=$SOLD + reserved=$RESERVED > total=$TOTAL"

echo "== 5. assert admission control held =="
echo "  peak concurrent checkouts=$PEAK_ADMITTED (limit=$MAX_CONCURRENT)"
[ "$PEAK_ADMITTED" -le "$MAX_CONCURRENT" ] || fail "waitroom admitted $PEAK_ADMITTED concurrent checkouts, limit is $MAX_CONCURRENT"

echo "== 6. assert nothing leaked once the holds expire =="
# Reservations outlive the run by design and the sweeper releases them, so
# polling sooner than the hold measures the hold rather than a leak.
for i in $(seq 1 40); do
  RESERVED=$(psql "SELECT reserved FROM ticket_class WHERE id='$TCID';")
  ACTIVE=$(psql "SELECT count(*) FROM reservation r JOIN ticket_class t ON r.ticket_class_id=t.id WHERE t.id='$TCID' AND r.status='ACTIVE';")
  echo "   [$i] reserved=$RESERVED active=$ACTIVE"
  [ "$RESERVED" -eq 0 ] && [ "$ACTIVE" -eq 0 ] && break
  sleep 15
done
[ "$RESERVED" -eq 0 ] || fail "reserved did not settle to 0 (got $RESERVED) — inventory leaked"
[ "$ACTIVE" -eq 0 ]   || fail "$ACTIVE reservations left ACTIVE — inventory leaked"

echo "== 7. assert the HPA scaled out =="
[ "$PEAK" -gt "$START_REPLICAS" ] || fail "app-gateway never scaled above $START_REPLICAS (peak=$PEAK)"
echo "  peaked at $PEAK replicas"

echo "== 8. assert it scales back in =="
# The HPA's scaleDown stabilizationWindowSeconds is 300 (values.yaml), so the
# replica count means nothing until that window has passed.
echo "   waiting out the 300s scale-down stabilization window..."
for i in $(seq 1 40); do
  R=$(kubectl -n $NS get deploy app-gateway -o jsonpath='{.spec.replicas}')
  echo "   [$i] replicas=$R"
  [ "$R" -le "$START_REPLICAS" ] && break
  sleep 15
done
[ "$R" -le "$START_REPLICAS" ] || fail "app-gateway stuck at $R replicas after the window"

echo
echo "GATE 4a PASSED: $SOLD/$TOTAL sold, zero oversell, peak admitted $PEAK_ADMITTED/$MAX_CONCURRENT, HPA $START_REPLICAS -> $PEAK -> $R"
