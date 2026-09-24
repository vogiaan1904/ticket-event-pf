# 0013 — The gateway sets security headers and throttles sign-in, not purchase

**Date:** 2026-09-24
**Status:** proposed
**Arc:** gateway-edge — [plan](../plans/2026-09-24-architect-calls-from-the-system-map.md)
**Where it lives:** `services/api-gateway/src/common/guards/auth-rate-limit.guard.ts`, `services/api-gateway/src/common/security.ts`

| Question | Chose | Instead of | Cost |
|---|---|---|---|
| Should the gateway rate-limit and set security headers? | Helmet on every response; a per-IP limit on sign-in and sign-up only, in memory, with the trusted proxy hops set per target | Headers only, with limiting left to an ALB or WAF later | Per replica and per IP: N replicas admit N times the limit, and buyers behind one carrier NAT share a budget |

## Context

`helmet` and `express-rate-limit` were gateway dependencies that `src/main.ts` never
wired, while the README claimed both.

Sign-in and sign-up each run an argon2 hash in the gateway process (node-argon2's
defaults: 64 MiB, on libuv's four threads), against a pod limit of one CPU and
384 MiB. An unthrottled flood of them degrades every route the gateway serves,
checkout included. The question put to the architect placed the hash in user-svc;
it runs in the gateway, which makes the exposure wider than the one described.

Purchase is a different case. The waitroom already controls admission, and a per-IP
limit there would refuse real buyers behind a shared NAT — and the load test, which
drives every buyer from one pod IP.

On EKS every request reaches the gateway from the ALB. Keyed on the socket, all
buyers would share one budget. On the k3s NodePort there is no proxy, and
`X-Forwarded-For` is whatever the client typed.

## Options

**Helmet, and a per-IP limit on sign-in and sign-up only.** Cheap, and it covers the
one expensive unauthenticated path. Costs the per-replica count and shared-NAT false
positives. It also needs the proxy hop count right per target, or a client can pick its
own key.

**Helmet only; rate limiting at the edge later.** No per-replica arithmetic, but sign-in
stays a denial-of-service path on k3s until an ALB or WAF exists to carry it.

**Neither; remove both packages.** Accepts the argon2 exposure as the price of a demo
platform whose admission control is the waitroom.

## Decision

"Helmet + auth-only limit" — the architect, 2026-09-24, choosing from the three
options above.

## Consequences

A throttled request is the gateway's own 429, labelled `RESOURCE_EXHAUSTED` — the
code's only use. The root `CLAUDE.md` taxonomy says so, and a downstream
`RESOURCE_EXHAUSTED` is still labelled `INTERNAL`. The limiter throws through the
exception filter, so a 429 is counted under its route rather than as `OK`.

`APP_TRUST_PROXY_HOPS` must match what fronts the gateway: 0 on k3s, 1 behind the
ALB. A CDN in front of the ALB makes it 2. Setting it higher than the real number of
proxies lets a client choose its own rate-limit key; setting it lower puts everyone
behind the proxy in one budget.

A flood from many addresses is not stopped: that is an edge job. The limit
(`APP_AUTH_RATE_LIMIT_PER_MINUTE`, 20 by default, 0 for off) is the lever if an on-sale
shows NAT false positives. Content-Security-Policy is off wherever Swagger UI is
served, because the policy blocks its inline scripts.
