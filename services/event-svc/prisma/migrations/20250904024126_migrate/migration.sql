/*
  Warnings:

  - You are about to drop the column `customerTicketLimit` on the `event_configs` table. All the data in the column will be lost.
  - You are about to drop the column `endSellDate` on the `event_configs` table. All the data in the column will be lost.
  - You are about to drop the column `startSellDate` on the `event_configs` table. All the data in the column will be lost.
  - Added the required column `maxAttendees` to the `event_configs` table without a default value. This is not possible if the table is not empty.
  - Added the required column `ticketSaleEndDate` to the `event_configs` table without a default value. This is not possible if the table is not empty.
  - Added the required column `ticketSaleStartDate` to the `event_configs` table without a default value. This is not possible if the table is not empty.
  - Added the required column `ownerId` to the `events` table without a default value. This is not possible if the table is not empty.
  - Added the required column `description` to the `organizers` table without a default value. This is not possible if the table is not empty.
  - Added the required column `logoUrl` to the `organizers` table without a default value. This is not possible if the table is not empty.

*/
-- AlterTable
ALTER TABLE "public"."event_configs" DROP COLUMN "customerTicketLimit",
DROP COLUMN "endSellDate",
DROP COLUMN "startSellDate",
ADD COLUMN     "allowWaitRoom" BOOLEAN NOT NULL DEFAULT true,
ADD COLUMN     "isPublic" BOOLEAN NOT NULL DEFAULT true,
ADD COLUMN     "maxAttendees" INTEGER NOT NULL,
ADD COLUMN     "requiresApproval" BOOLEAN NOT NULL DEFAULT false,
ADD COLUMN     "ticketSaleEndDate" TIMESTAMP(3) NOT NULL,
ADD COLUMN     "ticketSaleStartDate" TIMESTAMP(3) NOT NULL;

-- AlterTable
ALTER TABLE "public"."events" ADD COLUMN     "ownerId" TEXT NOT NULL;

-- AlterTable
ALTER TABLE "public"."organizers" ADD COLUMN     "description" TEXT NOT NULL,
ADD COLUMN     "logoUrl" TEXT NOT NULL;
