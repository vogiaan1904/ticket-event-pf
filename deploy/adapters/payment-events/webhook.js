const http = require('http');
const { Pool } = require('pg');
const { randomUUID } = require('crypto');

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
      `INSERT INTO outbox (id, "aggregateId", "aggregateType", "eventType", payload, published, "createdAt", "retryCount")
       VALUES ($1, $2, 'Payment', 'PAYMENT_COMPLETED', $3, false, now(), 0)`,
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
      res.writeHead(r.code, { 'Content-Type': 'application/json' }).end(JSON.stringify(r.payload || { error: 'payment not found' }));
    } catch (e) { console.error(e); res.writeHead(500).end(JSON.stringify({ error: e.message })); }
    return;
  }
  res.writeHead(404).end();
}).listen(8080, () => console.log('payment-webhook listening on :8080'));
