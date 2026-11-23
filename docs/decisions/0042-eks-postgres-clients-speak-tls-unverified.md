# 0042 — EKS's Postgres clients speak TLS to RDS, encrypted but not verified

**Date:** 2026-11-23
**Status:** proposed
**Arc:** next-work — [plan](../plans/2026-11-20-eks-rds-then-the-door.md), [design](../design/eks-stateful-tier.md)
**Where it lives:** `deploy/helm/ticketbottle/templates/_postgres.tpl` (`tb.pgSsl`), `templates/infra/temporal.yaml`, `values-eks.yaml` (`postgres.tls`)

| Question | Chose | Instead of | Cost |
|---|---|---|---|
| RDS refuses clients without TLS; which side gives? | The clients: `postgres.tls` on EKS turns TLS on in each client's own dialect, without verifying the server's certificate | A parameter group with `rds.force_ssl = 0`, so the clients stay as they are | Three per-client settings that differ by target; a client would talk to an impostor that answered on the RDS address inside the VPC; verifying needs the RDS CA bundle in every pod |

## Context

The first `eks-deploy.sh` against RDS, 2026-11-23: inventory, Temporal and the
outbox relay crash-looped on `no pg_hba.conf entry … no encryption`, and order-service
and order-consumer followed Temporal down. `rds.force_ssl` is `1` (source `system`) in
`default.postgres15`; the design had said 15 leaves it off. Prisma's services and every
`ensure-db` init container ran, because Prisma and psql try TLS first.

| Client | Library | Default without a setting |
|---|---|---|
| user, event, payment-service, migrations | Prisma | tries TLS, falls back |
| inventory-service | pgx | the DSN said `sslmode=disable` |
| outbox-relay, payment-webhook | node-postgres 8, `pg-connection-string` 2.14 | no TLS |
| temporal (schema tool, then server) | auto-setup 1.27.2 | no TLS |

## Options

**A. Turn `force_ssl` off.** A custom parameter group and one reboot per instance; no
client changes. Database traffic in plaintext inside the VPC, against AWS's default,
and Postgres 16 asks the question again.

**B. The clients use TLS (chosen).** One value, `postgres.tls`, true on EKS:

| Client | Setting |
|---|---|
| inventory (pgx) | `sslmode=require`, else `disable` as before |
| node-postgres | `sslmode=no-verify`; its `require` now warns and will mean `verify-full` |
| temporal | `POSTGRES_TLS_ENABLED` and `POSTGRES_TLS_DISABLE_HOST_VERIFICATION` (schema tool), `SQL_TLS_ENABLED` (server) |
| Prisma | unchanged |

**B'. B, verified.** The RDS CA bundle mounted into every pod and each client pointed
at it. Closes the impostor case; one more artifact to ship and rotate, for a cluster
that lives hours.

## Decision

With A and B and their costs on the table, the architect answered, 2026-11-23:
"record this decision of option B, and change it, then commit, then show me steps to
continue". B' was named in the costs of B and not asked for.

## Consequences

- The k3s render is unchanged: its Postgres offers no TLS, and `postgres.tls` stays
  false there. The k3s golden passes unmodified.
- On k3s the relay and Prisma share one DSN Secret; `postgres.tls` is honoured only on
  the Secrets Manager path, where each pod builds its own DSN.
- Holds while the VPC is trusted. Verifying is B': the CA bundle, `sslrootcert`,
  `verify-full`, and `SQL_CA` with host verification on.
- `assert-render.sh` fails if any of the four EKS clients loses its setting; each check
  was seen to fail under a mutation.
