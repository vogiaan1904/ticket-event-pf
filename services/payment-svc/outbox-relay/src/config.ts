export const BATCH = Number(process.env.OUTBOX_BATCH_SIZE ?? 100);

// Invariant: the claim and the exhausted-rows gauge cut at this same count.
export const MAX_RETRIES = Number(process.env.OUTBOX_MAX_RETRIES ?? 5);
