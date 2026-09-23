---
name: system-map
description: Map of where TicketBottle's design is written down — which document owns each question about the whole platform or one service (api-gateway, user, event, order, payment, inventory, waitroom), how far to trust it, and which code to verify it in. Use before explaining, designing, reviewing or changing anything that depends on how the system works — "how does X work", "why is X like this", "what should I read before touching Y", starting work in an unfamiliar service, briefing a subagent, or picking up after a context compaction. Also use when two documents disagree, or a document disagrees with the code, and when adding, moving or deleting a design document.
---

# System map

Where each part of TicketBottle is written down, and how far to trust it.

This skill holds facts about **documents**, never facts about the system. A
mechanism lives in the one document that owns it — the root `CLAUDE.md` rule, "a
fact has exactly one home". Read the owner, not a summary of it: summaries are
how `Reserve` went on being described as `SELECT … FOR UPDATE` long after
`35e864f` removed it.

Invoked with an argument — a service or a question — route that directly.

## Already in your context

The root `CLAUDE.md` loads in every session: the service and port table, the
one-paragraph architecture, the **decision register**, the error taxonomy, the
metric contract, the alerting policy and the comment conventions. Start from the
register: if a row owns your question, go to that owner.

A service's `CLAUDE.md` loads only once you read a file under that service. For
work that crosses services, open each one deliberately.

## How far to trust each kind of document

| Kind | Answers | For current behaviour |
|---|---|---|
| Code and its tests | what runs | ground truth |
| Root `CLAUDE.md` | system-wide rules; who owns which question | binding |
| A service's `CLAUDE.md` | that service's invariants and traps | current — kept with the code |
| Skills in `.claude/skills/` | one procedure or subsystem, file by file | current when written; verify paths |
| `docs/decisions/` | why, and who chose | frozen at their date; check for `superseded` |
| `docs/design/` | a target state | read its Status line: built or only specified |
| A service's `docs/` | a deep dive on one mechanism | mixed — each guide rates them |
| `README.md`, `docs/ARCHITECTURE.md` | the narrative for a human reader | summary only; never cite for a mechanism |
| `docs/plans/` | what was done on one day | true on that date |

When two disagree, the higher row wins, and the code beats all of them. Still read
the owner first: it names the invariant the code protects, which the code alone
does not.

## Route by question

| You need | Go to |
|---|---|
| Is this decided, open, or yours to decide? | root `CLAUDE.md`, *Decision register* |
| Why X was chosen over Y, and by whom | `docs/decisions/README.md` — the index by arc, then the record |
| How documents are organised here | `docs/README.md` |
| A purchase end to end, or an order that is stuck | `.claude/skills/trace-purchase-flow/SKILL.md` |
| Payment events: outbox, relay, webhook | `.claude/skills/outbox-relay/SKILL.md` |
| A gRPC contract, and who consumes it | `proto/`, then `.claude/skills/proto-change/SKILL.md` |
| A new RPC, end to end | `.claude/skills/add-grpc-endpoint/SKILL.md` |
| A new service | `.claude/skills/add-service/SKILL.md` |
| Which gRPC code, which HTTP status | root `CLAUDE.md`, *Error taxonomy*; mapped in `services/api-gateway/src/common/filters/global-exception.filter.ts` |
| A Kafka topic's producer and consumer | the names in `services/order-svc/internal/order/delivery/kafka/constants.go`, `services/waitroom-svc/internal/delivery/kafka/constants.go`, `services/payment-svc/outbox-relay/src/kafka.ts` |
| Metrics, dashboards, alerts, PromQL | `.claude/skills/observability/SKILL.md`; the contract is `docs/METRICS.md` |
| An alert fired | `docs/RUNBOOK.md`, one section per alert |
| What runs where; Terraform, Helm, cost | `.claude/skills/deployment-architecture/SKILL.md`, then `deploy/README.md` |
| A service's config values | `deploy/helm/ticketbottle/templates/apps/config.yaml` |
| Postgres, Redis, Redpanda, Temporal | `deploy/helm/ticketbottle/templates/infra/`; their limits in the deployment skill |
| The stateful tier on EKS | `docs/design/eks-stateful-tier.md` |
| Recording a decision | `.claude/skills/decision-records/SKILL.md` |
| The architecture diagram | `docs/diagrams/README.md` |
| End-to-end acceptance, load, chaos | `deploy/scripts/gate1-purchase-flow.sh`, `deploy/loadtest/`, `deploy/scripts/gate4b-chaos.sh` |
| What CI runs | `.github/workflows/` |
| How the agent and the architect split decisions | `docs/decisions/0002-the-agent-is-a-mentor-not-a-gate.md`; `docs/design/decision-protocol.md` is the superseded first version |

## Route by service

| Service | Guide |
|---|---|
| API Gateway — HTTP edge, auth, error mapping | [references/api-gateway.md](references/api-gateway.md) |
| User — accounts, credentials | [references/user.md](references/user.md) |
| Event — events, organisers, lifecycle | [references/event.md](references/event.md) |
| Order — the saga, Temporal, `order-consumer` | [references/order.md](references/order.md) |
| Payment — intents, outbox, `outbox-relay`, `payment-webhook` | [references/payment.md](references/payment.md) |
| Inventory — ticket classes, holds | [references/inventory.md](references/inventory.md) |
| Waitroom — queue, admission, checkout tokens | [references/waitroom.md](references/waitroom.md) |

Each guide gives the documents in reading order with what each one covers and how
far to trust it, the code to verify in, the edges to other services, the decisions
and plans that shaped the service, and what not to trust.

## Procedure

1. **Find the owner** — the register, then the tables above, then the service guide.
2. **Read the owner itself.** A line in this map says what a document covers, not
   what it says.
3. **Verify a mechanism in the code the guide names** before relying on it, and cite
   `file:line` when you state it.
4. **When a document disagrees with the code**, the code is what runs. Say so, and
   fix the document in the same change or name it to the architect. Do not carry the
   wrong version into an answer or into code.
5. **Pass owners, not conclusions** — across a compaction, into a subagent brief, or
   to the architect. "Read `services/inventory-svc/CLAUDE.md`, *Idempotency*"
   survives; a paraphrase of it is one more copy to go stale. Tell a subagent to load
   this skill.

## Keeping the map true

- Adding, moving or deleting a design-bearing document: update the guide that owns
  it, or the tables here, in the same change.
- Cite repo-relative paths, one per backtick span, so the check can resolve them.
- `python3 .claude/skills/system-map/scripts/check_map.py` fails on a cited path that
  is gone and on a tracked document the map omits. CI runs it and its tests
  (`scripts/check_map_test.py`). What it cannot see is a document whose content
  drifted — that is step 4.
- Plans and decision records are reached through `docs/plans/` and the decisions
  index, not listed one by one here.
