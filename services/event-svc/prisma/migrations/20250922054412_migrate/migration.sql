/*
  Warnings:

  - You are about to drop the column `organizerId` on the `events` table. All the data in the column will be lost.
  - You are about to drop the column `ownerId` on the `organizers` table. All the data in the column will be lost.
  - You are about to drop the `locations` table. If the table is not empty, all the data it contains will be lost.
  - You are about to drop the `organizer_members` table. If the table is not empty, all the data it contains will be lost.
  - A unique constraint covering the columns `[eventId]` on the table `organizers` will be added. If there are existing duplicate values, this will fail.

*/
-- DropForeignKey
ALTER TABLE "public"."events" DROP CONSTRAINT "events_organizerId_fkey";

-- DropForeignKey
ALTER TABLE "public"."locations" DROP CONSTRAINT "locations_eventId_fkey";

-- DropForeignKey
ALTER TABLE "public"."organizer_members" DROP CONSTRAINT "organizer_members_organizerId_fkey";

-- AlterTable
ALTER TABLE "public"."events" DROP COLUMN "organizerId",
ADD COLUMN     "createdAt" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP;

-- AlterTable
ALTER TABLE "public"."organizers" DROP COLUMN "ownerId",
ADD COLUMN     "eventId" TEXT;

-- DropTable
DROP TABLE "public"."locations";

-- DropTable
DROP TABLE "public"."organizer_members";

-- DropEnum
DROP TYPE "public"."organizer_member_roles";

-- CreateTable
CREATE TABLE "public"."event_locations" (
    "id" TEXT NOT NULL,
    "eventId" TEXT NOT NULL,
    "name" TEXT NOT NULL,
    "street" TEXT,
    "city" TEXT,
    "postalCode" TEXT,
    "state" TEXT,
    "province" TEXT,
    "district" TEXT,
    "ward" TEXT,
    "country" TEXT NOT NULL,
    "createdAt" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "updatedAt" TIMESTAMP(3) NOT NULL,

    CONSTRAINT "event_locations_pkey" PRIMARY KEY ("id")
);

-- CreateIndex
CREATE UNIQUE INDEX "event_locations_eventId_key" ON "public"."event_locations"("eventId");

-- CreateIndex
CREATE UNIQUE INDEX "organizers_eventId_key" ON "public"."organizers"("eventId");

-- AddForeignKey
ALTER TABLE "public"."event_locations" ADD CONSTRAINT "event_locations_eventId_fkey" FOREIGN KEY ("eventId") REFERENCES "public"."events"("id") ON DELETE RESTRICT ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "public"."organizers" ADD CONSTRAINT "organizers_eventId_fkey" FOREIGN KEY ("eventId") REFERENCES "public"."events"("id") ON DELETE SET NULL ON UPDATE CASCADE;
