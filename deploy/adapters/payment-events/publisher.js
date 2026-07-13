const { Pool } = require('pg');
const { Kafka } = require('kafkajs');

const TOPIC_MAP = {
  PAYMENT_COMPLETED: 'payment.completed',
  PAYMENT_FAILED: 'payment.failed',
  PAYMENT_CANCELLED: 'payment.cancelled',
};

const pool = new Pool({ connectionString: process.env.DATABASE_URL });
const kafka = new Kafka({ clientId: 'payment-events', brokers: (process.env.KAFKA_BROKERS || 'redpanda:9093').split(',') });
const producer = kafka.producer();

async function tick() {
  const { rows } = await pool.query(
    'SELECT id, "eventType", payload FROM outbox WHERE published = false ORDER BY "createdAt" ASC LIMIT 50',
  );
  for (const row of rows) {
    const topic = TOPIC_MAP[row.eventType];
    if (!topic) { console.warn('unknown eventType', row.eventType); continue; }
    await producer.send({ topic, messages: [{ value: JSON.stringify(row.payload) }] });
    await pool.query('UPDATE outbox SET published = true, "publishedAt" = now() WHERE id = $1', [row.id]);
    console.log('published', { id: row.id, topic, order_code: row.payload.order_code });
  }
}

(async () => {
  await producer.connect();
  console.log('outbox-publisher connected; polling every 2s');
  setInterval(() => tick().catch((e) => console.error('tick error', e.message)), 2000);
})();
