// One virtual user = one attempted purchase through the real chain:
//   signup -> waitroom join -> mint checkout token -> create order
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
const JWT_SECRET      = __ENV.JWT_SECRET;

const completed  = new Counter('tb_orders_completed');
const soldOut    = new Counter('tb_sold_out_4xx');
const unexpected = new Counter('tb_unexpected_errors');

export const options = {
  scenarios: {
    rush: {
      executor: 'per-vu-iterations',
      vus: Number(__ENV.VUS || 300),
      iterations: 1,
      maxDuration: '6m',
    },
  },
  thresholds: {
    // A "sold out" rejection is a PASS. A 5xx, a hang, or a silent success
    // beyond the allotment is not. This is the assertion that matters.
    tb_unexpected_errors: ['count==0'],
  },
};

const b64url = (s) => encoding.b64encode(s, 'rawurl');

function jwtClaims(token) {
  const payload = token.split('.')[1];
  return JSON.parse(encoding.b64decode(payload, 'rawurl', 's'));
}

function mintCheckoutToken(sessionId, userId, eventId) {
  const now = Math.floor(Date.now() / 1000);
  const h = b64url(JSON.stringify({ alg: 'HS256', typ: 'JWT' }));
  const p = b64url(JSON.stringify({
    session_id: sessionId, user_id: userId, event_id: eventId,
    iat: now - 10, exp: now + 900,
  }));
  const sig = crypto.hmac('sha256', JWT_SECRET, `${h}.${p}`, 'base64rawurl');
  return `${h}.${p}.${sig}`;
}

export default function () {
  const json = { headers: { 'Content-Type': 'application/json' } };

  // 1. signup — a unique user per VU per run
  const email = `load-${__VU}-${Date.now()}@example.com`;
  const su = http.post(`${GW}/auth/signup`, JSON.stringify({
    firstName: 'Load', lastName: `VU${__VU}`, email, password: 'Password123!',
  }), json);
  if (su.status !== 201 && su.status !== 200) {
    unexpected.add(1, { step: 'signup', status: su.status });
    return;
  }
  const accessToken = su.json('data.accessToken');
  if (!accessToken) { unexpected.add(1, { step: 'signup-token' }); return; }
  const userId = jwtClaims(accessToken).sub;
  const auth = { headers: { 'Content-Type': 'application/json',
                            Authorization: `Bearer ${accessToken}` } };

  // 2. join the waitroom
  const jr = http.post(`${GW}/waitroom/join`,
    JSON.stringify({ eventId: EVENT_ID }), auth);
  const sessionId = jr.json('data.sessionId');
  if (!sessionId) { unexpected.add(1, { step: 'waitroom', status: jr.status }); return; }

  // 3. create the order. order-svc only verifies the checkout token
  //    cryptographically (HS256, shared JWT_SECRET), so minting it here is
  //    byte-equivalent to what waitroom signs — see gate1-purchase-flow.sh.
  const or = http.post(`${GW}/orders`, JSON.stringify({
    eventId: EVENT_ID, userFullname: `Load VU${__VU}`, userEmail: email,
    userPhone: '0900000000', paymentMethod: 'ZALOPAY',
    items: [{ ticketClassId: TICKET_CLASS_ID, quantity: 1 }],
    currency: 'VND',
    checkoutToken: mintCheckoutToken(sessionId, userId, EVENT_ID),
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
  for (let i = 0; i < 30; i++) {
    const o = http.get(`${GW}/orders/code/${code}`, auth);
    if (o.json('data.status') === 'COMPLETED') { completed.add(1); return; }
    sleep(2);
  }
  unexpected.add(1, { step: 'never-completed', code });
}