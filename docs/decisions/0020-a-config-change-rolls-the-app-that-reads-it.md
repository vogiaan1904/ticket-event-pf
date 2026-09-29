# 0020 — A config change rolls the app that reads it

**Date:** 2026-09-29
**Status:** proposed
**Arc:** admission-sizing — [plan](../plans/2026-09-29-admission-sizing.md)
**Where it lives:** `deploy/helm/ticketbottle/templates/apps/_appservice.tpl` (`tb.configDigest`, `checksum/config`)

| Question | Chose | Instead of | Cost |
|---|---|---|---|
| How does a changed ConfigMap reach the pods that read it? | Each app's pod template carries a digest of its own ConfigMap, so `helm upgrade` rolls exactly the apps whose config changed | `kubectl rollout restart` after a deploy | A template helper that finds each app's ConfigMap in `apps/config.yaml`, and one roll of every app when the digests first appear |

## Context

Pods read their ConfigMap into the environment once, when the container starts,
and the pod templates carried a digest of their Secret but not of their config. On
k3s on 2026-09-29, `waitroom-config` was patched to a release rate of 9. The
waitroom pod kept its name and start time, and its process still ran
`batch_size: 10`.

A deploy of a new build restarts every pod anyway, because every image tag
changes. So the gap only bites a config-only deploy: tuning a value, such as the
door speed that `docs/design/admission-sizing.md` sets per target.

## Options

**A digest of each app's own ConfigMap.** The deploy carries the change, and
`helm rollback` rolls the pods back too. It works the same from a Makefile, CI or
a GitOps sync, and it restarts only apps whose config changed. The cost is a
helper that parses `config.yaml`; a ConfigMap it cannot find fails the render.

**A digest of the whole config file.** It needs no parsing and is the form in
Helm's own documentation. But one app's change rolls all eight.

**`kubectl rollout restart` after a deploy.** No chart change, and a command
everyone knows. Whoever deploys has to remember it, and has to know which apps
read which ConfigMap (`order-config` feeds two). `helm rollback` also needs a
second restart. Forgetting it leaves the file saying one value while the box
runs another.

## Decision

The architect asked for a neutral comparison: "wait, for the problem about
checksum config, verify deeply again and explain to me why you suggested to add
the checksum ? my instinct is just kubectl rollout restart ? be neutral and
explain to me" (2026-09-29). Given the live test and the comparison, the architect
chose the digest: "yes A, and continue" (2026-09-29).

Using each app's own ConfigMap, not the whole file, came inside the approved plan
and was never put as its own choice.

## Consequences

- A value in a values overlay is the value the pods run, once the deploy
  finishes.
- `outbox-relay`, `payment-webhook` and the migration Jobs are not built from
  `tb.appService` and carry no config digest yet.
- A comment inside an app's ConfigMap block changes its digest and restarts it.
