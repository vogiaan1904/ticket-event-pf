// Holds a constant rate of GET /events/:id while a rollout-drain.sh run restarts pods,
// and logs every non-200 as one JSON line. Status 0 is a connection-level failure.
// The route is the probe because the gateway calls event-svc directly: no Temporal
// retry absorbs a refused reconnect, and no sign-in throttle measures the limiter.
import http from 'k6/http';
import crypto from 'k6/crypto';
import encoding from 'k6/encoding';
import { Counter } from 'k6/metrics';

const GW            = __ENV.GW;              // http://app-gateway:3000/api
const EVENT_ID      = __ENV.EVENT_ID;
const ACCESS_SECRET = __ENV.ACCESS_SECRET;   // gateway: access token

const failures = new Counter('tb_probe_failures');

export const options = {
  scenarios: {
    probe: {
      executor: 'constant-arrival-rate',
      rate: Number(__ENV.RATE || 50),
      timeUnit: '1s',
      duration: __ENV.DURATION,
      preAllocatedVUs: 20,
      maxVUs: 200,
    },
  },
};

const b64url = (s) => encoding.b64encode(s, 'rawurl');

function sign(payload, secret) {
  const h = b64url(JSON.stringify({ alg: 'HS256', typ: 'JWT' }));
  const p = b64url(JSON.stringify(payload));
  return `${h}.${p}.${crypto.hmac('sha256', secret, `${h}.${p}`, 'base64rawurl')}`;
}

export function setup() {
  const now = Math.floor(Date.now() / 1000);
  return { token: sign({ sub: 'rollout-probe', email: 'rollout-probe@example.com',
                         iat: now - 10, exp: now + 3600 }, ACCESS_SECRET) };
}

export default function (data) {
  const res = http.get(`${GW}/events/${EVENT_ID}`, {
    headers: { Authorization: `Bearer ${data.token}` },
    timeout: '5s',
  });
  if (res.status !== 200) {
    failures.add(1, { status: String(res.status) });
    console.log(JSON.stringify({ t: new Date().toISOString(), status: res.status,
                                 error: res.error_code ? res.error : undefined }));
  }
}
