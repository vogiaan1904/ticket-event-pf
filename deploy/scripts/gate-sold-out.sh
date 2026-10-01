#!/usr/bin/env bash
# The door when tickets run out, run on k3s by `make -C deploy k3s-gate-sold-out`.
# One ticket, three buyers: A holds it -> B waits, paused, unadmitted -> A pays ->
# B's poll and C's join get 409 WTR012 -> the tick has ended B's session.
# Rule: docs/design/admission-sizing.md#when-tickets-run-out.
set -euo pipefail
GW=${GW:-http://localhost:3000/api}
NS=ticketbottle
HERE="$(cd "$(dirname "$0")" && pwd)"
RUN=$(date +%s)

# JSON path extractor: `echo "$json" | getval a.b.c` -> value, or empty on any error.
getval() { python3 -c "import sys,json
d=json.load(sys.stdin)
for k in sys.argv[1].split('.'):
    d=d[k]
print(d)" "$1" 2>/dev/null || true; }

fail() { echo "SOLD-OUT GATE FAILED: $1"; exit 1; }

# signup <name> -> an access token
signup() {
  curl -s -X POST "$GW/auth/signup" -H 'Content-Type: application/json' \
    -d "{\"firstName\":\"$1\",\"lastName\":\"Buyer\",\"email\":\"soldout+$1-$RUN@example.com\",\"password\":\"Password123!\"}" \
    | getval data.accessToken
}

# join <token> <out-file> -> the HTTP code; the body lands in <out-file>
join() {
  curl -s -o "$2" -w '%{http_code}' -X POST "$GW/waitroom/join" -H "Authorization: Bearer $1" \
    -H 'Content-Type: application/json' -d "{\"eventId\":\"$EVENT_ID\"}"
}

# poll <token> <session> <out-file> -> the HTTP code; the body lands in <out-file>
poll() {
  curl -s -o "$3" -w '%{http_code}' "$GW/waitroom/status/$2" -H "Authorization: Bearer $1"
}

echo "== 1. three buyers =="
TOK_A=$(signup A); TOK_B=$(signup B); TOK_C=$(signup C)
[ -n "$TOK_A" ] && [ -n "$TOK_B" ] && [ -n "$TOK_C" ] || fail "a signup returned no accessToken"

echo "== 2. an event with one ticket =="
CATEGORY_ID=gate1-category
kubectl -n $NS exec statefulset/postgres -- psql -U root -d ticketbottle_event -qc \
  "INSERT INTO categories (id, name, \"createdAt\", \"updatedAt\") VALUES ('$CATEGORY_ID','Gate1',now(),now()) ON CONFLICT (id) DO NOTHING;"
EVT=$(curl -s -X POST "$GW/events" -H "Authorization: Bearer $TOK_A" -H 'Content-Type: application/json' -d "{
  \"name\":\"Sold-out Show\",\"description\":\"e2e\",
  \"startDate\":\"2027-01-01T00:00:00Z\",\"endDate\":\"2027-01-02T00:00:00Z\",
  \"thumbnailUrl\":\"https://example.com/t.png\",\"venue\":\"Test Arena\",
  \"street\":\"1 St\",\"city\":\"HCMC\",\"country\":\"VN\",\"categoryIds\":[\"$CATEGORY_ID\"],
  \"organizerName\":\"Gate Org\",\"organizerDescription\":\"e2e organizer\",
  \"organizerLogoUrl\":\"https://example.com/logo.png\"
}")
EVENT_ID=$(echo "$EVT" | getval data.id)
[ -n "$EVENT_ID" ] || fail "event create returned no id: $EVT"
CFG=$(curl -s -X POST "$GW/events/$EVENT_ID/config" -H "Authorization: Bearer $TOK_A" -H 'Content-Type: application/json' -d '{
  "ticketSaleStartDate":"2020-01-01T00:00:00Z","ticketSaleEndDate":"2030-01-01T00:00:00Z",
  "isFree":false,"maxAttendees":100,"isPublic":true,"requiresApproval":false,
  "allowWaitRoom":true,"isNewTrending":false
}')
[ -n "$(echo "$CFG" | getval data.id)" ] || fail "config create failed: $CFG"
kubectl -n $NS exec statefulset/postgres -- psql -U root -d ticketbottle_event -qc \
  "UPDATE events SET status='PUBLISHED' WHERE id='$EVENT_ID';"
TCID=$("$HERE/seed-ticketclass.sh" "$EVENT_ID" 1 10000)
[ -n "$TCID" ] || fail "seed-ticketclass returned no id"
echo "  eventId=$EVENT_ID ticketClassId=$TCID (total 1)"

echo "== 3. A is admitted and holds the only ticket =="
join "$TOK_A" /tmp/so-a.json >/dev/null
SESSION_A=$(getval data.sessionId < /tmp/so-a.json)
[ -n "$SESSION_A" ] || fail "A's join returned no sessionId: $(cat /tmp/so-a.json)"
CHECKOUT=""
for i in $(seq 1 30); do
  poll "$TOK_A" "$SESSION_A" /tmp/so-a-st.json >/dev/null
  CHECKOUT=$(getval data.checkoutToken < /tmp/so-a-st.json)
  [ -n "$CHECKOUT" ] && break
  sleep 1
done
[ -n "$CHECKOUT" ] || fail "A not admitted within 30s: $(cat /tmp/so-a-st.json)"
ORD=$(curl -s -X POST "$GW/orders" -H "Authorization: Bearer $TOK_A" -H 'Content-Type: application/json' -d "{
  \"eventId\":\"$EVENT_ID\",\"userFullname\":\"A Buyer\",\"userEmail\":\"soldout+A-$RUN@example.com\",
  \"userPhone\":\"0900000000\",\"paymentMethod\":\"ZALOPAY\",
  \"items\":[{\"ticketClassId\":\"$TCID\",\"quantity\":1}],\"currency\":\"VND\",
  \"checkoutToken\":\"$CHECKOUT\",\"redirectUrl\":\"https://example.com/done\"}")
ORDER_CODE=$(echo "$ORD" | getval data.order.code)
[ -n "$ORDER_CODE" ] || fail "A's order returned no order.code: $ORD"
echo "  A holds it: orderCode=$ORDER_CODE"

echo "== 4. B joins; the door is paused and B keeps its place =="
join "$TOK_B" /tmp/so-b.json >/dev/null
SESSION_B=$(getval data.sessionId < /tmp/so-b.json)
[ -n "$SESSION_B" ] || fail "B's join returned no sessionId: $(cat /tmp/so-b.json)"
PAUSED=""
for i in $(seq 1 10); do
  poll "$TOK_B" "$SESSION_B" /tmp/so-b-st.json >/dev/null
  PAUSED=$(getval data.paused < /tmp/so-b-st.json)
  [ "$PAUSED" = True ] && break
  sleep 1
done
[ "$PAUSED" = True ] || fail "B's status never said paused: $(cat /tmp/so-b-st.json)"
for i in 1 2 3; do
  sleep 1
  poll "$TOK_B" "$SESSION_B" /tmp/so-b-st.json >/dev/null
  [ -z "$(getval data.checkoutToken < /tmp/so-b-st.json)" ] || fail "B was admitted through a paused door: $(cat /tmp/so-b-st.json)"
  [ "$(getval data.paused < /tmp/so-b-st.json)" = True ] || fail "B's door reopened with the ticket still held: $(cat /tmp/so-b-st.json)"
done
echo "  paused for 3s, unadmitted, position=$(getval data.position < /tmp/so-b-st.json)"

echo "== 5. A pays; the event is sold out =="
kubectl -n $NS run so-curl --rm -i --restart=Never --image=curlimages/curl:8.10.1 -- \
  -s -X POST "http://payment-webhook:8080/complete/$ORDER_CODE" >/dev/null 2>&1 || true
STATUS=""
for i in $(seq 1 20); do
  STATUS=$(curl -s -H "Authorization: Bearer $TOK_A" "$GW/orders/code/$ORDER_CODE" | getval data.status)
  [ "$STATUS" = COMPLETED ] && break
  sleep 2
done
[ "$STATUS" = COMPLETED ] || fail "A's order did not reach COMPLETED: ${STATUS:-?}"
echo "  A's order COMPLETED"

echo "== 6. B's poll and C's join are refused: 409 WTR012 =="
CODE=""
for i in $(seq 1 10); do
  CODE=$(poll "$TOK_B" "$SESSION_B" /tmp/so-b-st.json)
  [ "$CODE" = 409 ] && break
  sleep 1
done
[ "$CODE" = 409 ] && grep -q WTR012 /tmp/so-b-st.json || fail "B's poll answered $CODE: $(cat /tmp/so-b-st.json)"
CODE=$(join "$TOK_C" /tmp/so-c.json)
[ "$CODE" = 409 ] && grep -q WTR012 /tmp/so-c.json || fail "C's join answered $CODE: $(cat /tmp/so-c.json)"
echo "  B: 409 WTR012; C: 409 WTR012"

echo "== 7. the tick has ended B's session and closed the line =="
SS=""
for i in $(seq 1 10); do
  SS=$(kubectl -n $NS exec statefulset/redis -- redis-cli GET "waitroom:session:$SESSION_B")
  echo "$SS" | grep -q '"status":"sold_out"' && break
  sleep 1
done
echo "$SS" | grep -q '"status":"sold_out"' || fail "B's session is not sold_out: $SS"
[ -z "$(kubectl -n $NS exec statefulset/redis -- redis-cli ZSCORE "waitroom:$EVENT_ID:queue" "$SESSION_B")" ] \
  || fail "B is still in the line"
echo "  B's session is sold_out and out of the line"

echo "SOLD-OUT GATE PASSED: event $EVENT_ID"
