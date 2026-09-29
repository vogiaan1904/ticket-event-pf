# 0022 — A migration runs before the code that needs it

**Date:** 2026-09-29
**Status:** proposed
**Arc:** schema-migrations — found by the [tickets-per-order deploy](../plans/2026-09-29-tickets-per-order.md#results)
**Where it lives:** `deploy/helm/ticketbottle/templates/apps/migrations.yaml` (`helm.sh/hook`)

| Question | Chose | Instead of | Cost |
|---|---|---|---|
| When does a schema migration run, relative to the code that reads it? | Before an upgrade's rollout (`pre-upgrade`); after a first install (`post-install`) | After the rollout, as before; an init container in each service | Every migration must work with the code already running, and the Job reads the ConfigMap and Secret from before the upgrade |

## Context

The Prisma migration Jobs for user-, event- and payment-svc were
`post-install,post-upgrade` hooks. With `--wait`, Helm runs a post-upgrade hook only
once every pod is Ready. The apps' readiness is a TCP check, which passes whether or
not the database has the schema they read.

On k3s on 2026-09-29, the deploy that added `maxTicketsPerOrder` started the new
event-service at 16:01:04Z. Its migration finished at 16:02:35Z. For 91s, event-svc
queried a column that did not exist. Every config read would have failed, and every
order create makes one. Nobody was buying.

Running that migration first was safe. The old code never reads the new column, and
`DEFAULT 4` fills it for the rows the old code inserts.

## Options

**Before the rollout, and after a first install.** If the migration fails, the
upgrade stops and the old pods keep serving. A first install still migrates
afterwards, because the same chart creates Postgres. It has two costs:
- A migration must work with the code before it: add a column now, drop one in a
  later release.
- The Job reads the ConfigMap and Secret already in the cluster. So a release that
  also moves a service's database migrates the old one.

**After the rollout, as before.** No chart change. But every upgrade whose code needs
a new schema serves errors until the Job finishes: 91s on k3s.

**An init container in each service.** Every replica runs the migration on every
start. A failed migration stops pods from starting, but does not stop the deploy.

## Decision

The architect was given the three options with the costs above, and the 91s
measurement. They answered: "B, the box is stopped" (2026-09-29).

Not also running the Job after the rollout was never put as its own choice. Running
it after too would migrate a database that the release moves to.

## Consequences

- A migration that drops or renames something the running code reads breaks that
  code. Such a change ships in two releases.
- A release that changes a service's database host or credentials migrates the old
  database. The next upgrade migrates the new one.
- A release that turns on `postgres.enabled` for an existing install waits on a
  Postgres that does not exist yet. It fails at `--timeout`.
- `deploy/helm/ticketbottle/tests/assert-render.sh` fails if a migration Job loses
  `pre-upgrade` or `post-install`.
