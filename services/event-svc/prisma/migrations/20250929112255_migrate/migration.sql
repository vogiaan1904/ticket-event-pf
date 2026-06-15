/*
  Warnings:

  - You are about to drop the column `status` on the `event_configs` table. All the data in the column will be lost.
  - You are about to drop the column `createdAt` on the `event_locations` table. All the data in the column will be lost.
  - You are about to drop the column `updatedAt` on the `event_locations` table. All the data in the column will be lost.

*/
-- AlterTable
ALTER TABLE "public"."event_configs" DROP COLUMN "status";

-- AlterTable
ALTER TABLE "public"."event_locations" DROP COLUMN "createdAt",
DROP COLUMN "updatedAt";

-- AlterTable
ALTER TABLE "public"."events" ADD COLUMN     "status" "public"."EventStatus" NOT NULL DEFAULT 'DRAFT';
