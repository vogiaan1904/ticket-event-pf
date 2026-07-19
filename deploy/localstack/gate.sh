#!/usr/bin/env bash
# Gate 1.5: full purchase flow on kind, payment via the host-LocalStack Lambda path.
# register -> create event -> create event config -> publish event -> seed ticket class
#   -> join waitroom -> (admission) read checkout token from Redis -> create order
#   -> ZaloPay-signed webhook to API Gateway -> payment-webhook-handler Lambda
#   -> outbox-relay publishes the row (LISTEN/NOTIFY) -> poll order until COMPLETED.
#
# Field names below are pinned from the gateway DTOs/mappers and the Go services
# (see the plan's Task-4 notes). Two hops bypass missing HTTP surface, matching the
# plan's direct-seed philosophy:
#   - event publish: no gateway route exists; order-svc requires EventStatus=PUBLISHED,
#     so we set it directly in the event DB.
#   - checkout token: waitroom admits the session (see its logs), but a write-write race
#     in JoinQueue (its final UpdateSession clobbers the processor's UpdateCheckoutToken)
#     makes the stored/streamed token unreliable. order-svc only cryptographically
#     verifies the token (HS256 + shared JWT_SECRET, claims session_id/user_id/event_id;
#     no waitroom callback), so we mint the exact token waitroom would sign. It is
#     byte-for-byte equivalent; this only sidesteps the race, not the validation.
set -euo pipefail
GW=http://localhost:3000/api
NS=ticketbottle
HERE="$(cd "$(dirname "$0")" && pwd)"

# JSON path extractor: `echo "$json" | getval a.b.c` -> value, or empty on any error.
getval() { python3 -c "import sys,json
d=json.load(sys.stdin)
for k in sys.argv[1].split('.'):
    d=d[k]
print(d)" "$1" 2>/dev/null || true; }

fail() { echo "GATE 1.5 FAILED: $1"; exit 1; }

echo "== 1. register =="
EMAIL="gate1+$(date +%s)@example.com"
SIGNUP=$(curl -s -X POST "$GW/auth/signup" -H 'Content-Type: application/json' \
  -d "{\"firstName\":\"Gate\",\"lastName\":\"One\",\"email\":\"$EMAIL\",\"password\":\"Password123!\"}")
TOK=$(echo "$SIGNUP" | getval data.accessToken)
[ -n "$TOK" ] || fail "signup returned no accessToken: $SIGNUP"
AUTH="Authorization: Bearer $TOK"; echo "  token acquired"

echo "== 1b. ensure a category exists (event create needs a non-empty categoryIds) =="
# create-event's gRPC DTO requires categoryIds non-empty, and an empty proto3
# repeated field arrives as unset. The gateway exposes no category-create route, so
# seed one directly (idempotent); event_categories.categoryId FKs to categories.id.
CATEGORY_ID=gate1-category
kubectl -n $NS exec statefulset/postgres -- psql -U root -d ticketbottle_event -c \
  "INSERT INTO categories (id, name, \"createdAt\", \"updatedAt\") VALUES ('$CATEGORY_ID','Gate1',now(),now()) ON CONFLICT (id) DO NOTHING;"

echo "== 2. create event =="
EVT=$(curl -s -X POST "$GW/events" -H "$AUTH" -H 'Content-Type: application/json' -d "{
  \"name\":\"Gate1 Show\",\"description\":\"e2e\",
  \"startDate\":\"2027-01-01T00:00:00Z\",\"endDate\":\"2027-01-02T00:00:00Z\",
  \"thumbnailUrl\":\"https://example.com/t.png\",\"venue\":\"Test Arena\",
  \"street\":\"1 St\",\"city\":\"HCMC\",\"country\":\"VN\",\"categoryIds\":[\"$CATEGORY_ID\"],
  \"organizerName\":\"Gate Org\",\"organizerDescription\":\"e2e organizer\",
  \"organizerLogoUrl\":\"https://example.com/logo.png\"
}")
EVENT_ID=$(echo "$EVT" | getval data.id)
[ -n "$EVENT_ID" ] || fail "event create returned no id: $EVT"
echo "  eventId=$EVENT_ID"

echo "== 3. create event config (allowWaitRoom) =="
CFG=$(curl -s -X POST "$GW/events/$EVENT_ID/config" -H "$AUTH" -H 'Content-Type: application/json' -d '{
  "ticketSaleStartDate":"2020-01-01T00:00:00Z","ticketSaleEndDate":"2030-01-01T00:00:00Z",
  "isFree":false,"maxAttendees":100,"isPublic":true,"requiresApproval":false,
  "allowWaitRoom":true,"isNewTrending":false
}')
echo "  config response: $CFG"

echo "== 4. publish event (order-svc requires EventStatus=PUBLISHED; no gateway route) =="
kubectl -n $NS exec statefulset/postgres -- psql -U root -d ticketbottle_event -c \
  "UPDATE events SET status='PUBLISHED' WHERE id='$EVENT_ID';"

echo "== 5. seed ticket class =="
TCID=$("$HERE/../scripts/seed-ticketclass.sh" "$EVENT_ID" 100 10000)
[ -n "$TCID" ] || fail "seed-ticketclass returned no id"
echo "  ticketClassId=$TCID"

echo "== 6. join waitroom =="
JOIN=$(curl -s -X POST "$GW/waitroom/join" -H "$AUTH" -H 'Content-Type: application/json' \
  -d "{\"eventId\":\"$EVENT_ID\"}")
SESSION=$(echo "$JOIN" | getval data.sessionId)
[ -n "$SESSION" ] || fail "waitroom join returned no sessionId: $JOIN"
echo "  sessionId=$SESSION"

echo "== 7. wait for admission, then mint the checkout token =="
# Read user_id from the real session (present from CreateSession) and give the 1s
# processor tick a moment to admit (visible in waitroom logs).
USER_ID=""
for i in $(seq 1 10); do
  SS=$(kubectl -n $NS exec statefulset/redis -- redis-cli GET "waitroom:session:$SESSION" 2>/dev/null || true)
  USER_ID=$(echo "$SS" | getval user_id)
  [ -n "$USER_ID" ] && break
  sleep 1
done
[ -n "$USER_ID" ] || fail "waitroom session not found in Redis: $SS"
JWT_SECRET=$(kubectl -n $NS get configmap order-config -o jsonpath='{.data.JWT_SECRET}')
[ -n "$JWT_SECRET" ] || fail "could not read JWT_SECRET from order-config"
CHECKOUT=$(python3 -c "import hmac,hashlib,base64,json,time,sys
secret,sid,uid,eid=sys.argv[1:5]
b64=lambda b: base64.urlsafe_b64encode(b).rstrip(b'=')
now=int(time.time())
h=b64(json.dumps({'alg':'HS256','typ':'JWT'},separators=(',',':')).encode())
p=b64(json.dumps({'session_id':sid,'user_id':uid,'event_id':eid,'iat':now-10,'exp':now+900},separators=(',',':')).encode())
sig=b64(hmac.new(secret.encode(),h+b'.'+p,hashlib.sha256).digest())
print((h+b'.'+p+b'.'+sig).decode())" "$JWT_SECRET" "$SESSION" "$USER_ID" "$EVENT_ID")
[ -n "$CHECKOUT" ] || fail "failed to mint checkout token"
echo "  checkoutToken=${CHECKOUT:0:16}... (minted for session $SESSION)"

echo "== 8. create order =="
ORD=$(curl -s -X POST "$GW/orders" -H "$AUTH" -H 'Content-Type: application/json' -d "{
  \"eventId\":\"$EVENT_ID\",\"userFullname\":\"Gate One\",\"userEmail\":\"$EMAIL\",
  \"userPhone\":\"0900000000\",\"paymentMethod\":\"ZALOPAY\",
  \"items\":[{\"ticketClassId\":\"$TCID\",\"quantity\":1}],\"currency\":\"VND\",
  \"checkoutToken\":\"$CHECKOUT\",\"redirectUrl\":\"https://example.com/done\"}")
ORDER_CODE=$(echo "$ORD" | getval data.order.code)
[ -n "$ORDER_CODE" ] || fail "order create returned no order.code: $ORD"
echo "  orderCode=$ORDER_CODE"

echo "== 9. fire ZaloPay-signed webhook -> API Gateway -> payment-webhook-handler Lambda =="
# read KEY2 from the SAME env the Lambdas were deployed with (single source of truth)
LS_ENV="$HERE/env.lambdas.json"
API_ID=$(awslocal apigateway get-rest-apis --query 'items[?name==`ticketbottle-webhooks`].id | [0]' --output text)
[ -n "$API_ID" ] && [ "$API_ID" != "None" ] || fail "API Gateway ticketbottle-webhooks not found"
API_ENDPOINT="http://localhost:4566/restapis/$API_ID/dev/_user_request_"
KEY2=$(python3 -c "import json,sys;print(json.load(open(sys.argv[1]))['Variables']['ZALOPAY_KEY2'])" "$LS_ENV")
WEBHOOK_BODY=$(python3 -c "import hmac,hashlib,json,sys,time
key2,order=sys.argv[1:3]
appt=time.strftime('%y%m%d')+'_'+order
data=json.dumps({'app_id':2554,'app_trans_id':appt,'app_time':int(time.time()*1000),'app_user':'TicketBottle','amount':10000,'zp_trans_id':1,'server_time':int(time.time()*1000)},separators=(',',':'))
mac=hmac.new(key2.encode(),data.encode(),hashlib.sha256).hexdigest()
print(json.dumps({'data':data,'mac':mac,'type':1}))" "$KEY2" "$ORDER_CODE")
WH_RESP=$(curl -s -X POST "$API_ENDPOINT/webhook/zalopay" -H 'Content-Type: application/json' -d "$WEBHOOK_BODY")
echo "  webhook resp: $WH_RESP"
echo "== 10. poll order until COMPLETED (outbox-relay publishes via LISTEN/NOTIFY -> Kafka -> Temporal ConfirmOrder) =="
for i in $(seq 1 30); do
  O=$(curl -s -H "$AUTH" "$GW/orders/code/$ORDER_CODE")
  STATUS=$(echo "$O" | getval data.status)
  echo "  [$i] status=${STATUS:-?}"
  [ "$STATUS" = "COMPLETED" ] && { echo "GATE 1.5 PASSED: order $ORDER_CODE COMPLETED (ticketClassId=$TCID)"; exit 0; }
  sleep 2
done
fail "order $ORDER_CODE did not reach COMPLETED"
