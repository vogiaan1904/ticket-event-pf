/*
  Warnings:

  - You are about to drop the column `categories` on the `event_configs` table. All the data in the column will be lost.
  - You are about to drop the column `email` on the `organizers` table. All the data in the column will be lost.

*/
-- AlterTable
ALTER TABLE "public"."event_configs" DROP COLUMN "categories";

-- AlterTable
ALTER TABLE "public"."events" ADD COLUMN     "categories" "public"."Category"[];

-- AlterTable
ALTER TABLE "public"."organizers" DROP COLUMN "email";
