#!/usr/bin/env bash
# An abandoned checkout, run on k3s by `make -C deploy k3s-gate-checkout-expiry`.
# Ten tickets, two buyers: A orders and never pays, B orders and pays -> A's order
# expires with its hold (~9m), after A's chair ended with its 5m window (0027);
# B's order is untouched.
# Rule: docs/design/admission-sizing.md#when-a-checkout-is-abandoned.
set -euo pipefail
GW=${GW:-http://localhost:3000/api}
NS=ticketbottle
HERE="$(cd "$(dirname "$0")" && pwd)"
RUN=$(date +%s)
REGION=${AWS_REGION:-us-east-1}
TABLE=ticketbottle-orders

# JSON path extractor: `echo "$json" | getval a.b.c` -> value, or empty on any error.
getval() { python3 -c "import sys,json
d=json.load(sys.stdin)
for k in sys.argv[1].split('.'):
    d=d[k]
print(d)" "$1" 2>/dev/null || true; }

fail() { echo "CHECKOUT-EXPIRY GATE FAILED: $1"; exit 1; }

redis() { kubectl -n $NS exec statefulset/redis -- redis-cli "$@"; }

# signup <name> -> an access token
signup() {
  curl -s -X POST "$GW/auth/signup" -H 'Content-Type: application/json' \
    -d "{\"firstName\":\"$1\",\"lastName\":\"Buyer\",\"email\":\"expiry+$1-$RUN@example.com\",\"password\":\"Password123!\"}" \
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

# admit <token> <name> -> sets SESSION and CHECKOUT, or fails the gate
admit() {
  join "$1" "/tmp/ce-$2.json" >/dev/null
  SESSION=$(getval data.sessionId < "/tmp/ce-$2.json")
  [ -n "$SESSION" ] || fail "$2's join returned no sessionId: $(cat "/tmp/ce-$2.json")"
  CHECKOUT=""
  for i in $(seq 1 30); do
    poll "$1" "$SESSION" "/tmp/ce-$2-st.json" >/dev/null
    CHECKOUT=$(getval data.checkoutToken < "/tmp/ce-$2-st.json")
    [ -n "$CHECKOUT" ] && return 0
    sleep 1
  done
  fail "$2 not admitted within 30s: $(cat "/tmp/ce-$2-st.json")"
}

# order <token> <name> <checkout> -> the order code, or empty
order() {
  curl -s -X POST "$GW/orders" -H "Authorization: Bearer $1" -H 'Content-Type: application/json' -d "{
    \"eventId\":\"$EVENT_ID\",\"userFullname\":\"$2 Buyer\",\"userEmail\":\"expiry+$2-$RUN@example.com\",
    \"userPhone\":\"0900000000\",\"paymentMethod\":\"ZALOPAY\",
    \"items\":[{\"ticketClassId\":\"$TCID\",\"quantity\":1}],\"currency\":\"VND\",
    \"checkoutToken\":\"$3\",\"redirectUrl\":\"https://example.com/done\"}" | getval data.order.code
}

# order_status <token> <code> -> the status the gateway shows the buyer
order_status() {
  curl -s -H "Authorization: Bearer $1" "$GW/orders/code/$2" | getval data.status
}

echo "== 1. two buyers =="
TOK_A=$(signup A); TOK_B=$(signup B)
[ -n "$TOK_A" ] && [ -n "$TOK_B" ] || fail "a signup returned no accessToken"

echo "== 2. an event with ten tickets =="
CATEGORY_ID=gate1-category
kubectl -n $NS exec statefulset/postgres -- psql -U root -d ticketbottle_event -qc \
  "INSERT INTO categories (id, name, \"createdAt\", \"updatedAt\") VALUES ('$CATEGORY_ID','Gate1',now(),now()) ON CONFLICT (id) DO NOTHING;"
EVT=$(curl -s -X POST "$GW/events" -H "Authorization: Bearer $TOK_A" -H 'Content-Type: application/json' -d "{
  \"name\":\"Abandoned Checkout Show\",\"description\":\"e2e\",
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
TCID=$("$HERE/seed-ticketclass.sh" "$EVENT_ID" 10 10000)
[ -n "$TCID" ] || fail "seed-ticketclass returned no id"
echo "  eventId=$EVENT_ID ticketClassId=$TCID (total 10)"

echo "== 3. A and B are admitted and each orders one ticket =="
admit "$TOK_A" A; SESSION_A=$SESSION; CHECKOUT_A=$CHECKOUT
admit "$TOK_B" B; SESSION_B=$SESSION; CHECKOUT_B=$CHECKOUT
CODE_A=$(order "$TOK_A" A "$CHECKOUT_A")
[ -n "$CODE_A" ] || fail "A's order returned no order.code"
T0=$(date +%s)
CODE_B=$(order "$TOK_B" B "$CHECKOUT_B")
[ -n "$CODE_B" ] || fail "B's order returned no order.code"
[ -n "$(redis ZSCORE "waitroom:$EVENT_ID:checkouts" "$SESSION_A")" ] || fail "A holds no chair after ordering"
echo "  A: session=$SESSION_A order=$CODE_A (never pays); B: order=$CODE_B"

echo "== 4. B pays =="
kubectl -n $NS run ce-curl --rm -i --restart=Never --image=curlimages/curl:8.10.1 -- \
  -s -X POST "http://payment-webhook:8080/complete/$CODE_B" >/dev/null 2>&1 || true
STATUS=""
for i in $(seq 1 20); do
  STATUS=$(order_status "$TOK_B" "$CODE_B")
  [ "$STATUS" = COMPLETED ] && break
  sleep 2
done
[ "$STATUS" = COMPLETED ] || fail "B's order did not reach COMPLETED: ${STATUS:-?}"
echo "  B's order COMPLETED"

echo "== 5. wait for A's order to expire (every 15s, up to 12m) =="
EXPIRED_AT=""
while [ $(( $(date +%s) - T0 )) -le 720 ]; do
  STATUS=$(order_status "$TOK_A" "$CODE_A")
  if [ "$STATUS" = EXPIRED ]; then
    EXPIRED_AT=$(date +%s)
    break
  fi
  echo "  $(( $(date +%s) - T0 ))s: A's order reads ${STATUS:-?}"
  sleep 15
done
[ -n "$EXPIRED_AT" ] || fail "A's order had not expired 12m after it was placed"
AFTER=$(( EXPIRED_AT - T0 ))
echo "  A's order expired ${AFTER}s after it was placed"

echo "== 6. A's checkout expired everywhere; B's is untouched =="
[ "$AFTER" -ge 510 ] && [ "$AFTER" -le 660 ] || fail "A's order expired at ${AFTER}s, want 510-660s (the hold is 540s)"
[ -z "$(redis ZSCORE "waitroom:$EVENT_ID:checkouts" "$SESSION_A")" ] || fail "A still holds a chair after its order expired"
KEY="{\"PK\":{\"S\":\"ORDER#$CODE_A\"},\"SK\":{\"S\":\"ORDER#$CODE_A\"}}"
STORED=$(aws dynamodb get-item --region "$REGION" --table-name "$TABLE" --key "$KEY" --query 'Item.status.S' --output text)
[ "$STORED" = TIMEOUT ] || fail "A's order is $STORED in DynamoDB, want TIMEOUT"
SS=$(redis GET "waitroom:session:$SESSION_A")
echo "$SS" | grep -q '"status":"expired"' || fail "A's session is not expired: $SS"
RSV=$(kubectl -n $NS exec statefulset/postgres -- psql -U root -d ticketbottle_inventory -tAc \
  "SELECT status FROM reservation WHERE order_code='$CODE_A'")
[ -n "$RSV" ] || fail "A has no reservation row"
! echo "$RSV" | grep -q ACTIVE || fail "A's reservation is still ACTIVE: $RSV"
STATUS=$(order_status "$TOK_B" "$CODE_B")
[ "$STATUS" = COMPLETED ] || fail "B's order reads ${STATUS:-?}, want COMPLETED"
echo "  A: EXPIRED on the wire, TIMEOUT stored, session expired, reservation $(echo $RSV); B: COMPLETED"

echo "== 7. B's clock found nothing to expire =="
SEEN=""
for i in $(seq 1 12); do
  # grep -c reads to the end: with pipefail, an early -q exit can fail the pipe.
  N=$(kubectl -n $NS logs deploy/order-consumer --since=20m | grep -F "$CODE_B" | grep -c 'nothing to expire' || true)
  [ "${N:-0}" -gt 0 ] && { SEEN=1; break; }
  sleep 10
done
[ -n "$SEEN" ] || fail "order-consumer never logged B's order ($CODE_B) as nothing to expire"
echo "  order-consumer: $CODE_B settled before its hold expired"

echo "CHECKOUT-EXPIRY GATE PASSED: event $EVENT_ID, order expired at ${AFTER}s"
