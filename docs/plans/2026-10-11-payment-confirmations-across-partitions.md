# Payment confirmations run across partitions

**Status: COMPLETE 2026-10-12.** `payment.completed` has 8 partitions before
order-consumer starts. On k3s, `ConfirmOrder` ran up to 4 at once where one partition
never ran 2, and all 368 purchases completed. Decision:
→ [0030](../decisions/0030-payment-confirmations-run-across-partitions.md), accepted.

**Goal:** `payment.completed` has 8 partitions before order-consumer starts, on a
fresh install, an upgrade and a restart, and the consumer reads a partition it has
no offset for from its oldest message, so confirmations run in parallel and none is
skipped.

**Question it serves:** the first wall in
`docs/plans/2026-10-09-capacity-on-a-bigger-box.md#what-this-says`: one partition and
one `ConfirmOrder` at a time cap purchases at about 9.5 a second, below EKS's door of
10, and a confirm that falls past a hold's 9 minutes can leave a paid buyer
`REFUND_REQUIRED`.

**Spec:** none. Derived from that plan's results and from
[0030](../decisions/0030-payment-confirmations-run-across-partitions.md).

## How it works

```
values.yaml  redpanda.partitions: { payment.completed: 8 }
                 │
                 ▼
order-consumer pod
  init container ensure-topics   (Redpanda image, files/ensure-topics.sh)
    wait for redpanda:9093 → create the topic at 8, or grow it to 8 → fail unless it has 8
                 │  only then
                 ▼
  container order-consumer       (sarama, Offsets.Initial = OffsetOldest)
    claims all 8 partitions; one ConfirmOrder at a time per partition
```

The relay already keys each message by payment id, so one payment's events stay on
one partition and in order.

## Found while writing this plan

- **A running consumer is slow to see new partitions.** sarama checks the partition
  count from its cached metadata every `Metadata.RefreshFrequency`, 10 minutes by
  default (`sarama@v1.46.1/consumer_group.go`, `loopCheckPartitionNumbers`), so new
  partitions can go unread for up to 20 minutes, past a hold's 3-minute grace. Hence
  the init container: the consumer starts after the count is final.
- **`OffsetNewest` drops events when partitions are added to a live topic.** A
  partition with no committed offset starts wherever its end is when the group
  claims it, and anything the relay wrote there first is skipped
  (`services/order-svc/pkg/kafka/consumer.go`). `ResetInvalidOffsets` defaults to
  true in sarama 1.46.1, so a stale offset also falls back to `Offsets.Initial`.
- **Reading from the oldest message is safe to repeat.** `ConfirmOrder` returns nil
  for a `COMPLETED` order, and every other final status is a no-op or already
  recorded (`services/order-svc/internal/workflows/confirm_order.go`).
- **The chart creates no topics.** The first client to ask for one creates it at
  Redpanda's default of one partition. A hook Job after install would lose that race.
- **The k3s box cannot show the throughput gain.** It is CPU-bound at about 2.7 a
  second, below one partition's 9.5. What it can show is concurrency: with one
  partition, no two `ConfirmOrder`s ever overlap (256 and 293 confirms on
  2026-10-11, never two at once). With 8, the door admits buyers two at a time, and
  their payments land on different partitions 7 times in 8.
- **The capacity overlay's render check fails on any `limits:` in a Deployment**
  (`tests/assert-render.sh`). The init container sets requests only.
- **Only k3s has a golden file.** The new render check renders both overlays.

## Global constraints

- Commit messages describe the platform, never study progress, and end with
  `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`.
- Pushing `dev` needs the architect's go-ahead.
- The k3s box and EKS never run at once: `aws eks list-clusters --region us-east-1`
  must print nothing before the box starts.
- Comment budget: 3 lines inline, 5 on a symbol, 8 for a module (root `CLAUDE.md`).
- Only `payment.completed` changes. `payment.failed` and the waitroom's topics keep
  one partition, and the waitroom's consumer keeps `OffsetNewest`.
- A partition count only grows. Lowering a value in `redpanda.partitions` does nothing.
- One build for the k3s session: pass its `sha-` tag as `REF`, never the moving `:dev`.

## Review focus

1. **A fresh install where the relay creates the topic first.** The init container's
   create then fails and the topic has 1 partition. It must exit non-zero and retry,
   never start the consumer on 1. The script's last check guarantees it (Task 2);
   nothing exercises the race itself.
2. **A cold boot where Redpanda is not ready.** The init container waits for the
   broker. Task 5 starts the box from stopped, so its log shows the wait or a clean
   first try.
3. **A consumer group whose offsets are gone,** for example after the box was stopped
   past the broker's offset retention. Reading from the oldest message replays what
   is retained; each replay finds a finished order and changes nothing.
4. **A change to `redpanda.image`.** The init container uses the same image, and rpk's
   flags can change between versions. Task 2's test reads the version from
   `values.yaml`; rerun it whenever that changes.
5. **Two consumer replicas starting together.** Both may grow the topic. Kafka's
   CreatePartitions request names the new total, so both ask for 8 and the second is
   refused; its init container fails, retries and finds 8. Both targets run one
   replica.

---

### Task 1: The consumer reads an unclaimed partition from its oldest message

**Files:**
- Modify: `services/order-svc/pkg/kafka/consumer.go`
- Create: `services/order-svc/pkg/kafka/consumer_test.go`
- Modify: `services/order-svc/CLAUDE.md` (the `cmd/consumer` line)

**Interfaces:**
- Produces: `newConsumerConfig() *sarama.Config`, unexported, used by `NewConsumer`.

- [ ] **Step 1: Write the failing test** at `pkg/kafka/consumer_test.go`:

```go
package kafka

import (
	"testing"

	"github.com/IBM/sarama"
)

// A partition added to a live topic is written to before the group claims it.
func TestAnUnclaimedPartitionIsReadFromItsOldestMessage(t *testing.T) {
	c := newConsumerConfig()
	if c.Consumer.Offsets.Initial != sarama.OffsetOldest {
		t.Fatalf("Offsets.Initial = %d, want sarama.OffsetOldest (%d)", c.Consumer.Offsets.Initial, sarama.OffsetOldest)
	}
	if !c.Consumer.Group.ResetInvalidOffsets {
		t.Fatal("ResetInvalidOffsets is off: a recreated topic would leave the group past its end")
	}
}
```

- [ ] **Step 2: Extract the config, behaviour unchanged.** In `consumer.go`, move the
  config into its own function and call it from `NewConsumer`:

```go
func newConsumerConfig() *sarama.Config {
	c := sarama.NewConfig()
	c.Version = sarama.V2_8_0_0
	c.Consumer.Group.Rebalance.Strategy = sarama.NewBalanceStrategyRoundRobin()
	c.Consumer.Offsets.Initial = sarama.OffsetNewest
	c.Consumer.Return.Errors = true
	return c
}

func NewConsumer(cfg ConsumerConfig) (sarama.ConsumerGroup, error) {
	consGroup, err := sarama.NewConsumerGroup(cfg.Brokers, cfg.GroupID, newConsumerConfig())
	if err != nil {
		return nil, fmt.Errorf("failed to create kafka consumer group: %w", err)
	}

	log.Printf("Kafka consumer connected to brokers: %v, group: %s\n", cfg.Brokers, cfg.GroupID)

	return consGroup, nil
}
```

- [ ] **Step 3: Run it.** From `services/order-svc`: `go test -count=1 ./pkg/kafka/`.
  Expected: `FAIL`, with `Offsets.Initial = -1, want sarama.OffsetOldest (-2)`. A
  compile error instead means Step 2 is incomplete.

- [ ] **Step 4: Read from the oldest.** In `newConsumerConfig`, the `Offsets.Initial`
  line becomes `sarama.OffsetOldest`, with its doc comment:

```go
// newConsumerConfig reads a partition with no committed offset from its oldest
// message. A partition added to a live topic is written to before the group claims
// it, so starting at the newest would skip payments (decision 0030).
```

- [ ] **Step 5: Run the package, then the service as CI does.** `go test -count=1
  ./pkg/kafka/`, then `go test -race -count=1 ./...`. Expected: `ok` for
  `pkg/kafka`, and no `FAIL` anywhere: 9 packages `ok`, one more than the 8 before
  this task.

- [ ] **Step 6: Document it.** In `services/order-svc/CLAUDE.md`, the line
  ``- `cmd/consumer` — Kafka consumer that triggers `ConfirmOrder` on `payment.completed`; its worker also runs `ExpireOrder`.``
  gains a second sentence:
  `` One `ConfirmOrder` runs at a time per partition; the partition count comes from `redpanda.partitions` in the chart, and a partition without a committed offset is read from its oldest message ([0030](../../docs/decisions/0030-payment-confirmations-run-across-partitions.md)).``

- [ ] **Step 7: Commit**

```bash
git add services/order-svc/pkg/kafka services/order-svc/CLAUDE.md
git commit -m "feat(order): read a partition without an offset from its oldest message"
```

---

### Task 2: A script that gives a topic its partitions

**Files:**
- Create: `deploy/helm/ticketbottle/files/ensure-topics.sh`
- Create: `deploy/helm/ticketbottle/tests/ensure-topics-test.sh`

**Interfaces:**
- Produces: `ensure-topics.sh BROKER TOPIC=PARTITIONS...`. It waits for the broker,
  creates each missing topic at its count or grows a smaller one to it, never
  shrinks one, prints `<topic>: <n> partitions` for each, and exits non-zero unless
  every topic has at least its count. Task 3 runs it as `sh -c "<script>"
  ensure-topics redpanda:9093 payment.completed=8`.

- [ ] **Step 1: Write the failing test** at `tests/ensure-topics-test.sh`, then
  `chmod +x` it:

```bash
#!/usr/bin/env bash
# The script order-consumer's init container runs (decision 0030), against a
# throwaway Redpanda of the chart's version. Needs Docker; CI does not run it.
set -euo pipefail
HERE="$(cd "$(dirname "$0")" && pwd)"
IMAGE=$(awk '/^redpanda:/ { r = 1 } r && /image:/ { print $2; exit }' "$HERE/../values.yaml")
NAME=ensure-topics-test-$$
fail() { echo "FAIL: $1"; exit 1; }

docker run -d --rm --name "$NAME" "$IMAGE" redpanda start --mode dev-container --smp 1 >/dev/null
trap 'docker rm -f "$NAME" >/dev/null' EXIT
run() { docker exec -i "$NAME" sh -s -- localhost:9092 "$@" < "$HERE/../files/ensure-topics.sh"; }
parts() { docker exec "$NAME" rpk topic list | awk -v t="$1" '$1 == t { print $2 }'; }

run payment.completed=8 >/dev/null
[ "$(parts payment.completed)" = 8 ] || fail "a missing topic was not created with 8 partitions"
run payment.completed=8 >/dev/null
[ "$(parts payment.completed)" = 8 ] || fail "a second run changed a topic that had its partitions"
docker exec "$NAME" rpk topic create older -p 1 >/dev/null
run older=4 >/dev/null
[ "$(parts older)" = 4 ] || fail "a topic with 1 partition did not grow to 4"
run payment.completed=2 >/dev/null
[ "$(parts payment.completed)" = 8 ] || fail "a topic shrank"
run a=2 b=3 >/dev/null
{ [ "$(parts a)" = 2 ] && [ "$(parts b)" = 3 ]; } || fail "two topics in one run were not both created"
echo "ensure-topics: creates, grows, never shrinks, leaves a finished topic alone"
```

- [ ] **Step 2: Run it.** `deploy/helm/ticketbottle/tests/ensure-topics-test.sh`. The
  first run pulls `redpandadata/redpanda:v24.2.7` if it is not local. Expected: a
  shell error naming `files/ensure-topics.sh: No such file or directory`, and a
  non-zero exit. `docker ps -a | grep ensure-topics-test` prints nothing afterwards.

- [ ] **Step 3: Write the script** at `files/ensure-topics.sh`, then `chmod +x` it:

```sh
#!/bin/sh
# Gives each topic at least its partition count, waiting for the broker first, and
# fails unless every topic has it. Never shrinks one: Kafka cannot remove a
# partition. Run by order-consumer's init container (decision 0030).
#   ensure-topics.sh BROKER TOPIC=PARTITIONS...
set -eu
broker=$1
shift
count() { rpk topic list -X brokers="$broker" | awk -v t="$1" '$1 == t { print $2 }'; }

until rpk cluster info -X brokers="$broker" >/dev/null 2>&1; do
  echo "waiting for $broker"
  sleep 2
done
for spec in "$@"; do
  topic=${spec%=*}
  want=${spec#*=}
  have=$(count "$topic")
  if [ -z "$have" ]; then
    rpk topic create "$topic" -p "$want" -X brokers="$broker"
  elif [ "$have" -lt "$want" ]; then
    rpk topic add-partitions "$topic" --num $((want - have)) -X brokers="$broker"
  fi
  got=$(count "$topic")
  echo "$topic: ${got:-0} partitions"
  [ "${got:-0}" -ge "$want" ] || { echo "$topic has ${got:-0} partitions, wants $want"; exit 1; }
done
```

- [ ] **Step 4: Run it again.** Expected, last line:
  `ensure-topics: creates, grows, never shrinks, leaves a finished topic alone`.

- [ ] **Step 5: Prove the test can go red.** Delete the `elif` branch (its two lines)
  and run again. Expected: `FAIL: a topic with 1 partition did not grow to 4`.
  Restore it. If disk is short, `docker rmi redpandadata/redpanda:v24.2.7`
  afterwards; Task 3 needs only the chart.

- [ ] **Step 6: Commit**

```bash
git add deploy/helm/ticketbottle/files/ensure-topics.sh deploy/helm/ticketbottle/tests/ensure-topics-test.sh
git commit -m "feat(chart): a script that gives a topic its partitions before a consumer starts"
```

---

### Task 3: Order-consumer starts only after its topics have their partitions

**Files:**
- Modify: `deploy/helm/ticketbottle/values.yaml` (`redpanda:`)
- Modify: `deploy/helm/ticketbottle/templates/apps/_appservice.tpl`
- Modify: `deploy/helm/ticketbottle/templates/apps/order.yaml`
- Modify: `deploy/helm/ticketbottle/tests/assert-render.sh`, `tests/golden/values-k3s.yaml` (regenerated)
- Modify: `docs/design/admission-sizing.md` (*The rule*)

**Interfaces:**
- Consumes: `files/ensure-topics.sh` (Task 2).
- Produces: the chart value `redpanda.partitions`, a map of topic to count, and the
  `tb.appService` parameter `ensureTopics` (bool).

- [ ] **Step 1: Write the failing check.** In `tests/assert-render.sh`, just before
  the comment `# A capacity session (decision 0029) lifts every limit`:

```bash
# order-consumer starts only once payment.completed has its partitions (decision 0030).
for overlay in k3s eks; do
  r=$(helm template tb "$CHART" -f "$CHART/values-$overlay.yaml" -f "$SECRETS")
  consumer=$(awk 'BEGIN { RS = "\n---\n" } /kind: Deployment/ && /\n  name: order-consumer\n/' <<<"$r")
  grep -q 'name: ensure-topics' <<<"$consumer" || fail "$overlay: order-consumer has no ensure-topics init container"
  grep -q '"payment.completed=8"' <<<"$consumer" || fail "$overlay: order-consumer does not ensure payment.completed has 8 partitions"
  others=$(awk 'BEGIN { RS = "\n---\n" } /kind: Deployment/ && /ensure-topics/ && !/\n  name: order-consumer\n/' <<<"$r")
  [ -z "$others" ] || fail "$overlay: a Deployment other than order-consumer runs ensure-topics"
done
echo "OK  order-consumer starts after payment.completed has 8 partitions"
```

- [ ] **Step 2: Run it.** `deploy/helm/ticketbottle/tests/assert-render.sh`. Expected:
  `FAIL: k3s: order-consumer has no ensure-topics init container`.

- [ ] **Step 3: The value.** In `values.yaml`, the `redpanda:` block becomes:

```yaml
redpanda:
  image: redpandadata/redpanda:v24.2.7
  storage: 2Gi
  # Partitions a topic has before order-consumer starts (decision 0030). A count
  # can grow but never shrink, and growing it moves keys to other partitions.
  partitions:
    payment.completed: 8
```

- [ ] **Step 4: The init container.** In `templates/apps/_appservice.tpl`, between the
  `drainSeconds` block's `{{- end }}` and `containers:`:

```yaml
      {{- if .ensureTopics }}
      {{- /* A consumer already running would not claim new partitions for up to
             20 minutes, so it starts after they exist. See docs/decisions/0030. */}}
      initContainers:
        - name: ensure-topics
          image: {{ $.Values.redpanda.image }}
          command:
            - sh
            - -c
            - {{ $.Files.Get "files/ensure-topics.sh" | quote }}
            - ensure-topics
            - redpanda:9093
            {{- range $topic, $n := $.Values.redpanda.partitions }}
            - {{ printf "%s=%d" $topic (int $n) | quote }}
            {{- end }}
          resources: { requests: { cpu: 10m, memory: 32Mi } }
      {{- end }}
```

  In `templates/apps/order.yaml`, the `order-consumer` line's dict gains
  `"ensureTopics" true` after `"metricsPort" 2112`.

- [ ] **Step 5: Run it.** Expected: the k3s golden diff fails first, with
  `FAIL: k3s drifted`, and the diff adds only the `initContainers` block to
  order-consumer. Read that diff: nothing else may change. Then run
  `deploy/helm/ticketbottle/tests/render-golden.sh` and the assertions again.
  Expected: `OK  order-consumer starts after payment.completed has 8 partitions`
  and `all render assertions passed`. The capacity check still passes, because the
  init container sets no limit.

- [ ] **Step 6: Prove the check can go red.** Change `payment.completed: 8` to `4` in
  `values.yaml`, render the goldens again, and run. Expected:
  `FAIL: k3s: order-consumer does not ensure payment.completed has 8 partitions`.
  Restore 8 and render the goldens once more; `git diff --stat` shows the golden
  with only the init container added.

- [ ] **Step 7: The rule it adds to the door.** In `docs/design/admission-sizing.md`,
  *The rule*, after the *Room size* bullet:

```markdown
- A door must stay below the confirm leg's capacity, which its sizing does not
  measure: about partitions ÷ confirm latency
  ([0030](../decisions/0030-payment-confirmations-run-across-partitions.md)).
```

  Then `python3 .claude/skills/system-map/scripts/check_map.py`. Expected:
  `every cited path exists and every tracked document is cited`.

- [ ] **Step 8: Commit**

```bash
git add deploy/helm/ticketbottle docs/design/admission-sizing.md
git commit -m "feat(chart): start order-consumer after payment.completed has 8 partitions"
```

---

### Task 4: A tool that shows how many confirms ran at once

**Files:**
- Create: `deploy/loadtest/confirm_overlap.py`, `deploy/loadtest/confirm_overlap_test.py`
- Create: `deploy/scripts/confirm-overlap.sh`

**Interfaces:**
- Produces: `confirm_overlap.py LIST.jsonl`, printing one line, and
  `confirm-overlap.sh FROM TO`, which lists the ConfirmOrder workflows started in
  `[FROM, TO)` and runs it. Task 5 uses the second.

- [ ] **Step 1: Write the failing tests** at `deploy/loadtest/confirm_overlap_test.py`:

```python
"""The ConfirmOrder overlap summary. Run: python3 deploy/loadtest/confirm_overlap_test.py"""
import json
import os
import sys
import unittest

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import confirm_overlap  # noqa: E402


def line(start, close=None):
    r = {"type": {"name": "ConfirmOrder"}, "startTime": start}
    if close:
        r["closeTime"] = close
    return json.dumps(r)


class Peak(unittest.TestCase):
    def test_two_open_at_once(self):
        self.assertEqual(confirm_overlap.peak([(0, 1), (0.5, 1.5)]), 2)

    def test_one_closing_as_the_next_starts_is_not_an_overlap(self):
        self.assertEqual(confirm_overlap.peak([(0, 1), (1, 2)]), 1)

    def test_none(self):
        self.assertEqual(confirm_overlap.peak([]), 0)


class Intervals(unittest.TestCase):
    def test_closed_workflows_in_start_order_and_running_ones_skipped(self):
        spans = confirm_overlap.intervals([
            line("2026-10-11T08:27:29.841762Z", "2026-10-11T08:27:29.996747Z"),
            line("2026-10-11T08:27:29.500000123Z", "2026-10-11T08:27:29.600000Z"),
            line("2026-10-11T08:27:30Z"),
            "",
        ])
        self.assertEqual(len(spans), 2)
        self.assertAlmostEqual(spans[0][1] - spans[0][0], 0.099999877, places=6)
        self.assertAlmostEqual(spans[1][1] - spans[1][0], 0.154985, places=6)


class Summary(unittest.TestCase):
    def test_one_line(self):
        self.assertEqual(confirm_overlap.summary([(0, 0.1), (0.05, 0.2), (1.0, 1.1)]),
                         "3 confirms over 1.1s, at most 2 at once, mean 117ms, 2.73/s")

    def test_none(self):
        self.assertEqual(confirm_overlap.summary([]), "0 confirms")


if __name__ == "__main__":
    unittest.main()
```

- [ ] **Step 2: Run them.** `PYTHONDONTWRITEBYTECODE=1 python3 deploy/loadtest/confirm_overlap_test.py`.
  Expected: `ModuleNotFoundError: No module named 'confirm_overlap'`.

- [ ] **Step 3: Write the module** at `deploy/loadtest/confirm_overlap.py`:

```python
"""How many ConfirmOrder workflows ran at once (decision 0030).

Reads `temporal workflow list -o jsonl` output and prints one line: how many closed,
over how long, the most open at one instant, their mean duration and rate. One
partition never shows more than 1.
  python3 deploy/loadtest/confirm_overlap.py LIST.jsonl
"""
import json
import sys
from datetime import datetime, timezone


def seconds(ts):
    """Epoch seconds from an RFC 3339 UTC time with any number of fractional digits."""
    base, _, frac = ts.rstrip("Z").partition(".")
    whole = datetime.fromisoformat(base).replace(tzinfo=timezone.utc).timestamp()
    return whole + (float("0." + frac) if frac else 0.0)


def intervals(lines):
    """(start, close) of each closed workflow, in start order."""
    rows = (json.loads(l) for l in lines if l.strip())
    return sorted((seconds(r["startTime"]), seconds(r["closeTime"])) for r in rows if r.get("closeTime"))


def peak(spans):
    """The most spans open at one instant; a close sorts before a start at the same time."""
    open_now, most = 0, 0
    for _, step in sorted([(s, 1) for s, _ in spans] + [(c, -1) for _, c in spans]):
        open_now += step
        most = max(most, open_now)
    return most


def summary(spans):
    if not spans:
        return "0 confirms"
    span = max(c for _, c in spans) - spans[0][0]
    mean = sum(c - s for s, c in spans) / len(spans)
    return (f"{len(spans)} confirms over {span:.1f}s, at most {peak(spans)} at once, "
            f"mean {1000 * mean:.0f}ms, {len(spans) / span:.2f}/s")


if __name__ == "__main__":
    with open(sys.argv[1]) as f:
        print(summary(intervals(f)))
```

- [ ] **Step 4: Run them.** The same command with `-W error::ResourceWarning` after
  `python3`. Expected: `Ran 6 tests`, `OK`.

- [ ] **Step 5: Prove they can go red.** In `peak`, sort with
  `key=lambda e: (e[0], -e[1])`. Expected: `test_one_closing_as_the_next_starts_is_not_an_overlap`
  fails. Restore. If the 2026-10-11 lists are still in `/tmp/capacity-sweep/`, also run
  the module on `confirm-v640.jsonl`. Expected:
  `256 confirms over 30.0s, at most 1 at once, mean 109ms, 8.54/s`.

- [ ] **Step 6: Write the wrapper** at `deploy/scripts/confirm-overlap.sh`, then
  `chmod +x` it and check it parses with `bash -n`:

```bash
#!/usr/bin/env bash
# How many ConfirmOrder workflows started in [FROM, TO) ran at once (decision 0030).
# One partition never shows more than 1. Temporal keeps a history for 24h.
#   deploy/scripts/confirm-overlap.sh 2026-10-11T08:27:00Z 2026-10-11T08:27:30Z
set -euo pipefail
HERE="$(cd "$(dirname "$0")" && pwd)"
FROM=${1:?usage: confirm-overlap.sh FROM TO  (RFC 3339, UTC)}
TO=${2:?usage: confirm-overlap.sh FROM TO}
LIST=$(mktemp)
trap 'rm -f "$LIST"' EXIT

kubectl -n ticketbottle exec deploy/temporal -- temporal workflow list \
  --query "WorkflowType=\"ConfirmOrder\" AND StartTime >= \"$FROM\" AND StartTime < \"$TO\"" \
  --limit 100000 -o jsonl > "$LIST"
python3 "$HERE/../loadtest/confirm_overlap.py" "$LIST"
```

- [ ] **Step 7: Commit**

```bash
git add deploy/loadtest/confirm_overlap.py deploy/loadtest/confirm_overlap_test.py deploy/scripts/confirm-overlap.sh
git commit -m "feat(loadtest): report how many ConfirmOrder workflows ran at once"
```

---

### Task 5: Verify on k3s

The architect runs it from this guide and pastes the output. About 40 minutes on the
t3.large.

- [ ] **Step 1: Push and build.** With the architect's go-ahead, push `dev`. CI builds
  every image, because Task 1 changes `services/**`. Note the tag:

```bash
aws ecr describe-images --region us-east-1 --repository-name ticketbottle/gateway \
  --image-ids imageTag=dev --query 'imageDetails[0].imageTags' --output text   # dev  sha-<HEAD>
export TAG=sha-$(git rev-parse HEAD)
```

- [ ] **Step 2: Start the box and open the session.**

```bash
aws eks list-clusters --region us-east-1     # "clusters": []
make -C deploy my-ip                         # matches the tfvars, or run k3s-allow-ip first
make -C deploy start-ec2-k3s                 # prints the tunnel command; run it in its own terminal
make -C deploy k3s-kubeconfig && export KUBECONFIG=/tmp/k3s.yaml
export GW=http://localhost:3000/api
kubectl -n ticketbottle get pods             # wait until every pod is Running
kubectl -n ticketbottle exec redpanda-0 -- rpk topic list | grep payment.completed   # payment.completed  1  1
```

- [ ] **Step 3: Deploy, and read the partitions back.**

```bash
make -C deploy k3s-deploy REF=$TAG
kubectl -n ticketbottle exec redpanda-0 -- rpk topic list | grep payment.completed   # payment.completed  8  1
kubectl -n ticketbottle logs deploy/order-consumer -c ensure-topics                  # ends "payment.completed: 8 partitions"
kubectl -n ticketbottle exec redpanda-0 -- rpk group describe order-service | grep -c '^payment.completed'   # 8
make -C deploy k3s-gate2                                                             # GATE 1 PASSED
```

  The init container's log shows the `add-partitions` result above its last line. A
  log ending `wants 8` means the topic did not grow, so stop.

- [ ] **Step 4: Predict before measuring.** Written here, 2026-10-11: under a 3-minute
  load at the k3s door of 2 a second, `confirm-overlap.sh` prints `at most 2 at once`
  or more, and no buyer's order fails to complete. With one partition it never
  printed more than 1.

- [ ] **Step 5: Load, then count the overlaps.**

```bash
TOTAL=100000 VUS=20 DURATION=3m deploy/scripts/gate4a-load.sh | tail -3          # load generator finished (3m, 20 VUs)
kubectl -n ticketbottle logs job/k6-load > /tmp/k6.log
python3 -c "import sys; sys.path.insert(0, 'deploy/loadtest'); import capacity; s = open('/tmp/k6.log').read(); print('completed', capacity.counter(s, 'tb_orders_completed'), 'unexpected', capacity.counter(s, 'tb_unexpected_errors'))"
read -r FROM TO <<<"$(kubectl -n ticketbottle get pod -l job-name=k6-load \
  -o jsonpath='{.items[0].status.startTime} {.items[0].status.containerStatuses[0].state.terminated.finishedAt}')"
deploy/scripts/confirm-overlap.sh "$FROM" "$TO"                                  # at most 2 at once, or more
```

  Expected: `completed <N> unexpected 0`. k6 prints its summary as indented JSON,
  which `capacity.counter` reads; a counter never incremented counts as 0.
  `unexpected 0` means every buyer who checked out saw `COMPLETED` within 3 minutes. An `at most 1 at once` line means confirms are still serial, so stop.

- [ ] **Step 6: A restart changes nothing.**

```bash
kubectl -n ticketbottle delete pod -l app=order-consumer && kubectl -n ticketbottle rollout status deploy/order-consumer
kubectl -n ticketbottle logs deploy/order-consumer -c ensure-topics    # only "payment.completed: 8 partitions"
kubectl -n ticketbottle exec redpanda-0 -- rpk topic list | grep payment.completed   # still 8
```

- [ ] **Step 7: Stop the box.** Kill the tunnel, run `make -C deploy stop-ec2-k3s`, and
  confirm `stopped`.

---

### Task 6: Record

**Files:**
- Modify: this plan (*Results*, status)
- Modify: `docs/decisions/0030-payment-confirmations-run-across-partitions.md`, `docs/decisions/README.md` (generated)
- Modify: `CLAUDE.md` (the register row)

- [ ] **Step 1: Fill *Results*** from the pasted output: the partition count before and
  after, the init container's lines, the consumer group's claims, gate2, the load's
  completed and unexpected counts, the overlap line, and the restart.

- [ ] **Step 2: Accept 0030.** `accepted`, with an `## Outcome` citing the commits, each
  test that went red then green and its mutation, the session's build, and the overlap
  line against one partition's never more than 1.

- [ ] **Step 3: The register row.** Its state becomes `built and verified on k3s
  <date>`.

- [ ] **Step 4: Check and commit.** `python3 docs/decisions/index.py`,
  `python3 docs/decisions/index.py --check`,
  `python3 .claude/skills/system-map/scripts/check_map.py`. Set this plan's status to
  COMPLETE, then:

```bash
git add docs CLAUDE.md
git commit -m "docs: record payment confirmations across partitions as verified on k3s"
```

## Results

Build `sha-021a129`, k3s t3.large, 2026-10-12. Tasks 1–4 landed as `39a4c81`,
`323f4f8`, `b4810b9`, `021a129`.

**The partitions.**

| Check | Result |
|---|---|
| `payment.completed` before the deploy | 1 partition |
| After `k3s-deploy` | 8 partitions |
| `ensure-topics` log | `payment.completed OK` from `add-partitions`, then `payment.completed: 8 partitions`; no wait for the broker |
| Partitions the group claims | 8 |
| gate2 | `GATE 1 PASSED`, order `TB-GATE1-20261012-8W8JQARJ` |
| Pod deleted and restarted | the log is only `payment.completed: 8 partitions`; still 8 |

**The load.** 20 buyers for 3 minutes at the k3s door of 2 a second. Order outcomes
are each order's stored status in DynamoDB.

| Run | Box | Purchases | Unexpected | Timeouts | Checkout p50 / max | At once | Confirm mean | Create→COMPLETED p99 / max |
|---|---|---|---|---|---|---|---|---|
| 11:48Z, void | Postgres 0.9 and Temporal 0.5 cores busy before the load | 77 in the window; all 94 created ended `COMPLETED` | 2 | 4 | 9.1s / 43.6s | 6 | 12,852ms | 254s (p90) / 312s |
| 12:40Z | idle: Postgres 131m, Temporal 53m | 368, all `COMPLETED` | 0 | 0 | 0.96s / 4.0s | **4** | 320ms | 2.76s / 3.30s |

The prediction held on the clean run: `at most 4 at once`, against `at most 1` on
both 2026-10-11 lists, and no buyer failed to complete. Confirms kept pace with the
door: 2.02 a second.

**Found.**
- **A stopped box replays overdue retention.** Temporal deletes a closed workflow
  24 hours after it closes. The 2026-10-11 sweep's workflows came due while the box
  was stopped, and all were deleted from its start at 11:33Z, with no workflow
  running. Those closed before 2026-10-12 fell from 19,905 to 17,780 in a minute. The
  clean run started once fewer than 100 were left. A load must wait for an idle box,
  not only for every pod `Running`.
- **`purchase.js` counts a timed-out checkout nowhere.** Status 0 passes
  `or.status < 400`, `or.json()` then throws, and the iteration ends uncounted: 4 on
  the void run. Not fixed here.
- **The void run's 2 unexpected completed late.** Both were `never-completed`: the
  buyer stopped polling after 3 minutes, and DynamoDB shows the order `COMPLETED`.
- **The plan's ensure-topics test exited silently on its mutation.** The script's
  own last check exits under `set -e` before the test's assertion runs, so `run` now
  fails with `ensure-topics <args> exited non-zero`.

**Review focus.** 2 (a cold boot) showed a clean first try. 1, 3, 4 and 5 were not
exercised, as written.
