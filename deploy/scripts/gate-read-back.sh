#!/usr/bin/env bash
# Read-back, run on k3s by `make -C deploy k3s-gate-read-back`.
# Every field a client sends through the gateway is read back through the gateway:
# a user, an event, its config, each created and then updated, an order. A field
# dropped anywhere between the gateway and the store shows here as a mismatch.
set -euo pipefail
GW=${GW:-http://localhost:3000/api}
NS=ticketbottle
HERE="$(cd "$(dirname "$0")" && pwd)"
RUN=$(date +%s)
BAD=0

# JSON path extractor: `echo "$json" | getval a.b.c` -> value, or empty on any error.
getval() { python3 -c "import sys,json
d=json.load(sys.stdin)
for k in sys.argv[1].split('.'):
    d=d[int(k)] if isinstance(d, list) else d[k]
print(d)" "$1" 2>/dev/null || true; }

fail() { echo "READ-BACK GATE FAILED: $1"; exit 1; }

# expect <label> <file> <spec>... -> prints each spec the read-back breaks; counts them.
#   a.b==v   equal (booleans as true/false)    a.b@=iso  the same instant
#   a.b~=v   contains v                         a.b[].k=x,y  the k of each element, as a set
#   !key     no key of that name anywhere in the body
expect() {
  local label=$1 file=$2; shift 2
  local out
  out=$(python3 - "$file" "$@" <<'PY'
import sys, json, datetime
d = json.load(open(sys.argv[1]))
def at(path):
    v = d
    for k in path.split('.'):
        v = v[int(k)] if isinstance(v, list) else v.get(k) if isinstance(v, dict) else None
        if v is None: return None
    return v
def norm(v): return json.dumps(v) if isinstance(v, bool) else str(v)
def instant(s): return datetime.datetime.fromisoformat(str(s).replace('Z', '+00:00'))
def keys(o):
    if isinstance(o, dict):
        for k, v in o.items():
            yield k; yield from keys(v)
    elif isinstance(o, list):
        for v in o: yield from keys(v)
for spec in sys.argv[2:]:
    if spec.startswith('!'):
        if spec[1:] in set(keys(d)): print(f"  {spec[1:]}: present, want absent")
        continue
    if '[].' in spec:
        path, rest = spec.split('[].', 1); key, want = rest.split('=', 1)
        got = sorted(str(e.get(key)) for e in (at(path) or []))
        if got != sorted(want.split(',')): print(f"  {path}[].{key}: {got}, want {sorted(want.split(','))}")
        continue
    for op in ('@=', '~=', '=='):
        if op in spec:
            path, want = spec.split(op, 1); got = at(path); break
    ok = (got is not None) and (
        instant(got) == instant(want) if op == '@=' else
        want in str(got) if op == '~=' else norm(got) == want)
    if not ok: print(f"  {path}: {got!r}, want {'contains ' if op == '~=' else ''}{want!r}")
PY
)
  if [ -n "$out" ]; then echo "  MISMATCH in $label:"; echo "$out"; BAD=$((BAD + $(echo "$out" | wc -l))); else echo "  $label: every field read back"; fi
}

# call <method> <token> <path> <out-file> [json] -> the HTTP code; the body lands in <out-file>
call() {
  if [ -n "${5:-}" ]; then
    curl -s -o "$4" -w '%{http_code}' -X "$1" "$GW$3" -H "Authorization: Bearer $2" -H 'Content-Type: application/json' -d "$5"
  else
    curl -s -o "$4" -w '%{http_code}' -X "$1" "$GW$3" -H "Authorization: Bearer $2"
  fi
}
ok() { case "$1" in 2??) ;; *) fail "$2 answered $1: $(cat "$3")";; esac; }

echo "== 1. a user: signed up, then updated =="
EMAIL="readback+$RUN@example.com"
TOK=$(curl -s -X POST "$GW/auth/signup" -H 'Content-Type: application/json' \
  -d "{\"firstName\":\"Ada\",\"lastName\":\"Lovelace\",\"email\":\"$EMAIL\",\"password\":\"Password123!\"}" | getval data.accessToken)
[ -n "$TOK" ] || fail "signup returned no accessToken"
ok "$(call GET "$TOK" /auth/me /tmp/rb-me.json)" "me" /tmp/rb-me.json
UID_=$(getval data.id < /tmp/rb-me.json)
expect "me" /tmp/rb-me.json "data.email==$EMAIL"
ok "$(call PATCH "$TOK" "/users/$UID_" /tmp/rb-u1.json '{"avatar":"https://example.com/ada.png"}')" "user patch" /tmp/rb-u1.json
expect "user after sign-up" /tmp/rb-u1.json "data.firstName==Ada" "data.lastName==Lovelace" "data.email==$EMAIL" \
  "data.avatar==https://example.com/ada.png" "!password"
ok "$(call PATCH "$TOK" "/users/$UID_" /tmp/rb-u2.json '{"firstName":"Grace","lastName":"Hopper"}')" "user patch" /tmp/rb-u2.json
expect "user after update" /tmp/rb-u2.json "data.firstName==Grace" "data.lastName==Hopper" \
  "data.avatar==https://example.com/ada.png" "!password"

echo "== 2. an event: created, then every field updated =="
# Category ids are UUIDs; fixed ones let every run reuse the same two rows.
CAT1=00000000-0000-4000-8000-0000000000c1; CAT2=00000000-0000-4000-8000-0000000000c2
for c in $CAT1 $CAT2; do
  kubectl -n $NS exec statefulset/postgres -- psql -U root -d ticketbottle_event -qc \
    "INSERT INTO categories (id, name, \"createdAt\", \"updatedAt\") VALUES ('$c','read-back $c',now(),now()) ON CONFLICT (id) DO NOTHING;"
done
EV1='{"name":"Read-back Show","description":"first description","startDate":"2027-01-01T10:00:00.000Z","endDate":"2027-01-02T10:00:00.000Z",
 "thumbnailUrl":"https://example.com/t1.png","venue":"Hall One","street":"1 First St","ward":"Ward One","district":"District One",
 "city":"HCMC","country":"VN","categoryIds":["'"$CAT1"'"],
 "organizerName":"Org One","organizerDescription":"first organizer","organizerLogoUrl":"https://example.com/o1.png"}'
ok "$(call POST "$TOK" /events /tmp/rb-ev.json "$EV1")" "event create" /tmp/rb-ev.json
EVENT_ID=$(getval data.id < /tmp/rb-ev.json)
[ -n "$EVENT_ID" ] || fail "event create returned no id: $(cat /tmp/rb-ev.json)"
ok "$(call GET "$TOK" "/events/$EVENT_ID" /tmp/rb-ev-get.json)" "event read" /tmp/rb-ev-get.json
expect "event after create" /tmp/rb-ev-get.json "data.name==Read-back Show" "data.description==first description" \
  "data.startDate@=2027-01-01T10:00:00Z" "data.endDate@=2027-01-02T10:00:00Z" "data.thumbnailUrl==https://example.com/t1.png" \
  "data.location.venue==Hall One" "data.location.address~=1 First St" "data.location.address~=Ward One" \
  "data.location.address~=District One" "data.location.address~=HCMC" "data.location.address~=VN" \
  "data.categories[].id=$CAT1" "data.organizer.name==Org One" "data.organizer.description==first organizer" \
  "data.organizer.logoUrl==https://example.com/o1.png"
EV2='{"name":"Read-back Show Two","description":"second description","startDate":"2027-02-01T10:00:00.000Z","endDate":"2027-02-02T10:00:00.000Z",
 "thumbnailUrl":"https://example.com/t2.png","venue":"Hall Two","street":"2 Second St","ward":"Ward Two","district":"District Two",
 "city":"Hanoi","country":"Vietnam","categoryIds":["'"$CAT1"'","'"$CAT2"'"],
 "organizerName":"Org Two","organizerDescription":"second organizer","organizerLogoUrl":"https://example.com/o2.png"}'
ok "$(call PUT "$TOK" "/events/$EVENT_ID" /tmp/rb-ev-put.json "$EV2")" "event update" /tmp/rb-ev-put.json
ok "$(call GET "$TOK" "/events/$EVENT_ID" /tmp/rb-ev-get2.json)" "event read" /tmp/rb-ev-get2.json
expect "event after update" /tmp/rb-ev-get2.json "data.name==Read-back Show Two" "data.description==second description" \
  "data.startDate@=2027-02-01T10:00:00Z" "data.endDate@=2027-02-02T10:00:00Z" "data.thumbnailUrl==https://example.com/t2.png" \
  "data.location.venue==Hall Two" "data.location.address~=2 Second St" "data.location.address~=Ward Two" \
  "data.location.address~=District Two" "data.location.address~=Hanoi" "data.location.address~=Vietnam" \
  "data.categories[].id=$CAT1,$CAT2" "data.organizer.name==Org Two" \
  "data.organizer.description==second organizer" "data.organizer.logoUrl==https://example.com/o2.png"

echo "== 3. its config: created, then every field flipped =="
CF1='{"ticketSaleStartDate":"2020-01-01T00:00:00.000Z","ticketSaleEndDate":"2030-01-01T00:00:00.000Z","isFree":true,"maxAttendees":77,
 "maxTicketsPerOrder":3,"isPublic":false,"requiresApproval":true,"allowWaitRoom":false,"isNewTrending":false}'
ok "$(call POST "$TOK" "/events/$EVENT_ID/config" /tmp/rb-cf.json "$CF1")" "config create" /tmp/rb-cf.json
ok "$(call GET "$TOK" "/events/$EVENT_ID/config" /tmp/rb-cf-get.json)" "config read" /tmp/rb-cf-get.json
expect "config after create" /tmp/rb-cf-get.json "data.ticketSaleStartDate@=2020-01-01T00:00:00Z" \
  "data.ticketSaleEndDate@=2030-01-01T00:00:00Z" "data.isFree==true" "data.maxAttendees==77" "data.maxTicketsPerOrder==3" \
  "data.isPublic==false" "data.requiresApproval==true" "data.allowWaitRoom==false" "data.isNewTrending==false"
CF2='{"ticketSaleStartDate":"2019-06-01T00:00:00.000Z","ticketSaleEndDate":"2031-06-01T00:00:00.000Z","isFree":false,"maxAttendees":88,
 "maxTicketsPerOrder":5,"isPublic":true,"requiresApproval":false,"allowWaitRoom":true,"isNewTrending":true}'
ok "$(call PUT "$TOK" "/events/$EVENT_ID/config" /tmp/rb-cf-put.json "$CF2")" "config update" /tmp/rb-cf-put.json
ok "$(call GET "$TOK" "/events/$EVENT_ID/config" /tmp/rb-cf-get2.json)" "config read" /tmp/rb-cf-get2.json
expect "config after update" /tmp/rb-cf-get2.json "data.ticketSaleStartDate@=2019-06-01T00:00:00Z" \
  "data.ticketSaleEndDate@=2031-06-01T00:00:00Z" "data.isFree==false" "data.maxAttendees==88" "data.maxTicketsPerOrder==5" \
  "data.isPublic==true" "data.requiresApproval==false" "data.allowWaitRoom==true" "data.isNewTrending==true"

echo "== 4. an order: two tickets at 12345 each =="
kubectl -n $NS exec statefulset/postgres -- psql -U root -d ticketbottle_event -qc \
  "UPDATE events SET status='PUBLISHED' WHERE id='$EVENT_ID';"
TCID=$("$HERE/seed-ticketclass.sh" "$EVENT_ID" 10 12345)
[ -n "$TCID" ] || fail "seed-ticketclass returned no id"
ok "$(call POST "$TOK" /waitroom/join /tmp/rb-join.json "{\"eventId\":\"$EVENT_ID\"}")" "join" /tmp/rb-join.json
SESSION=$(getval data.sessionId < /tmp/rb-join.json)
CHECKOUT=""
for i in $(seq 1 30); do
  call GET "$TOK" "/waitroom/status/$SESSION" /tmp/rb-st.json >/dev/null
  CHECKOUT=$(getval data.checkoutToken < /tmp/rb-st.json)
  [ -n "$CHECKOUT" ] && break
  sleep 1
done
[ -n "$CHECKOUT" ] || fail "not admitted within 30s: $(cat /tmp/rb-st.json)"
expect "admitted session" /tmp/rb-st.json "data.sessionId==$SESSION" "data.status==READY" "data.paused==false"
OR="{\"eventId\":\"$EVENT_ID\",\"userFullname\":\"Grace Hopper\",\"userEmail\":\"$EMAIL\",\"userPhone\":\"0912345678\",
 \"paymentMethod\":\"ZALOPAY\",\"items\":[{\"ticketClassId\":\"$TCID\",\"quantity\":2}],\"currency\":\"VND\",
 \"checkoutToken\":\"$CHECKOUT\",\"redirectUrl\":\"https://example.com/done\"}"
ok "$(call POST "$TOK" /orders /tmp/rb-or.json "$OR")" "order create" /tmp/rb-or.json
CODE=$(getval data.order.code < /tmp/rb-or.json)
[ -n "$CODE" ] || fail "order create returned no code: $(cat /tmp/rb-or.json)"
ok "$(call GET "$TOK" "/orders/code/$CODE" /tmp/rb-or-get.json)" "order read" /tmp/rb-or-get.json
expect "order" /tmp/rb-or-get.json "data.code==$CODE" "data.eventId==$EVENT_ID" "data.userId==$UID_" \
  "data.userFullname==Grace Hopper" "data.userEmail==$EMAIL" "data.userPhone==0912345678" "data.paymentMethod==ZALOPAY" \
  "data.currency==VND" "data.status==PENDING" "data.totalAmountCents==24690" \
  "data.items.0.ticketClassId==$TCID" "data.items.0.quantity==2" "data.items.0.priceCents==12345"
call DELETE "$TOK" "/orders/code/$CODE" /tmp/rb-del.json >/dev/null

[ "$BAD" = 0 ] || fail "$BAD field(s) did not read back as sent"
echo "READ-BACK GATE PASSED: user $UID_, event $EVENT_ID, order $CODE"
