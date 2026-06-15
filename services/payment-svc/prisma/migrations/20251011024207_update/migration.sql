/*
  Warnings:

  - Changed the type of `currency` on the `payments` table. No cast exists, the column would be dropped and recreated, which cannot be done if there is data, since the column is required.

*/
-- CreateEnum
CREATE TYPE "Currency" AS ENUM ('VND');

-- AlterTable
ALTER TABLE "payments" DROP COLUMN "currency",
ADD COLUMN     "currency" "Currency" NOT NULL;
