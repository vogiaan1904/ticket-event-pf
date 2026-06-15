/*
  Warnings:

  - You are about to drop the column `name` on the `event_locations` table. All the data in the column will be lost.
  - You are about to drop the column `postalCode` on the `event_locations` table. All the data in the column will be lost.
  - You are about to drop the column `province` on the `event_locations` table. All the data in the column will be lost.
  - You are about to drop the column `state` on the `event_locations` table. All the data in the column will be lost.
  - Added the required column `venue` to the `event_locations` table without a default value. This is not possible if the table is not empty.
  - Made the column `street` on table `event_locations` required. This step will fail if there are existing NULL values in that column.
  - Made the column `city` on table `event_locations` required. This step will fail if there are existing NULL values in that column.

*/
-- AlterTable
ALTER TABLE "public"."event_locations" DROP COLUMN "name",
DROP COLUMN "postalCode",
DROP COLUMN "province",
DROP COLUMN "state",
ADD COLUMN     "venue" TEXT NOT NULL,
ALTER COLUMN "street" SET NOT NULL,
ALTER COLUMN "city" SET NOT NULL;
