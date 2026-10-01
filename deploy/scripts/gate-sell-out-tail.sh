#!/usr/bin/env bash
# The end of a sell-out, run on k3s by `make -C deploy k3s-gate-sell-out-tail` (~6 min).
# Two tickets, three buyers, nobody orders: two are admitted and the third waits ->
# the two chairs expire with their 5-minute window -> the third is admitted then,
# not at the old 15 minutes.
# Rule: docs/design/admission-sizing.md#the-end-of-a-sell-out.
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

fail() { echo "SELL-OUT TAIL GATE FAILED: $1"; exit 1; }

redis() { kubectl -n $NS exec statefulset/redis -- redis-cli "$@"; }

# signup <name> -> an access token
signup() {
  curl -s -X POST "$GW/auth/signup" -H 'Content-Type: application/json' \
    -d "{\"firstName\":\"$1\",\"lastName\":\"Buyer\",\"email\":\"tail+$1-$RUN@example.com\",\"password\":\"Password123!\"}" \
    | getval data.accessToken
}

# join <token> <out-file> -> the HTTP code; the body lands in <out-file>
join() {
  curl -s -o "$2" -w '%{http_code}' -X POST "$GW/waitroom/join" -H "Authorization: Bearer $1" \
    -H 'Content-Type: application/json' -d "{\"eventId\":\"$EVENT_ID\"}"
}

inside()  { redis ZCARD "waitroom:$EVENT_ID:checkouts" | tr -d '[:space:]'; }
waiting() { redis ZCARD "waitroom:$EVENT_ID:queue" | tr -d '[:space:]'; }

# until_room <inside> <waiting> <seconds> -> succeeds once both counts read as given
until_room() {
  for i in $(seq 1 "$3"); do
    [ "$(inside)" = "$1" ] && [ "$(waiting)" = "$2" ] && return 0
    sleep 1
  done
  return 1
}

echo "== 1. three buyers =="
TOKS=()
for n in A B C; do
  T=$(signup $n)
  [ -n "$T" ] || fail "signup $n returned no accessToken"
  TOKS+=("$T")
done

echo "== 2. an event with two tickets =="
CATEGORY_ID=gate1-category
kubectl -n $NS exec statefulset/postgres -- psql -U root -d ticketbottle_event -qc \
  "INSERT INTO categories (id, name, \"createdAt\", \"updatedAt\") VALUES ('$CATEGORY_ID','Gate1',now(),now()) ON CONFLICT (id) DO NOTHING;"
EVT=$(curl -s -X POST "$GW/events" -H "Authorization: Bearer ${TOKS[0]}" -H 'Content-Type: application/json' -d "{
  \"name\":\"Sell-out Tail Show\",\"description\":\"e2e\",
  \"startDate\":\"2027-01-01T00:00:00Z\",\"endDate\":\"2027-01-02T00:00:00Z\",
  \"thumbnailUrl\":\"https://example.com/t.png\",\"venue\":\"Test Arena\",
  \"street\":\"1 St\",\"city\":\"HCMC\",\"country\":\"VN\",\"categoryIds\":[\"$CATEGORY_ID\"],
  \"organizerName\":\"Gate Org\",\"organizerDescription\":\"e2e organizer\",
  \"organizerLogoUrl\":\"https://example.com/logo.png\"
}")
EVENT_ID=$(echo "$EVT" | getval data.id)
[ -n "$EVENT_ID" ] || fail "event create returned no id: $EVT"
CFG=$(curl -s -X POST "$GW/events/$EVENT_ID/config" -H "Authorization: Bearer ${TOKS[0]}" -H 'Content-Type: application/json' -d '{
  "ticketSaleStartDate":"2020-01-01T00:00:00Z","ticketSaleEndDate":"2030-01-01T00:00:00Z",
  "isFree":false,"maxAttendees":100,"isPublic":true,"requiresApproval":false,
  "allowWaitRoom":true,"isNewTrending":false
}')
[ -n "$(echo "$CFG" | getval data.id)" ] || fail "config create failed: $CFG"
kubectl -n $NS exec statefulset/postgres -- psql -U root -d ticketbottle_event -qc \
  "UPDATE events SET status='PUBLISHED' WHERE id='$EVENT_ID';"
TCID=$("$HERE/seed-ticketclass.sh" "$EVENT_ID" 2 10000)
[ -n "$TCID" ] || fail "seed-ticketclass returned no id"
echo "  eventId=$EVENT_ID ticketClassId=$TCID (total 2)"

echo "== 3. all three join; two are admitted, the third waits =="
for i in 0 1 2; do
  join "${TOKS[$i]}" "/tmp/tail-$i.json" >/dev/null
  [ -n "$(getval data.sessionId < "/tmp/tail-$i.json")" ] || fail "join $i returned no sessionId: $(cat "/tmp/tail-$i.json")"
done
until_room 2 1 15 || fail "inside=$(inside) waiting=$(waiting) after 15s, want 2 and 1"
T0=$(date +%s)
# A chair is scored by its expiry, in ms: both must end with the 5-minute window.
LAST=$(redis ZRANGE "waitroom:$EVENT_ID:checkouts" -1 -1 WITHSCORES | tail -1 | tr -d '[:space:]')
LEFT=$(( ${LAST%.*} / 1000 - T0 ))
[ "$LEFT" -ge 280 ] && [ "$LEFT" -le 310 ] || fail "a chair ends ${LEFT}s from now, want about 300s"
echo "  2 inside, 1 waiting; the chairs end in ${LEFT}s"

echo "== 4. the chairs expire, and the third buyer is admitted then =="
ADMITTED_AT=""
while [ $(( $(date +%s) - T0 )) -le 420 ]; do
  if [ "$(waiting)" = 0 ]; then ADMITTED_AT=$(date +%s); break; fi
  sleep 5
done
[ -n "$ADMITTED_AT" ] || fail "the third buyer still waits 420s later: inside=$(inside) waiting=$(waiting)"
AFTER=$(( ADMITTED_AT - T0 ))
[ "$AFTER" -ge 280 ] && [ "$AFTER" -le 340 ] || fail "the third buyer was admitted ${AFTER}s later, want 280-340s"
echo "  the third buyer admitted ${AFTER}s after the room filled"

echo "SELL-OUT TAIL GATE PASSED: event $EVENT_ID, third buyer admitted at ${AFTER}s"
