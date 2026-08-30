// One virtual user = one attempted purchase through the real chain:
//   authenticate -> waitroom join -> wait for admission -> create order
//   -> fire the payment webhook -> poll until COMPLETED
//
// The event, its config and the ticket class are seeded ONCE by
// gate4a-load.sh before this Job starts, and arrive as env vars. Seeding
// per-VU would measure event creation, not the purchase path.
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
const soldOut    = new Counter('tb_sold_out_4xx');
const unexpected = new Counter('tb_unexpected_errors');

// Two shapes, one script. Gate 4a wants a THUNDERING HERD: every buyer arrives
// at once, contends for a fixed allotment, and leaves. Gate 4b wants a SUSTAINED
// STREAM: it has to terminate a node while orders are genuinely mid-saga, and a
// one-shot burst drains in ~10s — long before any fixed sleep expires, which is
// how a chaos test ends up killing an idle cluster and proving nothing.
const DURATION = __ENV.DURATION;   // set => chaos mode

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
  // A "sold out" rejection is a PASS. A 5xx, a hang, or a silent success beyond
  // the allotment is not. This is the assertion that matters.
  //
  // In chaos mode it is deliberately dropped: we are terminating a node under
  // this load, so in-flight requests to the dying pod SHOULD fail. Counting
  // those as a threshold breach would fail the run for doing its job. Gate 4b's
  // own assertions on reservations and the outbox are the verdict there.
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
  //    minted here — signing up in-band costs a bcrypt hash per VU and saturates
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

  if (or.status >= 400 && or.status < 500) { soldOut.add(1); return; }   // correct rejection
  if (or.status >= 500) { unexpected.add(1, { step: 'order', status: or.status }); return; }

  const code = or.json('data.order.code');
  if (!code) { unexpected.add(1, { step: 'order-code' }); return; }

  // 4. complete the payment
  const wh = http.post(`${WEBHOOK}/complete/${code}`);
  if (wh.status >= 400) { unexpected.add(1, { step: 'webhook', status: wh.status }); return; }

  // 5. poll to COMPLETED (Kafka -> Temporal ConfirmOrder)
  // The confirm leg is asynchronous (webhook -> outbox -> Kafka -> ConfirmOrder)
  // and lags under load; polling for less than it takes reports a working saga
  // as a failure.
  for (let i = 0; i < CONFIRM_POLLS; i++) {
    const o = http.get(`${GW}/orders/code/${code}`, auth);
    if (o.json('data.status') === 'COMPLETED') { completed.add(1); return; }
    sleep(2);
  }
  unexpected.add(1, { step: 'never-completed', code });
}