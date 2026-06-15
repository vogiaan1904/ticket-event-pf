-- CreateEnum
CREATE TYPE "public"."EventStatus" AS ENUM ('DRAFT', 'PUBLISHED', 'CANCELLED');

-- CreateEnum
CREATE TYPE "public"."Category" AS ENUM ('MUSIC', 'SPORT', 'THEATERS_AND_ART', 'OTHER');

-- CreateTable
CREATE TABLE "public"."events" (
    "id" TEXT NOT NULL,
    "name" TEXT NOT NULL,
    "description" TEXT NOT NULL,
    "startDate" TIMESTAMP(3) NOT NULL,
    "endDate" TIMESTAMP(3) NOT NULL,
    "thumbnailUrl" TEXT NOT NULL,
    "organizerId" TEXT NOT NULL,
    "updatedAt" TIMESTAMP(3) NOT NULL,

    CONSTRAINT "events_pkey" PRIMARY KEY ("id")
);

-- CreateTable
CREATE TABLE "public"."event_configs" (
    "id" TEXT NOT NULL,
    "startSellDate" TIMESTAMP(3) NOT NULL,
    "endSellDate" TIMESTAMP(3) NOT NULL,
    "isFree" BOOLEAN NOT NULL,
    "customerTicketLimit" INTEGER NOT NULL,
    "isNewTrending" BOOLEAN NOT NULL DEFAULT false,
    "status" "public"."EventStatus" NOT NULL DEFAULT 'DRAFT',
    "categories" "public"."Category"[],
    "eventId" TEXT NOT NULL,
    "createdAt" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "updatedAt" TIMESTAMP(3) NOT NULL,

    CONSTRAINT "event_configs_pkey" PRIMARY KEY ("id")
);

-- CreateTable
CREATE TABLE "public"."locations" (
    "id" TEXT NOT NULL,
    "name" TEXT NOT NULL,
    "street" TEXT,
    "city" TEXT,
    "postalCode" TEXT,
    "state" TEXT,
    "province" TEXT,
    "district" TEXT,
    "ward" TEXT,
    "country" TEXT NOT NULL,
    "eventId" TEXT NOT NULL,
    "createdAt" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "updatedAt" TIMESTAMP(3) NOT NULL,

    CONSTRAINT "locations_pkey" PRIMARY KEY ("id")
);

-- CreateTable
CREATE TABLE "public"."organizers" (
    "id" TEXT NOT NULL,
    "name" TEXT NOT NULL,
    "email" TEXT NOT NULL,
    "createdAt" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "updatedAt" TIMESTAMP(3) NOT NULL,

    CONSTRAINT "organizers_pkey" PRIMARY KEY ("id")
);

-- CreateIndex
CREATE UNIQUE INDEX "event_configs_eventId_key" ON "public"."event_configs"("eventId");

-- CreateIndex
CREATE UNIQUE INDEX "locations_eventId_key" ON "public"."locations"("eventId");

-- AddForeignKey
ALTER TABLE "public"."events" ADD CONSTRAINT "events_organizerId_fkey" FOREIGN KEY ("organizerId") REFERENCES "public"."organizers"("id") ON DELETE RESTRICT ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "public"."event_configs" ADD CONSTRAINT "event_configs_eventId_fkey" FOREIGN KEY ("eventId") REFERENCES "public"."events"("id") ON DELETE RESTRICT ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "public"."locations" ADD CONSTRAINT "locations_eventId_fkey" FOREIGN KEY ("eventId") REFERENCES "public"."events"("id") ON DELETE RESTRICT ON UPDATE CASCADE;
