#!/usr/bin/env bash
# Gate 4a — correctness under concurrency.
#
# Seeds ONE ticket class with a fixed allotment, drives more concurrent buyers
# than there are tickets, then asserts:
#   1. no oversell and no undersell  (sold == TOTAL, reserved == 0)
#   2. every rejected buyer got a 4xx, never a 5xx
#   3. no reservation left ACTIVE
#   4. the HPA scaled app-gateway out AND back in
set -euo pipefail
HERE="$(cd "$(dirname "$0")" && pwd)"
NS=ticketbottle
TOTAL=${TOTAL:-200}
VUS=${VUS:-300}
GW=${GW:?set GW to the ALB url, e.g. http://<alb>/api}
USER_PREFIX=gate4a-u

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
echo "  eventId=$EVENT_ID ticketClassId=$TCID total=$TOTAL"

# Seed one user per VU. Signing up in-band costs a bcrypt hash per buyer and
# saturates the gateway long before inventory is contended — the run would
# measure password hashing, not the purchase path. The password is a placeholder;
# purchase.js mints its access token directly.
echo "== 2. seed $VUS buyers =="
kubectl -n $NS exec statefulset/postgres -- psql -U root -d ticketbottle_user -c \
  "INSERT INTO users (id, email, \"firstName\", \"lastName\", password, \"updatedAt\")
   SELECT '$USER_PREFIX' || n, '$USER_PREFIX' || n || '@example.com', 'Load', 'VU' || n, 'seeded-no-signin', now()
   FROM generate_series(1, $VUS) AS n ON CONFLICT (id) DO NOTHING;" >/dev/null

echo "== 3. run $VUS concurrent buyers, in-cluster =="
kubectl -n $NS delete job k6-load --ignore-not-found >/dev/null
PURCHASE_JS=$(sed 's/^/    /' "$HERE/../loadtest/purchase.js") \
EVENT_ID="$EVENT_ID" TICKET_CLASS_ID="$TCID" VUS="$VUS" USER_PREFIX="$USER_PREFIX" \
  envsubst < "$HERE/../loadtest/job.yaml" | kubectl apply -f -

# Watch both terminal conditions: --for=condition=complete alone never fires on
# a failed Job, so a crossed k6 threshold would stall here for the full timeout.
kubectl -n $NS wait --for=condition=complete job/k6-load --timeout=10m >/dev/null 2>&1 &
DONE_PID=$!
kubectl -n $NS wait --for=condition=failed job/k6-load --timeout=10m >/dev/null 2>&1 &
FAIL_PID=$!

echo "   watching app-gateway replicas while the load runs..."
PEAK=$START_REPLICAS
while kill -0 $DONE_PID 2>/dev/null && kill -0 $FAIL_PID 2>/dev/null; do
  R=$(kubectl -n $NS get deploy app-gateway -o jsonpath='{.status.replicas}' 2>/dev/null || echo 0)
  if [ "${R:-0}" -gt "$PEAK" ]; then PEAK=$R; echo "   peak replicas: $PEAK"; fi
  sleep 5
done
kill $DONE_PID $FAIL_PID 2>/dev/null || true
wait $DONE_PID $FAIL_PID 2>/dev/null || true

if [ "$(kubectl -n $NS get job k6-load -o jsonpath='{.status.conditions[?(@.type=="Failed")].status}')" = "True" ]; then
  kubectl -n $NS logs job/k6-load | tail -40
  fail "k6 job failed — see the summary above"
fi

kubectl -n $NS logs job/k6-load | tail -40

echo "== 4. assert the ledger =="
SOLD=$(psql "SELECT sold FROM ticket_class WHERE id='$TCID';")
RESERVED=$(psql "SELECT reserved FROM ticket_class WHERE id='$TCID';")
ACTIVE=$(psql "SELECT count(*) FROM reservation r JOIN ticket_class t ON r.ticket_class_id=t.id WHERE t.id='$TCID' AND r.status='ACTIVE';")
echo "  sold=$SOLD reserved=$RESERVED activeReservations=$ACTIVE (total=$TOTAL)"

[ "$SOLD" -le "$TOTAL" ]  || fail "OVERSELL: sold=$SOLD > total=$TOTAL"
[ "$SOLD" -eq "$TOTAL" ]  || fail "UNDERSELL: sold=$SOLD < total=$TOTAL with demand $VUS"
[ "$RESERVED" -eq 0 ]     || fail "reserved did not settle to 0 (got $RESERVED)"
[ "$ACTIVE" -eq 0 ]       || fail "$ACTIVE reservations left ACTIVE"

echo "== 5. assert the HPA scaled out =="
[ "$PEAK" -gt "$START_REPLICAS" ] || fail "app-gateway never scaled above $START_REPLICAS (peak=$PEAK)"
echo "  peaked at $PEAK replicas"

echo "== 6. assert it scales back in =="
# The HPA's scaleDown stabilizationWindowSeconds is 300 (values.yaml). Polling
# sooner than that will always "fail" — the wait IS the assertion.
echo "   waiting out the 300s scale-down stabilization window..."
for i in $(seq 1 40); do
  R=$(kubectl -n $NS get deploy app-gateway -o jsonpath='{.spec.replicas}')
  echo "   [$i] replicas=$R"
  [ "$R" -le "$START_REPLICAS" ] && break
  sleep 15
done
[ "$R" -le "$START_REPLICAS" ] || fail "app-gateway stuck at $R replicas after the window"

echo
echo "GATE 4a PASSED: $SOLD/$TOTAL sold, zero oversell, HPA $START_REPLICAS -> $PEAK -> $R"