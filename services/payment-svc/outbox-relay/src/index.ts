import { startRuntime } from './runtime';
import { logger } from './logger';

(async () => {
  const shutdown = await startRuntime();
  // k8s sends SIGTERM on pod stop; SIGINT covers local Ctrl-C.
  for (const sig of ['SIGTERM', 'SIGINT'] as const) {
    process.on(sig, async () => {
      logger.info(`received ${sig}`);
      await shutdown();
      process.exit(0);
    });
  }
})().catch((e) => {
  logger.error('relay failed to start', { error: e.message });
  process.exit(1);
});
