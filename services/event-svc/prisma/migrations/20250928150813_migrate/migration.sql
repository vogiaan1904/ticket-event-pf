/*
  Warnings:

  - You are about to drop the column `categories` on the `events` table. All the data in the column will be lost.

*/
-- AlterTable
ALTER TABLE "public"."events" DROP COLUMN "categories";

-- DropEnum
DROP TYPE "public"."Category";

-- CreateTable
CREATE TABLE "public"."categories" (
    "id" TEXT NOT NULL,
    "name" TEXT NOT NULL,
    "createdAt" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "updatedAt" TIMESTAMP(3) NOT NULL,

    CONSTRAINT "categories_pkey" PRIMARY KEY ("id")
);

-- CreateTable
CREATE TABLE "public"."event_categories" (
    "id" TEXT NOT NULL,
    "eventId" TEXT NOT NULL,
    "categoryId" TEXT NOT NULL,
    "createdAt" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "updatedAt" TIMESTAMP(3) NOT NULL,

    CONSTRAINT "event_categories_pkey" PRIMARY KEY ("id")
);

-- CreateIndex
CREATE UNIQUE INDEX "event_categories_eventId_categoryId_key" ON "public"."event_categories"("eventId", "categoryId");

-- AddForeignKey
ALTER TABLE "public"."event_categories" ADD CONSTRAINT "event_categories_eventId_fkey" FOREIGN KEY ("eventId") REFERENCES "public"."events"("id") ON DELETE RESTRICT ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "public"."event_categories" ADD CONSTRAINT "event_categories_categoryId_fkey" FOREIGN KEY ("categoryId") REFERENCES "public"."categories"("id") ON DELETE RESTRICT ON UPDATE CASCADE;
