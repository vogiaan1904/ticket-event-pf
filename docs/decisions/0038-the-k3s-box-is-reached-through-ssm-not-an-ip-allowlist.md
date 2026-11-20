# 0038 — The k3s box is reached through SSM, not an IP allowlist

**Date:** 2026-11-20
**Status:** accepted
**Arc:** k3s-access — no plan; settled in session, built the same day
**Where it lives:** `deploy/terraform/modules/ec2-k3s/main.tf`, `deploy/Makefile` (`SSH_OPTS`)

| Question | Chose | Instead of | Cost |
|---|---|---|---|
| How does the workstation reach the k3s box when its egress IP will not hold still? | SSH carried by an SSM Session Manager session to the instance id; no inbound rule on the box | Widening the SSH allowlist to the carrier's `/19` | A local `session-manager-plugin`, AWS credentials for every tunnel, an extra hop through SSM's regional endpoint, and the agent must register before the first tunnel |

## Context

Port 22 accepted one `/32`, `my_ip_cidr`. The home network egresses through VNPT's
carrier NAT, and on 2026-11-20 it showed six addresses in `113.185.64.0/19` within
minutes (`.76.55`, `.78.19`, `.79.98`, `.80.131`, `.81.133`, `.86.105`); a `/32`
applied for one was stale before the apply finished. Trap #4 in the k3s deploy notes,
"set `my_ip_cidr` to the other address", assumed two addresses, and no longer held.

## Options

**A. Widen the allowlist to `113.185.64.0/19`.** One apply. Opens port 22 to the
carrier's other subscribers (key-only auth, so scan noise), and breaks again when the
pool moves outside the range.

**B. SSH over SSM (chosen).** The agent dials out to Systems Manager; `ssh` reaches it
through `ProxyCommand aws ssm start-session --document-name AWS-StartSSHSession`. The
security group takes no ingress; who may connect is IAM. The existing `ssh -L` tunnel
and `scp` keep working, so one session still carries both forwards.

**C. Pure SSM port forwarding** (`AWS-StartPortForwardingSession`). Drops the SSH key,
but one session per port, and `scp` of the kubeconfig needs another path.

## Decision

Option B was put with its costs beside A; the architect answered "yes do it for the
SSM", 2026-11-20. Option C was named and not chosen, as more moving parts for one user.

## Consequences

- The k3s env drops `my_ip_cidr`, `k3s-allow-ip` and the EC2 Instance Connect rule;
  the console's Session Manager is the fallback path instead.
- The EKS public endpoint and the ALB still allowlist `my_ip_cidr`, so the rotating
  pool still bites an EKS run.
- `ServerAliveInterval=30` keeps a tunnel from Session Manager's 20-minute idle cut.
- The agent registers only once the role carries `AmazonSSMManagedInstanceCore`; an
  agent started before that backs off, and a reboot brought it online.
- Holds while the box keeps outbound HTTPS to SSM; in a private subnet it would need
  VPC interface endpoints.

## Outcome

Built and applied 2026-11-20. `terraform plan`: 1 to add (the policy attachment), 1 to
change (the security group's ingress emptied), 0 to destroy; a plan afterwards showed no
differences. The agent stayed unregistered for 7 minutes and came `Online` 2 minutes after
a reboot. Through `make -C deploy tunnel-ec2-k3s`, ssh listened on 6443 and 3000,
`k3s-kubeconfig` took 12.7s, and `kubectl get pods` returned 15 Running and 2 Completed
in 3.0s, against about 2s over direct SSH: one sample.
