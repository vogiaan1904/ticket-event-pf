# 0041 — The EKS API is gated by IAM, not an IP allowlist

**Date:** 2026-11-23
**Status:** accepted
**Arc:** k3s-access — no plan; settled in session, after 0038's EKS consequence bit a run
**Where it lives:** `deploy/terraform/modules/eks/main.tf` (`public_access_cidrs`), `deploy/scripts/eks-deploy.sh` (`MYIP`), `deploy/Makefile` (`eks-allow-ip`)

| Question | Chose | Instead of | Cost |
|---|---|---|---|
| How does the workstation reach the EKS API and ALB when its egress IP will not hold still? | The API endpoint open to `0.0.0.0/0`, gated by IAM alone; the ALB keeps a `/32`, taken from the current IP at deploy time and re-pointed by a helm-only `eks-allow-ip` | A private endpoint and internal ALB reached through an SSM-managed bastion | The API is scannable while a cluster lives, and an API server flaw reachable before authentication would be exposed; an IP change still breaks the ALB until `eks-allow-ip` runs |

## Context

The workstation must run Cloudflare WARP, whose egress address is Cloudflare's and
rotates (`104.28.205.73` → `104.28.237.74` within one `eks-bootstrap.sh`, 2026-11-23).
The EKS public endpoint admitted one `/32`, `my_ip_cidr`, so the bootstrap lost the
API between its second and third step. Excluding AWS from WARP falls back to the
carrier NAT whose pool 0038 already gave up on. 0038 recorded this as its open
consequence: "the rotating pool still bites an EKS run."

Two things allowlisted the IP, and they protect different things. The API endpoint
already requires an IAM-signed token mapped by an access entry
(`authentication_mode = "API"`); a principal that can sign one can also edit the
allowlist. The ALB fronts the app, which ships dev secrets and open sign-up.

## Options

**A. Open the API; keep the ALB's `/32` (chosen).** No new parts. The allowlist
stays where it is the only gate.

**B. A, and open the ALB.** No IP handling at all; anyone who finds the hostname can
sign up and buy against a stack with dev secrets and no TLS.

**C. SSM bastion.** Private endpoint and internal ALB, each port-forwarded through an
SSM instance: a new instance, a kubeconfig TLS server-name override, and two forwards,
for one user of an ephemeral cluster.

**D. Allowlist WARP's range.** Opens both to every WARP user, and breaks when the pool
moves: 0038's option A again.

## Decision

With options A–C and their costs on the table, the architect answered, 2026-11-23:
"A, draft 0041 and build it. But first, making public access cidrs to 0.0.0.0 like
this is acceptable or risky ?" The agent's answer to the question is the Cost column
and the last Consequence.

## Consequences

- Terraform holds no IP; `my_ip_cidr` leaves the variables, the outputs and the
  tfvars. `update-my-ip` is gone; `my-ip` compares against the live ALB.
- The in-cluster load generator reaches the gateway by its Service, not the ALB, so
  a sweep survives an IP change; only gates run from the workstation do not.
- `endpoint_private_access` stays: private nodes reach the API only through it.
- Holds while the cluster is ephemeral and its admin is one IAM principal. A
  long-lived cluster, or one shared, would want option C.

## Outcome

Built in `1fe8d71`. Every EKS bootstrap, deploy and gate from 2026-11-23 to 2026-11-24
ran from behind WARP with no API allowlist; the only failure on re-running the
bootstrap was the LB controller's webhook certificate (`10c38fb`), unrelated to the
endpoint. The door sweep loads the gateway by its Service, so no run depended on the
ALB's `/32`.
