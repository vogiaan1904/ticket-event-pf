/*
  Warnings:

  - Changed the type of `status` on the `payments` table. No cast exists, the column would be dropped and recreated, which cannot be done if there is data, since the column is required.

*/
-- CreateEnum
CREATE TYPE "PaymentStatus" AS ENUM ('PENDING', 'COMPLETED', 'FAILED');

-- AlterTable
ALTER TABLE "payments" DROP COLUMN "status",
ADD COLUMN     "status" "PaymentStatus" NOT NULL;
