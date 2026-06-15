import { defineConfig } from 'prisma/config';
import { config } from 'dotenv';
import { env } from 'process';

config();
export default defineConfig({
  schema: './prisma/schema',

  migrations: {
    path: './prisma/migrations',
    seed: `ts-node -O '{"module":"CommonJS"}' prisma/seed.ts`,
  },
  views: {
    path: './prisma/views',
  },
});
