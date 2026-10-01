#!/usr/bin/env bash
# Room size per event, run on k3s by `make -C deploy k3s-gate-room`.
# Three tickets, five buyers, nobody orders: three are admitted and two wait while the
# three inside could take every ticket -> two tickets are added -> both are admitted.
# Rule: docs/design/admission-sizing.md#room-size-per-event.
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

fail() { echo "ROOM GATE FAILED: $1"; exit 1; }

redis() { kubectl -n $NS exec statefulset/redis -- redis-cli "$@"; }

# signup <name> -> an access token
signup() {
  curl -s -X POST "$GW/auth/signup" -H 'Content-Type: application/json' \
    -d "{\"firstName\":\"$1\",\"lastName\":\"Buyer\",\"email\":\"room+$1-$RUN@example.com\",\"password\":\"Password123!\"}" \
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

echo "== 1. five buyers =="
TOKS=()
for n in A B C D E; do
  T=$(signup $n)
  [ -n "$T" ] || fail "signup $n returned no accessToken"
  TOKS+=("$T")
done

echo "== 2. an event with three tickets =="
CATEGORY_ID=gate1-category
kubectl -n $NS exec statefulset/postgres -- psql -U root -d ticketbottle_event -qc \
  "INSERT INTO categories (id, name, \"createdAt\", \"updatedAt\") VALUES ('$CATEGORY_ID','Gate1',now(),now()) ON CONFLICT (id) DO NOTHING;"
EVT=$(curl -s -X POST "$GW/events" -H "Authorization: Bearer ${TOKS[0]}" -H 'Content-Type: application/json' -d "{
  \"name\":\"Room Size Show\",\"description\":\"e2e\",
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
TCID=$("$HERE/seed-ticketclass.sh" "$EVENT_ID" 3 10000)
[ -n "$TCID" ] || fail "seed-ticketclass returned no id"
echo "  eventId=$EVENT_ID ticketClassId=$TCID (total 3)"

echo "== 3. all five join; three are admitted, two wait =="
SESSIONS=()
for i in 0 1 2 3 4; do
  join "${TOKS[$i]}" "/tmp/room-$i.json" >/dev/null
  S=$(getval data.sessionId < "/tmp/room-$i.json")
  [ -n "$S" ] || fail "join $i returned no sessionId: $(cat "/tmp/room-$i.json")"
  SESSIONS+=("$S")
done
until_room 3 2 15 || fail "inside=$(inside) waiting=$(waiting) after 15s, want 3 and 2"
# The door admits 2 a second: five more ticks would have admitted both waiters.
sleep 5
[ "$(inside)" = 3 ] && [ "$(waiting)" = 2 ] || fail "after 5s more: inside=$(inside) waiting=$(waiting), want 3 and 2"
echo "  3 inside, 2 waiting, held for 5s"

echo "== 4. a waiter held by the room is queued, not paused =="
W=""
for i in 0 1 2 3 4; do
  [ -z "$(redis ZSCORE "waitroom:$EVENT_ID:checkouts" "${SESSIONS[$i]}")" ] && { W=$i; break; }
done
[ -n "$W" ] || fail "no session is waiting"
CODE=$(poll "${TOKS[$W]}" "${SESSIONS[$W]}" /tmp/room-w.json)
[ "$CODE" = 200 ] || fail "the waiter's poll answered $CODE: $(cat /tmp/room-w.json)"
ST=$(getval data.status < /tmp/room-w.json); PAUSED=$(getval data.paused < /tmp/room-w.json)
[ "$ST" = QUEUED ] && [ "$PAUSED" = False ] || fail "waiter reads status=$ST paused=$PAUSED, want QUEUED and False"
echo "  waiter: QUEUED, paused False, position $(getval data.position < /tmp/room-w.json)"

echo "== 5. the organizer adds two tickets; both waiters are admitted =="
kubectl -n $NS exec statefulset/postgres -- psql -U root -d ticketbottle_inventory -qc \
  "UPDATE ticket_class SET total = total + 2 WHERE id='$TCID';"
until_room 5 0 15 || fail "inside=$(inside) waiting=$(waiting) 15s after adding two tickets, want 5 and 0"
echo "  5 inside, 0 waiting"

echo "ROOM GATE PASSED: event $EVENT_ID"
