/*
  Warnings:

  - A unique constraint covering the columns `[orderCode]` on the table `payments` will be added. If there are existing duplicate values, this will fail.

*/
-- CreateIndex
CREATE UNIQUE INDEX "payments_orderCode_key" ON "payments"("orderCode");
