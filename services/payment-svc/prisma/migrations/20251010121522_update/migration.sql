/*
  Warnings:

  - You are about to drop the column `errorCode` on the `payments` table. All the data in the column will be lost.
  - You are about to drop the column `errorMessage` on the `payments` table. All the data in the column will be lost.
  - You are about to drop the column `orderId` on the `payments` table. All the data in the column will be lost.
  - You are about to drop the column `paymentProvider` on the `payments` table. All the data in the column will be lost.
  - You are about to drop the column `providerResponse` on the `payments` table. All the data in the column will be lost.
  - Added the required column `orderCode` to the `payments` table without a default value. This is not possible if the table is not empty.
  - Added the required column `provider` to the `payments` table without a default value. This is not possible if the table is not empty.
  - Added the required column `redirectUrl` to the `payments` table without a default value. This is not possible if the table is not empty.

*/
-- AlterTable
ALTER TABLE "payments" DROP COLUMN "errorCode",
DROP COLUMN "errorMessage",
DROP COLUMN "orderId",
DROP COLUMN "paymentProvider",
DROP COLUMN "providerResponse",
ADD COLUMN     "orderCode" TEXT NOT NULL,
ADD COLUMN     "provider" TEXT NOT NULL,
ADD COLUMN     "redirectUrl" TEXT NOT NULL;

-- CreateIndex
CREATE INDEX "payments_orderCode_idx" ON "payments"("orderCode");
