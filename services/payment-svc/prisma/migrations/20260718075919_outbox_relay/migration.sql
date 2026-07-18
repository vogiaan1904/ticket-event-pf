-- DropIndex (Prisma's old composite index name)
DROP INDEX IF EXISTS "outbox_published_createdAt_idx";

-- DropColumn
ALTER TABLE "outbox" DROP COLUMN IF EXISTS "published";

-- Partial index that actually serves the relay's hot query
CREATE INDEX IF NOT EXISTS "idx_outbox_unpublished"
  ON "outbox" ("createdAt")
  WHERE "publishedAt" IS NULL;

-- NOTIFY on every insert so the long-lived relay wakes immediately
CREATE OR REPLACE FUNCTION outbox_notify() RETURNS trigger AS $$
BEGIN
  PERFORM pg_notify('outbox_new', NEW.id::text);
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS outbox_notify_trg ON "outbox";
CREATE TRIGGER outbox_notify_trg
  AFTER INSERT ON "outbox"
  FOR EACH ROW EXECUTE FUNCTION outbox_notify();
