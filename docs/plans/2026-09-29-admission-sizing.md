# The waitroom's door speed, sized on the k3s box

**Status: PROPOSED 2026-09-29.** Not started. Decision:
→ [0019](../decisions/0019-the-waitroom-door-speed-is-sized-per-target.md), proposed.

**Goal:** The waitroom admits buyers at a rate the deployment target was measured to
serve within the checkout SLO. For k3s, that rate comes from a sweep on the box.

**Spec:** [`docs/design/admission-sizing.md`](../design/admission-sizing.md).

**Found while writing this plan:** a ConfigMap change never reaches a running pod.
`templates/apps/_appservice.tpl` gives pods a digest of their Secret, "Env is resolved at
container creation, so a Secret change alone leaves pods running the old values", but
has no digest for config. A new door speed would deploy, show in `waitroom-config`, and
never run. Task 2 fixes this for every app built from that template, one digest per app,
so that changing one app's config rolls only that app.

## Global constraints

- Commit messages describe the platform, never study progress, and end with
  `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`.
- Pushing `dev` needs the architect's go-ahead.
- The k3s box and EKS never run at once: `aws eks list-clusters --region us-east-1`
  must be empty before starting the box.
- Comment budget: 3 lines inline, 5 on a symbol (root `CLAUDE.md`).
- The chart default stays 10 and counts as unmeasured. Only `values-k3s.yaml` carries
  a measured value.
- The door speed chosen is **the highest rate whose checkout p99 stays under 2s in
  both of its runs** (the design).
- One build throughout the sweep: pass its `sha-` tag as `REF`, never the moving `:dev`.

## Review focus

1. **A config-only deploy must restart the waitroom at the new rate.** Task 2's
   render check, and the sweep's check on the running pod before each run.
2. **A sweep `--set` must not outlive the sweep.** The next plain deploy must bring
   back the value from `values-k3s.yaml`. Task 5 Step 4 reads the running pod.
3. **A digest must name a ConfigMap that exists.** An app whose ConfigMap is renamed
   must fail the render, not digest an empty string. Task 2 Step 5.
4. **The door must be the limit in every run.** A run whose queue empties measured
   the buyers, not the door. Task 4 Step 5 voids it.
5. **Unrelated apps must not roll on a waitroom config change.** Task 2's render check
   compares `order-service`'s digest.

## Setup, every session on the box

```bash
aws eks list-clusters --region us-east-1                  # must print nothing
make -C deploy start-ec2-k3s
make -C deploy my-ip                                      # if it differs from the tfvars:
make -C deploy k3s-allow-ip                               # plan, check only the SG changes, apply
```

The local network leaves from two rotating addresses. Sample them with
`for i in $(seq 1 12); do curl -fsS https://api.ipify.org; echo; done | sort -u`, and
add any second one as a /32 SSH rule on the box's security group
(`aws ec2 authorize-security-group-ingress`). The next `k3s-allow-ip` removes it.

```bash
ssh -N -o ServerAliveInterval=30 -L 6443:127.0.0.1:6443 -L 3000:127.0.0.1:30000 ec2-user@<ip> &
make -C deploy k3s-kubeconfig && export KUBECONFIG=/tmp/k3s.yaml
kubectl -n monitoring port-forward svc/kps-kube-prometheus-stack-prometheus 9090:9090 &
export GW=http://localhost:3000/api
```

After a cold boot, `order-service` and `order-consumer` crash-loop until Temporal is up.

---

### Task 1: Door speed comes from values

**Files:**
- Modify: `deploy/helm/ticketbottle/templates/apps/config.yaml` (`QUEUE_DEFAULT_RELEASE_RATE`)
- Modify: `deploy/helm/ticketbottle/values.yaml` (new `waitroom:` block after `order:`)
- Test: `deploy/helm/ticketbottle/tests/assert-render.sh`

**Interfaces:**
- Produces: the chart value `waitroom.releaseRate` (integer, buyers per 1s tick, default
  10). Tasks 2, 4 and 5 set it.

- [ ] **Step 1: Write the failing check.** In `tests/assert-render.sh`, just before the
  final `echo "all render assertions passed"`:

```bash
# Door speed is a per-target value, so it must reach the waitroom's ConfigMap.
dr=$(helm template tb "$CHART" -f "$CHART/values-k3s.yaml" -f "$SECRETS" --set waitroom.releaseRate=3)
grep -q 'QUEUE_DEFAULT_RELEASE_RATE: "3"' <<<"$dr" || fail "waitroom.releaseRate does not reach waitroom-config"
echo "OK  the waitroom's door speed comes from values"
```

- [ ] **Step 2: Run it.** `deploy/helm/ticketbottle/tests/assert-render.sh`
  Expected: `FAIL: waitroom.releaseRate does not reach waitroom-config`.

- [ ] **Step 3: Take the value from values.** In `templates/apps/config.yaml`, replace
  `QUEUE_DEFAULT_RELEASE_RATE: "10"` with:

```yaml
  QUEUE_DEFAULT_RELEASE_RATE: {{ .Values.waitroom.releaseRate | quote }}
```

In `values.yaml`, after the `order:` block and before `temporal:`:

```yaml
waitroom:
  # Door speed: buyers admitted a second. Sized per target from a measurement;
  # 10 is unmeasured. See docs/design/admission-sizing.md.
  releaseRate: 10
```

- [ ] **Step 4: Run it.** Same command. Expected: `all render assertions passed`. The
  k3s render is byte-identical to its golden file, since `10` quoted is `"10"`.

- [ ] **Step 5: Commit**

```bash
git add deploy/helm/ticketbottle/templates/apps/config.yaml deploy/helm/ticketbottle/values.yaml deploy/helm/ticketbottle/tests/assert-render.sh
git commit -m "feat(chart): take the waitroom's door speed from values"
```

---

### Task 2: A config change rolls the app that reads it

**Files:**
- Modify: `deploy/helm/ticketbottle/templates/apps/_appservice.tpl` (pod annotations; a
  new `tb.configDigest` helper)
- Test: `deploy/helm/ticketbottle/tests/assert-render.sh`
- Regenerate: `deploy/helm/ticketbottle/tests/golden/values-k3s.yaml`

**Interfaces:**
- Consumes: `waitroom.releaseRate` (Task 1).
- Produces: the pod annotation `checksum/config` on every Deployment built by
  `tb.appService`.

- [ ] **Step 1: Write the failing check.** After Task 1's check:

```bash
# A ConfigMap change must roll the app that reads it, and only that app: each app
# carries a digest of its own ConfigMap. Change the waitroom's and compare.
digest() { awk -v d="$2" '/^---/{k=0;f=0} /^kind: Deployment$/{k=1} k && $0=="  name: "d{f=1} f && /checksum\/config:/{print $2; exit}' <<<"$1"; }
r7=$(helm template tb "$CHART" -f "$CHART/values-k3s.yaml" -f "$SECRETS" --set waitroom.releaseRate=7)
r8=$(helm template tb "$CHART" -f "$CHART/values-k3s.yaml" -f "$SECRETS" --set waitroom.releaseRate=8)
[ -n "$(digest "$r7" waitroom-service)" ] || fail "waitroom-service carries no checksum/config"
[ "$(digest "$r7" waitroom-service)" != "$(digest "$r8" waitroom-service)" ] || fail "a waitroom config change does not roll the waitroom"
[ "$(digest "$r7" order-service)" = "$(digest "$r8" order-service)" ] || fail "a waitroom config change rolls order-service too"
echo "OK  a config change rolls the app that reads it, and only that app"
```

- [ ] **Step 2: Run it.** Expected: `FAIL: waitroom-service carries no checksum/config`.

- [ ] **Step 3: Add the digest.** In `templates/apps/_appservice.tpl`, above
  `{{- define "tb.appService" -}}`:

```
{{- /* The digest of one app's own ConfigMap, so a change to it rolls that app alone. */}}
{{- define "tb.configDigest" -}}
{{- $want := printf "name: %s," .name -}}
{{- $doc := "" -}}
{{- range splitList "\n---" (include (print .ctx.Template.BasePath "/apps/config.yaml") .ctx) -}}
{{- if contains $want . }}{{ $doc = . }}{{ end -}}
{{- end -}}
{{- required (printf "no ConfigMap named %s in apps/config.yaml" .name) $doc | sha256sum -}}
{{- end -}}
```

And replace the pod template's annotation block:

```
      {{- if .secret }}
      annotations:
        # Env is resolved at container creation, so a Secret change alone leaves
        # pods running the old values. The digest rolls them.
        checksum/secret: {{ include (print $.Template.BasePath "/apps/secrets.yaml") $ | sha256sum }}
      {{- end }}
```

with:

```
      annotations:
        # Env is resolved at container creation, so a ConfigMap or Secret change
        # alone leaves pods running the old values. The digests roll them.
        checksum/config: {{ include "tb.configDigest" (dict "ctx" $ "name" .config) }}
        {{- if .secret }}
        checksum/secret: {{ include (print $.Template.BasePath "/apps/secrets.yaml") $ | sha256sum }}
        {{- end }}
```

- [ ] **Step 4: Run it.** Expected: `FAIL: k3s drifted`. The golden file lacks the new
  annotations; the diff shows only `checksum/config` and comment lines, on 8
  Deployments. Then run `deploy/helm/ticketbottle/tests/render-golden.sh` and the
  assertions again. Expected: `all render assertions passed`.

- [ ] **Step 5: Prove the checks can go red.** Each mutation, run, then
  `git checkout` the file.
  - Whole-file digest: replace the `checksum/config:` line's value with
    `{{ include (print $.Template.BasePath "/apps/config.yaml") $ | sha256sum }}`.
    Expected: `FAIL: a waitroom config change rolls order-service too`.
  - A ConfigMap that does not exist: in `templates/apps/waitroom.yaml`, change
    `"config" "waitroom-config"` to `"config" "waitroom-cfg"`. Expected: the render
    fails with `no ConfigMap named waitroom-cfg in apps/config.yaml`.

- [ ] **Step 6: Commit**

```bash
git add deploy/helm/ticketbottle/templates/apps/_appservice.tpl deploy/helm/ticketbottle/tests
git commit -m "fix(chart): roll an app when its own config changes"
```

---

### Task 3: The box's credit mode is written down

**Files:**
- Modify: `deploy/terraform/modules/ec2-k3s/variables.tf`
- Modify: `deploy/terraform/modules/ec2-k3s/main.tf` (`aws_instance.k3s`)

- [ ] **Step 1: Add the variable** to `variables.tf`:

```hcl
variable "cpu_credits" {
  type        = string
  default     = "unlimited"
  description = "T-family CPU credit mode. The box's measured capacity needs unlimited: docs/design/admission-sizing.md"
}
```

And to `aws_instance.k3s` in `main.tf`, after `metadata_options`:

```hcl
  credit_specification {
    cpu_credits = var.cpu_credits
  }
```

- [ ] **Step 2: Plan.** `terraform -chdir=deploy/terraform/envs/k3s validate`, then
  `terraform -chdir=deploy/terraform/envs/k3s plan`.
  Expected: `Success!`, and no change to `module.ec2_k3s.aws_instance.k3s`, because the
  box already runs `unlimited`. An SSH rule change from a new IP can appear; it is
  unrelated.

- [ ] **Step 3: Prove it is wired.** Set the default to `"standard"` and plan again.
  Expected: an in-place update, `cpu_credits = "unlimited" -> "standard"`. Restore
  `"unlimited"`, and do not apply.

- [ ] **Step 4: Commit**

```bash
git add deploy/terraform/modules/ec2-k3s/variables.tf deploy/terraform/modules/ec2-k3s/main.tf
git commit -m "feat(k3s): write the box's CPU credit mode down"
```

---

### Task 4: Sweep the door speed

**Files:**
- Create: `deploy/scripts/door-sweep.sh`

**Interfaces:**
- Consumes: `waitroom.releaseRate` (Task 1) and the config digest (Task 2), through
  `make -C deploy k3s-deploy HELM_EXTRA=...`; `deploy/scripts/purchase-cost.sh`,
  `deploy/scripts/saga-histories.sh`, `deploy/loadtest/saga_latency.py --burst-first`.
- Produces: one summary line per run, and the raw files under `$OUT`
  (default `/tmp/door-sweep`).

- [ ] **Step 1: Write the script**, then `chmod +x`:

```bash
#!/usr/bin/env bash
# Finds the k3s box's door speed. Each argument is one run: the waitroom's
# release rate is deployed at that rate, 40 buyers queue for 5 minutes, and one
# line of checkout latency, throughput, cost and queue is printed. The rule for
# choosing a rate: docs/design/admission-sizing.md. Needs REF, GW, KUBECONFIG and
# the Prometheus port-forward on :9090.
#   REF=sha-<build> deploy/scripts/door-sweep.sh 1 2 3 4 4 3 2 1
set -euo pipefail
HERE="$(cd "$(dirname "$0")" && pwd)"
NS=ticketbottle
PROM=${PROM:-http://localhost:9090}
OUT=${OUT:-/tmp/door-sweep}
: "${GW:?set GW, e.g. http://localhost:3000/api}"
export REF=${REF:?set REF to the sha- tag being measured; :dev can move mid-sweep}
mkdir -p "$OUT"

at() {
  curl -fsS "$PROM/api/v1/query" --data-urlencode "query=$1" --data-urlencode "time=$2" |
    python3 -c 'import sys, json; r = json.load(sys.stdin)["data"]["result"]; print(r[0]["value"][1] if r else "nan")'
}
temporal_idle() {
  local q='sum(rate(container_cpu_usage_seconds_total{namespace="ticketbottle",pod=~"temporal-[a-z0-9]+-[a-z0-9]+",container!=""}[1m]))'
  until python3 -c "import sys; sys.exit(0 if float('$(at "$q" "$(date -u +%FT%TZ)")') < 0.05 else 1)"; do sleep 10; done
}

n=0
for RATE in "$@"; do
  n=$((n + 1)); L="r$RATE-$n"
  make -C "$HERE/.." k3s-deploy HELM_EXTRA="--set waitroom.releaseRate=$RATE" > "$OUT/deploy-$L.txt" 2>&1 ||
    { echo "$L: deploy failed: $(tail -1 "$OUT/deploy-$L.txt")"; exit 1; }
  kubectl -n $NS rollout status deploy/waitroom-service --timeout=5m > /dev/null
  # The ConfigMap can say one rate while the pod still runs the last: ask the pod.
  started=$(kubectl -n $NS logs deploy/waitroom-service | grep 'Starting queue processor' | tail -1)
  grep -qE "batch_size: $RATE([^0-9]|$)" <<<"$started" || { echo "$L: waitroom runs '$started', not rate $RATE"; exit 1; }
  temporal_idle

  TOTAL=100000 VUS=40 DURATION=5m "$HERE/gate4a-load.sh" > "$OUT/run-$L.txt" 2>&1
  kubectl -n $NS logs job/k6-load > "$OUT/k6-$L.log"
  read -r FROM TO <<<"$(kubectl -n $NS get pod -l job-name=k6-load \
    -o jsonpath='{.items[0].status.startTime} {.items[0].status.containerStatuses[0].state.terminated.finishedAt}')"

  "$HERE/purchase-cost.sh" "$FROM" "$TO" "$OUT/k6-$L.log" > "$OUT/cost-$L.txt"
  QMIN=$(at 'min_over_time(sum(tb_waitroom_queue_depth)[4m:15s])' "$TO")
  SMAX=$(at 'max_over_time(sum(tb_waitroom_slots_in_use)[4m:15s])' "$TO")
  "$HERE/saga-histories.sh" "$FROM" "$TO" "$OUT/h-$L.txt" > /dev/null
  OPEN=$(python3 "$HERE/../loadtest/saga_latency.py" "$OUT/h-$L.txt" --burst-first 20 |
    awk '/first workflow task queued/ { print $5 "/" $8; exit }')

  python3 - "$L" "$RATE" "$FROM" "$TO" "$OUT" "$QMIN" "$SMAX" "$OPEN" "$HERE" <<'PY'
import os, re, sys
from datetime import datetime
L, rate, frm, to, out, qmin, smax, opening, here = sys.argv[1:]
sys.path.insert(0, os.path.join(here, "..", "loadtest"))
import saga_latency
log = open(f"{out}/k6-{L}.log").read()
ok = sorted(float(ms) / 1000 for ms, st in re.findall(r'msg="CHECKOUT \S+ ([\d.]+) (\d+)"', log) if int(st) < 400)
q = lambda p: ok[min(len(ok) - 1, round(p / 100 * (len(ok) - 1)))]
secs = (datetime.fromisoformat(to.replace("Z", "+00:00")) - datetime.fromisoformat(frm.replace("Z", "+00:00"))).total_seconds()
temporal = re.search(r"temporal\s+\S+ core-s\s+(\S+)", open(f"{out}/cost-{L}.txt").read()).group(1)
calm, stalled = saga_latency.without_stalls(saga_latency.load_histories(f"{out}/h-{L}.txt"), saga_latency.load_client(f"{out}/k6-{L}.log"))
print(f"{L:<7} rate {rate}/s | {len(ok)} purchases, {len(ok) / secs:.2f}/s | checkout p50 {q(50):.2f}s p99 {q(99):.2f}s,"
      f" {100 * sum(x <= 2 for x in ok) / len(ok):.1f}% under 2s | without payment stalls p99 {saga_latency.pct(calm, 99):.2f}s, {stalled} stalled"
      f" | temporal {temporal} core-s | queue min {float(qmin):.0f},"
      f" slots max {float(smax):.0f} | opening first-task wait p50/max {opening}s")
PY
done
```

The summary block was checked offline against the 2026-09-29 after-3 run's files. It
printed that run's known numbers: 923 purchases, 3.02/s, p99 4.33s, 86.3% under 2s.

- [ ] **Step 2: Deploy the chart once, before sweeping.** Setup first, then pick the
  build: `aws ecr describe-images --region us-east-1 --repository-name ticketbottle/gateway --image-ids imageTag=dev --query 'imageDetails[0].imageTags'`
  gives its `sha-` tag. Then run `make -C deploy k3s-deploy REF=<sha-tag>`.
  Every app rolls once, because the `checksum/config` annotation is new. Then check
  `kubectl -n ticketbottle logs deploy/waitroom-service | grep 'Starting queue processor'`.
  Expected: `batch_size: 10`.

- [ ] **Step 3: Predict before measuring.** Written here, 2026-09-29:
  - Every run: queue min above 0, and Temporal about 0.12 core-s per purchase.
  - At a passing rate: purchases a second close to the rate, and the opening
    first-task wait under 1s at its max.
  - The box served 3.0 purchases a second saturated, at a p99 of 4.3s.
  - So: rate 1 and rate 2 pass; rate 3 fails its p99; the door speed is **2**.

- [ ] **Step 4: Sweep, rates up then down.** Two runs of each rate, spread in time so
  that drift falls on both. About 75 minutes:

```bash
REF=<sha-tag> deploy/scripts/door-sweep.sh 1 2 3 4 4 3 2 1 | tee /tmp/door-sweep-summary.txt
```

- [ ] **Step 5: Read every line.** A run with `queue min 0` is void, because the door
  was not the limit: rerun that rate. A run whose Temporal cost is outside 0.11–0.13
  means the build or the box changed: stop, because the reason comes first.

- [ ] **Step 6: Fill *Results*** from the summary lines, and commit the script with them:

```bash
git add deploy/scripts/door-sweep.sh docs/plans/2026-09-29-admission-sizing.md
git commit -m "feat(loadtest): sweep the waitroom's door speed on the k3s box"
```

---

### Task 5: Set the k3s door speed and record it

**Files:**
- Modify: `deploy/helm/ticketbottle/values-k3s.yaml`
- Regenerate: `deploy/helm/ticketbottle/tests/golden/values-k3s.yaml`
- Modify: `docs/design/admission-sizing.md`, `docs/decisions/0019-the-waitroom-door-speed-is-sized-per-target.md`,
  `docs/decisions/README.md` (generated), `CLAUDE.md`,
  `.claude/skills/system-map/references/waitroom.md`, `services/waitroom-svc/CLAUDE.md`,
  this plan

- [ ] **Step 1: Apply the rule.** The door speed N is the highest rate whose checkout
  p99 is under 2s in both runs. If no rate passes, N is not set: stop, because the
  box's capacity at SLO is below 1 a second, and that goes to the architect.

- [ ] **Step 2: Set it** in `values-k3s.yaml`, with the build and date of the sweep:

```yaml
waitroom:
  # Measured <date> on a t3.large with unlimited CPU credits at <sha-tag>.
  # Re-measure when either changes: docs/design/admission-sizing.md.
  releaseRate: N
```

- [ ] **Step 3: Render.** `deploy/helm/ticketbottle/tests/render-golden.sh`, then
  `deploy/helm/ticketbottle/tests/assert-render.sh`. Expected: the golden diff is
  `QUEUE_DEFAULT_RELEASE_RATE` and the waitroom's `checksum/config`, nothing else. Then
  `all render assertions passed`.

- [ ] **Step 4: Deploy without `--set`.** `make -C deploy k3s-deploy REF=<sha-tag>`, then
  the log check from Task 4 Step 2. Expected: `batch_size: N`. The sweep's last
  `--set` did not outlive it.

- [ ] **Step 5: Record.**
  - **Design:** in `admission-sizing.md`, the Status line becomes built, and the
    *Current values* row for k3s gets N, the date and the build.
  - **0019:** `accepted`, with an `## Outcome` citing:
    - the commits;
    - the two render checks, red then green, and the two mutations;
    - the sweep's lines for N and N+1.
  - **Root `CLAUDE.md`:** the Open row "How fast may the waitroom admit buyers…" moves
    to *Decided*, owned by `docs/design/admission-sizing.md`.
  - **Waitroom map guide:** the row says built and measured.
  - **`services/waitroom-svc/CLAUDE.md`:** before *Single-replica constraint*:

```markdown
## Door speed is set per deployment target

`QUEUE_DEFAULT_RELEASE_RATE` is how fast buyers are admitted, and so how fast work
reaches the box. It comes from the chart's `waitroom.releaseRate`, measured per target
(`docs/design/admission-sizing.md`); the chart default of 10 is unmeasured. Room size,
`QUEUE_DEFAULT_MAX_CONCURRENT`, is a separate, per-event question.
```

- [ ] **Step 6: Check and commit.** Run `python3 docs/decisions/index.py`,
  `python3 docs/decisions/index.py --check` and
  `python3 .claude/skills/system-map/scripts/check_map.py`. Set this plan's status to
  COMPLETE, write *What this says*, then:

```bash
git add deploy/helm/ticketbottle docs CLAUDE.md .claude/skills/system-map services/waitroom-svc/CLAUDE.md
git commit -m "feat(k3s): admit buyers at the rate the box was measured to serve"
```

- [ ] **Step 7:** Stop the box: kill the port-forward and the tunnel, then run
  `make -C deploy stop-ec2-k3s` and confirm `stopped`.

## Results

| Run | Rate | Purchases/s | Checkout p50 / p99 | Under 2s | p99 without payment stalls, stalled | Temporal core-s | Queue min | Slots max | Opening wait p50 / max |
|---|---|---|---|---|---|---|---|---|---|
| r1-1 | 1 | 0.99 | 0.56 / 1.39s | 99.4% | 1.39s, 0 | 0.129 | 25 | 2 | 0.02 / 0.09s |
| r2-2 | 2 | 1.92 | 0.70 / **24.54s** | 93.0% | 1.59s, 40 | 0.120 | 32 | 11 | 0.04 / 0.11s |
| r3-3 | 3 | 2.95 | 1.11 / 2.49s | 95.1% | 2.49s, 0 | 0.115 | 27 | 10 | 0.08 / 0.32s |
| r4-4 | 4 | 3.15 | 1.35 / 3.38s | 82.2% | 3.38s, 0 | 0.111 | 1 | 37 | 0.14 / 2.51s |
| r4-5 | 4 | 3.15 | 1.25 / 3.24s | 87.1% | 3.24s, 0 | 0.118 | 1 | 38 | 0.10 / 0.35s |
| r3-6 | 3 | 2.96 | 1.04 / 2.42s | 95.6% | 2.42s, 0 | 0.116 | 22 | 14 | 0.09 / 0.18s |
| r2-7 | 2 | 1.98 | 0.68 / 1.56s | 100.0% | 1.56s, 0 | 0.126 | 31 | 4 | 0.03 / 0.11s |
| r1-8 | 1 | 0.99 | 0.56 / 1.51s | 99.7% | 1.51s, 0 | 0.140 | 29 | 2 | 0.02 / 0.06s |

2026-09-29, 05:53–07:17Z, build `sha-8312562`, 40 buyers for 5 minutes a run, rates
up then down. The door was the limit in every run: the queue never emptied. At rate
4 it came within one buyer of emptying, because the box tops out at 3.15 purchases a
second.

**One run in eight met an external stall.** In r2-2, `CreatePaymentIntent` (the
payment service's call to the ZaloPay sandbox) ran up to 25s for 40 checkouts
inside 25 seconds, while every Temporal queue wait stayed under 0.4s. The same
step stalled on 2026-09-28 at 20 buyers.

**The rule, amended by the architect.** Given raw p99, and the same p99 with every
checkout whose payment call ran past 5s left out and counted, the architect chose
the second: "A" (2026-09-29). The normal payment call peaks at 1.8s. A slower door
cannot shorten a provider stall, so raw p99 would size the box on ZaloPay.
`saga_latency.without_stalls` computes it, and the sweep prints it.

**Door speed: 2.** Rate 2 holds p99 under 2s in both runs (1.59s and 1.56s without
stalls). Rate 3 fails both, at 2.49s and 2.42s, with no stall in either.

## What this says

Not yet written.

## Found, not fixed

- **`outbox-relay`, `payment-webhook` and the migration Jobs carry no config digest.**
  They are not built from `tb.appService`, so a change to `payment-config` or a
  service's migration config does not roll them.
- **A door-speed change restarts the one waitroom replica.** During an on-sale that
  pauses admission for the restart. Redis keeps the queue and the slots, so no buyer
  is lost, but deploy a change between sales.
