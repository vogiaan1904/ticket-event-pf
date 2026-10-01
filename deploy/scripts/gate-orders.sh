#!/usr/bin/env bash
# The order contract, run on k3s by `make -C deploy k3s-gate-orders`.
# A holds an unpaid order -> lists it on the first page -> a stranger S reads and
# cancels it and gets 404 both times -> A cancels it -> it reads CANCELED, its
# reservation is released, and a second cancel answers 409.
# Rule: docs/decisions/0026-the-order-contract-tells-the-buyer-what-happened.md.
set -euo pipefail
GW=${GW:-http://localhost:3000/api}
NS=ticketbottle
HERE="$(cd "$(dirname "$0")" && pwd)"
RUN=$(date +%s)

# JSON path extractor: `echo "$json" | getval a.b.c` -> value, or empty on any error.
getval() { python3 -c "import sys,json
d=json.load(sys.stdin)
for k in sys.argv[1].split('.'):
    d=d[int(k)] if isinstance(d, list) else d[k]
print(d)" "$1" 2>/dev/null || true; }

fail() { echo "ORDERS GATE FAILED: $1"; exit 1; }

# signup <name> -> an access token
signup() {
  curl -s -X POST "$GW/auth/signup" -H 'Content-Type: application/json' \
    -d "{\"firstName\":\"$1\",\"lastName\":\"Buyer\",\"email\":\"orders+$1-$RUN@example.com\",\"password\":\"Password123!\"}" \
    | getval data.accessToken
}

# call <method> <token> <path> <out-file> -> the HTTP code; the body lands in <out-file>
call() {
  curl -s -o "$4" -w '%{http_code}' -X "$1" "$GW$3" -H "Authorization: Bearer $2"
}

echo "== 1. a buyer and a stranger =="
TOK_A=$(signup A); TOK_S=$(signup S)
[ -n "$TOK_A" ] && [ -n "$TOK_S" ] || fail "a signup returned no accessToken"

echo "== 2. an event with five tickets =="
CATEGORY_ID=gate1-category
kubectl -n $NS exec statefulset/postgres -- psql -U root -d ticketbottle_event -qc \
  "INSERT INTO categories (id, name, \"createdAt\", \"updatedAt\") VALUES ('$CATEGORY_ID','Gate1',now(),now()) ON CONFLICT (id) DO NOTHING;"
EVT=$(curl -s -X POST "$GW/events" -H "Authorization: Bearer $TOK_A" -H 'Content-Type: application/json' -d "{
  \"name\":\"Orders Show\",\"description\":\"e2e\",
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
TCID=$("$HERE/seed-ticketclass.sh" "$EVENT_ID" 5 10000)
[ -n "$TCID" ] || fail "seed-ticketclass returned no id"
echo "  eventId=$EVENT_ID ticketClassId=$TCID (total 5)"

echo "== 3. A is admitted and orders two tickets, unpaid =="
curl -s -o /tmp/ord-a.json -X POST "$GW/waitroom/join" -H "Authorization: Bearer $TOK_A" \
  -H 'Content-Type: application/json' -d "{\"eventId\":\"$EVENT_ID\"}"
SESSION_A=$(getval data.sessionId < /tmp/ord-a.json)
[ -n "$SESSION_A" ] || fail "A's join returned no sessionId: $(cat /tmp/ord-a.json)"
CHECKOUT=""
for i in $(seq 1 30); do
  call GET "$TOK_A" "/waitroom/status/$SESSION_A" /tmp/ord-a-st.json >/dev/null
  CHECKOUT=$(getval data.checkoutToken < /tmp/ord-a-st.json)
  [ -n "$CHECKOUT" ] && break
  sleep 1
done
[ -n "$CHECKOUT" ] || fail "A not admitted within 30s: $(cat /tmp/ord-a-st.json)"
ORD=$(curl -s -X POST "$GW/orders" -H "Authorization: Bearer $TOK_A" -H 'Content-Type: application/json' -d "{
  \"eventId\":\"$EVENT_ID\",\"userFullname\":\"A Buyer\",\"userEmail\":\"orders+A-$RUN@example.com\",
  \"userPhone\":\"0900000000\",\"paymentMethod\":\"ZALOPAY\",
  \"items\":[{\"ticketClassId\":\"$TCID\",\"quantity\":2}],\"currency\":\"VND\",
  \"checkoutToken\":\"$CHECKOUT\",\"redirectUrl\":\"https://example.com/done\"}")
CODE=$(echo "$ORD" | getval data.order.code)
[ -n "$CODE" ] || fail "A's order returned no order.code: $ORD"
echo "  orderCode=$CODE"

echo "== 4. A reads it, with its items, and lists it on the first page =="
[ "$(call GET "$TOK_A" "/orders/code/$CODE" /tmp/ord-get.json)" = 200 ] || fail "A's read: $(cat /tmp/ord-get.json)"
[ "$(getval data.status < /tmp/ord-get.json)" = PENDING ] || fail "A's order reads $(getval data.status < /tmp/ord-get.json), want PENDING"
[ "$(getval data.items.0.quantity < /tmp/ord-get.json)" = 2 ] || fail "A's order has no 2-ticket item: $(cat /tmp/ord-get.json)"
[ "$(call GET "$TOK_A" "/orders?limit=10&eventId=$EVENT_ID" /tmp/ord-list.json)" = 200 ] || fail "A's list: $(cat /tmp/ord-list.json)"
[ "$(getval data.0.code < /tmp/ord-list.json)" = "$CODE" ] || fail "A's first page lacks $CODE: $(cat /tmp/ord-list.json)"
echo "  read PENDING with 2 tickets; listed on the first page"

echo "== 5. a stranger can neither read nor cancel it =="
C=$(call GET "$TOK_S" "/orders/code/$CODE" /tmp/ord-s.json); [ "$C" = 404 ] || fail "S's read answered $C: $(cat /tmp/ord-s.json)"
C=$(call DELETE "$TOK_S" "/orders/code/$CODE" /tmp/ord-s.json); [ "$C" = 404 ] || fail "S's cancel answered $C: $(cat /tmp/ord-s.json)"
call GET "$TOK_A" "/orders/code/$CODE" /tmp/ord-get.json >/dev/null
[ "$(getval data.status < /tmp/ord-get.json)" = PENDING ] || fail "after S's cancel the order reads $(getval data.status < /tmp/ord-get.json)"
echo "  S: 404 and 404; the order is still PENDING"

echo "== 6. A cancels it; it is released, and a second cancel is refused =="
C=$(call DELETE "$TOK_A" "/orders/code/$CODE" /tmp/ord-del.json); [ "$C" = 200 ] || fail "A's cancel answered $C: $(cat /tmp/ord-del.json)"
call GET "$TOK_A" "/orders/code/$CODE" /tmp/ord-get.json >/dev/null
[ "$(getval data.status < /tmp/ord-get.json)" = CANCELED ] || fail "A's order reads $(getval data.status < /tmp/ord-get.json), want CANCELED"
RSV=$(kubectl -n $NS exec statefulset/postgres -- psql -U root -d ticketbottle_inventory -tAc \
  "SELECT status FROM reservation WHERE order_code='$CODE'")
[ -n "$RSV" ] || fail "A has no reservation row"
! echo "$RSV" | grep -q ACTIVE || fail "A's reservation is still ACTIVE: $RSV"
C=$(call DELETE "$TOK_A" "/orders/code/$CODE" /tmp/ord-del.json); [ "$C" = 409 ] || fail "a second cancel answered $C: $(cat /tmp/ord-del.json)"
echo "  CANCELED; reservation $(echo $RSV); second cancel 409"

echo "ORDERS GATE PASSED: order $CODE"
