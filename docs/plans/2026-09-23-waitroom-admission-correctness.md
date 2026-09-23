# Waitroom: admit the right buyer, and prove it end to end

**Status: NOT STARTED.** D1 and D2 were decided on 2026-09-23: the first option in
each.

**Goal:** Fix the admission defects found on 2026-09-23, each reproduced against
real Redis, then run on k3s the end-to-end check the stampede work order meant to
run and could not.

**Derived from:** `2026-09-23-waitroom-join-stampede.md` and a verification pass
the same day. That plan names `make -C deploy gate1` as the check that admission
discovery works end to end. It is not one (P1); this plan owns that fact now.

## What is wrong

| # | Defect | Evidence | Task |
|---|---|---|---|
| P1 | Gate 1 never exercises admission discovery. Step 7 reads the session out of Redis with `kubectl exec` and signs its own checkout token; it never calls `GET /waitroom/status`. | `deploy/scripts/gate1-purchase-flow.sh:83-111` | 4 |
| P2 | A processor tick that lands inside `JoinQueue` is undone. `JoinQueue` enqueues, publishes `queue.joined` synchronously, then `SET`s its stale in-memory session — reverting `admitted` + token to `queued`. The session is out of the queue and holds a slot. | Reproduced: `status=queued position=-1 token="" holding_slot=true`, unchanged after 3 more ticks | 1 |
| P3 | `dev` is 11 commits ahead of `origin/dev`, and CI builds only on push, so ECR `:dev` predates all four stampede phases. A deploy now runs none of them. | `ticketbottle/waitroom:dev` is `sha-b22d268`, pushed 2026-09-22 — `origin/dev`'s head, before any stampede commit | 6 |
| P4 | `make -C deploy gate1` pinned neither `KUBECONFIG` nor `GW`, and the default kubectl context is `ticketbottle-eks`: its `kubectl` steps would have gone to EKS while its HTTP went through the tunnel. | `kubectl config current-context` | Resolved: the target went with kind; `k3s-gate2` pins both |
| P5 | The stampede plan and the register's open row both name `gate1` as the check. | — | 6 |
| N1 | **Admission ignores the sale start.** `SaleStartAt` only feeds the queue score; `ProcessEventQueue` admits whenever a slot is free. The first `MaxConcurrent` pre-open joiners are admitted in arrival order, before the doors open, on 15-minute tokens that `Reserve` refuses until the ticket class's own `sale_start_at` (`ErrSaleClosed` → 409). The draw orders only the overflow. | Reproduced: a joiner admitted an hour before open; with 3 slots, arrivals #0–2 admitted in order, #3–5 shuffled | 2 |
| N2 | `LeaveQueue` skips `mapGRPCError`, so an unknown session is `INTERNAL` — a 500 that the taxonomy reserves for bugs, and that pages. | `internal/delivery/grpc/service.go:79-83`; test red: `code = Internal` | 5 |
| G1 | The draw's order in a live sorted set had no test. A throwaway one passed — every score inside the band, order not arrival's in 4 of 4 runs — so it needs to become permanent. | — | 3 |
| G2 | No ownership check on status or leave. Neither request carries the caller, and the gateway sends only `sessionId`. Status hands the checkout token to any logged-in user holding the id; leave lets them dequeue its owner. | `proto/waitroom.proto:31-33, 48-50`; `waitroom.service.ts:43-55` | 5 |

**P2's window** is one synchronous Kafka round trip (sarama `SyncProducer`, acks=1,
3 retries), open whenever a slot is free — so every join once the sale is open,
widening as Kafka slows. The fix is a deletion: the stale write carries only
`Position`, and nothing reads the stored copy (`GetQueueStatus` recomputes it with
`ZRANK`).

### Checked and ruled out

- **The gateway drops the token.** No: `QueueStatusMapper` maps `checkoutToken`;
  `admitted` reaches the client as `READY`.
- **Polling is new with phase 1.** No: the route landed in `ef43d6f` (2026-08-29) and
  `deploy/loadtest/purchase.js:105` has polled it since. `k3s-load` runs k6's
  sustained scenario, which sets no thresholds — it counts a stranded buyer without
  failing.

## Decisions

**D1 — How admission waits for the sale (Task 2).**

- *(Chosen)* **The queue's own scores decide.** `PeekQueue` reads only scores
  below the current whole second. Pre-open draws sit in `[saleStart-1, saleStart)`,
  so none is due before the doors open, and admission gains no dependency. Costs: a
  post-open joiner waits up to one extra second; a sale start moved after people
  joined is not honoured for them, since their scores were drawn against the old one.
- **The processor asks the event gate for `SaleStartAt` each tick.** Honours a moved
  sale start, but puts event-svc in the admission loop: with the cache cold and
  event-svc down, the processor must either hold everyone (a stalled on-sale) or
  admit (N1 again, exactly when it matters).

**D2 — What a stranger's request returns (Task 5).**

- *(Chosen)* **`NOT_FOUND`.** The session is invisible to anyone else, so an id
  cannot be probed for existence. Reuses `ErrSessionNotFound`; no new error code.
- **`PERMISSION_DENIED` (403).** The taxonomy's literal wording, "authenticated but not
  allowed", but it confirms the session exists.

## Running the waitroom suite

```bash
docker run -d --name tb-waitroom-test-redis -p 63799:6379 redis:7-alpine
cd services/waitroom-svc
export WAITROOM_TEST_REDIS_ADDR=localhost:63799
go test -race -count=1 ./...
```

Without the variable the Redis tests skip locally; CI sets it
(`.github/workflows/go-tests.yml:102-124`). Run `gofmt -w` on every Go file you
touch — `service.go` and `waitroom_service.go` already carry drift at HEAD.

---

### Task 1: A join must not undo an admission (P2)

**Files:**
- Create: `services/waitroom-svc/internal/service/admission_test.go`
- Modify: `services/waitroom-svc/internal/service/waitroom_service.go:111-113`

**Interfaces:**
- Produces: `newAdmissionRig(t, saleStart time.Time, slots int, prod *interleavingProducer) *admissionRig`;
  methods `join(t, userID string) string`, `tick(t)`, `status(t, ssID string) *QueueStatusOutput`;
  `sleepPastSecond()`. Tasks 2, 3 and 5 add tests to this file.

- [ ] **Step 1: Write the rig and the failing test**

`interleavingProducer` runs one processor tick inside `PublishQueueJoined` — after
the enqueue, before `JoinQueue`'s last write. It first sleeps into the next whole
second, so the session is due under Task 2's rule as well.

```go
package service

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/vogiaan1904/ticketbottle-waitroom/config"
	"github.com/vogiaan1904/ticketbottle-waitroom/internal/delivery/kafka"
	"github.com/vogiaan1904/ticketbottle-waitroom/internal/models"
	repo "github.com/vogiaan1904/ticketbottle-waitroom/internal/repository/redis"
	"github.com/vogiaan1904/ticketbottle-waitroom/pkg/logger"
	"github.com/vogiaan1904/ticketbottle-waitroom/pkg/redis"
)

// --- rig ---------------------------------------------------------------------

// admissionRig runs the real session and queue services against a test Redis,
// so a test can place a processor tick exactly where a race needs it.
type admissionRig struct {
	svc  WaitroomService
	proc *queueProcessor
	cli  *redis.Client
	eID  string
}

// interleavingProducer runs one processor tick inside PublishQueueJoined:
// after JoinQueue has enqueued the session, before JoinQueue returns.
type interleavingProducer struct {
	fakeProducer
	proc       *queueProcessor
	tickOnJoin bool
	ticked     bool
}

func (p *interleavingProducer) PublishQueueJoined(ctx context.Context, e kafka.QueueJoinedEvent) error {
	if !p.tickOnJoin || p.ticked {
		return nil
	}
	p.ticked = true
	sleepPastSecond()
	return p.proc.ProcessEventQueue(ctx, e.EventID)
}

// sleepPastSecond waits into the next whole second, when a queue score
// drawn now has come due.
func sleepPastSecond() {
	time.Sleep(time.Until(time.Now().Truncate(time.Second).Add(time.Second + 20*time.Millisecond)))
}

func newAdmissionRig(t *testing.T, saleStart time.Time, slots int, prod *interleavingProducer) *admissionRig {
	t.Helper()

	addr := os.Getenv("WAITROOM_TEST_REDIS_ADDR")
	if addr == "" {
		if os.Getenv("CI") != "" {
			t.Fatalf("CI requires WAITROOM_TEST_REDIS_ADDR to point at a test redis")
		}
		t.Skip("WAITROOM_TEST_REDIS_ADDR not set; skipping Redis integration test")
	}

	cli := redis.NewClient(config.RedisConfig{Addr: addr, PoolSize: 5, MinIdleConns: 1})
	l := logger.InitializeZapLogger(logger.ZapConfig{Level: "error", Mode: "development", Encoding: "console"})

	ssSvc := NewSessionService(repo.NewRedisSessionRepository(cli, l), config.JWTConfig{Secret: "test", Expiry: 15 * time.Minute}, l)
	qSvc := NewQueueService(repo.NewRedisQueueRepository(cli, l), l)
	proc := &queueProcessor{
		qSvc:  qSvc,
		ssSvc: ssSvc,
		prod:  prod,
		l:     l,
		cfg: ProcessorConfig{
			MaxConcurrentPerEvent: slots,
			BatchSize:             10,
			RetryAttempts:         2,
			RetryDelay:            time.Millisecond,
			CheckoutTTL:           15 * time.Minute,
		},
		stopCh: make(chan struct{}),
	}
	prod.proc = proc
	ev := &fakeEventClient{allowWaitRoom: true, saleStart: saleStart.UTC().Format(time.RFC3339)}

	eID := fmt.Sprintf("%s-%d", t.Name(), time.Now().UnixNano())
	t.Cleanup(func() {
		ctx := context.Background()
		cli.Del(ctx, "waitroom:"+eID+":queue")
		cli.Del(ctx, "waitroom:"+eID+":checkouts")
		cli.SRem(ctx, "waitroom:active_events", eID)
	})

	return &admissionRig{
		svc:  NewWaitroomService(qSvc, ssSvc, ev, prod, l, proc, time.Minute),
		proc: proc,
		cli:  cli,
		eID:  eID,
	}
}

func (r *admissionRig) join(t *testing.T, userID string) string {
	t.Helper()
	out, err := r.svc.JoinQueue(context.Background(), &JoinQueueInput{UserID: userID, EventID: r.eID})
	if err != nil {
		t.Fatalf("join: %v", err)
	}
	return out.SessionID
}

func (r *admissionRig) tick(t *testing.T) {
	t.Helper()
	if err := r.proc.ProcessEventQueue(context.Background(), r.eID); err != nil {
		t.Fatalf("tick: %v", err)
	}
}

func (r *admissionRig) status(t *testing.T, ssID string) *QueueStatusOutput {
	t.Helper()
	st, err := r.svc.GetQueueStatus(context.Background(), ssID)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	return st
}

// --- tests -------------------------------------------------------------------

func TestAPostOpenJoinerIsAdmittedWithinASecond(t *testing.T) {
	r := newAdmissionRig(t, time.Now().Add(-time.Hour), 10, &interleavingProducer{})

	ssID := r.join(t, "u-1")
	sleepPastSecond()
	r.tick(t)

	if st := r.status(t, ssID); st.Status != models.SessionStatusAdmitted || st.CheckoutToken == "" {
		t.Fatalf("want admitted with a token, got status=%s token=%q", st.Status, st.CheckoutToken)
	}
}

// Invariant: a join never undoes an admission that lands while it is in flight.
func TestAnAdmissionDuringTheJoinIsNotUndone(t *testing.T) {
	prod := &interleavingProducer{tickOnJoin: true}
	r := newAdmissionRig(t, time.Now().Add(-time.Hour), 10, prod)

	ssID := r.join(t, "u-1")
	if !prod.ticked {
		t.Fatal("no tick ran inside the join, so nothing was asserted")
	}

	if st := r.status(t, ssID); st.Status != models.SessionStatusAdmitted || st.CheckoutToken == "" {
		t.Fatalf("admitted during the join, then reverted: status=%s token=%q", st.Status, st.CheckoutToken)
	}
}
```

- [ ] **Step 2: Run it and watch it fail**

Run: `go test -count=1 -v -run 'TestAPostOpen|TestAnAdmission' ./internal/service/`

Expected: `TestAnAdmissionDuringTheJoinIsNotUndone` FAILS with
`admitted during the join, then reverted: status=queued token=""`.
`TestAPostOpenJoinerIsAdmittedWithinASecond` passes; it guards Task 2 against
holding too much, and is mutation-checked there.

- [ ] **Step 3: Delete the stale write**

In `JoinQueue`, remove:

```go
	if err := s.ssSvc.UpdateSession(ctx, ss); err != nil {
		return nil, fmt.Errorf("failed to update session: %w", err)
	}
```

- [ ] **Step 4: Run the whole suite**

Run: `go test -race -count=1 ./...` — all packages PASS.

- [ ] **Step 5: Commit**

```bash
git add services/waitroom-svc/internal/service/admission_test.go \
        services/waitroom-svc/internal/service/waitroom_service.go
git commit -m "fix(waitroom): stop a join from undoing the admission it raced"
```

---

### Task 2: Nobody is admitted before the sale opens (N1) — per D1

**Files:**
- Modify: `services/waitroom-svc/pkg/redis/client.go:127-130` (`ZRange` → `ZRangeBelow`)
- Modify: `services/waitroom-svc/internal/repository/redis/queue_repository.go:20, 147-157`
- Modify: `services/waitroom-svc/internal/repository/redis/queue_repository_test.go:198, 234`
- Modify: `services/waitroom-svc/internal/service/queue_service.go:161`
- Modify: `services/waitroom-svc/CLAUDE.md` (draw section; `:159`)
- Test: `services/waitroom-svc/internal/service/admission_test.go`

**Interfaces:**
- Consumes: Task 1's rig.
- Produces: `QueueRepository.GetQueueMembers(ctx, eID string, before float64, count int64) ([]string, error)`
  and `(*redis.Client).ZRangeBelow(ctx, key string, max float64, count int64) ([]string, error)`.
  `(*redis.Client).ZRange` is removed; nothing else calls it.

- [ ] **Step 1: Add the failing tests to `admission_test.go`**

```go
func TestNobodyIsAdmittedBeforeTheSaleOpens(t *testing.T) {
	r := newAdmissionRig(t, time.Now().Add(time.Hour), 10, &interleavingProducer{})

	ssID := r.join(t, "u-1")
	sleepPastSecond()
	r.tick(t)

	if st := r.status(t, ssID); st.Status != models.SessionStatusQueued {
		t.Fatalf("admitted an hour before the sale opens: status=%s", st.Status)
	}
}

func TestPreOpenJoinersAreAdmittedOnceTheSaleOpens(t *testing.T) {
	saleStart := time.Now().Truncate(time.Second).Add(2 * time.Second)
	r := newAdmissionRig(t, saleStart, 10, &interleavingProducer{})
	ids := []string{r.join(t, "u-1"), r.join(t, "u-2"), r.join(t, "u-3")}

	r.tick(t)
	for _, id := range ids {
		if st := r.status(t, id); st.Status != models.SessionStatusQueued {
			t.Fatalf("admitted before the sale opened: status=%s", st.Status)
		}
	}

	time.Sleep(time.Until(saleStart.Add(20 * time.Millisecond)))
	r.tick(t)
	for _, id := range ids {
		if st := r.status(t, id); st.Status != models.SessionStatusAdmitted {
			t.Fatalf("still waiting after the sale opened: status=%s", st.Status)
		}
	}
}
```

- [ ] **Step 2: Run them and watch them fail**

Run: `go test -count=1 -v -run 'TestNobody|TestPreOpenJoiners' ./internal/service/`

Expected: FAIL with `admitted an hour before the sale opens: status=admitted` and
`admitted before the sale opened: status=admitted`.

- [ ] **Step 3: Peek only what is due**

`pkg/redis/client.go` — add `"strconv"` to the imports and replace `ZRange` with:

```go
// ZRangeBelow returns up to count members scored strictly below max, lowest
// first. A negative count returns them all.
func (r *Client) ZRangeBelow(ctx context.Context, key string, max float64, count int64) ([]string, error) {
	return r.redis.ZRangeByScore(ctx, key, &redis.ZRangeBy{
		Min:   "-inf",
		Max:   "(" + strconv.FormatFloat(max, 'f', -1, 64),
		Count: count,
	}).Result()
}
```

`queue_repository.go` — the interface line becomes
`GetQueueMembers(ctx context.Context, eID string, before float64, count int64) ([]string, error)`,
and the method:

```go
// GetQueueMembers returns up to count members scored below `before`, head first.
func (r *redisQueueRepository) GetQueueMembers(ctx context.Context, eID string, before float64, count int64) ([]string, error) {
	qKey := r.queueKey(eID)

	mems, err := r.cli.ZRangeBelow(ctx, qKey, before, count)
	if err != nil {
		r.l.Errorf(ctx, "redisQueueRepository.GetQueueMembers: %v", err)
		return nil, err
	}

	return mems, nil
}
```

`queue_service.go`, in `PeekQueue`:

```go
	// Due once its whole second has passed. Pre-open draws sit in
	// [saleStart-1, saleStart), so none is due before the doors open.
	due := float64(time.Now().Unix())
	sessionIDs, err := s.repo.GetQueueMembers(ctx, eventID, due, int64(count))
```

`queue_repository_test.go` — add `"math"` to the imports; the two calls become
`repo.GetQueueMembers(ctx, eID, math.Inf(1), 2)` and
`repo.GetQueueMembers(ctx, eID, math.Inf(1), -1)`.

- [ ] **Step 4: Run the suite, then prove the guard can fail**

Run: `go test -race -count=1 ./...` — all PASS.

Then change `due := float64(time.Now().Unix())` to `due := float64(time.Now().Unix()) - 5`
and run `go test -count=1 -run TestAPostOpen ./internal/service/`. It must FAIL with
`want admitted with a token, got status=queued`. Restore the line.

- [ ] **Step 5: Update `services/waitroom-svc/CLAUDE.md`**

Under *Order is a draw before the sale opens*, after the code block:

```markdown
Nothing is admitted before its score comes due: `PeekQueue` reads only scores below
the current whole second, so the pre-open band waits for the doors without the
processor asking event-svc anything.
```

At `:159`, "`PeekQueue` is a plain `ZRANGE`" becomes "`PeekQueue` is a plain read".

- [ ] **Step 6: Commit**

```bash
git add services/waitroom-svc
git commit -m "fix(waitroom): admit nobody before the sale opens, so the draw decides"
```

---

### Task 3: The draw, in a real sorted set (G1)

**Files:**
- Test: `services/waitroom-svc/internal/service/admission_test.go`

**Interfaces:**
- Consumes: Task 1's rig, and its `cli` field.

- [ ] **Step 1: Add the test**

```go
// The draw must survive a real sorted set: every pre-open score inside
// [saleStart-1, saleStart), and the resulting order not arrival's.
func TestThePreOpenQueueIsOrderedByLotInRedis(t *testing.T) {
	saleStart := time.Now().Add(time.Hour).Truncate(time.Second)
	r := newAdmissionRig(t, saleStart, 0, &interleavingProducer{})

	const n = 40
	arrival := make([]string, n)
	for i := range n {
		arrival[i] = r.join(t, fmt.Sprintf("u-%d", i))
	}

	zs, err := r.cli.GetClient().ZRangeWithScores(context.Background(), "waitroom:"+r.eID+":queue", 0, -1).Result()
	if err != nil {
		t.Fatalf("read queue: %v", err)
	}
	if len(zs) != n {
		t.Fatalf("queue holds %d, want %d", len(zs), n)
	}

	lo, hi := float64(saleStart.Unix()-1), float64(saleStart.Unix())
	inPlace := 0
	for i, z := range zs {
		if z.Score < lo || z.Score >= hi {
			t.Fatalf("score %v outside the pre-open band [%v, %v)", z.Score, lo, hi)
		}
		if z.Member == arrival[i] {
			inPlace++
		}
	}
	// A shuffle of 40 leaves about one in place; arrival order leaves all 40.
	if inPlace == n {
		t.Fatal("the queue is in arrival order; the draw did not reach Redis")
	}
}
```

- [ ] **Step 2: It passes on arrival, so prove each assertion can fail**

Run: `go test -count=1 -run TestThePreOpen ./internal/service/` — PASS.

Mutant A, at the top of `models.DrawQueueScore`:
`return float64(queuedAt.UnixNano()) / 1e9`. The test must FAIL with
`score … outside the pre-open band`.

Mutant B, keeping scores in the band but in arrival order:

```go
var mutSeq float64

func DrawQueueScore(queuedAt, saleStartAt time.Time) float64 {
	if !saleStartAt.IsZero() && queuedAt.Before(saleStartAt) {
		mutSeq++
		return float64(saleStartAt.Unix()-1) + mutSeq/1e6
	}
	// ...existing body
```

The test must FAIL with `the queue is in arrival order`. Restore `session.go`.

- [ ] **Step 3: Commit**

```bash
git add services/waitroom-svc/internal/service/admission_test.go
git commit -m "test(waitroom): pin the draw's order in a real sorted set"
```

---

### Task 4: Gate 1 finds admission the way a client does (P1)

**Files:**
- Modify: `deploy/scripts/gate1-purchase-flow.sh:3-16, 83-111`

It proves that the gateway route, the status mapping and the owner check hand a
real client its token. It cannot catch P2, whose window is milliseconds — Task 1's
test owns that.

- [ ] **Step 1: Replace the header's bypass notes (lines 4–16)**

```bash
#   -> join waitroom -> poll status until admitted -> create order
#   -> trigger payment webhook -> poll order until COMPLETED.
#
# Field names below are pinned from the gateway DTOs/mappers and the Go services.
# One hop bypasses missing HTTP surface: order-svc requires EventStatus=PUBLISHED and
# no gateway route publishes an event, so it is set directly in the event DB.
```

- [ ] **Step 2: Replace step 7 (lines 83–111), from its `echo` to the line before `== 8.`**

```bash
echo "== 7. poll status until admitted, as a client does =="
CHECKOUT=""
for i in $(seq 1 30); do
  ST=$(curl -s "$GW/waitroom/status/$SESSION" -H "$AUTH")
  CHECKOUT=$(echo "$ST" | getval data.checkoutToken)
  [ -n "$CHECKOUT" ] && break
  case "$(echo "$ST" | getval data.status)" in
    EXPIRED|CANCELLED|FAILED) fail "session ended before admission: $ST" ;;
  esac
  sleep 1
done
[ -n "$CHECKOUT" ] || fail "not admitted within 30s: $ST"
echo "  checkoutToken=${CHECKOUT:0:16}... (from GET /waitroom/status)"
```

- [ ] **Step 3: Check it parses, and that nothing still signs a token**

Run: `bash -n deploy/scripts/gate1-purchase-flow.sh && grep -n 'JWT_SECRET\|USER_ID\|redis-cli' deploy/scripts/gate1-purchase-flow.sh`

Expected: no output from `grep`. The script runs for real in Task 6.

- [ ] **Step 4: Commit**

```bash
git add deploy/scripts/gate1-purchase-flow.sh
git commit -m "test(gate): take the checkout token from the status route, not a forgery"
```

---

### Task 5: Only a session's owner can read or leave it (G2, N2) — per D2

**Files:**
- Modify: `proto/waitroom.proto:31-33, 48-50`
- Regenerate: `services/waitroom-svc/protogen/waitroom/waitroom.pb.go`, `services/api-gateway/src/protogen/waitroom.pb.ts`
- Copy: `services/{api-gateway,event-svc,payment-svc,user-svc}/src/protos/waitroom.proto`
- Modify: `services/waitroom-svc/internal/service/waitroom_service.go` (interface, `GetQueueStatus`, `LeaveQueue`)
- Modify: `services/waitroom-svc/internal/delivery/grpc/service.go:52, 79-83`
- Create: `services/waitroom-svc/internal/delivery/grpc/service_test.go`
- Modify: `services/api-gateway/src/modules/waitroom/waitroom.controller.ts`, `waitroom.service.ts`
- Test: `services/waitroom-svc/internal/service/admission_test.go`

**Interfaces:**
- Consumes: Task 1's rig.
- Produces: `GetQueueStatus(ctx, ssID, userID string) (*QueueStatusOutput, error)`,
  `LeaveQueue(ctx, ssID, userID string) error`; `user_id = 2` on both requests.

- [ ] **Step 1: Write the failing tests**

In `admission_test.go`: add `"errors"` to the imports; add `owners map[string]string`
to `admissionRig` and `owners: make(map[string]string),` to its literal; in `join`,
record `r.owners[out.SessionID] = userID` before returning; in `status`, call
`r.svc.GetQueueStatus(context.Background(), ssID, r.owners[ssID])`. Then add:

```go
func TestOnlyTheOwnerCanReadOrLeaveASession(t *testing.T) {
	r := newAdmissionRig(t, time.Now().Add(-time.Hour), 10, &interleavingProducer{})
	ssID := r.join(t, "u-owner")
	ctx := context.Background()

	if _, err := r.svc.GetQueueStatus(ctx, ssID, "u-other"); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("status read by a stranger: err=%v, want ErrSessionNotFound", err)
	}
	if err := r.svc.LeaveQueue(ctx, ssID, "u-other"); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("leave by a stranger: err=%v, want ErrSessionNotFound", err)
	}
	if st := r.status(t, ssID); st.Status != models.SessionStatusQueued {
		t.Fatalf("a stranger's leave removed the owner: status=%s", st.Status)
	}
}
```

Create `internal/delivery/grpc/service_test.go`:

```go
package grpc

import (
	"context"
	"testing"

	"github.com/vogiaan1904/ticketbottle-waitroom/internal/service"
	"github.com/vogiaan1904/ticketbottle-waitroom/pkg/logger"
	waitroompb "github.com/vogiaan1904/ticketbottle-waitroom/protogen/waitroom"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// leaveSvc answers LeaveQueue with a fixed error; any other call panics.
type leaveSvc struct {
	service.WaitroomService
	err error
}

func (f *leaveSvc) LeaveQueue(context.Context, string, string) error { return f.err }

// A session the caller cannot see is a 404, never an INTERNAL that pages.
func TestLeavingAnUnknownSessionIsNotFound(t *testing.T) {
	s := &grpcService{
		svc: &leaveSvc{err: service.ErrSessionNotFound},
		l:   logger.InitializeZapLogger(logger.ZapConfig{Level: "error", Mode: "development", Encoding: "console"}),
	}

	_, err := s.LeaveQueue(context.Background(), &waitroompb.LeaveQueueRequest{SessionId: "ss-1", UserId: "u-1"})
	if got := status.Code(err); got != codes.NotFound {
		t.Fatalf("code = %s, want NotFound", got)
	}
}
```

- [ ] **Step 2: Run them — the first red is a build failure**

Run: `go test -count=1 ./internal/...`

Expected: build fails — too many arguments to `GetQueueStatus` / `LeaveQueue`, and
unknown field `UserId`. Step 6 shows each assertion can go red on its own.

- [ ] **Step 3: Change the contract and regenerate**

In `proto/waitroom.proto`, add `string user_id = 2;` to both `GetQueueStatusRequest`
and `LeaveQueueRequest`. Then:

```bash
make -C services/waitroom-svc protoc-all
for s in api-gateway event-svc payment-svc user-svc; do
  cp proto/waitroom.proto services/$s/src/protos/waitroom.proto
done
(cd services/api-gateway && npm run proto:waitroom)
```

Use `proto:waitroom`, not `update:proto`: the latter regenerates `order.pb.ts` too,
which breaks `src/modules/orders/` (see the register's open row). Expected diff in
`waitroom.pb.ts`: two added `userId` fields, nothing else.

- [ ] **Step 4: Check ownership in the service**

`waitroom_service.go` — the interface lines become
`GetQueueStatus(ctx context.Context, ssID, userID string) (*QueueStatusOutput, error)` and
`LeaveQueue(ctx context.Context, ssID, userID string) error`. Both methods take
`userID` and, right after loading the session:

```go
	// Why: the response carries a checkout token, which is a bearer credential.
	if ss.UserID != userID {
		return nil, ErrSessionNotFound
	}
```

(`LeaveQueue` returns `ErrSessionNotFound` alone and drops the comment.)

`delivery/grpc/service.go`:

```go
	out, err := s.svc.GetQueueStatus(ctx, req.SessionId, req.UserId)
```

```go
	if err := s.svc.LeaveQueue(ctx, req.SessionId, req.UserId); err != nil {
		s.l.Errorf(ctx, "Failed to leave queue: %v", err)
		return nil, resp.ParseGRPCError(s.mapGRPCError(err))
	}
```

- [ ] **Step 5: Run the suite**

Run: `go test -race -count=1 ./...` — all PASS.

- [ ] **Step 6: Prove each check can fail**

Delete the owner check in `GetQueueStatus` → `TestOnlyTheOwnerCanReadOrLeaveASession`
FAILS with `status read by a stranger: err=<nil>`. Restore. Replace
`s.mapGRPCError(err)` with `err` in `LeaveQueue` → `TestLeavingAnUnknownSessionIsNotFound`
FAILS with `code = Internal, want NotFound`. Restore.

- [ ] **Step 7: Pass the caller from the gateway**

`waitroom.controller.ts`:

```ts
  async leaveQueue(
    @Req() req: RequestWithUser,
    @Body() dto: LeaveQueueDto,
  ): Promise<LeaveQueueRespDto> {
    const protoResponse = await this.waitroomService.leaveQueue(req.user, dto);
```

```ts
  async getStatus(
    @Req() req: RequestWithUser,
    @Param('sessionId') sessionId: string,
  ): Promise<QueueStatusRespDto> {
    return QueueStatusMapper.toDto(await this.waitroomService.getQueueStatus(req.user, sessionId));
```

`waitroom.service.ts`:

```ts
  async leaveQueue(user: RequestUser, dto: LeaveQueueDto): Promise<LeaveQueueResponse> {
    const leaveQueueResp = await firstValueFrom(
      this.waitroomService.leaveQueue({
        sessionId: dto.sessionId,
        userId: user.id,
      }),
```

```ts
  async getQueueStatus(user: RequestUser, sessionId: string): Promise<QueueStatusResponse> {
    return firstValueFrom(this.waitroomService.getQueueStatus({ sessionId, userId: user.id }));
```

Run in `services/api-gateway`: `npx tsc --noEmit -p tsconfig.json` (exit 0) and
`npx jest` (20 pass). If `tsc` cannot find `../lib/tsc.js`, reinstall:
`rm -rf node_modules && npm install`. The gateway has no waitroom unit test; Task 6's
gate covers the wiring, since a missing `userId` turns every status poll into a 404.

- [ ] **Step 8: Commit**

```bash
git add proto/waitroom.proto services/waitroom-svc services/api-gateway/src \
        services/event-svc/src/protos services/payment-svc/src/protos services/user-svc/src/protos
git commit -m "fix(waitroom): only a session's owner can read its token or leave its place"
```

---

### Task 6: Run it on k3s (P3, P5)

**Needs:** Tasks 1–5 committed on `dev`.

- [ ] **Step 1: Prove EKS is off.** `aws login`, then `make -C deploy eks-status` —
  nothing billing. The box and EKS must never run together, and `start-ec2-k3s`
  does not check; only `eks-up` calls `eks-guard`.
- [ ] **Step 2: Build the images.** `git push origin dev`, and wait for
  `build-push-ecr` to go green. Confirm the tag moved:
  `aws ecr describe-images --region us-east-1 --repository-name ticketbottle/waitroom --image-ids imageTag=dev --query 'imageDetails[0].imagePushedAt'`
  is after the push.
- [ ] **Step 3: Start the box.** `make -C deploy start-ec2-k3s`; open the printed
  tunnel in its own terminal; `make -C deploy k3s-kubeconfig`.
- [ ] **Step 4: Deploy.** `make -C deploy k3s-deploy`. `pullPolicy: Always` picks up
  the new `:dev`.
- [ ] **Step 5: Gate.** `make -C deploy k3s-gate2`. Pass: `GATE 1 PASSED`, having
  polled for its token.
- [ ] **Step 6: Load.** `make -C deploy k3s-load`, then read the k6 summary it prints.
  `tb_unexpected_errors` must be absent or zero. The sustained scenario sets no
  thresholds, so the target exits 0 either way; an `admission-timeout` there means
  P2 or N1 is back.
- [ ] **Step 7: Stop the box.** `make -C deploy stop-ec2-k3s`.
- [ ] **Step 8: Close the records.** In the root `CLAUDE.md`, move *Do the waitroom's
  stampede changes survive an end-to-end run?* to *Decided*, owned by this plan, with
  the run's date. Mark this plan `Status: COMPLETE`.

## Not in scope

- **`HandleCheckout*` revert the token invalidation.** Each writes back a session read
  before `InvalidateCheckoutToken` ran. Latent: `IsTokenInvalidated` is called only
  from `ValidateCheckoutToken`, which has no caller. Fix it when revocation is wired.
- **The sale start has two homes.** The waitroom reads the event config's
  `ticketSaleStartDate`; `Reserve` enforces the ticket class's `sale_start_at`. If
  they disagree, admission opens at one time and reservation at the other.
- **Per-event `maxConcurrent`** — still open in the register.
