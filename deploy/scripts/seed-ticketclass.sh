#!/usr/bin/env bash
# Usage: seed-ticketclass.sh <event_id> [total] [price_cents]
# Prints the new ticket_class id. The app has no create path (gateway inventory module is a stub),
# so Gate-1 seeds inventory directly.
set -euo pipefail
EVENT_ID="$1"; TOTAL="${2:-100}"; PRICE="${3:-10000}"
# CTE wrapper so the outer statement is a SELECT: psql -tA then prints only the id,
# without INSERT's "INSERT 0 1" command tag (which would pollute the captured id).
kubectl -n ticketbottle exec statefulset/postgres -- psql -U root -d ticketbottle_inventory -tAc \
  "WITH ins AS (
     INSERT INTO ticket_class (event_id, name, price_cents, currency, total, reserved, sold, status, created_at, updated_at)
     VALUES ('${EVENT_ID}', 'GA', ${PRICE}, 'VND', ${TOTAL}, 0, 0, 'ACTIVE', now(), now())
     RETURNING id
   ) SELECT id FROM ins;" | tr -d '[:space:]'
