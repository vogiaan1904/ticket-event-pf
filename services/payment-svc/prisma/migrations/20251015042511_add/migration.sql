-- CreateTable
CREATE TABLE "outbox" (
    "id" TEXT NOT NULL,
    "aggregateId" TEXT NOT NULL,
    "aggregateType" TEXT NOT NULL,
    "eventType" TEXT NOT NULL,
    "payload" JSONB NOT NULL,
    "published" BOOLEAN NOT NULL DEFAULT false,
    "publishedAt" TIMESTAMP(3),
    "createdAt" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "retryCount" INTEGER NOT NULL DEFAULT 0,
    "lastError" TEXT,

    CONSTRAINT "outbox_pkey" PRIMARY KEY ("id")
);

-- CreateIndex
CREATE INDEX "outbox_published_createdAt_idx" ON "outbox"("published", "createdAt");
