import { Kysely, PostgresDialect } from 'kysely';
import { Pool } from 'pg';
import { Database } from './types';

let db: Kysely<Database> | null = null;
let pool: Pool | null = null;

export const getPool = (): Pool => {
  if (!pool) {
    if (!process.env.DATABASE_URL) throw new Error('DATABASE_URL is required');
    pool = new Pool({
      connectionString: process.env.DATABASE_URL,
      max: Number(process.env.PG_POOL_MAX ?? 5),
    });
  }
  return pool;
};

export const getDb = (): Kysely<Database> => {
  if (!db) db = new Kysely<Database>({ dialect: new PostgresDialect({ pool: getPool() }) });
  return db;
};

export const closeDb = async (): Promise<void> => {
  if (db) {
    await db.destroy();
    db = null;
    pool = null;
  }
};
