const http = require('http');
const { Pool } = require('pg');
const { randomUUID } = require('crypto');
const { Counter, collectDefaultMetrics, register } = require('prom-client');

const METRICS_PORT = Number(process.env.SERVER_METRICS_PORT || 2112);

collectDefaultMetrics();

// result is about the EVENT, not about whose fault it was: a 500 means the
// event was not accepted, the same as a 404.
const webhookEvents = new Counter({
  name: 'tb_webhook_events_total',
  help: 'Provider webhook events received, by event type and outcome.',
  labelNames: ['type', 'result'],
});

const EVENT_TYPE = 'PAYMENT_COMPLETED';

const pool = new Pool({ connectionString: process.env.DATABASE_URL });

async function complete(orderCode) {
  const client = await pool.connect();
  try {
    await client.query('BEGIN');
    const pay = await client.query(
      `UPDATE payments SET status = 'COMPLETED', "completedAt" = now()
       WHERE "orderCode" = $1 RETURNING id, "amountCents", currency, provider`,
      [orderCode],
    );
    if (pay.rowCount === 0) { await client.query('ROLLBACK'); return { code: 404 }; }
    const p = pay.rows[0];
    const payload = {
      order_code: orderCode, payment_id: p.id, amount_cents: p.amountCents,
      currency: p.currency, provider: p.provider, paid_at: new Date().toISOString(),
    };
    await client.query(
      `INSERT INTO outbox (id, "aggregateId", "aggregateType", "eventType", payload, "createdAt", "retryCount")
       VALUES ($1, $2, 'Payment', 'PAYMENT_COMPLETED', $3, now(), 0)`,
      [randomUUID(), p.id, JSON.stringify(payload)],
    );
    await client.query('COMMIT');
    return { code: 200, payload };
  } catch (e) { await client.query('ROLLBACK'); throw e; }
  finally { client.release(); }
}

http.createServer(async (req, res) => {
  if (req.method === 'GET' && req.url === '/healthz') { res.writeHead(200).end('ok'); return; }
  const m = req.url.match(/^\/complete\/(.+)$/);
  if (req.method === 'POST' && m) {
    try {
      const r = await complete(decodeURIComponent(m[1]));
      webhookEvents.inc({ type: EVENT_TYPE, result: r.code === 200 ? 'accepted' : 'rejected' });
      res.writeHead(r.code, { 'Content-Type': 'application/json' }).end(JSON.stringify(r.payload || { error: 'payment not found' }));
    } catch (e) {
      webhookEvents.inc({ type: EVENT_TYPE, result: 'rejected' });
      console.error(e); res.writeHead(500).end(JSON.stringify({ error: e.message }));
    }
    return;
  }
  res.writeHead(404).end();
}).listen(8080, () => console.log('payment-webhook listening on :8080'));

// A second listener, not a route on 8080: the ServiceMonitor selects a port
// named `metrics`, and the webhook port is public.
http.createServer(async (req, res) => {
  if (req.url !== '/metrics') { res.writeHead(404).end(); return; }
  try {
    res.writeHead(200, { 'Content-Type': register.contentType }).end(await register.metrics());
  } catch (e) { res.writeHead(500).end(); }
}).listen(METRICS_PORT, () => console.log(`payment-webhook metrics on :${METRICS_PORT}`));
