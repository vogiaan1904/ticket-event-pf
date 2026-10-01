# Room size per event

**Status: not started.** The architect answered both calls on 2026-10-01 with the
recommended option, so every task stands as written. Decision:
→ [0025](../decisions/0025-an-event-admits-buyers-only-while-it-has-tickets-for-them.md), proposed.

**Goal:** A tick admits only while the event has more tickets available than buyers
inside, and `QUEUE_DEFAULT_MAX_CONCURRENT` is gone.

**Spec:** `docs/design/admission-sizing.md`, *Room size per event*. This is step 4 of
that design's *What comes next*. Where this plan and the spec disagree, the spec wins.

**Architecture:**
- `StockGate` keeps the count inventory gives it, not only the door it implies:
  `Stock{Door, Available, Counted}`. `Get` still returns the door, so the join and
  status paths do not change.
- `admitCount(due, stock, inside)` sets each tick's batch: `min(due, available −
  inside)` when inventory counted the event, every buyer due when it did not.
- The waitroom is the only service that changes. No contract, event-svc, inventory or
  gateway change: if a task seems to need one, stop and say so.

## Resuming after a break or a compaction

- Progress lives in the ledger,
  `.superpowers/sdd/2026-10-01-room-size-per-event/progress.md`, which is gitignored. A
  task with a `Task N: complete` line there is done: resume at the first task without
  one.
- `git log --oneline` shows each task's commit; the messages are given in the tasks.
- Before resuming, read the spec section, 0025's Decision and this plan's *Global
  constraints* again.

## Global constraints

- Commit messages describe the platform, never study progress, and end with
  `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`.
- **Pushing `dev` needs the architect's go-ahead.** So does starting the k3s box. The
  box and EKS never run at once: `aws eks list-clusters --region us-east-1` must print
  no cluster before the box starts.
- Comment budget: 3 lines inline, 5 on a symbol (an indented table does not count, but
  keep the whole block under 10).
- The waitroom's Redis tests need `WAITROOM_TEST_REDIS_ADDR=localhost:6379`. The
  container is `waitroom-svc-redis-1`. Without the variable they `SKIP` locally, and a
  skipped test asserts nothing.
- `gofmt -l internal` already lists `internal/service/types.go` and
  `internal/service/queue_processor_test.go`. Check formatting only on the files a task
  touches.
- Address code by its quoted text, not by line number: earlier tasks move the lines
  later tasks cite.

## Review focus

1. **Inventory down must not shut the door.** With nothing counted, every buyer due is
   admitted, as before this rule. Task 2, `TestWithNothingToJudgeByOnlyTheDoorPaces`.
2. **An event with no class that can still sell keeps admitting.** Its `total` is 0,
   and a limit of `available − inside` would read 0. Task 1,
   `TestStockGateCountsNothingWhenNoClassCanSell`; Task 2, `TestAdmitCount`'s
   "nothing to judge by" case.
3. **The join and status paths are unchanged.** `Get` still returns the door; the
   existing door tests in `door_admission_test.go` pass untouched.
4. **A sold-out line still closes with buyers inside.** Closing happens before the
   chairs are counted. Task 2, `TestASoldOutLineClosesWithBuyersStillInside`.
5. **Nothing still reads the deleted key.** Gate 4a read it from the ConfigMap, and an
   empty limit would fail its integer comparison. Task 3's grep.

---

### Task 1: the stock gate keeps the count it is given

**Files:**
- Modify: `services/waitroom-svc/internal/service/stock_gate.go`
- Test: `services/waitroom-svc/internal/service/stock_gate_test.go`

**Interfaces — produces:**
- `type Stock struct { Door Door; Available int64; Counted bool }`;
- `func (g *StockGate) Stock(ctx context.Context, eID string) Stock`;
- `Get` keeps its signature and returns `Stock(ctx, eID).Door`.

`Counted` is true only when inventory answered and `total > 0`.

- [ ] **Step 1: Write the failing tests.** Append to `stock_gate_test.go`:

```go
func TestStockGateKeepsTheCountItWasGiven(t *testing.T) {
	inv := &fakeInventoryClient{}
	inv.set(20, 5, 3)
	g := NewStockGate(inv, time.Minute, quietLogger())

	want := Stock{Door: DoorOpen, Available: 3, Counted: true}
	if got := g.Stock(context.Background(), "e-1"); got != want {
		t.Fatalf("stock = %+v, want %+v", got, want)
	}
}

// Unanswered, the door fails open and sets no limit on who may be inside.
func TestStockGateCountsNothingWhenUnanswered(t *testing.T) {
	inv := &fakeInventoryClient{err: status.Error(codes.Unavailable, "down")}
	g := NewStockGate(inv, time.Minute, quietLogger())

	if got := g.Stock(context.Background(), "e-1"); got != (Stock{Door: DoorOpen}) {
		t.Fatalf("stock = %+v, want an open door with nothing counted", got)
	}
}

// With no class that can still sell there is nothing to judge a room by.
func TestStockGateCountsNothingWhenNoClassCanSell(t *testing.T) {
	inv := &fakeInventoryClient{}
	inv.set(0, 0, 0)
	g := NewStockGate(inv, time.Minute, quietLogger())

	if got := g.Stock(context.Background(), "e-1"); got != (Stock{Door: DoorOpen}) {
		t.Fatalf("stock = %+v, want an open door with nothing counted", got)
	}
}
```

- [ ] **Step 2: Run them.**
  `cd services/waitroom-svc && go test ./internal/service/ -run TestStockGate 2>&1 | head`.
  Expected: it fails to compile, because `Stock` and `g.Stock` are undefined.

- [ ] **Step 3: Implement.** In `stock_gate.go`:
  - Above `// stockFetchTimeout bounds one question to inventory`, add:

```go
// Stock is one answer about an event's tickets. Counted is false when inventory gave
// nothing to judge by: it did not answer, or no class can still sell.
type Stock struct {
	Door      Door
	Available int64
	Counted   bool
}
```

  - In `stockGateEntry`, replace `door      Door` with `stock     Stock`.
  - Replace `Get`, `lookup`, `store` and `fetch` with:

```go
// Get returns the event's door, asking inventory at most once per TTL.
func (g *StockGate) Get(ctx context.Context, eID string) Door {
	return g.Stock(ctx, eID).Door
}

// Stock returns the event's answer, asking inventory at most once per TTL.
func (g *StockGate) Stock(ctx context.Context, eID string) Stock {
	if s, ok := g.lookup(eID); ok {
		return s
	}

	v, _, _ := g.group.Do(eID, func() (any, error) {
		// Detached: every caller collapsed behind this fetch shares its answer.
		fetchCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), stockFetchTimeout)
		defer cancel()

		// Stored even when failed open, so a down inventory is not asked per join.
		s := g.fetch(fetchCtx, eID)
		g.store(eID, s)
		return s, nil
	})
	return v.(Stock)
}

func (g *StockGate) lookup(eID string) (Stock, bool) {
	g.mu.RLock()
	defer g.mu.RUnlock()

	e, ok := g.entries[eID]
	if !ok || time.Now().After(e.expiresAt) {
		return Stock{}, false
	}
	return e.stock, true
}

func (g *StockGate) store(eID string, s Stock) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.entries[eID] = stockGateEntry{stock: s, expiresAt: time.Now().Add(g.ttl)}
}

func (g *StockGate) fetch(ctx context.Context, eID string) Stock {
	out, err := g.inv.GetEventStock(ctx, &inventory.GetEventStockRequest{EventId: eID})
	if err != nil {
		metrics.StockChecks.WithLabelValues("unavailable").Inc()
		g.l.Warnf(ctx, "StockGate: inventory did not answer for event_id=%s, door stays open: %v", eID, err)
		return Stock{Door: DoorOpen}
	}

	d := doorFor(out.GetTotal(), out.GetSold(), out.GetAvailable())
	metrics.StockChecks.WithLabelValues(d.String()).Inc()
	return Stock{Door: d, Available: out.GetAvailable(), Counted: out.GetTotal() > 0}
}
```

- [ ] **Step 4: Run them.**
  `go vet ./... && go test ./internal/service/ -run 'TestStockGate|TestDoorFor' -v 2>&1 | grep -E "^(--- |ok|FAIL)"`.
  Expected: 8 PASS (5 existing, 3 new). `gofmt -l internal/service/stock_gate.go internal/service/stock_gate_test.go`
  prints nothing. Break it: change `Counted: out.GetTotal() > 0` to `Counted: true`,
  and `TestStockGateCountsNothingWhenNoClassCanSell` FAILS. Restore.

- [ ] **Step 5: Commit.**
  `git commit -m "feat(waitroom): keep inventory's count, not only the door it implies"`, with
  the trailer.

---

### Task 2: a tick admits only while tickets outnumber the buyers inside

**Files:**
- Modify:
  - `services/waitroom-svc/internal/service/queue_processor.go`
  - `services/waitroom-svc/config/config.go`
  - `services/waitroom-svc/internal/metrics/metrics.go` (a comment)
  - `deploy/helm/ticketbottle/templates/apps/config.yaml`
  - `deploy/helm/ticketbottle/tests/golden/values-k3s.yaml` (regenerated)
- Test:
  - Create `services/waitroom-svc/internal/service/room_test.go`
  - Modify `queue_processor_test.go`, `door_test.go` and `admission_test.go`, which set
    the deleted field

**Interfaces:**
- Consumes: Task 1's `Stock` and `StockGate.Stock`.
- Produces: `func admitCount(due int, s Stock, inside int64) int`;
  `ProcessorConfig` has no `MaxConcurrentPerEvent`; `config.QueueConfig` has no
  `DefaultMaxConcurrent`; the chart sets no `QUEUE_DEFAULT_MAX_CONCURRENT`.

- [ ] **Step 1: Write the failing behaviour tests.** Create `room_test.go`. These
  compile against the code as it is, so they fail on behaviour:

```go
package service

import (
	"fmt"
	"slices"
	"testing"

	"github.com/vogiaan1904/ticketbottle-waitroom/internal/models"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// withInside seats n buyers in the event, each holding a chair.
func withInside(q *fakeQueue, n int) *fakeQueue {
	for i := range n {
		q.processing[fmt.Sprintf("in-%d", i)] = true
	}
	return q
}

func queuedSessions(ids ...string) *fakeSessions {
	s := &fakeSessions{sessions: map[string]*models.Session{}}
	for _, id := range ids {
		s.sessions[id] = queuedSession(id)
	}
	return s
}

func TestTheRoomAdmitsOnlyAsManyAsTicketsLeftOver(t *testing.T) {
	inv := &fakeInventoryClient{}
	inv.set(20, 5, 3)
	q := withInside(newFakeQueue("ss-1", "ss-2", "ss-3", "ss-4"), 1)
	qp, _ := newDoorProcessor(q, queuedSessions("ss-1", "ss-2", "ss-3", "ss-4"), inv)

	tick(t, qp)

	if want := []string{"ss-3", "ss-4"}; !slices.Equal(q.queued, want) {
		t.Fatalf("queue = %v, want %v: 3 tickets and 1 inside leave room for 2", q.queued, want)
	}
}

func TestNoTicketBeyondTheBuyersInsideAdmitsNobody(t *testing.T) {
	inv := &fakeInventoryClient{}
	inv.set(20, 5, 3)
	q := withInside(newFakeQueue("ss-1", "ss-2"), 3)
	qp, p := newDoorProcessor(q, queuedSessions("ss-1", "ss-2"), inv)

	tick(t, qp)

	if want := []string{"ss-1", "ss-2"}; !slices.Equal(q.queued, want) || len(p.published) != 0 {
		t.Fatalf("queue = %v, published %v; want %v kept and nobody admitted", q.queued, p.published, want)
	}
}

// The room is the event's tickets, not a constant.
func TestAHundredInsideStillAdmitsWhileTicketsAreLeft(t *testing.T) {
	q := withInside(newFakeQueue("ss-1"), 100)
	qp, _ := newDoorProcessor(q, queuedSessions("ss-1"), plentyOfStock())

	tick(t, qp)

	if len(q.queued) != 0 {
		t.Fatalf("queue = %v, want ss-1 admitted: 1000 tickets, 100 inside", q.queued)
	}
}

// Unanswered, inventory sets no limit: the door speed alone paces admission.
func TestWithNothingToJudgeByOnlyTheDoorPaces(t *testing.T) {
	inv := &fakeInventoryClient{err: status.Error(codes.Unavailable, "down")}
	q := withInside(newFakeQueue("ss-1", "ss-2"), 50)
	qp, _ := newDoorProcessor(q, queuedSessions("ss-1", "ss-2"), inv)

	tick(t, qp)

	if len(q.queued) != 0 {
		t.Fatalf("queue = %v, want both admitted", q.queued)
	}
}
```

- [ ] **Step 2: Run them.**
  `go test ./internal/service/ -run 'TestTheRoom|TestNoTicketBeyond|TestAHundred|TestWithNothingToJudge' -v 2>&1 | grep -E "^(--- |ok|FAIL)|room_test.go:[0-9]+:"`.
  Expected: 4 FAIL, each on behaviour, because the test processor's room of 10 decides:
  - `TestTheRoomAdmitsOnlyAsManyAsTicketsLeftOver`: `queue = [], want [ss-3 ss-4]`;
  - `TestNoTicketBeyondTheBuyersInsideAdmitsNobody`: `queue = [], published [ss-1 ss-2]`;
  - `TestAHundredInsideStillAdmitsWhileTicketsAreLeft`: `queue = [ss-1]`;
  - `TestWithNothingToJudgeByOnlyTheDoorPaces`: `queue = [ss-1 ss-2]`.

- [ ] **Step 3: Write the rule's own test.** Append to `room_test.go`:

```go
func TestAdmitCount(t *testing.T) {
	counted := func(available int64) Stock { return Stock{Door: DoorOpen, Available: available, Counted: true} }
	cases := []struct {
		name   string
		due    int
		stock  Stock
		inside int64
		want   int
	}{
		{"tickets for every buyer due", 5, counted(100), 10, 5},
		{"tickets for some of them", 5, counted(12), 10, 2},
		{"as many inside as tickets", 5, counted(10), 10, 0},
		{"more inside than tickets", 5, counted(3), 10, 0},
		{"nothing to judge by", 5, Stock{Door: DoorOpen}, 10_000, 5},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := admitCount(c.due, c.stock, c.inside); got != c.want {
				t.Fatalf("admitCount(%d, %+v, %d) = %d, want %d", c.due, c.stock, c.inside, got, c.want)
			}
		})
	}
}
```

  Run `go test ./internal/service/ -run TestAdmitCount 2>&1 | head -3`. Expected: it
  fails to compile, because `admitCount` is undefined.

- [ ] **Step 4: Implement.**
  - **`queue_processor.go`, the tick.** Replace

```go
	switch qp.stock.Get(processingCtx, eventID) {
```

    with

```go
	stock := qp.stock.Stock(processingCtx, eventID)
	switch stock.Door {
```

    and replace

```go
	processingCount, err := qp.qSvc.GetProcessingCount(processingCtx, eventID)
	if err != nil {
		return fmt.Errorf("failed to get processing count: %w", err)
	}

	availableSlots := int64(qp.cfg.MaxConcurrentPerEvent) - processingCount
	if availableSlots <= 0 {
		qp.l.Debugf(processingCtx, "No available slots for event, event_id: %s, processing_count: %d, max_concurrent: %d", eventID, processingCount, qp.cfg.MaxConcurrentPerEvent)
		return nil
	}
	ssIDs = ssIDs[:min(int64(len(ssIDs)), availableSlots)]
```

    with

```go
	inside, err := qp.qSvc.GetProcessingCount(processingCtx, eventID)
	if err != nil {
		return fmt.Errorf("failed to get processing count: %w", err)
	}

	n := admitCount(len(ssIDs), stock, inside)
	if n == 0 {
		qp.l.Debugf(processingCtx, "No ticket left beyond the buyers inside - event_id: %s, inside: %d, available: %d", eventID, inside, stock.Available)
		return nil
	}
	ssIDs = ssIDs[:n]
```

  - **`queue_processor.go`, the rule.** Above `// sweepBatchSize is how many entries
    one tick may end or drop`, add:

```go
// admitCount is how many of the buyers due a tick lets in.
//
//	inventory counted the event -> while tickets available outnumber the buyers inside
//	nothing to judge by         -> all of them: the door speed alone paces admission
//
// Each buyer inside counts as one ticket still to take.
// See docs/design/admission-sizing.md#room-size-per-event.
func admitCount(due int, s Stock, inside int64) int {
	if !s.Counted {
		return due
	}
	return int(max(0, min(int64(due), s.Available-inside)))
}
```

  - **`queue_processor.go`, the field.** Delete
    `MaxConcurrentPerEvent int           // Max users in checkout per event` from
    `ProcessorConfig` and `MaxConcurrentPerEvent: cfg.DefaultMaxConcurrent,` from
    `NewQueueProcessor`. In `Start`, the log becomes:

```go
	qp.l.Infof(ctx, "Starting queue processor - interval: %v, batch_size: %d",
		qp.cfg.ProcessInterval, qp.cfg.BatchSize)
```

  - **`config/config.go`:** delete `DefaultMaxConcurrent   int` from `QueueConfig` and
    `DefaultMaxConcurrent:   getEnvAsInt("QUEUE_DEFAULT_MAX_CONCURRENT", 100),` from
    `Load`.
  - **`internal/metrics/metrics.go`:** the comment above `SlotsInUse` becomes:

```go
	// Per event, because what it is read against, the event's tickets available, is
	// per event. A cluster-wide sum cannot be compared to it.
```

  - **The tests that set the deleted field:**
    - `queue_processor_test.go`: delete `MaxConcurrentPerEvent: 10,` from
      `newTestProcessor`, and delete `TestNoSlotsAvailableLeavesQueueUntouched` whole.
      `TestNoTicketBeyondTheBuyersInsideAdmitsNobody` replaces it.
    - `door_test.go`: rename `TestASoldOutLineClosesEvenWithEveryChairTaken` to
      `TestASoldOutLineClosesWithBuyersStillInside`, and delete its line
      `qp.cfg.MaxConcurrentPerEvent = 1`.
    - `admission_test.go`: `newAdmissionRig` loses its `slots int` parameter and its
      `MaxConcurrentPerEvent: slots,` line. Its ten callers drop the argument (nine
      pass `10`, one passes `0`):
      `sed -i '' -E 's/newAdmissionRig\(t, ([^,]+), (10|0), /newAdmissionRig(t, \1, /' internal/service/*_test.go`.
      The `0` one, `TestThePreOpenQueueIsOrderedByLotInRedis`, never ticks, so it
      needed no room.
  - `gofmt -w config/config.go internal/service/queue_processor.go internal/service/queue_processor_test.go internal/service/admission_test.go internal/service/door_test.go internal/service/room_test.go`.
    The deleted fields leave their neighbours misaligned.

- [ ] **Step 5: Run the whole service.**
  `go vet ./... && WAITROOM_TEST_REDIS_ADDR=localhost:6379 go test -count=1 ./... 2>&1 | grep -E "^(ok|FAIL)"`.
  Expected: 8 packages `ok`, `internal/service` among them.
  `grep -rn "MaxConcurrent\|MAX_CONCURRENT" --include='*.go' . | grep -v vendor` prints
  nothing. Break it:
  - Change `s.Available-inside` to `s.Available`: `TestTheRoomAdmitsOnlyAsManyAsTicketsLeftOver`,
    `TestNoTicketBeyondTheBuyersInsideAdmitsNobody` and three `TestAdmitCount` cases FAIL.
  - Delete the `if !s.Counted { return due }` branch:
    `TestWithNothingToJudgeByOnlyTheDoorPaces` and `TestAdmitCount/nothing_to_judge_by`
    FAIL.

  Restore after each.

- [ ] **Step 6: The chart.** In `deploy/helm/ticketbottle/templates/apps/config.yaml`,
  delete the line `  QUEUE_DEFAULT_MAX_CONCURRENT: "100"`. Then, from the repo root:
  `bash deploy/helm/ticketbottle/tests/render-golden.sh && bash deploy/helm/ticketbottle/tests/assert-render.sh`.
  Expected: `all render assertions passed`. `git diff --stat deploy/` shows the golden
  losing that line and its waitroom `checksum/config` changing, so the deploy rolls the
  waitroom (0020).

- [ ] **Step 7: Commit.**
  `git commit -m "feat(waitroom): admit only while an event has tickets for the buyers inside"`,
  with the trailer.

---

### Task 3: the acceptance run, gate 4a, and the documents

**Files:**
- Create: `deploy/scripts/gate-room.sh` (executable)
- Modify:
  - `deploy/Makefile`: add `k3s-gate-room`
  - `deploy/scripts/gate4a-load.sh`
  - `services/waitroom-svc/CLAUDE.md`
  - `services/waitroom-svc/docs/QUEUE_PROCESSOR_GUIDE.md`
  - `services/waitroom-svc/docs/SYSTEM_FLOW_README.md`
  - `docs/METRICS.md`
  - `.claude/skills/deployment-architecture/references/eks.md`
  - `docs/design/eks-stateful-tier.md`
  - `.claude/skills/system-map/SKILL.md`
  - root `CLAUDE.md` and the design's `Status:`

- [ ] **Step 1: The script.** Create `deploy/scripts/gate-room.sh` with the content below,
  then `chmod +x` it. It needs no order and no payment, and on the code before Task 2
  it fails at step 3, since a room of 100 admits all five.

```bash
#!/usr/bin/env bash
# Room size per event, run on k3s by `make -C deploy k3s-gate-room`.
# Three tickets, five buyers, nobody orders: three are admitted and two wait while the
# three inside could take every ticket -> two tickets are added -> both are admitted.
# Rule: docs/design/admission-sizing.md#room-size-per-event.
set -euo pipefail
GW=${GW:-http://localhost:3000/api}
NS=ticketbottle
HERE="$(cd "$(dirname "$0")" && pwd)"
RUN=$(date +%s)

# JSON path extractor: `echo "$json" | getval a.b.c` -> value, or empty on any error.
getval() { python3 -c "import sys,json
d=json.load(sys.stdin)
for k in sys.argv[1].split('.'):
    d=d[k]
print(d)" "$1" 2>/dev/null || true; }

fail() { echo "ROOM GATE FAILED: $1"; exit 1; }

redis() { kubectl -n $NS exec statefulset/redis -- redis-cli "$@"; }

# signup <name> -> an access token
signup() {
  curl -s -X POST "$GW/auth/signup" -H 'Content-Type: application/json' \
    -d "{\"firstName\":\"$1\",\"lastName\":\"Buyer\",\"email\":\"room+$1-$RUN@example.com\",\"password\":\"Password123!\"}" \
    | getval data.accessToken
}

# join <token> <out-file> -> the HTTP code; the body lands in <out-file>
join() {
  curl -s -o "$2" -w '%{http_code}' -X POST "$GW/waitroom/join" -H "Authorization: Bearer $1" \
    -H 'Content-Type: application/json' -d "{\"eventId\":\"$EVENT_ID\"}"
}

# poll <token> <session> <out-file> -> the HTTP code; the body lands in <out-file>
poll() {
  curl -s -o "$3" -w '%{http_code}' "$GW/waitroom/status/$2" -H "Authorization: Bearer $1"
}

inside()  { redis ZCARD "waitroom:$EVENT_ID:checkouts" | tr -d '[:space:]'; }
waiting() { redis ZCARD "waitroom:$EVENT_ID:queue" | tr -d '[:space:]'; }

# until_room <inside> <waiting> <seconds> -> succeeds once both counts read as given
until_room() {
  for i in $(seq 1 "$3"); do
    [ "$(inside)" = "$1" ] && [ "$(waiting)" = "$2" ] && return 0
    sleep 1
  done
  return 1
}

echo "== 1. five buyers =="
TOKS=()
for n in A B C D E; do
  T=$(signup $n)
  [ -n "$T" ] || fail "signup $n returned no accessToken"
  TOKS+=("$T")
done

echo "== 2. an event with three tickets =="
CATEGORY_ID=gate1-category
kubectl -n $NS exec statefulset/postgres -- psql -U root -d ticketbottle_event -qc \
  "INSERT INTO categories (id, name, \"createdAt\", \"updatedAt\") VALUES ('$CATEGORY_ID','Gate1',now(),now()) ON CONFLICT (id) DO NOTHING;"
EVT=$(curl -s -X POST "$GW/events" -H "Authorization: Bearer ${TOKS[0]}" -H 'Content-Type: application/json' -d "{
  \"name\":\"Room Size Show\",\"description\":\"e2e\",
  \"startDate\":\"2027-01-01T00:00:00Z\",\"endDate\":\"2027-01-02T00:00:00Z\",
  \"thumbnailUrl\":\"https://example.com/t.png\",\"venue\":\"Test Arena\",
  \"street\":\"1 St\",\"city\":\"HCMC\",\"country\":\"VN\",\"categoryIds\":[\"$CATEGORY_ID\"],
  \"organizerName\":\"Gate Org\",\"organizerDescription\":\"e2e organizer\",
  \"organizerLogoUrl\":\"https://example.com/logo.png\"
}")
EVENT_ID=$(echo "$EVT" | getval data.id)
[ -n "$EVENT_ID" ] || fail "event create returned no id: $EVT"
CFG=$(curl -s -X POST "$GW/events/$EVENT_ID/config" -H "Authorization: Bearer ${TOKS[0]}" -H 'Content-Type: application/json' -d '{
  "ticketSaleStartDate":"2020-01-01T00:00:00Z","ticketSaleEndDate":"2030-01-01T00:00:00Z",
  "isFree":false,"maxAttendees":100,"isPublic":true,"requiresApproval":false,
  "allowWaitRoom":true,"isNewTrending":false
}')
[ -n "$(echo "$CFG" | getval data.id)" ] || fail "config create failed: $CFG"
kubectl -n $NS exec statefulset/postgres -- psql -U root -d ticketbottle_event -qc \
  "UPDATE events SET status='PUBLISHED' WHERE id='$EVENT_ID';"
TCID=$("$HERE/seed-ticketclass.sh" "$EVENT_ID" 3 10000)
[ -n "$TCID" ] || fail "seed-ticketclass returned no id"
echo "  eventId=$EVENT_ID ticketClassId=$TCID (total 3)"

echo "== 3. all five join; three are admitted, two wait =="
SESSIONS=()
for i in 0 1 2 3 4; do
  join "${TOKS[$i]}" "/tmp/room-$i.json" >/dev/null
  S=$(getval data.sessionId < "/tmp/room-$i.json")
  [ -n "$S" ] || fail "join $i returned no sessionId: $(cat "/tmp/room-$i.json")"
  SESSIONS+=("$S")
done
until_room 3 2 15 || fail "inside=$(inside) waiting=$(waiting) after 15s, want 3 and 2"
# The door admits 2 a second: five more ticks would have admitted both waiters.
sleep 5
[ "$(inside)" = 3 ] && [ "$(waiting)" = 2 ] || fail "after 5s more: inside=$(inside) waiting=$(waiting), want 3 and 2"
echo "  3 inside, 2 waiting, held for 5s"

echo "== 4. a waiter held by the room is queued, not paused =="
W=""
for i in 0 1 2 3 4; do
  [ -z "$(redis ZSCORE "waitroom:$EVENT_ID:checkouts" "${SESSIONS[$i]}")" ] && { W=$i; break; }
done
[ -n "$W" ] || fail "no session is waiting"
CODE=$(poll "${TOKS[$W]}" "${SESSIONS[$W]}" /tmp/room-w.json)
[ "$CODE" = 200 ] || fail "the waiter's poll answered $CODE: $(cat /tmp/room-w.json)"
ST=$(getval data.status < /tmp/room-w.json); PAUSED=$(getval data.paused < /tmp/room-w.json)
[ "$ST" = QUEUED ] && [ "$PAUSED" = False ] || fail "waiter reads status=$ST paused=$PAUSED, want QUEUED and False"
echo "  waiter: QUEUED, paused False, position $(getval data.position < /tmp/room-w.json)"

echo "== 5. the organizer adds two tickets; both waiters are admitted =="
kubectl -n $NS exec statefulset/postgres -- psql -U root -d ticketbottle_inventory -qc \
  "UPDATE ticket_class SET total = total + 2 WHERE id='$TCID';"
until_room 5 0 15 || fail "inside=$(inside) waiting=$(waiting) 15s after adding two tickets, want 5 and 0"
echo "  5 inside, 0 waiting"

echo "ROOM GATE PASSED: event $EVENT_ID"
```

  Run `bash -n deploy/scripts/gate-room.sh`. Expected: no output.

- [ ] **Step 2: The target.** In `deploy/Makefile`, add `k3s-gate-room` to the k3s
  `.PHONY` line, and after `k3s-gate-checkout-expiry`:

```make
k3s-gate-room:     ## A room follows the tickets left: three of five admitted, then all five (tunnel must be open)
	KUBECONFIG=$(KUBECONFIG_FILE) GW=http://localhost:3000/api ./scripts/gate-room.sh
```

  `make -C deploy -n k3s-gate-room` prints the command.

- [ ] **Step 3: Gate 4a.** In `deploy/scripts/gate4a-load.sh`:
  - the header's check 2 becomes
    `#   2. admission control held  (buyers inside <= TOTAL: a tick admits only while tickets outnumber them)`;
  - delete the line that sets `MAX_CONCURRENT=` from the ConfigMap;
  - the check becomes:

```bash
echo "  peak concurrent checkouts=$PEAK_ADMITTED (tickets=$TOTAL)"
[ "$PEAK_ADMITTED" -le "$TOTAL" ] || fail "waitroom admitted $PEAK_ADMITTED concurrent checkouts for $TOTAL tickets"
```

  - in the final `GATE 4a PASSED` line, `peak admitted $PEAK_ADMITTED/$MAX_CONCURRENT`
    becomes `peak admitted $PEAK_ADMITTED/$TOTAL`.

  `bash -n deploy/scripts/gate4a-load.sh` prints nothing. This check is an invariant
  there, not this change's regression test: gate 4a's buyers stay seconds, so its room
  never fills. `gate-room.sh` is the regression test.

- [ ] **Step 4: The documents.**
  - **`services/waitroom-svc/CLAUDE.md`.**
    - The Role bullet
      `- Bounded concurrency — at most N users in "checkout" at once (configurable via `Queue` config; ~100 default).`
      becomes
      `- Room size per event — a tick admits only while the event has more tickets available than buyers inside (`admitCount`); there is no fixed cap ([0025](../../docs/decisions/0025-an-event-admits-buyers-only-while-it-has-tickets-for-them.md)).`
    - Under *The door asks inventory whether tickets are left*, after the bullet that
      begins `**The tick asks only with someone due**`, add:
      `- **The same answer sizes the room.** `Stock` carries `available` as well as the door, and the tick admits only while it exceeds the buyers inside (`admitCount`). Unanswered, or with no class that can still sell, it sets no limit.`
    - In *Door speed is set per deployment target*, the sentence that wraps as

```
Room size,
`QUEUE_DEFAULT_MAX_CONCURRENT`, is a separate, per-event question.
```

      becomes

```
Room size
is not a setting: it is each event's tickets available, read each tick
(`docs/design/admission-sizing.md`, *Room size per event*).
```
    - In *Deploy note*, the text that wraps as

```
the service can briefly admit up to `MaxConcurrent` extra users on top of those
already checking out. Bounded (<=100/event, <=15 min) but real
```

      becomes

```
the service can briefly admit extra users on top of those already
checking out, as many as the event's tickets available allow, for up to 15 min. Real
```
  - **`services/waitroom-svc/docs/QUEUE_PROCESSOR_GUIDE.md`:** delete the line
    `    MaxConcurrentPerEvent:  100,               // Max in checkout`.
  - **`services/waitroom-svc/docs/SYSTEM_FLOW_README.md`:**
    - `│   ├─ Check available checkout slots (max 100)` becomes
      `│   ├─ Ask inventory what the event has left (cached half a tick)`;
    - `│   ├─ Calculate: available = maxConcurrent - processingCount` becomes
      `│   ├─ Calculate: admit = min(due, tickets available - buyers inside)`;
    - delete `QUEUE_DEFAULT_MAX_CONCURRENT=100   # Max users in checkout per event`.
  - **`docs/METRICS.md`**, under *Labels under a cardinality condition*: replace
    ``is per event because the limit it is read against,
`QUEUE_DEFAULT_MAX_CONCURRENT`, is `MaxConcurrentPerEvent`. A cluster-wide sum
cannot be compared to a per-event cap in either direction`` with
    ``is per event because what it is read against, the event's
tickets available, is per event. A cluster-wide sum cannot be compared to a per-event
limit in either direction``.
  - **`.claude/skills/deployment-architecture/references/eks.md`**, *Ceiling 2*: replace
    ``The waitroom admits
at `QUEUE_DEFAULT_MAX_CONCURRENT / checkout duration` — 100 slots against a 15-minute `JWT_EXPIRY`,
i.e. under 1 admit/sec against a ~1300/sec ceiling.`` with
    ``The waitroom admits
at most its door speed — 10 a second on EKS, unmeasured, and 2 on k3s — against a ~1300/sec
ceiling.``
  - **`docs/design/eks-stateful-tier.md`:**
    - In the "Low traffic" note, replace
      ``At the ~100 the platform is configured for, `shared` is
> idle. Raise `QUEUE_DEFAULT_MAX_CONCURRENT` into the thousands and Temporal
> becomes the platform's heaviest writer`` with
      ``Concurrent checkouts are bounded by each event's tickets
> and by door speed × the 15-minute token: up to 9000 at EKS's unmeasured door of
> 10 a second ([0025](../decisions/0025-an-event-admits-buyers-only-while-it-has-tickets-for-them.md)).
> Into the thousands, Temporal becomes the platform's heaviest writer``.
    - Replace
      ``the waitroom admits at `maxConcurrent / checkout duration`,
  which at the shipped `QUEUE_DEFAULT_MAX_CONCURRENT: 100` against a 15-minute
  `JWT_EXPIRY` is under 1/sec.`` with
      ``the waitroom admits at most its door speed, 10 a
  second at EKS's unmeasured default and 2 on k3s.``
  - **`.claude/skills/system-map/SKILL.md`:** the acceptance row adds
    `deploy/scripts/gate-room.sh`.
  - **Root `CLAUDE.md`:** the register row for "How many buyers may hold inventory at
    once" reads `built, not yet verified on k3s` in place of `decided 2026-10-01, not
    built`.
  - **Design** `docs/design/admission-sizing.md`, *Room size per event*: `Status:`
    becomes `specified 2026-10-01; its two calls answered the same day; built; not yet verified on k3s.`

- [ ] **Step 5: Checks.**
  - `grep -rln "MAX_CONCURRENT\|MaxConcurrent" deploy services/waitroom-svc .claude docs/METRICS.md docs/design/eks-stateful-tier.md --exclude-dir=vendor`
    prints nothing. Plans and decision records keep the name: they are true on their
    date.
  - `python3 .claude/skills/system-map/scripts/check_map.py` and
    `python3 docs/decisions/index.py && python3 docs/decisions/index.py --check` are
    clean.

- [ ] **Step 6: Commit.**
  `git commit -m "test(deploy): check an event's room follows the tickets it has left"`,
  with the trailer.

---

### Task 4: verify on k3s, then record

This needs the architect's go-ahead: it pushes `dev` and starts the box.

1. Push. Wait for `build-push-ecr`, `go-tests` and `chart-assertions` on the new SHA.
2. `aws eks list-clusters --region us-east-1` prints no cluster. Then:
   - start the box;
   - allow your IP: `make -C deploy update-my-ip`, then a saved
     `terraform plan -out` in `deploy/terraform/envs/k3s`, checked to change only the
     SSH CIDR, then applied;
   - open the tunnel with `-o ServerAliveInterval=15`;
   - run `k3s-kubeconfig`.
3. `make -C deploy k3s-deploy`. Expected:
   - every app is on the new SHA;
   - `kubectl -n ticketbottle get cm waitroom-config -o yaml | grep -c MAX_CONCURRENT`
     prints `0`;
   - the waitroom's log has `Starting queue processor - interval: 1s, batch_size: 2`.
4. `make -C deploy k3s-gate2` and `make -C deploy k3s-gate-sold-out`. Expected: both
   pass. Admission and the door are unchanged where tickets are plenty, and where they
   run out.
5. `make -C deploy k3s-gate-room 2>&1 | tee /tmp/room.log`, with `set -o pipefail`.
   Expected: `ROOM GATE PASSED`.
6. Stop the box.
7. Record:
   - 0025 becomes `accepted`, with an Outcome;
   - the design's `Status:` reads verified, and its step 4 reads built and verified;
   - the register row reads verified;
   - this plan reads COMPLETE, with *Results*;
   - regenerate the decisions index, commit, and push on the go-ahead.
