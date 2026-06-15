/*
  Warnings:

  - You are about to drop the column `ownerId` on the `events` table. All the data in the column will be lost.
  - Added the required column `ownerId` to the `organizers` table without a default value. This is not possible if the table is not empty.

*/
-- CreateEnum
CREATE TYPE "public"."event_role_types" AS ENUM ('ADMIN', 'EDITOR', 'VIEWER');

-- CreateEnum
CREATE TYPE "public"."organizer_member_roles" AS ENUM ('ADMIN', 'MEMBER');

-- AlterTable
ALTER TABLE "public"."events" DROP COLUMN "ownerId";

-- AlterTable
ALTER TABLE "public"."organizers" ADD COLUMN     "ownerId" TEXT NOT NULL,
ALTER COLUMN "logoUrl" DROP NOT NULL;

-- CreateTable
CREATE TABLE "public"."event_roles" (
    "id" TEXT NOT NULL,
    "userId" TEXT NOT NULL,
    "eventId" TEXT NOT NULL,
    "role" "public"."event_role_types" NOT NULL,
    "createdAt" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "updatedAt" TIMESTAMP(3) NOT NULL,

    CONSTRAINT "event_roles_pkey" PRIMARY KEY ("id")
);

-- CreateTable
CREATE TABLE "public"."organizer_members" (
    "id" TEXT NOT NULL,
    "userId" TEXT NOT NULL,
    "organizerId" TEXT NOT NULL,
    "role" "public"."organizer_member_roles" NOT NULL DEFAULT 'MEMBER',
    "createdAt" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT "organizer_members_pkey" PRIMARY KEY ("id")
);

-- CreateIndex
CREATE UNIQUE INDEX "event_roles_userId_eventId_key" ON "public"."event_roles"("userId", "eventId");

-- CreateIndex
CREATE UNIQUE INDEX "organizer_members_userId_organizerId_key" ON "public"."organizer_members"("userId", "organizerId");

-- AddForeignKey
ALTER TABLE "public"."event_roles" ADD CONSTRAINT "event_roles_eventId_fkey" FOREIGN KEY ("eventId") REFERENCES "public"."events"("id") ON DELETE CASCADE ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "public"."organizer_members" ADD CONSTRAINT "organizer_members_organizerId_fkey" FOREIGN KEY ("organizerId") REFERENCES "public"."organizers"("id") ON DELETE CASCADE ON UPDATE CASCADE;
