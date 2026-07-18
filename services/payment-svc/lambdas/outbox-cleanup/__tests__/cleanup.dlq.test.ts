import { routeExhaustedEvents } from '../handlers/cleanup.handler';

jest.mock('@/common/logger');

test('sends each exhausted row to the DLQ and emits a metric with the count', async () => {
  const sendMessage = jest.fn().mockResolvedValue({});
  const putMetric = jest.fn().mockResolvedValue({});
  const failed = [
    { id: 'x', aggregateId: 'p1', eventType: 'PaymentCompleted', retryCount: 3, lastError: 'boom' },
  ];
  await routeExhaustedEvents(failed as any, { sendMessage, putMetric, dlqUrl: 'q' });
  expect(sendMessage).toHaveBeenCalledTimes(1);
  expect(putMetric).toHaveBeenCalledWith(expect.objectContaining({ value: 1 }));
});

test('no failed rows => no DLQ sends, metric value 0', async () => {
  const sendMessage = jest.fn();
  const putMetric = jest.fn().mockResolvedValue({});
  await routeExhaustedEvents([], { sendMessage, putMetric, dlqUrl: 'q' });
  expect(sendMessage).not.toHaveBeenCalled();
  expect(putMetric).toHaveBeenCalledWith(expect.objectContaining({ value: 0 }));
});
