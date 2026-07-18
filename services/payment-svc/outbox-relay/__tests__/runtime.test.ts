import { createDrainScheduler } from '../src/runtime';

const tick = () => new Promise((r) => setImmediate(r));

test('overlapping triggers coalesce into exactly one follow-up drain', async () => {
  let active = 0;
  let maxActive = 0;
  let calls = 0;
  let release!: () => void;
  const drain = jest.fn(async () => {
    calls++;
    active++;
    maxActive = Math.max(maxActive, active);
    await new Promise<void>((r) => {
      release = r;
    });
    active--;
  });
  const sched = createDrainScheduler(drain);
  sched.trigger(); // starts drain #1
  await tick();
  sched.trigger(); // arrives mid-drain -> should set pending, not start #2
  sched.trigger(); // another -> still just one pending
  expect(maxActive).toBe(1); // never concurrent
  release();
  await tick(); // #1 finishes -> one coalesced follow-up runs
  release();
  await tick(); // follow-up finishes
  expect(calls).toBe(2); // exactly two drains total, not three
});
