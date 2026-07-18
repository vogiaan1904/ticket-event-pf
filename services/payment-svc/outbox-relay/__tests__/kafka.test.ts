import { topicFor } from '../src/kafka';

test('maps outbox eventType values to their Kafka topics', () => {
  expect(topicFor('PAYMENT_COMPLETED')).toBe('payment.completed');
  expect(topicFor('PAYMENT_FAILED')).toBe('payment.failed');
  expect(topicFor('PAYMENT_CANCELLED')).toBe('payment.cancelled');
});

test('an unknown eventType falls back to payment.failed', () => {
  expect(topicFor('SOMETHING_UNKNOWN')).toBe('payment.failed');
});
