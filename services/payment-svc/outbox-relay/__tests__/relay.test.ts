import { drainOnce } from '../src/relay';

const rows = [
  { id: 'a', aggregateId: 'p1', aggregateType: 'payment', eventType: 'PaymentCompleted', payload: {} },
  { id: 'b', aggregateId: 'p2', aggregateType: 'payment', eventType: 'PaymentCompleted', payload: {} },
];

test('publishes each claimed row and marks the successful ones in one batch', async () => {
  const published: string[] = [];
  const markedPublished: string[][] = [];
  const deps = {
    claim: jest.fn().mockResolvedValueOnce(rows),
    publish: jest.fn(async (r: any) => { published.push(r.id); }),
    markPublished: jest.fn(async (ids: string[]) => { markedPublished.push(ids); }),
    markFailed: jest.fn(),
    topicFor: () => 'payment-completed',
    batchSize: 50, maxRetries: 3,
  };
  const res = await drainOnce(deps as any);
  expect(published.sort()).toEqual(['a', 'b']);
  expect(markedPublished).toEqual([['a', 'b']]);   // single batched mark
  expect(res).toEqual({ processed: 2, succeeded: 2, failed: 0 });
});

test('a publish failure increments retry, does not mark published', async () => {
  const deps = {
    claim: jest.fn().mockResolvedValueOnce(rows),
    publish: jest.fn(async (r: any) => { if (r.id === 'b') throw new Error('kafka down'); }),
    markPublished: jest.fn(),
    markFailed: jest.fn(),
    topicFor: () => 'payment-completed',
    batchSize: 50, maxRetries: 3,
  };
  const res = await drainOnce(deps as any);
  expect(deps.markPublished).toHaveBeenCalledWith(['a']);
  expect(deps.markFailed).toHaveBeenCalledWith('b', expect.stringContaining('kafka down'));
  expect(res).toEqual({ processed: 2, succeeded: 1, failed: 1 });
});
