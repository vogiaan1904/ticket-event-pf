# 0028 — What a client sends is read back, and stubs are regenerated in CI

**Date:** 2026-10-01
**Status:** accepted
**Arc:** read-back — [plan](../plans/2026-10-01-read-back.md)
**Where it lives:** `deploy/scripts/gate-read-back.sh`, `.github/workflows/proto-copies.yml` (`stubs`), the repository suites `services/event-svc/src/modules/events/repository/events.repository.spec.ts`, `services/payment-svc/src/modules/payment/repository/payment.repository.spec.ts`, `services/waitroom-svc/internal/repository/redis/session_repository_test.go`, `services/inventory-svc/internal/services/ticketclass_readback_test.go`

| Question | Chose | Instead of | Cost |
|---|---|---|---|
| How do we know a field a client sends is stored and read back, and a stub matches its contract? | A k3s gate that sends every field through each gateway create and update and reads it back; CI regenerating every stub and failing on any difference; repository suites against a real database where a mapping layer sits between request and store | Unit tests with mocks alone; a contract-testing framework between gateway and services; a repository suite for every store, pass-through ones included | CI needs Postgres services and protoc pinned to the stubs' versions; the gate runs on k3s, not in CI |

## Context

On 2026-10-01 three defects of one kind surfaced, each the first time something read
data back: the gateway's order stub, stale against its contract since at least
2026-09-24; three order fields never stored; and the gateway dropping every zero value
from a reply. Mocked unit tests passed through all three, because a mock returns what
the test hands it.

## Options

- *Read back through the real path* (chosen). A gateway gate on k3s finds a drop at
  any layer, then a test at the layer that dropped it keeps it fixed in CI.
- *Mocks alone*: what existed. They check a layer against the test's own picture of
  its neighbour, so a field both sides forget never shows.
- *A contract-testing framework* such as Pact: a broker and per-pair contracts for
  what the generated stubs and one wire spec already state.
- *A repository suite for every store*: user-svc writes its request straight to
  Prisma, so a suite there would test Prisma.

## Decision

Delegated: "yes, merge to main then do the read-back work" (2026-10-01). The options
above were weighed by the agent; none was put to the architect as a separate choice.

## Consequences

- A contract edited without regenerating its stubs fails CI.
- event-svc and payment-svc CI jobs run a Postgres service; their repository suites
  fail rather than skip when it is missing.
- The read-back gate joins the k3s gates; it is the one that sees a drop between
  layers that each pass their own tests.

## Outcome

The gate's first runs on k3s found four defects, each fixed test-first at the layer
that dropped the field:

| Defect | Layer | Fixed in |
|---|---|---|
| Event and sale updates accepted only a date, so no update could set a time of day | gateway DTOs | `cd456ea` |
| An event update's venue never reached event-svc: the contract named it `venue_name` | contract | `dd97cef` |
| An event update ignored categories, and erased ward and district on a partial update | event-svc repository | `a2d90a1` |
| An order item's `price_cents` carried the line total | order-svc presenter | `41bbd37` |

The CI stub check (`39a10ff`) went red locally, naming both stale stubs, when
`order.proto` gained a value nothing regenerated, and green on the current tree and in
CI. Each repository suite fails when one stored field is dropped or blanked.

On k3s, 2026-10-01, revision 63 (`sha-568ec78`): `make -C deploy k3s-gate-read-back`
passed, every field of a user, an event, its config, a session and an order reading
back as sent. gate 2, the room and orders gates passed. The sold-out gate failed once
when `CreatePaymentIntent` hung for the order's 60s create deadline, a payment
provider stall, and passed on its rerun.
