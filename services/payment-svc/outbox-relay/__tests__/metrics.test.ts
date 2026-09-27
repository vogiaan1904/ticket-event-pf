import { register } from 'prom-client';
import { countUnpublished } from '../../lambdas/common/db/outbox.repo';
import { refreshOutboxRows } from '../src/metrics';

jest.mock('../../lambdas/common/db/outbox.repo');
// Not the default of 5, so a hard-coded cut-off in metrics.ts fails the test.
jest.mock('../src/config', () => ({ MAX_RETRIES: 7 }));

const db = {} as Parameters<typeof refreshOutboxRows>[0];
const gauge = async (name: string) => (await register.getSingleMetric(name)!.get()).values[0].value;

beforeEach(() => {
  jest.mocked(countUnpublished).mockResolvedValue({ pending: 3, exhausted: 2 });
});

test('refreshOutboxRows sets the pending and exhausted gauges apart', async () => {
  await refreshOutboxRows(db);
  expect(await gauge('tb_outbox_pending_rows')).toBe(3);
  expect(await gauge('tb_outbox_exhausted_rows')).toBe(2);
});

test('refreshOutboxRows counts at the retry cut-off the claim uses', async () => {
  await refreshOutboxRows(db);
  expect(countUnpublished).toHaveBeenCalledWith(db, 7);
});
