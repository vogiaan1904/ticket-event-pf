# Short saga steps as local activities

**Status: MEASURED 2026-09-29.** Tasks 1–5 done; Task 6 through Step 3. Keeping
or reverting the change is the architect's call. Decision:
→ [0018](../decisions/0018-short-saga-steps-run-as-local-activities.md), proposed.

**Goal:** Cut what Temporal costs per purchase by running the six short,
idempotent saga steps as local activities, and measure the cut on the same box
before and after.

**Spec:** [0018](../decisions/0018-short-saga-steps-run-as-local-activities.md)
and the evidence in `docs/plans/2026-09-27-checkout-latency-decomposition.md`.

## Why this, measured

On the k3s box, from three runs on 2026-09-27 (`deploy/scripts/purchase-cost.sh`,
Task 1):

| Load | Node core-s per purchase | Temporal | Postgres | History events per purchase |
|---|---|---|---|---|
| 10 buyers | 0.875 | 0.237 | 0.193 | 29 + 35 |
| 20 buyers | 0.936 | 0.235 | 0.192 | 29 + 35 |
| 40 buyers | 1.044 | 0.248 | 0.190 | 29 + 35 |

**Temporal's cost per purchase holds within ±3% across loads,** so it is the
number this plan moves and judges by. The node figure grows with the number of
polling buyers and is only comparable at equal load.

## What the change is

| Step | Workflow | Becomes | Why it is safe to run twice |
|---|---|---|---|
| `GetOrder` | Confirm | local | a read |
| `CreateOrder` | Create | local | a duplicate write reads the order back (`order_activity.go`, `ErrOrderAlreadyExists`) |
| `CreateOrderItems` | Create | local, **after Task 3** | not safe today; Task 3 makes it so |
| `UpdateOrderStatus` | Confirm | local | an unconditional write of the same status |
| `ReleasePurchaseSlot` | Confirm | local | a refused conditional delete returns `nil` |
| `PublishCheckoutCompleted` | Confirm | local | a duplicate re-invalidates a token and re-removes a session (`waitroom_service.go` `HandleCheckoutCompleted`) |
| `ReserveInventory`, `ConfirmInventory`, `CreatePaymentIntent`, compensations | — | stay remote | contended or external; they keep retry isolation and visibility |

**Found while writing this plan:** `CreateManyItems` names each item
`time.Now().UnixNano()` (`pkg/dynamodb/helpers.go`), so a retried
`CreateOrderItems` writes a second set of items. Checked against dynamodb-local:
two writes of a 2-item order leave 4 items. This is live today for the regular
activity; a local activity widens it.

### Predicted, before anything is measured

```
History events   CreateOrder 29 -> 20   start 1, three workflow tasks 9, two remote
                                        activities 6, version + two local markers 3,
                                        complete 1
                 ConfirmOrder 35 -> 16  start 1, two workflow tasks 6, one remote
                                        activity 3, version + four local markers 5,
                                        complete 1
Temporal core-s per purchase            falls 30-45%, roughly with the events
Opening burst, first-task wait          falls; the box stays saturated
```

A result outside these is a finding about the model, and Task 6 says which.

### Every check can go red

Verified on a scratch copy before this plan was written:

| Check | Correct code | Mutated |
|---|---|---|
| Replay of a recorded pre-change history | passes with the version guard | `TMPRL1100` non-determinism without it |
| `TestCreateOrder_ShortStepsRunAsLocalActivities` | 2 local activities | 0 when the step runs remote |
| `TestConfirmOrder_ShortStepsRunAsLocalActivities` | 4 local activities | 0 when the step runs remote |
| `TestCreateManyItems_ARetryWritesTheSameItems` | 2 items | 4 when the ID varies per call |

The whole order-svc suite passed on that copy.

## Traps this plan works around

- **The SDK's test suite cannot mock a local activity registered on a struct.**
  The workflow resolves the method to the registry's reflect-built function
  (`sdk@v1.37.0/internal/workflow.go:1056`), and the test suite names the
  activity from that function, not from `ActivityType`
  (`internal_workflow_testsuite.go:1560`), so `OnActivity("CreateOrder")` never
  matches and the real method runs. The existing tests are pinned to the
  pre-change path with `OnGetVersion`; the local path is tested with real
  activities over fakes.
- **A local activity's end-to-end timeout defaults to one attempt's timeout.**
  Left unset, a 5s attempt would cap all retries at 5s. `executeShortStep` sets
  `ScheduleToCloseTimeout` from `activityRetryBudget`, so the retry policy keeps
  its attempts.

## Setup, every session

```bash
make -C deploy start-ec2-k3s           # prints the tunnel command; open it in its own terminal
make -C deploy k3s-kubeconfig && export KUBECONFIG=/tmp/k3s.yaml
kubectl -n monitoring port-forward svc/kps-kube-prometheus-stack-prometheus 9090:9090 &
export GW=http://localhost:3000/api
docker compose -f services/order-svc/docker-compose.dev.yml up -d   # repository tests
```

---

### Task 1: Measure the cost per purchase before the change

The box must run the build deployed now, before any of Tasks 2–4 is deployed.

**Files:**
- Create: `deploy/scripts/purchase-cost.sh`

- [ ] **Step 1: Write the cost tool**

`deploy/scripts/purchase-cost.sh`, then `chmod +x`:

```bash
#!/usr/bin/env bash
# What one purchase costs the k3s box over a load run: CPU core-seconds per
# completed purchase, whole node and Temporal and Postgres, and history events
# per saga. Needs the Prometheus port-forward on :9090 and KUBECONFIG.
#   deploy/scripts/purchase-cost.sh FROM TO K6_LOG   (RFC 3339, UTC)
set -euo pipefail
FROM=${1:?usage: purchase-cost.sh FROM TO K6_LOG}
TO=${2:?usage: purchase-cost.sh FROM TO K6_LOG}
K6_LOG=${3:?usage: purchase-cost.sh FROM TO K6_LOG}
PROM=${PROM:-http://localhost:9090}

at() {
  curl -fsS "$PROM/api/v1/query" --data-urlencode "query=$1" --data-urlencode "time=$2" |
    python3 -c 'import sys, json; r = json.load(sys.stdin)["data"]["result"]; print(r[0]["value"][1] if r else "nan")'
}
# Raw counter deltas, not increase(): increase() extrapolates to the window's edges.
delta() { python3 -c "print(float('$(at "$1" "$TO")') - float('$(at "$1" "$FROM")'))"; }

NODE=$(delta 'sum(node_cpu_seconds_total{mode!="idle"})')
TEMPORAL=$(delta 'sum(container_cpu_usage_seconds_total{namespace="ticketbottle",pod=~"temporal-[a-z0-9]+-[a-z0-9]+",container!=""})')
POSTGRES=$(delta 'sum(container_cpu_usage_seconds_total{namespace="ticketbottle",pod="postgres-0",container!=""})')
PURCHASES=$(python3 -c "import re; s = open('$K6_LOG').read(); print(re.search(r'\"tb_orders_completed\":\s*\{\s*\"count\":\s*(\d+)', s).group(1))")

events() {
  kubectl -n ticketbottle exec deploy/temporal -- temporal workflow list --limit 100000 -o jsonl \
    --query "WorkflowType=\"$1\" AND StartTime >= \"$FROM\" AND StartTime < \"$TO\"" |
    python3 -c 'import sys, json; h = [int(json.loads(l)["historyLength"]) for l in sys.stdin if l.strip()]; print(f"{sum(h) / len(h):.1f} over {len(h)}" if h else "none")'
}

python3 - "$NODE" "$TEMPORAL" "$POSTGRES" "$PURCHASES" <<'EOF'
import sys
node, temporal, postgres, n = map(float, sys.argv[1:])
print(f"purchases {n:.0f}")
for name, cores in (("node", node), ("temporal", temporal), ("postgres", postgres)):
    print(f"  {name:<9} {cores:7.1f} core-s  {cores / n:6.3f} per purchase")
EOF
echo "  history events per saga: CreateOrder $(events CreateOrder), ConfirmOrder $(events ConfirmOrder)"
```

The `temporal-…` pattern matches the server pod and not `temporal-ui-…`, which
has one more name segment.

- [ ] **Step 2: Two runs at 20 buyers, back to back**

```bash
for L in before-1 before-2; do
  TOTAL=100000 VUS=20 DURATION=5m deploy/scripts/gate4a-load.sh > /tmp/run-$L.txt 2>&1
  kubectl -n ticketbottle logs job/k6-load > /tmp/k6-$L.log
  kubectl -n ticketbottle get pod -l job-name=k6-load \
    -o jsonpath='{.items[0].status.startTime} {.items[0].status.containerStatuses[0].state.terminated.finishedAt}{"\n"}' | tee /tmp/window-$L.txt
done
```

- [ ] **Step 3: Cost and burst for each**

With FROM and TO from `/tmp/window-$L.txt`:

```bash
deploy/scripts/purchase-cost.sh <FROM> <TO> /tmp/k6-$L.log
deploy/scripts/saga-histories.sh <FROM> <TO plus 30s> /tmp/h-$L.txt
python3 deploy/loadtest/saga_latency.py /tmp/h-$L.txt /tmp/k6-$L.log | sed -n '1,8p'
```

Expected: Temporal between 0.23 and 0.25 core-s per purchase and history events
`CreateOrder 29.0`, `ConfirmOrder 35.0`, as on 2026-09-27. Outside that, stop:
the baseline has moved and the reason comes first.

- [ ] **Step 4: Record** the two rows of *Results*, and the spread between them:
  that spread is the noise floor Task 6 judges against.

- [ ] **Step 5: Commit**

```bash
git add deploy/scripts/purchase-cost.sh
git commit -m "feat(loadtest): measure what one purchase costs the box"
```

---

### Task 2: Replay recorded sagas against the code

Replay is what catches a change that alters the commands a workflow emits: on
a deploy, every saga in flight replays its history on the new worker.

**Files:**
- Create: `services/order-svc/internal/workflows/testdata/create_order.json`
- Create: `services/order-svc/internal/workflows/testdata/confirm_order.json`
- Create: `services/order-svc/internal/workflows/replay_test.go`

- [ ] **Step 1: Capture one completed history of each workflow**

From a Task 1 run, while the pre-change build is still deployed:

```bash
T=services/order-svc/internal/workflows/testdata; mkdir -p $T
for W in CreateOrder:create_order ConfirmOrder:confirm_order; do
  kubectl -n ticketbottle exec deploy/temporal -- sh -c "
    id=\$(temporal workflow list --limit 1 -o jsonl \
      --query 'WorkflowType=\"${W%%:*}\" AND ExecutionStatus=\"Completed\"' \
      | sed -n 's/.*\"workflowId\":\"\([^\"]*\)\".*/\1/p')
    temporal workflow show -w \"\$id\" -o json" > $T/${W#*:}.json
done
```

- [ ] **Step 2: Read every payload before committing it**

```bash
python3 - <<'EOF'
import base64, json
def walk(o, out):
    if isinstance(o, dict):
        if isinstance(o.get("data"), str) and "metadata" in o:
            out.append(base64.b64decode(o["data"]).decode("utf-8", "replace"))
        for v in o.values(): walk(v, out)
    elif isinstance(o, list):
        for v in o: walk(v, out)
for f in ("create_order", "confirm_order"):
    out = []; walk(json.load(open(f"services/order-svc/internal/workflows/testdata/{f}.json")), out)
    print(f, *(p[:160] for p in out), sep="\n  ")
EOF
```

Expected: seeded load-test buyers (`gate4a-u<N>@example.com`) and at most a
ZaloPay **sandbox** payment link, whose one-time token expires in 360s.
Anything else — a real address, a secret — do not commit it; capture another.

- [ ] **Step 3: Write the replay test**

`services/order-svc/internal/workflows/replay_test.go`:

```go
package workflows

import (
	"testing"

	"go.temporal.io/sdk/worker"
)

// TestReplay_RecordedHistories replays sagas recorded on k3s against today's code.
// A change that alters the commands a workflow emits, unguarded by
// workflow.GetVersion, fails here instead of on the sagas in flight at deploy.
func TestReplay_RecordedHistories(t *testing.T) {
	r := worker.NewWorkflowReplayer()
	r.RegisterWorkflow(CreateOrder)
	r.RegisterWorkflow(ConfirmOrder)

	for _, f := range []string{"testdata/create_order.json", "testdata/confirm_order.json"} {
		if err := r.ReplayWorkflowHistoryFromJSONFile(nil, f); err != nil {
			t.Errorf("replay %s: %v", f, err)
		}
	}
}
```

- [ ] **Step 4: Run it**

Run: `cd services/order-svc && go test ./internal/workflows/ -run TestReplay -count=1`
Expected: `ok`. It is green on arrival by design — it replays the code that
recorded the histories — so Step 5 is what proves it.

- [ ] **Step 5: Prove it can go red**

In `internal/workflows/steps.go`, `createOrder`, replace the call with an
unguarded local one:

```go
	lctx := workflow.WithLocalActivityOptions(ctx, workflow.LocalActivityOptions{StartToCloseTimeout: 5 * time.Second})
	err := workflow.ExecuteLocalActivity(lctx, oActs.CreateOrder, opt).Get(ctx, &o)
```

(with `"time"` imported). Run Step 4's command. Expected: `FAIL`, with
`[TMPRL1100] lookup failed for scheduledEventID to activityID`. Restore with
`git checkout -- internal/workflows/steps.go` and rerun: `ok`.

- [ ] **Step 6: Commit**

```bash
git add services/order-svc/internal/workflows/replay_test.go services/order-svc/internal/workflows/testdata
git commit -m "test(order): replay recorded sagas against the workflow code"
```

---

### Task 3: A retried item write writes the same items

**Files:**
- Create: `services/order-svc/internal/order/repository/item_test.go`
- Modify: `services/order-svc/pkg/dynamodb/helpers.go` (`GenerateItemID`)
- Modify: `services/order-svc/internal/order/repository/item_builder.go`
- Modify: `services/order-svc/internal/order/repository/item.go`

- [ ] **Step 1: Write the failing test**

`services/order-svc/internal/order/repository/item_test.go`:

```go
package repository

import (
	"context"
	"testing"
)

// CreateOrderItems is retried by Temporal, and as a local activity it re-runs when
// the workflow task that ran it fails. A retry must rewrite the same items, not
// add a second set to the order.
func TestCreateManyItems_ARetryWritesTheSameItems(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()
	opts := []CreateOrderItemOption{
		{OrderCode: "TB-ITEMS-0001", TicketClassID: "1", TicketClassName: "GA", Quantity: 1},
		{OrderCode: "TB-ITEMS-0001", TicketClassID: "2", TicketClassName: "VIP", Quantity: 1},
	}

	for attempt := 1; attempt <= 2; attempt++ {
		if _, err := repo.CreateManyItems(ctx, "TB-ITEMS-0001", opts); err != nil {
			t.Fatalf("attempt %d: %v", attempt, err)
		}
	}

	got, err := repo.ListItemByOrderCode(ctx, "TB-ITEMS-0001")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != len(opts) {
		t.Fatalf("order has %d items after a retried write, want %d", len(got), len(opts))
	}
}
```

- [ ] **Step 2: Run it and watch it fail**

Run: `cd services/order-svc && go test ./internal/order/repository/ -run TestCreateManyItems -count=1`
Expected: `order has 4 items after a retried write, want 2`. A `SKIP` means
dynamodb-local is not up; see *Setup*.

- [ ] **Step 3: Derive the item ID**

In `pkg/dynamodb/helpers.go`, replace `GenerateItemID` and drop the now-unused
`"time"` import:

```go
// OrderItemID names an order's pos-th item. Derived, not generated, so a retried
// write puts the same items again instead of adding a second set.
func OrderItemID(orderCode string, pos int) string {
	return fmt.Sprintf("%s-%d", orderCode, pos+1)
}
```

In `internal/order/repository/item_builder.go`:

```go
func (r *implRepository) buildOrderItemModel(orderCode string, pos int, opt CreateOrderItemOption) models.OrderItem {
	now := r.clock()
	itemID := pkgDynamo.OrderItemID(orderCode, pos)
```

In `internal/order/repository/item.go`, `CreateManyItems`:

```go
	for i, opt := range opts {
		item := r.buildOrderItemModel(orderCode, i, opt)
```

`GenerateItemID` had no other caller; `grep -rn GenerateItemID services/order-svc`
must print nothing.

- [ ] **Step 4: Run the repository and dynamodb packages**

Run: `cd services/order-svc && go build ./... && go test ./internal/order/repository/ ./pkg/... -count=1`
Expected: `ok` for both.

- [ ] **Step 5: Prove it can go red**

Replace `r.buildOrderItemModel(orderCode, i, opt)` with
`r.buildOrderItemModel(orderCode, i+int(r.clock().UnixNano()%1000000), opt)`,
rerun Step 2's command, confirm `4 items`, restore, rerun: `ok`.

- [ ] **Step 6: Commit**

```bash
git add services/order-svc/internal/order/repository services/order-svc/pkg/dynamodb/helpers.go
git commit -m "fix(order): write the same order items on a retry

An item's ID was the clock's nanoseconds, so a retried CreateOrderItems added
a second set of items to the order. The ID is now the order code and the
item's position.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 4: Run the short steps as local activities

**Files:**
- Create: `services/order-svc/internal/workflows/short_steps_test.go`
- Modify: `services/order-svc/internal/workflows/options.go`
- Modify: `services/order-svc/internal/workflows/steps.go`
- Modify: `services/order-svc/internal/workflows/testenv_test.go`
- Modify: `services/order-svc/CLAUDE.md`, *Temporal workflows*

**Consumes:** Task 2's replay test, which must stay green; Task 3's idempotent
`CreateOrderItems`.

- [ ] **Step 1: Write the failing test**

`services/order-svc/internal/workflows/short_steps_test.go`:

```go
package workflows

import (
	"context"
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/vogiaan1904/ticketbottle-order/internal/activities"
	"github.com/vogiaan1904/ticketbottle-order/internal/models"
	"github.com/vogiaan1904/ticketbottle-order/internal/order/delivery/kafka"
	"github.com/vogiaan1904/ticketbottle-order/internal/order/delivery/kafka/producer"
	repo "github.com/vogiaan1904/ticketbottle-order/internal/order/repository"
	"github.com/vogiaan1904/ticketbottle-order/pkg/grpc/payment"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"
)

// A local activity runs the registered struct, so these tests register real
// activities over fakes; the SDK's test suite cannot match a mock to a local
// activity registered on a struct. Steps that stay remote are still mocked.
type fakeRepo struct {
	repo.Repository
	calls []string
}

func (f *fakeRepo) Create(_ context.Context, opt repo.CreateOrderOption) (models.Order, error) {
	f.calls = append(f.calls, "Create")
	return models.Order{Code: opt.Code}, nil
}

func (f *fakeRepo) CreateManyItems(_ context.Context, _ string, _ []repo.CreateOrderItemOption) ([]models.OrderItem, error) {
	f.calls = append(f.calls, "CreateManyItems")
	return []models.OrderItem{}, nil
}

func (f *fakeRepo) GetOne(_ context.Context, opt repo.GetOneOrderOption) (models.Order, error) {
	f.calls = append(f.calls, "GetOne")
	return models.Order{Code: opt.Code, Status: models.OrderStatusPending, SessionID: "sess-1", UserID: "u1", EventID: "e1"}, nil
}

func (f *fakeRepo) Update(_ context.Context, code string, _ repo.UpdateOrderOption) (models.Order, error) {
	f.calls = append(f.calls, "Update")
	return models.Order{Code: code}, nil
}

func (f *fakeRepo) ReleasePurchaseSlot(_ context.Context, _, _ string) error {
	f.calls = append(f.calls, "ReleasePurchaseSlot")
	return nil
}

type fakeProducer struct {
	producer.Producer
	published int
}

func (f *fakeProducer) PublishCheckoutCompleted(_ context.Context, _ kafka.CheckoutCompletedEvent) error {
	f.published++
	return nil
}

func newShortStepEnv(t *testing.T, r *fakeRepo, p *fakeProducer) (*testsuite.TestWorkflowEnvironment, *int) {
	t.Helper()
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()
	env.RegisterActivity(&activities.InventoryActivities{})
	env.RegisterActivity(&activities.OrderActivities{Repo: r})
	env.RegisterActivity(&activities.PaymentActivities{})
	env.RegisterActivity(&activities.EventPublishingActivities{Prod: p})
	local := new(int)
	env.SetOnLocalActivityStartedListener(func(*activity.Info, context.Context, []interface{}) { *local++ })
	return env, local
}

func TestCreateOrder_ShortStepsRunAsLocalActivities(t *testing.T) {
	r := &fakeRepo{}
	env, local := newShortStepEnv(t, r, &fakeProducer{})
	env.OnActivity("ReserveInventory", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)
	env.OnActivity("CreatePaymentIntent", mock.Anything, mock.Anything).
		Return(&payment.CreatePaymentIntentResponse{PaymentUrl: "https://pay"}, nil)

	env.ExecuteWorkflow(CreateOrder, testCreateInput())

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow failed: %v", err)
	}
	if *local != 2 {
		t.Fatalf("%d local activities ran, want 2 (order, items)", *local)
	}
	if len(r.calls) != 2 || r.calls[0] != "Create" || r.calls[1] != "CreateManyItems" {
		t.Fatalf("repository calls %v, want [Create CreateManyItems]", r.calls)
	}
}

func TestConfirmOrder_ShortStepsRunAsLocalActivities(t *testing.T) {
	r, p := &fakeRepo{}, &fakeProducer{}
	env, local := newShortStepEnv(t, r, p)
	env.OnActivity("ConfirmInventory", mock.Anything, mock.Anything).Return(nil)

	env.ExecuteWorkflow(ConfirmOrder, &ConfirmOrderWorkflowInput{
		OrderCode: "TB-TEST-0001", Status: models.OrderStatusCompleted,
	})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow failed: %v", err)
	}
	if *local != 4 {
		t.Fatalf("%d local activities ran, want 4 (get, update, release, publish)", *local)
	}
	if p.published != 1 {
		t.Fatalf("checkout.completed published %d times, want 1", p.published)
	}
}
```

- [ ] **Step 2: Run it and watch it fail**

Run: `cd services/order-svc && go test ./internal/workflows/ -run ShortSteps -count=1`
Expected: both fail on `0 local activities ran` — the steps run remote, over the
fakes, and complete.

- [ ] **Step 3: Add the version and the attempt bound**

In `internal/workflows/options.go`, above `getCreateOrderActivityOptions`:

```go
// shortStepsChangeID versions the move of short steps to local activities.
const shortStepsChangeID = "short-steps-local"

// shortStepAttemptTimeout bounds one attempt of a local step, well inside the
// 10s workflow task it runs in.
const shortStepAttemptTimeout = 5 * time.Second
```

- [ ] **Step 4: Route the six steps through one helper**

In `internal/workflows/steps.go`, above `validateOrder`:

```go
// executeShortStep runs a short, idempotent step inside the workflow worker as a
// local activity, keeping the context's retry policy. Sagas that started before
// the change replay on the regular path. See docs/decisions/0018.
func executeShortStep(ctx workflow.Context, activity interface{}, args ...interface{}) workflow.Future {
	if workflow.GetVersion(ctx, shortStepsChangeID, workflow.DefaultVersion, 1) == workflow.DefaultVersion {
		return workflow.ExecuteActivity(ctx, activity, args...)
	}

	ao := workflow.GetActivityOptions(ctx)
	ao.StartToCloseTimeout = shortStepAttemptTimeout
	lctx := workflow.WithLocalActivityOptions(ctx, workflow.LocalActivityOptions{
		StartToCloseTimeout:    shortStepAttemptTimeout,
		ScheduleToCloseTimeout: activityRetryBudget(ao),
		RetryPolicy:            ao.RetryPolicy,
	})
	return workflow.ExecuteLocalActivity(lctx, activity, args...)
}
```

Then in each of the six helpers replace `workflow.ExecuteActivity(ctx, ` with
`executeShortStep(ctx, ` for exactly these first arguments:
`oActs.GetOrder` (`validateOrder`), `oActs.CreateOrder` (`createOrder`),
`oActs.CreateOrderItems` (`createOrderItems`), `oActs.UpdateOrderStatus`
(`updateOrderStatus`), `oActs.ReleasePurchaseSlot` (`releasePurchaseSlot`),
`epActs.PublishCheckoutCompleted` (`publishCheckoutCompleted`). Nothing else:
`grep -c 'executeShortStep(ctx, ' internal/workflows/steps.go` prints 6.

Every caller of these helpers runs under `WithActivityOptions`
(`create_order.go`, `confirm_order.go`), which `GetActivityOptions` reads; the
compensations keep their own context and stay remote.

- [ ] **Step 5: Pin the mocked tests to the pre-change path**

In `internal/workflows/testenv_test.go`, import `"go.temporal.io/sdk/workflow"`
and add before `t.Cleanup` in `newTestEnv`:

```go
	// These tests mock short steps by name, which the test suite cannot do for a
	// local activity; short_steps_test.go covers the local path.
	env.OnGetVersion(shortStepsChangeID, workflow.DefaultVersion, 1).Return(workflow.DefaultVersion).Maybe()
```

- [ ] **Step 6: Run the workflow package**

Run: `cd services/order-svc && go vet ./internal/workflows/ && go test ./internal/workflows/ -count=1 -v | grep -c '^--- PASS'`
Expected: `19` top-level tests (two more subtests pass beneath them), and no
`--- FAIL`. The replay test is among them: recorded
histories carry no version marker, so they take the `DefaultVersion` branch.

- [ ] **Step 7: Prove the new checks can go red**

Each with `-count=1`, then restore:

1. Replace the `GetVersion` condition with `true` (always remote): both
   `ShortSteps` tests fail on `0 local activities ran`.
2. Replace it with `false` (no version guard): the pinned tests fail, and
   `TestReplay_RecordedHistories` fails with `TMPRL1100`.

- [ ] **Step 8: Record the rule where the next step-writer will see it**

In `services/order-svc/CLAUDE.md`, *Temporal workflows*, after the
`ConfirmOrder` bullet:

```markdown
- **Short steps run as local activities** through `executeShortStep`
  ([0018](../../docs/decisions/0018-short-saga-steps-run-as-local-activities.md)):
  `GetOrder`, `CreateOrder`, `CreateOrderItems`, `UpdateOrderStatus`,
  `ReleasePurchaseSlot`, `PublishCheckoutCompleted`. A local step re-runs when the
  workflow task that ran it fails, so a step routed there must be safe to run
  twice. Their workflow tests run real activities over fakes
  (`short_steps_test.go`); the SDK's test suite cannot mock them by name.
```

- [ ] **Step 9: Run the whole service and commit**

Run: `cd services/order-svc && go test ./... -count=1`
Expected: every package `ok`.

```bash
git add services/order-svc/internal/workflows services/order-svc/CLAUDE.md
git commit -m "feat(order): run the short saga steps as local activities

Six short, idempotent steps now run inside the workflow worker instead of
through Temporal's task queue, behind a workflow version so sagas in flight at
deploy replay on the old path. Reserve, confirm and the payment intent stay
remote. See docs/decisions/0018.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 5: Deploy with sagas in flight

This is the version guard's real test: sagas started on the old build finish on
the new one.

- [ ] **Step 1: Push and build.** Pushing `dev` needs the architect's go-ahead.
  Then `gh run watch` on the `build-push-ecr` run for the pushed commit, until
  every image is built.
- [ ] **Step 2: Deploy under load**

```bash
TOTAL=100000 VUS=10 DURATION=5m deploy/scripts/gate4a-load.sh > /tmp/run-deploy.txt 2>&1 &
sleep 90 && make -C deploy k3s-deploy
wait
```

- [ ] **Step 3: No saga failed to replay**

```bash
kubectl -n ticketbottle logs deploy/order-service --since=15m | grep -c -e TMPRL1100 -e nondeterministic
kubectl -n ticketbottle logs deploy/order-consumer --since=15m | grep -c -e TMPRL1100 -e nondeterministic
kubectl -n ticketbottle exec deploy/temporal -- temporal workflow count --query 'ExecutionStatus="Running"'
```

Expected: `0`, `0`, and a running count that falls to `0` within a few minutes.
k6 errors from the rollout itself are the drain records' territory (0015, 0016),
not this one.

- [ ] **Step 4: Record the new build's histories for replay**

Capture one completed `CreateOrder` and one `ConfirmOrder` started after the
deploy, as Task 2 Step 1 did, into `create_order_v1.json` and
`confirm_order_v1.json`; read them as Task 2 Step 2 did; add both to the list in
`replay_test.go`; run it: `ok`. Each should show the version marker and local
activity markers: `grep -c MARKER_RECORDED` on each file is at least 3 and 5.

```bash
git add services/order-svc/internal/workflows/replay_test.go services/order-svc/internal/workflows/testdata
git commit -m "test(order): replay sagas recorded after the local-step change"
```

---

### Task 6: Measure after, and decide

- [ ] **Step 1:** Task 1 Steps 2 and 3 again, labelled `after-1` and `after-2`.
- [ ] **Step 2:** Fill *Results*. Compare Temporal core-s per purchase against
  the noise floor from Task 1 Step 4; claim only a difference larger than it.
  Compare history events and the burst's first-task wait against *Predicted*.
- [ ] **Step 3:** Write *What this says*: whether the cut is real, how it
  compares with the prediction, and what the node and throughput did.
- [ ] **Step 4:** Take the result to the architect. If the change is kept, set
  0018 to `accepted` with an `## Outcome` citing the commits, the tests that went
  red then green, and the numbers. If it is not worth its cost, say so in the
  record and name the revert.
- [ ] **Step 5:** Set this plan's status line, update the root `CLAUDE.md` Open
  row for this question, run `python3 docs/decisions/index.py`, and commit:

```bash
git add docs/plans/2026-09-27-saga-short-steps-local.md docs/decisions CLAUDE.md
git commit -m "docs: record what the local saga steps saved per purchase"
```

## Results

| Run | Build | Purchases | Node core-s / purchase | Temporal | Postgres | History events | Burst first-task wait p50 / max |
|---|---|---|---|---|---|---|---|
| before-1 | old | 499 | 1.023 | 0.252 | 0.209 | 29 + 35 | 1.03s / 4.66s |
| before-2 | old | 493 | 1.025 | 0.261 | 0.210 | 29 + 35 | 0.82s / 2.36s |
| after-1 | new | 913 | 0.593 | 0.116 | 0.118 | 21 + 17 | 0.97s / 3.31s |
| after-2 | new | 669 | lost | lost | lost | — | 0.29s / 2.26s |
| after-3 | new | 923 | 0.595 | 0.119 | 0.118 | 21 + 17 | 0.89s / 1.68s |
| before-3 | old | 526 | 1.008 | 0.240 | 0.198 | 29 + 35 | 0.43s / 4.08s |
| after-4 | new | 897 | 0.624 | 0.120 | 0.122 | 21 + 17 | 0.14s / 0.87s |

Every run is 20 buyers for 5 minutes. The burst is the first 20 sagas to start
(`saga_latency.py --burst-first 20`), which reproduces the before rows as first read.

Task 1, 2026-09-27, 14:35–14:47Z, 20 buyers, 5 minutes each, back to back.

**Noise floor, same session:** Temporal core-s per purchase 3.5% between the two
runs, node 0.2%, Postgres 0.5%. The burst's first-task wait varied twofold at
its max, so one burst per build decides nothing.

**The baseline drifted across the day, and Task 1 stopped on it.** All three
costs are 7–10% above the 3-minute runs of 09:33Z (Temporal 0.235, Postgres
0.192, node 0.936), with the box as busy as then: user plus system time about
1.5 cores in both, and steal 0.03 cores. Throughput fell instead, from 1.72 to
1.6 purchases a second. Not explained. One candidate: Temporal's database kept
growing inside its 24-hour retention, to 5,208 executions and 85,845
`history_node` rows by 14:50Z. Whatever it is, a drift of this size across hours
would sit inside a small effect, so Task 6's comparison has to bracket time, not
just builds.

before-2's burst of 20 spanned three admission ticks, 3.1s, and was read with
`--burst-secs 4`; a fixed time window does not define the burst reliably.

**Task 6 bracketed time, not just builds.** after-1 and after-2 ran on
2026-09-28, 02:48–02:59Z. after-3, before-3 and after-4 ran on 2026-09-29,
01:21–02:00Z, in one session: new build, then a drain to no running saga and a
rollback to `sha-11df387`, then the new build again, deployed under load as
Task 5.

**What went wrong, and why no number above rests on it.**
- Task 5's first load never started: `GW` was unset. The first deploy ran with
  no saga in flight, so Task 5 was repeated on 2026-09-29, after before-3.
- after-2's costs were lost. Its Prometheus port-forward died, and Prometheus
  keeps six hours.
- after-2 also stalled for 100s on `CreatePaymentIntent`, which ran up to 43s
  over six retries against the ZaloPay sandbox. The change does not touch that
  step, which stays remote.
- k6 does not keep its summary keys in order, and `purchase-cost.sh` could not
  read after-1's count until `dbc46e8`.
- A 12MB history fetch over `kubectl exec` cut off mid-stream in three of five
  tries until `fdcfc87`.

**Idle cost.** With nothing running, Temporal used 0.020–0.025 cores,
Postgres 0.010 and the node 0.46–0.65. Temporal's idle is under 0.015 core-s
per purchase at either build's rate. Subtracting it leaves the old build at
0.228 and the new at 0.108–0.112. The node's 0.6 idle cores are spread over
more purchases as throughput rises. Above idle, the node fell from 0.657 to
0.374–0.398, close to the raw ratio.

**Task 5, 2026-09-29.** Ten buyers ran on the old build, and the new build was
deployed 90s in (revision 40, 01:47:33–01:51:23Z).
- 544 purchases.
- `TMPRL1100` or `nondeterministic` lines in `order-service` and
  `order-consumer`: 0 and 0.
- Running sagas fell to 0.
- Of the 880 sagas started during the rollout, 557 carry the version marker.
  One crossed builds: `ConfirmOrder:TB-4RGATE-20260929-Y8XVHQKM` ran five
  workflow tasks on the old consumer and its last on the new one, and
  completed on the `DefaultVersion` path.

The replay test covers replaying an old history. This saga is the only
evidence that a new worker can also continue one.

## What this says

**The cut is real, and larger than predicted.** In one session, bracketed new,
old, new, Temporal's cost per purchase went 0.119 → 0.240 → 0.120. The new build
halves it: −50%, against a same-session noise floor of 3.5% and a prediction of
30–45%. Postgres fell 40% (0.198 → 0.118–0.122) and the node 38–41%. History
events fell from 64 to 38, not the predicted 36. `GetVersion` also records its
change ID as a search attribute, one `UPSERT_WORKFLOW_SEARCH_ATTRIBUTES` event
per saga, and the prediction left that out.

**Temporal fell more than its events.** Events per purchase fell 41% and
Temporal's CPU 50%. The likeliest reading: a remote activity costs the server
more than its three events, because it takes a dispatch through the task queue
and timers for its timeouts, and a local activity costs the server none of that.
That reading was not measured.

**The box turned the saving into throughput, not latency.** Every run kept the
box saturated, with 1.6–1.8 of its 2 cores busy. So at 20 buyers the saving came
back as purchases: 1.6–1.7 a second before, 2.9–3.0 after. Each buyer starts
again as soon as a purchase completes, so the faster build also sent more
checkouts at once into the same saturated box. The share of checkouts under 2s
fell, from 90–91% to 78–86%. What the change does to latency at an equal arrival
rate is not measured here; that needs a load defined by arrival rate, not by
buyers.

**The opening burst: suggestive, not shown.** The burst's longest first-task
wait was 0.87–3.31s on the new build against 2.36–4.66s on the old. The ranges
overlap, and each run has one burst.

**The drift is not Temporal's stored history.** after-3 started with 3,182
stored executions and after-4 with 7,168, and cost the same, 0.119 and 0.120;
after-1 started at 5,196 and cost 0.116. The old build cost 0.240 on 2026-09-29,
against 0.252–0.261 on the afternoon of 2026-09-27 and 0.235 that morning. That
drift of up to 10% still has no cause. It is a fifth of the effect measured here.

## Found, not fixed

- **`CreateManyItems` ignores `UnprocessedItems`.** A throttled batch reports
  success with items missing. Returning an error so Temporal retries is safe once
  Task 3 lands, but the repository holds a concrete `*dynamodb.Client`, so there
  is no seam to test it.
- **A re-run `UpdateOrderStatus` to `REFUND_REQUIRED` counts the refund twice**
  in `metrics.OrdersRefundRequired`, which `OrdersNeedingRefund` reads. Only on
  the refund path, and only when a workflow task fails after the local step ran.
- **The `DefaultVersion` branch** stays until no pre-change saga can replay.
  Removing it is its own change, with the Task 2 histories deleted from the
  replay list in the same commit.
