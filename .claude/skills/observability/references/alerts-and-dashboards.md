# Alerts and dashboards

## The taxonomy is the alerting policy

| Code | Meaning | Alert? |
|---|---|---|
| `INTERNAL` | We have a bug | **Page**, on any sustained rate above zero |
| `UNAVAILABLE` | A dependency is down | Indirectly — `TargetDown` and the saga rules |
| `DEADLINE_EXCEEDED` | We did not answer in time | Covered by the latency burn rate |
| `FAILED_PRECONDITION` | Sold out, sale closed, wrong state | **Never** |
| `NOT_FOUND`, `ALREADY_EXISTS`, `INVALID_ARGUMENT`, `PERMISSION_DENIED`, `UNAUTHENTICATED` | Client-side outcomes | Never — a spike is a dashboard question |
| `RESOURCE_EXHAUSTED` | Deliberately unused | — |

The absence of a `FAILED_PRECONDITION` rule is the policy's load-bearing half, so it is recorded as a comment in `prometheusrule.yaml` and asserted against the live object. YAML comments never reach the API server, so a non-zero count means someone wrote a real rule:

```bash
kubectl -n ticketbottle get prometheusrule ticketbottle -o yaml | grep -c FAILED_PRECONDITION   # 0
```

**`OrdersNeedingRefund` is the one apparent exception, and it proves the rule.** Inventory refusing a confirm and an event selling out are both `FAILED_PRECONDITION`; they invert at the ledger:

```
   sold out          never charged, holds no ticket  → nothing owed  → silent
   REFUND_REQUIRED   WAS charged,   holds no ticket  → money owed    → pages
```

The decision follows the money, not the code.

## What is in the file

`deploy/helm/ticketbottle/templates/apps/prometheusrule.yaml`, values-gated on `monitoring.enabled`. **5 recording + 8 alerting rules.**

Recording rules use `level:metric:operations` — `tb:checkout_good:ratio1h`. The colon is the marker: nothing scraped from an exporter contains one, so a colon means a rule in your own config produced it and the definition is one grep away.

| Alert | `for` | Severity |
|---|---|---|
| `TicketBottleInternalErrors` | 5m | page |
| `OutboxBacklogGrowing` | 10m | page |
| `SagaCompensationSpike` | 10m | page |
| `OrdersNeedingRefund` | 10m | page |
| `TargetDown` | 3m | page |
| `MetricsMissing` | 5m | ticket |
| `CheckoutBurnRateFast` | 2m | page |
| `CheckoutBurnRateSlow` | 15m | ticket |

`TicketBottleInternalErrors` is deliberately first in the group, so a JSON-patch by index (`/spec/groups/1/rules/0/for`) reaches it.

## Two clocks

```
rate(x[5m])   smooths the MEASUREMENT — average over 5 min of samples, one number per evaluation
for: 5m       smooths the DECISION    — the expression must keep returning its series for 10
                                        consecutive evaluations (30s interval) before firing
```

Swapping their jobs gives one of two broken alerts:

| | Result |
|---|---|
| short window + long `for` | the signal decays before the timer completes — **never fires** |
| long window + `for: 0s` | fires on one sample, **flaps** as pods cycle |

Diagnosing a flapping alert — compare the signal at two windows. Spiky at 30s but steady at 5m means the signal is bursty and the rule needs a `for`; steady at both means the system really is failing.

## Burn rates

The SLO is **99% of `POST /api/orders` under 2s**, measured at the gateway because that is the request the buyer makes. The error budget is the complementary 1%; the alert answers *how fast it is being spent*.

`14.4` comes from "page when 2% of a 30-day budget burns in one hour": one hour is 1/720 of 30 days, so `0.02 × 720 = 14.4`.

Each alert `and`s a long and a short window, and both must exceed:

| Window | Job | Without it |
|---|---|---|
| long (1h, 6h) | significance — enough requests that the ratio is a measurement | one bad minute at low traffic pages you |
| short (5m, 30m) | reset — stops firing when the current rate drops | an incident that ended 40 min ago still pages |

**The SLI is latency-only.** `tb_grpc_request_duration_seconds` carries no `code` label, so a request that failed with `INTERNAL` in 40ms counts as *good*. Correctness is `TicketBottleInternalErrors`; the two are read together, and neither is a substitute for the other.

## `min_over_time` for spiky gauges

`tb_outbox_pending_rows` is instantaneous: the relay claims a batch, publishes, and the gauge drops back within seconds, so a scrape landing mid-batch reads a value that was true for 200ms. `min_over_time(...[10m]) > 50` only holds when the backlog never drained.

The cost is latency — the 10m window must fill, then `for: 10m` must elapse, so this pages roughly **20 minutes** after a relay stops. Size any experiment against that number.

## Escaping Prometheus templating in chart templates

Alert annotations use Prometheus's own `{{ }}` syntax, and Helm evaluates `{{ }}` first:

```
Error: parse error at (prometheusrule.yaml:51): undefined variable "$value"
```

This fails the **whole file**, not just the annotation. Wrap the inner template in a Go raw string:

```yaml
summary: "{{`{{ $labels.job }}`}}/{{`{{ $labels.pod }}`}} has not been scraped successfully"
```

Verify by rendering and confirming the plain form comes out the other side:

```bash
helm template tb deploy/helm/ticketbottle -f deploy/helm/ticketbottle/values-k3s.yaml \
  -s templates/apps/prometheusrule.yaml $(grep -oE 'secrets\.[a-zA-Z0-9]+' \
  deploy/helm/ticketbottle/templates/apps/secrets.yaml | sort -u | sed 's/secrets\.//' \
  | awk '{printf "--set secrets.%s=x ", $1}')
```

⚠️ Rendering this chart requires dummy `secrets.*` values — they are gitignored, so `helm template` fails on a missing key before it reaches your rule file. The `$(...)` above generates them.

**A bare `sum()` drops all labels**, so an alert aggregated that way cannot use `{{ $labels.service }}` in its summary — it renders empty. Either aggregate `by (service)`, or push the drill-down into the runbook's first check.

## Alertmanager

`deploy/monitoring/values-kps.yaml`. Keep the `alertmanagerSpec` sizing block when adding `config` — the two are siblings, and replacing the block drops `replicas` and the resource requests the t3.large needs.

| Setting | Delays | Does not delay |
|---|---|---|
| `group_wait` 30s | the first notification dispatch | the alert appearing in `/api/v2/alerts` |
| `group_interval` 5m | a follow-up when the group's alert set changes | the first send |
| `repeat_interval` 4h | re-sending an unchanged firing notification | the first send |
| `inhibit_rules` | suppresses targets while a source fires | their presence in the API — they show `status.state: "suppressed"` |

The `null` receiver is deliberate: alerts reach `/api/v2/alerts` whether or not a receiver exists, so the gates need no Slack webhook. The inhibit rule drops `severity="page"` alerts in a namespace while `TargetDown` fires there — so during a node failure, check for `suppressed` before concluding nothing else is wrong.

## A rolling restart is `UNAVAILABLE`, not `INTERNAL`

Restarting a deployment under load makes the gateway's calls fail to connect, which is a dependency being down. It is **not** a bug, so it must not produce `INTERNAL` — and empirically it does not:

```promql
count by (code) (count_over_time(tb_grpc_requests_total[6h]))
   OK, UNAVAILABLE          # no INTERNAL from pod cycling
```

Any test that expects a restart to trip an `INTERNAL` alert is wrong about the taxonomy. To drive the `for` state machine, point the rule at `UNAVAILABLE`, which a restart genuinely produces. If a restart *did* produce `INTERNAL`, the error mapping is the defect.

## Dashboards

Three, in `deploy/monitoring/dashboards/`, ordered symptom-first:

| | Watches | Method | Answers |
|---|---|---|---|
| `gateway-red.json` | the edge | RED | "is it broken, and for whom" |
| `saga-health.json` | the order workflow | RED | "which step of the machinery is failing" |
| `queue-outbox.json` | waitroom + outbox | USE | "is anything falling behind" |

**RED vs USE is a mechanical choice:** does the object have a request you can count? Yes → Rate/Errors/Duration. No (a queue, a pool) → Utilization/Saturation/Errors.

Reading B or C before A means diagnosing a healthy saga while the gateway is refusing connections.

**A panel must answer two questions before it is built:** what it computes (precisely — "the ratio of non-`OK` gRPC responses to all responses, per service, over 5 minutes"), and what a spike means plus what you do next. If the second answer is "I don't know", it does not ship — an uninterpretable panel is still read during an incident and still steers the next action.

**Split errors by `code`.** One error-rate line merges a sell-out with an outage; A4 is stacked by `code` so the band is readable.

## Provisioning

Grafana's DB here is an `emptyDir` and dies with the pod. Dashboards live as **labelled ConfigMaps**, re-imported on every start:

```bash
kubectl -n monitoring create configmap tb-dash-<name> \
  --from-file=<name>.json=deploy/monitoring/dashboards/<name>.json \
  --dry-run=client -o yaml | kubectl label --local -f - grafana_dashboard=1 -o yaml | kubectl apply -f -
```

The `grafana-sc-dashboard` sidecar watches for the label key, writes each data key into a volume shared with Grafana, and the file provisioner imports it — seconds, no restart.

⚠️ **The ConfigMap must be in a namespace the sidecar searches** (`monitoring` by default; `grafana.sidecar.dashboards.searchNamespace: ALL` widens it). One in `ticketbottle` is ignored with no error.

⚠️ **Export v1, not v2.** Grafana 13 stores dashboards in schema v2 (`spec.elements`), which is what Settings → JSON Model and the UI export give you. The sidecar's file provisioner reads classic v1, and every `jq '.panels[]'` walks v1. Ask the API:

```bash
curl -s -u admin:"$PW" \
  'localhost:3001/apis/dashboard.grafana.app/v1beta1/namespaces/default/dashboards/<uid>' | jq '.spec'
jq -r '.schemaVersion, (.panels|length)' <file>    # want a number and a count, not null
```

A v2 file fails every check silently by returning no rows rather than an error.

## Runbooks

One section per alert, and the annotation on each rule points at it. The template:

```markdown
## <AlertName>
**Means:** <what is true about the system when this fires>
**Does not mean:** <the nearest thing it gets confused with>
**First three checks**
1. <a command, and what its output must say>
2. <a panel, named, and what to read off it>
3. <the one query separating the two most likely causes>
**Resolved looks like:** <the observable that returns to normal, and how long that takes>
**If it does not resolve:** <the next escalation>
```

"Does not mean" is the line that does the work — it is where the taxonomy gets restated at the moment someone is about to misread a code.

**"Resolved looks like" is not always "the alert cleared."** `OrdersNeedingRefund` clears ten minutes after the last occurrence, but nothing consumes `order.refund_required`, so no one has been refunded. Say so in the runbook, or it lies during an incident.
