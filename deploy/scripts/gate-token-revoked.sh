#!/usr/bin/env bash
# A spent checkout token no longer buys, run on k3s by `make -C deploy k3s-gate-token-revoked`.
# A buys through to COMPLETED, B orders and cancels. Once the waitroom reports
# each session ended, its token's first reuse must answer 401 ORD016.
# Rule: docs/decisions/0033-order-svc-asks-the-waitroom-whether-a-checkout-token-holds.md.
set -euo pipefail
GW=${GW:-http://localhost:3000/api}
NS=ticketbottle
HERE="$(cd "$(dirname "$0")" && pwd)"
. "$HERE/pg.sh"
RUN=$(date +%s)

getval() { python3 -c "import sys,json
d=json.load(sys.stdin)
for k in sys.argv[1].split('.'):
    d=d[int(k)] if isinstance(d, list) else d[k]
print(d)" "$1" 2>/dev/null || true; }

fail() { echo "TOKEN GATE FAILED: $1"; exit 1; }

signup() {
  curl -s -X POST "$GW/auth/signup" -H 'Content-Type: application/json' \
    -d "{\"firstName\":\"$1\",\"lastName\":\"Buyer\",\"email\":\"revoke+$1-$RUN@example.com\",\"password\":\"Password123!\"}" \
    | getval data.accessToken
}

# admit <access-token> -> "<session-id> <checkout-token>", once one is handed out
admit() {
  local sid tok
  sid=$(curl -s -X POST "$GW/waitroom/join" -H "Authorization: Bearer $1" \
    -H 'Content-Type: application/json' -d "{\"eventId\":\"$EVENT_ID\"}" | getval data.sessionId)
  [ -n "$sid" ] || return 1
  for _ in $(seq 1 30); do
    tok=$(curl -s "$GW/waitroom/status/$sid" -H "Authorization: Bearer $1" | getval data.checkoutToken)
    [ -n "$tok" ] && { echo "$sid $tok"; return 0; }
    sleep 1
  done
  return 1
}

# order <access-token> <checkout-token> <out-file> -> the HTTP code
order() {
  curl -s -o "$3" -w '%{http_code}' -X POST "$GW/orders" -H "Authorization: Bearer $1" \
    -H 'Content-Type: application/json' -d "{
    \"eventId\":\"$EVENT_ID\",\"userFullname\":\"Revoke Buyer\",\"userEmail\":\"revoke-$RUN@example.com\",
    \"userPhone\":\"0900000000\",\"paymentMethod\":\"ZALOPAY\",
    \"items\":[{\"ticketClassId\":\"$TCID\",\"quantity\":1}],\"currency\":\"VND\",
    \"checkoutToken\":\"$2\",\"redirectUrl\":\"https://example.com/done\"}"
}

# refused <access-token> <session-id> <checkout-token> <who>: wait until the
# waitroom answers the session as ended (4xx), then one reuse must be 401 ORD016.
refused() {
  local c
  for i in $(seq 1 30); do
    c=$(curl -s -o /tmp/rv-st.json -w '%{http_code}' "$GW/waitroom/status/$2" -H "Authorization: Bearer $1")
    case "$c" in 4*) break ;; esac
    sleep 1
  done
  case "$c" in 4*) ;; *) fail "$4's session not ended after 30s: $c $(cat /tmp/rv-st.json)" ;; esac
  echo "  $4's session ended after ${i}s ($c)"
  c=$(order "$1" "$3" /tmp/rv-reuse.json)
  [ "$c" = 401 ] && grep -q ORD016 /tmp/rv-reuse.json || fail "$4's spent token answered $c: $(cat /tmp/rv-reuse.json)"
  echo "  $4's reuse: 401 ORD016"
}

echo "== 1. two buyers =="
TOK_A=$(signup A); TOK_B=$(signup B)
[ -n "$TOK_A" ] && [ -n "$TOK_B" ] || fail "a signup returned no accessToken"

echo "== 2. an event with ten tickets, behind a waitroom =="
CATEGORY_ID=gate1-category
pg ticketbottle_event -qc \
  "INSERT INTO categories (id, name, \"createdAt\", \"updatedAt\") VALUES ('$CATEGORY_ID','Gate1',now(),now()) ON CONFLICT (id) DO NOTHING;"
EVT=$(curl -s -X POST "$GW/events" -H "Authorization: Bearer $TOK_A" -H 'Content-Type: application/json' -d "{
  \"name\":\"Revoke Show\",\"description\":\"e2e\",
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
pg ticketbottle_event -qc \
  "UPDATE events SET status='PUBLISHED' WHERE id='$EVENT_ID';"
TCID=$("$HERE/seed-ticketclass.sh" "$EVENT_ID" 10 10000)
[ -n "$TCID" ] || fail "seed-ticketclass returned no id"
echo "  eventId=$EVENT_ID ticketClassId=$TCID"

echo "== 3. A buys one ticket through to COMPLETED =="
read -r SID_A CHK_A < <(admit "$TOK_A") || fail "A was not admitted within 30s"
case "$(order "$TOK_A" "$CHK_A" /tmp/rv-a.json)" in 2*) ;; *) fail "A's order: $(cat /tmp/rv-a.json)" ;; esac
CODE_A=$(getval data.order.code < /tmp/rv-a.json)
kubectl -n $NS run rv-curl --rm -i --restart=Never --image=curlimages/curl:8.10.1 -- \
  -s -X POST "http://payment-webhook:8080/complete/$CODE_A" >/dev/null 2>&1 || true
for _ in $(seq 1 20); do
  ST=$(curl -s -H "Authorization: Bearer $TOK_A" "$GW/orders/code/$CODE_A" | getval data.status)
  [ "$ST" = COMPLETED ] && break
  sleep 2
done
[ "$ST" = COMPLETED ] || fail "A's order $CODE_A reads $ST, want COMPLETED"
echo "  $CODE_A COMPLETED"

echo "== 4. A's token, spent, no longer buys =="
refused "$TOK_A" "$SID_A" "$CHK_A" A

echo "== 5. B orders, then cancels =="
read -r SID_B CHK_B < <(admit "$TOK_B") || fail "B was not admitted within 30s"
case "$(order "$TOK_B" "$CHK_B" /tmp/rv-b.json)" in 2*) ;; *) fail "B's order: $(cat /tmp/rv-b.json)" ;; esac
CODE_B=$(getval data.order.code < /tmp/rv-b.json)
C=$(curl -s -o /tmp/rv-del.json -w '%{http_code}' -X DELETE "$GW/orders/code/$CODE_B" -H "Authorization: Bearer $TOK_B")
[ "$C" = 200 ] || fail "B's cancel answered $C: $(cat /tmp/rv-del.json)"
echo "  $CODE_B cancelled"

echo "== 6. B's token, cancelled, no longer buys =="
refused "$TOK_B" "$SID_B" "$CHK_B" B

echo "TOKEN GATE PASSED: $CODE_A completed, $CODE_B cancelled; both tokens refused"
