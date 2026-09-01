// One virtual user = one attempted purchase through the real chain:
//   authenticate -> waitroom join -> wait for admission -> create order
//   -> fire the payment webhook -> poll until COMPLETED
//
// The event, its config and the ticket class are seeded once by gate4a-load.sh
// before this Job starts and arrive as env vars; seeding per-VU would measure
// event creation rather than the purchase path.
import http from 'k6/http';
import crypto from 'k6/crypto';
import encoding from 'k6/encoding';
import { Counter } from 'k6/metrics';
import { sleep } from 'k6';

const GW              = __ENV.GW;                 // http://app-gateway:3000/api
const WEBHOOK         = __ENV.WEBHOOK;            // http://payment-webhook:8080
const EVENT_ID        = __ENV.EVENT_ID;
const TICKET_CLASS_ID = __ENV.TICKET_CLASS_ID;
const ACCESS_SECRET   = __ENV.ACCESS_SECRET;      // gateway: access token
const USER_PREFIX     = __ENV.USER_PREFIX;        // set => buyers are pre-seeded, skip signup
const ADMIT_POLLS     = Number(__ENV.ADMIT_POLLS || 180);
const CONFIRM_POLLS   = Number(__ENV.CONFIRM_POLLS || 90);   // x2s = 3min

const completed  = new Counter('tb_orders_completed');
const rejected   = new Counter('tb_orders_rejected_409');
const unexpected = new Counter('tb_unexpected_errors');

// Two load shapes, one script. Gate 4a wants a burst: every buyer arrives at
// once, contends for a fixed allotment and leaves. Gate 4b needs a sustained
// stream instead, because it terminates a node mid-saga and a burst drains in
// ~10s — long before the kill lands.
const DURATION = __ENV.DURATION;   // set => sustained run

export const options = {
  scenarios: {
    rush: DURATION
      ? {
          // Each VU loops for the whole window. Safe to repeat as the same
          // buyer: waitroom JoinQueue calls CreateSession unconditionally, so a
          // rejoin yields a fresh session and a fresh checkout token.
          executor: 'constant-vus',
          vus: Number(__ENV.VUS || 20),
          duration: DURATION,
        }
      : {
          executor: 'per-vu-iterations',
          vus: Number(__ENV.VUS || 300),
          iterations: 1,
          maxDuration: '12m',
        },
  },
  // A sold-out rejection is a pass; a 5xx or a hang is not.
  //
  // Dropped in a sustained run: Gate 4b terminates a node under this load, so
  // in-flight requests to the dying pod are expected to fail. That gate's own
  // assertions on reservations and the outbox are the verdict there.
  thresholds: DURATION ? {} : {
    tb_unexpected_errors: ['count==0'],
  },
};

const b64url = (s) => encoding.b64encode(s, 'rawurl');

function jwtClaims(token) {
  const payload = token.split('.')[1];
  return JSON.parse(encoding.b64decode(payload, 'rawurl', 's'));
}

function sign(payload, secret) {
  const h = b64url(JSON.stringify({ alg: 'HS256', typ: 'JWT' }));
  const p = b64url(JSON.stringify(payload));
  return `${h}.${p}.${crypto.hmac('sha256', secret, `${h}.${p}`, 'base64rawurl')}`;
}

export default function () {
  const json = { headers: { 'Content-Type': 'application/json' } };

  // 1. authenticate. With USER_PREFIX the buyer already exists and the token is
  //    minted here: signing up in-band costs a bcrypt hash per VU and saturates
  //    the gateway before inventory is ever contended.
  let userId, email, accessToken;
  if (USER_PREFIX) {
    const now = Math.floor(Date.now() / 1000);
    userId = `${USER_PREFIX}${__VU}`;
    email = `${userId}@example.com`;
    accessToken = sign({ sub: userId, email, iat: now - 10, exp: now + 3600 }, ACCESS_SECRET);
  } else {
    email = `load-${__VU}-${Date.now()}@example.com`;
    const su = http.post(`${GW}/auth/signup`, JSON.stringify({
      firstName: 'Load', lastName: `VU${__VU}`, email, password: 'Password123!',
    }), json);
    if (su.status !== 201 && su.status !== 200) {
      unexpected.add(1, { step: 'signup', status: su.status });
      return;
    }
    accessToken = su.json('data.accessToken');
    if (!accessToken) { unexpected.add(1, { step: 'signup-token' }); return; }
    userId = jwtClaims(accessToken).sub;
  }
  const auth = { headers: { 'Content-Type': 'application/json',
                            Authorization: `Bearer ${accessToken}` } };

  // 2. join the waitroom and wait to be admitted. Forging the checkout token
  //    here would bypass admission control and put every VU into checkout at
  //    once — the exact concurrency the queue exists to prevent.
  const jr = http.post(`${GW}/waitroom/join`,
    JSON.stringify({ eventId: EVENT_ID }), auth);
  const sessionId = jr.json('data.sessionId');
  if (!sessionId) { unexpected.add(1, { step: 'waitroom', status: jr.status }); return; }

  let checkoutToken;
  for (let i = 0; i < ADMIT_POLLS; i++) {
    const st = http.get(`${GW}/waitroom/status/${sessionId}`, auth);
    checkoutToken = st.json('data.checkoutToken');
    if (checkoutToken) break;
    const status = st.json('data.status');
    if (status === 'EXPIRED' || status === 'CANCELLED' || status === 'FAILED') {
      unexpected.add(1, { step: 'admission', status });
      return;
    }
    sleep(1);
  }
  if (!checkoutToken) { unexpected.add(1, { step: 'admission-timeout' }); return; }

  // 3. create the order
  const or = http.post(`${GW}/orders`, JSON.stringify({
    eventId: EVENT_ID, userFullname: `Load VU${__VU}`, userEmail: email,
    userPhone: '0900000000', paymentMethod: 'ZALOPAY',
    items: [{ ticketClassId: TICKET_CLASS_ID, quantity: 1 }],
    currency: 'VND',
    checkoutToken,
    redirectUrl: 'https://example.com/done',
  }), auth);

  // 409 is the only correct rejection: sold out, sale closed and wrong-state
  // all arrive as FAILED_PRECONDITION. Any other 4xx is the harness at fault --
  // a stale token, a malformed body -- and counting it as a buyer who lost the
  // race is how a broken run passes as a green one.
  if (or.status === 409) { rejected.add(1); return; }
  if (or.status >= 400) { unexpected.add(1, { step: 'order', status: or.status }); return; }

  const code = or.json('data.order.code');
  if (!code) { unexpected.add(1, { step: 'order-code' }); return; }

  // 4. complete the payment
  const wh = http.post(`${WEBHOOK}/complete/${code}`);
  if (wh.status >= 400) { unexpected.add(1, { step: 'webhook', status: wh.status }); return; }

  // 5. poll to COMPLETED. The confirm leg is asynchronous (webhook -> outbox ->
  //    Kafka -> ConfirmOrder) and lags under load, so the poll budget has to
  //    cover it or a working saga reads as a failure.
  for (let i = 0; i < CONFIRM_POLLS; i++) {
    const o = http.get(`${GW}/orders/code/${code}`, auth);
    if (o.json('data.status') === 'COMPLETED') { completed.add(1); return; }
    sleep(2);
  }
  unexpected.add(1, { step: 'never-completed', code });
}
