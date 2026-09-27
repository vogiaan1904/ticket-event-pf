# 0010 — kind is removed; k3s is the only full-stack environment

**Date:** 2026-09-23
**Status:** accepted
**Arc:** deploy-targets — commit `40ae841`
**Where it lives:** `deploy/Makefile`, `deploy/helm/ticketbottle/values.yaml`

| Question | Chose | Instead of | Cost |
|---|---|---|---|
| Where does the full stack run? | k3s on one EC2 instance, from images CI pushes to ECR | A local kind cluster | No free local full stack; every full-stack run needs the box started, billed by the hour |

## Context

kind held gigabytes of node state and loaded images on a workstation without the
disk for them, and had not run since the k3s box became the everyday environment.
Its chart path — an in-cluster DynamoDB pod, static AWS keys, a Redpanda listener
advertised on the kind node's hostname — was exercised by no deploy.

## Options

**Keep kind** as the free, offline target.

**Remove it.** Single-service work stays on each service's `docker-compose.dev.yml`.

## Decision

"completely clear anythings about kind … i dont have disk for it ! and also
remove completly the kind docker in this repo !" — the architect, 2026-09-23.

## Consequences

The chart has two targets, k3s and EKS, and its render assertions run against
k3s. Real DynamoDB is the chart default. Any full-stack check costs box time.

## Outcome

`40ae841`. The k3s and EKS renders were byte-identical before and after, apart
from one comment line.
