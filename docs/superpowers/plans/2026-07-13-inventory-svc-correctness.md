# Inventory Service Correctness Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `inventory-svc` reservation flow atomic, deadlock-free, and idempotent under saga retries, drain expiry backlog, and add the missing concurrency test suite.

**Architecture:** Rewrite `Reserve` from a per-item goroutine-per-transaction fan-out into a single transaction that locks all target `ticket_class` rows in ascending id order, validates, increments counters, and batch-inserts reservations — all-or-nothing. Make `Reserve`/`Confirm`/`Release` key off `order_code` and treat "already in the target state / nothing to do" as success. Loop the expiry worker until backlog is drained. Delete the dead methods this exposes.

**Tech Stack:** Go 1.25, GORM v1.31.0 (`gorm.io/gorm`, `gorm.io/gorm/clause`), PostgreSQL 15, gRPC, the repo's zap logger wrapper. Tests run against a real PostgreSQL (the compose DB on `localhost:5435`); no new dependencies.

## Global Constraints

- **Language/ORM:** Go 1.25; keep GORM (`gorm.io/gorm v1.31.0`). Do not switch to raw SQL.
- **Oversell invariant:** every quantity mutation happens inside a transaction, guarded by either `SELECT … FOR UPDATE` (`clause.Locking{Strength:"UPDATE"}`) or an atomic conditional `UPDATE … WHERE …` with `gorm.Expr`. Never read-modify-write a counter outside a locked transaction.
- **Deadlock-freedom invariant:** when locking multiple `ticket_class` rows, lock them in **ascending id order**.
- **Idempotency contract:** `Reserve`/`Confirm`/`Release` key off `order_code`. "Already in the target state / nothing to do" → return `nil` (success). Only genuine state conflicts → error.
- **Logging:** zap wrapper, ctx-first `f`-suffixed methods, messages prefixed `package.type.Method` (e.g. `s.l.Errorf(ctx, "service.reservation.Reserve.LockTicketClasses: %v", err)`).
- **No contract changes:** do not edit `proto/` or regenerate stubs. The gRPC RPC surface is unchanged.
- **Out of scope (deferred to a separate plan):** Kafka producers; versioned migrations + deploy Helm jobs (spec Phase C / S4). This plan keeps boot-time `AutoMigrate` and adds the new expiry index via an idempotent `CREATE INDEX IF NOT EXISTS` executed at boot.

**Prerequisite for running tests:** the inventory Postgres container must be up (`cd development && make up-aws`), and a `ticketbottle_inventory_test` database must exist (Task 1 adds a `make test-db` target that creates it).

**Spec:** `docs/superpowers/specs/2026-07-13-inventory-svc-optimization-design.md`

---

### Task 1: Test harness

Establishes a real-Postgres test helper and `make` targets. Nothing else in the plan can be tested without it.

**Files:**
- Create: `services/inventory-svc/internal/services/setup_test.go`
- Modify: `services/inventory-svc/Makefile`

**Interfaces:**
- Produces (used by Tasks 2–5; tests live **in** package `service`, so they call `NewReservationService` directly):
  - `func newTestDB(t *testing.T) *pkgGorm.Repository` — connects to the test DB, `AutoMigrate`s both models, truncates on cleanup, `t.Skip`s if the DB is unreachable.
  - `func newTestLogger() pkgLog.Logger`
  - `func seedTicketClass(t *testing.T, repo *pkgGorm.Repository, total, reserved, sold int) models.TicketClass`
  - (`must`/`future` shared helpers are appended to this same file in Task 3; `reserveSvc`/`ticketClassByID` live in Task 2's test file.)

- [ ] **Step 1: Add the test-DB make targets**

Modify `services/inventory-svc/Makefile`, appending:

```makefile
test-db:
	@docker exec ticketbottle-postgres-inventory psql -U root -d ticketbottle_inventory \
		-c "CREATE DATABASE ticketbottle_inventory_test" 2>/dev/null || true

test: test-db
	go test ./internal/... -race -count=1
```

- [ ] **Step 2: Write the test harness**

Create `services/inventory-svc/internal/services/setup_test.go`:

```go
package service

import (
	"context"
	"os"
	"testing"
	"time"

	cfg "github.com/vogiaan/ticketbottle-inventory/config"
	"github.com/vogiaan/ticketbottle-inventory/internal/models"
	pkgGorm "github.com/vogiaan/ticketbottle-inventory/pkg/gorm"
	pkgLog "github.com/vogiaan/ticketbottle-inventory/pkg/logger"
)

const defaultTestDSN = "postgresql://root:root@localhost:5435/ticketbottle_inventory_test?sslmode=disable"

func newTestDB(t *testing.T) *pkgGorm.Repository {
	t.Helper()
	dsn := os.Getenv("TEST_POSTGRES_URL")
	if dsn == "" {
		dsn = defaultTestDSN
	}
	db, err := pkgGorm.New(&cfg.PostgresConfig{
		URL:             dsn,
		MaxOpenConns:    25,
		MaxIdleConns:    10,
		ConnMaxLifetime: 5 * time.Minute,
		ConnMaxIdleTime: 10 * time.Minute,
	})
	if err != nil {
		t.Skipf("skipping: cannot reach test postgres (%s): %v", dsn, err)
	}
	if err := db.AutoMigrate(&models.TicketClass{}, &models.Reservation{}); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	t.Cleanup(func() {
		db.Exec("TRUNCATE reservation, ticket_class RESTART IDENTITY CASCADE")
	})
	return pkgGorm.NewRepository(db)
}

func newTestLogger() pkgLog.Logger {
	return pkgLog.InitializeZapLogger(pkgLog.ZapConfig{
		Level:    "error",
		Mode:     "development",
		Encoding: "console",
	})
}

func seedTicketClass(t *testing.T, repo *pkgGorm.Repository, total, reserved, sold int) models.TicketClass {
	t.Helper()
	tc := models.TicketClass{
		EventID:    "evt-" + t.Name(),
		Name:       "GA-" + t.Name(),
		PriceCents: 1000,
		Currency:   "USD",
		Total:      total,
		Reserved:   reserved,
		Sold:       sold,
		Status:     models.TicketClassStatusActive,
	}
	if err := repo.Create(context.Background(), &tc); err != nil {
		t.Fatalf("seed ticket class: %v", err)
	}
	return tc
}
```

- [ ] **Step 3: Add a smoke test proving the harness connects**

Append to `services/inventory-svc/internal/services/setup_test.go`:

```go
func TestHarness_Connects(t *testing.T) {
	repo := newTestDB(t)
	tc := seedTicketClass(t, repo, 100, 0, 0)
	if tc.ID == 0 {
		t.Fatal("expected a generated ticket class id")
	}
}
```

- [ ] **Step 4: Run the smoke test**

Run:
```bash
cd services/inventory-svc && make test-db && go test ./internal/services/ -run TestHarness_Connects -v
```
Expected: PASS (or SKIP with a clear message if the compose Postgres isn't running — start it with `cd development && make up-aws`, then re-run and expect PASS).

- [ ] **Step 5: Commit**

```bash
git add services/inventory-svc/internal/services/setup_test.go services/inventory-svc/Makefile
git commit -m "test(inventory): real-postgres test harness + make targets"
```

---

### Task 2: `Reserve` — atomic single transaction

Replaces the goroutine fan-out with one all-or-nothing transaction, locking ticket classes in ascending id order, plus a `Reserve`-level idempotency guard. Fixes the `wgErr` data race, the partial-failure `reserved` capacity leak, and the cross-order deadlock risk.

**Files:**
- Modify: `services/inventory-svc/internal/services/reservation.go` (rewrite `Reserve`; delete `Create`, `DeleteByOrderCode`; add helpers)
- Test: `services/inventory-svc/internal/services/reservation_reserve_test.go`

**Interfaces:**
- Consumes (from Task 1): `newTestDB`, `newTestLogger`, `seedTicketClass`.
- Produces: `Reserve(ctx, ReserveInput) error` with new semantics; internal helpers `aggregateDemand([]ReserveItem) (ids []int64, qtyByID map[int64]int)` and `indexByID([]models.TicketClass) map[int64]models.TicketClass`.

- [ ] **Step 1: Write the failing tests**

Create `services/inventory-svc/internal/services/reservation_reserve_test.go`:

```go
package service

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/vogiaan/ticketbottle-inventory/internal/models"
	pkgGorm "github.com/vogiaan/ticketbottle-inventory/pkg/gorm"
)

func reserveSvc(t *testing.T) (ReservationService, *pkgGorm.Repository) {
	repo := newTestDB(t)
	return NewReservationService(newTestLogger(), repo), repo
}

func ticketClassByID(t *testing.T, repo *pkgGorm.Repository, id int64) models.TicketClass {
	t.Helper()
	var tc models.TicketClass
	if err := repo.FindByID(context.Background(), &tc, id); err != nil {
		t.Fatalf("reload ticket class %d: %v", id, err)
	}
	return tc
}

func TestReserve_SingleItem_IncrementsReserved(t *testing.T) {
	svc, repo := reserveSvc(t)
	tc := seedTicketClass(t, repo, 100, 0, 0)

	err := svc.Reserve(context.Background(), ReserveInput{
		OrderCode: "order-1",
		ExpiresAt: time.Now().UTC().Add(15 * time.Minute),
		Items:     []ReserveItem{{TicketClassID: tc.ID, Qty: 3}},
	})
	if err != nil {
		t.Fatalf("Reserve: %v", err)
	}
	if got := ticketClassByID(t, repo, tc.ID); got.Reserved != 3 {
		t.Fatalf("reserved = %d, want 3", got.Reserved)
	}
}

// The capacity-leak test: a two-item order where the second item cannot be
// satisfied must leave BOTH ticket classes untouched (fully atomic).
func TestReserve_PartialFailure_NoCapacityLeak(t *testing.T) {
	svc, repo := reserveSvc(t)
	tcOK := seedTicketClass(t, repo, 100, 0, 0)
	tcFull := seedTicketClass(t, repo, 1, 0, 0)

	err := svc.Reserve(context.Background(), ReserveInput{
		OrderCode: "order-2",
		ExpiresAt: time.Now().UTC().Add(15 * time.Minute),
		Items: []ReserveItem{
			{TicketClassID: tcOK.ID, Qty: 2},
			{TicketClassID: tcFull.ID, Qty: 5}, // impossible
		},
	})
	if err == nil {
		t.Fatal("expected Reserve to fail on insufficient stock")
	}
	if got := ticketClassByID(t, repo, tcOK.ID); got.Reserved != 0 {
		t.Fatalf("tcOK.reserved = %d, want 0 (no leak)", got.Reserved)
	}
	var count int64
	repo.WithContext(context.Background()).Model(&models.Reservation{}).
		Where("order_code = ?", "order-2").Count(&count)
	if count != 0 {
		t.Fatalf("reservation rows = %d, want 0", count)
	}
}

func TestReserve_Idempotent_SecondCallNoOp(t *testing.T) {
	svc, repo := reserveSvc(t)
	tc := seedTicketClass(t, repo, 100, 0, 0)
	in := ReserveInput{
		OrderCode: "order-3",
		ExpiresAt: time.Now().UTC().Add(15 * time.Minute),
		Items:     []ReserveItem{{TicketClassID: tc.ID, Qty: 4}},
	}
	if err := svc.Reserve(context.Background(), in); err != nil {
		t.Fatalf("first Reserve: %v", err)
	}
	if err := svc.Reserve(context.Background(), in); err != nil {
		t.Fatalf("second Reserve should be a no-op, got: %v", err)
	}
	if got := ticketClassByID(t, repo, tc.ID); got.Reserved != 4 {
		t.Fatalf("reserved = %d, want 4 (not double-counted)", got.Reserved)
	}
}

func TestReserve_Concurrent_NoOversell(t *testing.T) {
	svc, repo := reserveSvc(t)
	tc := seedTicketClass(t, repo, 10, 0, 0) // capacity 10

	var wg sync.WaitGroup
	var okCount int64
	var mu sync.Mutex
	for i := 0; i < 20; i++ { // 20 orders each wanting 1
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			err := svc.Reserve(context.Background(), ReserveInput{
				OrderCode: "cc-" + string(rune('a'+n)),
				ExpiresAt: time.Now().UTC().Add(15 * time.Minute),
				Items:     []ReserveItem{{TicketClassID: tc.ID, Qty: 1}},
			})
			if err == nil {
				mu.Lock()
				okCount++
				mu.Unlock()
			}
		}(i)
	}
	wg.Wait()

	got := ticketClassByID(t, repo, tc.ID)
	if got.Reserved > got.Total {
		t.Fatalf("OVERSOLD: reserved=%d > total=%d", got.Reserved, got.Total)
	}
	if okCount != 10 {
		t.Fatalf("successful reserves = %d, want exactly 10", okCount)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run:
```bash
cd services/inventory-svc && go test ./internal/services/ -run TestReserve -race -v
```
Expected: FAIL. `TestReserve_PartialFailure_NoCapacityLeak` fails (old code leaks `reserved` on the committed first item) and `TestReserve_Idempotent_SecondCallNoOp` fails (old code hits the `(order_code, ticket_class_id)` unique violation on the second call).

- [ ] **Step 3: Rewrite `Reserve` and add helpers**

In `services/inventory-svc/internal/services/reservation.go`, add `"sort"` to the imports, and replace the entire `Reserve` function **and** the `Create` function (lines 38–125) with:

```go
func (s implReservationService) Reserve(ctx context.Context, in ReserveInput) error {
	ids, qtyByID := aggregateDemand(in.Items)

	return s.repo.GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Idempotency: reservations already exist for this order → no-op.
		var existing int64
		if err := tx.Model(&models.Reservation{}).
			Where("order_code = ?", in.OrderCode).Count(&existing).Error; err != nil {
			s.l.Errorf(ctx, "service.reservation.Reserve.CountExisting: %v", err)
			return err
		}
		if existing > 0 {
			s.l.Infof(ctx, "service.reservation.Reserve: reservations already exist for order_code=%s, no-op", in.OrderCode)
			return nil
		}

		// Lock all target ticket classes in ascending id order (deadlock-free).
		var tcs []models.TicketClass
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id IN ?", ids).Order("id").Find(&tcs).Error; err != nil {
			s.l.Errorf(ctx, "service.reservation.Reserve.LockTicketClasses: %v", err)
			return err
		}
		if len(tcs) != len(ids) {
			s.l.Warnf(ctx, "service.reservation.Reserve: ticket classes not found (want=%d got=%d)", len(ids), len(tcs))
			return gorm.ErrRecordNotFound
		}
		byID := indexByID(tcs)

		// Validate availability against the locked rows.
		for _, id := range ids {
			tc := byID[id]
			q := qtyByID[id]
			if tc.Total-tc.Reserved-tc.Sold < q {
				s.l.Warnf(ctx, "service.reservation.Reserve: insufficient stock for ticket_class_id=%d (available=%d, requested=%d)",
					id, tc.Total-tc.Reserved-tc.Sold, q)
				return gorm.ErrInvalidData
			}
		}

		// Increment reserved counters (guarded) and build reservation rows.
		rs := make([]models.Reservation, 0, len(ids))
		for _, id := range ids {
			q := qtyByID[id]
			res := tx.Model(&models.TicketClass{}).
				Where("id = ? AND reserved + sold + ? <= total", id, q).
				Update("reserved", gorm.Expr("reserved + ?", q))
			if res.Error != nil {
				s.l.Errorf(ctx, "service.reservation.Reserve.IncrementReserved: ticket_class_id=%d: %v", id, res.Error)
				return res.Error
			}
			if res.RowsAffected == 0 {
				s.l.Warnf(ctx, "service.reservation.Reserve: availability guard failed for ticket_class_id=%d", id)
				return gorm.ErrInvalidData
			}
			rs = append(rs, s.buildModel(in.OrderCode, in.ExpiresAt, ReserveItem{TicketClassID: id, Qty: q}))
		}

		if err := tx.Create(&rs).Error; err != nil {
			s.l.Errorf(ctx, "service.reservation.Reserve.InsertReservations: %v", err)
			return err
		}

		s.l.Infof(ctx, "service.reservation.Reserve: created %d reservations for order_code=%s", len(rs), in.OrderCode)
		return nil
	})
}

// aggregateDemand collapses items to one entry per ticket class (summing qty)
// and returns the ticket-class ids sorted ascending for deterministic locking.
func aggregateDemand(items []ReserveItem) (ids []int64, qtyByID map[int64]int) {
	qtyByID = make(map[int64]int, len(items))
	for _, it := range items {
		if _, ok := qtyByID[it.TicketClassID]; !ok {
			ids = append(ids, it.TicketClassID)
		}
		qtyByID[it.TicketClassID] += it.Qty
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids, qtyByID
}

func indexByID(tcs []models.TicketClass) map[int64]models.TicketClass {
	m := make(map[int64]models.TicketClass, len(tcs))
	for _, tc := range tcs {
		m[tc.ID] = tc
	}
	return m
}
```

- [ ] **Step 4: Remove the two now-dead bodies and keep the interface satisfied**

The `Reserve` rewrite in Step 3 already deleted the `Create` body (`Create` is **not** in the `ReservationService` interface, so nothing else is needed for it). Two more edits in `services/inventory-svc/internal/services/reservation.go`:

1. Delete the `DeleteByOrderCode` method body (its only caller was the removed compensation) **and** delete the `DeleteByOrderCode` line from the `ReservationService` interface. Both are required together — if the body is gone but the interface still lists it, `NewReservationService` no longer satisfies the interface and the build fails.
2. Remove the now-unused `"sync"` import.

Leave `Delete`, `UpdateStatus`, `UpdateStatusByOrderCode`, and the remaining unused getters in place — the full interface trim is Task 6. Keep the `"time"` import (still used by `confirmReservationTx`) and the newly added `"sort"` import.

- [ ] **Step 5: Run the tests to verify they pass**

Run:
```bash
cd services/inventory-svc && go build ./... && go test ./internal/services/ -run TestReserve -race -v
```
Expected: PASS (all four `TestReserve_*` tests), no race warnings.

- [ ] **Step 6: Commit**

```bash
git add services/inventory-svc/internal/services/reservation.go services/inventory-svc/internal/services/reservation_reserve_test.go
git commit -m "fix(inventory): atomic single-tx Reserve (race, capacity leak, deadlock, idempotency)"
```

---

### Task 3: `Confirm` — idempotent + conflict semantics

Makes `Confirm` a no-op when already confirmed and return a distinct conflict error for expired/cancelled holds, so Temporal retries can't fail a completed order. Introduces the `ErrStateConflict` sentinel and its gRPC mapping.

**Files:**
- Create: `services/inventory-svc/internal/services/errors.go`
- Modify: `services/inventory-svc/pkg/errors/errors.go` (add `ErrConflict`)
- Modify: `services/inventory-svc/internal/delivery/grpc/errors.go` (map `ErrStateConflict`)
- Modify: `services/inventory-svc/internal/services/reservation.go` (`confirmReservationTx`)
- Test: `services/inventory-svc/internal/services/reservation_confirm_test.go`

**Interfaces:**
- Consumes (from Task 2): `Reserve`, `newTestDB`, `seedTicketClass`, `ticketClassByID`.
- Produces: `var ErrStateConflict error` (package `service`); `pkgErrors.ErrConflict` (`codes.FailedPrecondition`); `Confirm` with new semantics.

- [ ] **Step 1: Write the failing tests**

Create `services/inventory-svc/internal/services/reservation_confirm_test.go`:

```go
package service

import (
	"context"
	"testing"
	"time"

	"github.com/vogiaan/ticketbottle-inventory/internal/models"
)

func TestConfirm_MovesReservedToSold(t *testing.T) {
	svc, repo := reserveSvc(t)
	tc := seedTicketClass(t, repo, 100, 0, 0)
	must(t, svc.Reserve(context.Background(), ReserveInput{
		OrderCode: "o-confirm", ExpiresAt: future(), Items: []ReserveItem{{TicketClassID: tc.ID, Qty: 5}},
	}))

	if err := svc.Confirm(context.Background(), "o-confirm"); err != nil {
		t.Fatalf("Confirm: %v", err)
	}
	got := ticketClassByID(t, repo, tc.ID)
	if got.Reserved != 0 || got.Sold != 5 {
		t.Fatalf("reserved=%d sold=%d, want 0/5", got.Reserved, got.Sold)
	}
}

func TestConfirm_Idempotent_SecondCallNoOp(t *testing.T) {
	svc, repo := reserveSvc(t)
	tc := seedTicketClass(t, repo, 100, 0, 0)
	must(t, svc.Reserve(context.Background(), ReserveInput{
		OrderCode: "o-conf2", ExpiresAt: future(), Items: []ReserveItem{{TicketClassID: tc.ID, Qty: 5}},
	}))
	must(t, svc.Confirm(context.Background(), "o-conf2"))

	if err := svc.Confirm(context.Background(), "o-conf2"); err != nil {
		t.Fatalf("second Confirm should be a no-op, got: %v", err)
	}
	got := ticketClassByID(t, repo, tc.ID)
	if got.Reserved != 0 || got.Sold != 5 {
		t.Fatalf("reserved=%d sold=%d, want 0/5 (not double-applied)", got.Reserved, got.Sold)
	}
}

func TestConfirm_Expired_ReturnsConflict(t *testing.T) {
	svc, repo := reserveSvc(t)
	tc := seedTicketClass(t, repo, 100, 0, 0)
	// Reserve already-expired.
	must(t, svc.Reserve(context.Background(), ReserveInput{
		OrderCode: "o-exp", ExpiresAt: time.Now().UTC().Add(-1 * time.Minute),
		Items: []ReserveItem{{TicketClassID: tc.ID, Qty: 5}},
	}))

	err := svc.Confirm(context.Background(), "o-exp")
	if err != ErrStateConflict {
		t.Fatalf("Confirm on expired hold = %v, want ErrStateConflict", err)
	}
}

func TestConfirm_NoReservations_ReturnsNotFound(t *testing.T) {
	svc, _ := reserveSvc(t)
	if err := svc.Confirm(context.Background(), "nope"); err == nil {
		t.Fatal("expected an error confirming an unknown order")
	}
}
```

Append shared test helpers to `services/inventory-svc/internal/services/setup_test.go`:

```go
func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func future() time.Time { return time.Now().UTC().Add(15 * time.Minute) }
```

- [ ] **Step 2: Run the tests to verify they fail**

Run:
```bash
cd services/inventory-svc && go test ./internal/services/ -run TestConfirm -v
```
Expected: FAIL — `TestConfirm_Idempotent_SecondCallNoOp` fails (old code returns `ErrInvalidData` on the second confirm) and `TestConfirm_Expired_ReturnsConflict` fails (old code returns `ErrInvalidData`, not `ErrStateConflict`). Compilation also fails until `ErrStateConflict` exists — that's expected RED.

- [ ] **Step 3: Add the domain sentinel and its gRPC error**

Create `services/inventory-svc/internal/services/errors.go`:

```go
package service

import "errors"

// ErrStateConflict signals a reservation is in a state that forbids the
// requested transition (e.g. confirming an expired hold, releasing a
// confirmed one). Distinct from insufficient-stock and not-found.
var ErrStateConflict = errors.New("reservation state conflict")
```

Add to the `var (...)` block in `services/inventory-svc/pkg/errors/errors.go`:

```go
	ErrConflict = NewGRPCError(codes.FailedPrecondition, "reservation state conflict")
```

In `services/inventory-svc/internal/delivery/grpc/errors.go`, add an import for the service package and a mapping. The file becomes:

```go
package grpc

import (
	svc "github.com/vogiaan/ticketbottle-inventory/internal/services"
	pkgErrors "github.com/vogiaan/ticketbottle-inventory/pkg/errors"
	"google.golang.org/grpc/codes"
	"gorm.io/gorm"
)

var (
	ErrValidationFailed = pkgErrors.NewGRPCError(codes.InvalidArgument, "validation failed")
)

func (s *grpcService) mapError(err error) error {
	switch err {
	case gorm.ErrRecordNotFound:
		return pkgErrors.ErrNotFound
	case gorm.ErrInvalidData:
		return pkgErrors.ErrInsufficientStock
	case svc.ErrStateConflict:
		return pkgErrors.ErrConflict
	default:
		return pkgErrors.ErrInternal
	}
}
```

- [ ] **Step 4: Rewrite `confirmReservationTx`**

In `services/inventory-svc/internal/services/reservation.go`, replace the `confirmReservationTx` function with:

```go
func (s implReservationService) confirmReservationTx(ctx context.Context, tx *gorm.DB, oCode string) error {
	var rs []models.Reservation
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("order_code = ?", oCode).Find(&rs).Error; err != nil {
		s.l.Errorf(ctx, "service.reservation.Confirm.LockReservations: %v", err)
		return err
	}
	if len(rs) == 0 {
		s.l.Warnf(ctx, "service.reservation.Confirm: no reservations for order_code=%s", oCode)
		return gorm.ErrRecordNotFound
	}

	// Idempotency: all already confirmed → no-op.
	allConfirmed := true
	for _, r := range rs {
		if r.Status != models.ReservationStatusConfirmed {
			allConfirmed = false
			break
		}
	}
	if allConfirmed {
		s.l.Infof(ctx, "service.reservation.Confirm: order_code=%s already confirmed, no-op", oCode)
		return nil
	}

	now := time.Now().UTC()
	tcUps := make(map[int64]int)
	rIDs := make([]int64, 0, len(rs))
	for _, r := range rs {
		if r.Status != models.ReservationStatusActive || now.After(r.ExpiresAt) {
			s.l.Warnf(ctx, "service.reservation.Confirm: conflict for reservation %d (status=%s, timeExpired=%v)",
				r.ID, r.Status, now.After(r.ExpiresAt))
			return ErrStateConflict
		}
		tcUps[r.TicketClassID] += r.Qty
		rIDs = append(rIDs, r.ID)
	}

	for tcID, qty := range tcUps {
		result := tx.Model(&models.TicketClass{}).
			Where("id = ? AND reserved >= ?", tcID, qty).
			Updates(map[string]any{
				"reserved": gorm.Expr("reserved - ?", qty),
				"sold":     gorm.Expr("sold + ?", qty),
			})
		if result.Error != nil {
			s.l.Errorf(ctx, "service.reservation.Confirm.UpdateTicketClass: ticket_class_id=%d: %v", tcID, result.Error)
			return result.Error
		}
		if result.RowsAffected == 0 {
			s.l.Errorf(ctx, "service.reservation.Confirm: insufficient reserved for ticket_class_id=%d (needed=%d)", tcID, qty)
			return gorm.ErrInvalidData
		}
	}

	if err := tx.Model(&models.Reservation{}).
		Where("id IN ?", rIDs).
		Update("status", models.ReservationStatusConfirmed).Error; err != nil {
		s.l.Errorf(ctx, "service.reservation.Confirm.UpdateReservations: %v", err)
		return err
	}

	s.l.Infof(ctx, "service.reservation.Confirm: confirmed %d reservations for order_code=%s", len(rs), oCode)
	return nil
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run:
```bash
cd services/inventory-svc && go build ./... && go test ./internal/services/ -run TestConfirm -v
```
Expected: PASS (all four `TestConfirm_*`).

- [ ] **Step 6: Commit**

```bash
git add services/inventory-svc/internal/services/errors.go services/inventory-svc/pkg/errors/errors.go services/inventory-svc/internal/delivery/grpc/errors.go services/inventory-svc/internal/services/reservation.go services/inventory-svc/internal/services/reservation_confirm_test.go services/inventory-svc/internal/services/setup_test.go
git commit -m "fix(inventory): idempotent Confirm with FailedPrecondition conflict semantics"
```

---

### Task 4: `Release` — idempotent + conflict semantics

Makes `Release` a no-op when there is nothing to release (already cancelled/expired, or no rows) and a conflict when the order is already confirmed.

**Files:**
- Modify: `services/inventory-svc/internal/services/reservation.go` (`cancelReservationTx`)
- Test: `services/inventory-svc/internal/services/reservation_release_test.go`

**Interfaces:**
- Consumes: `Reserve`, `Confirm`, `ErrStateConflict`, `newTestDB`, `seedTicketClass`, `ticketClassByID`, `must`, `future`.
- Produces: `Release` with new semantics.

- [ ] **Step 1: Write the failing tests**

Create `services/inventory-svc/internal/services/reservation_release_test.go`:

```go
package service

import (
	"context"
	"testing"
)

func TestRelease_FreesReserved(t *testing.T) {
	svc, repo := reserveSvc(t)
	tc := seedTicketClass(t, repo, 100, 0, 0)
	must(t, svc.Reserve(context.Background(), ReserveInput{
		OrderCode: "o-rel", ExpiresAt: future(), Items: []ReserveItem{{TicketClassID: tc.ID, Qty: 6}},
	}))

	if err := svc.Release(context.Background(), "o-rel"); err != nil {
		t.Fatalf("Release: %v", err)
	}
	if got := ticketClassByID(t, repo, tc.ID); got.Reserved != 0 {
		t.Fatalf("reserved = %d, want 0", got.Reserved)
	}
}

func TestRelease_Idempotent_SecondCallNoOp(t *testing.T) {
	svc, repo := reserveSvc(t)
	tc := seedTicketClass(t, repo, 100, 0, 0)
	must(t, svc.Reserve(context.Background(), ReserveInput{
		OrderCode: "o-rel2", ExpiresAt: future(), Items: []ReserveItem{{TicketClassID: tc.ID, Qty: 6}},
	}))
	must(t, svc.Release(context.Background(), "o-rel2"))

	if err := svc.Release(context.Background(), "o-rel2"); err != nil {
		t.Fatalf("second Release should be a no-op, got: %v", err)
	}
}

func TestRelease_NoReservations_NoOp(t *testing.T) {
	svc, _ := reserveSvc(t)
	if err := svc.Release(context.Background(), "ghost"); err != nil {
		t.Fatalf("Release on unknown order should be a no-op, got: %v", err)
	}
}

func TestRelease_Confirmed_ReturnsConflict(t *testing.T) {
	svc, repo := reserveSvc(t)
	tc := seedTicketClass(t, repo, 100, 0, 0)
	must(t, svc.Reserve(context.Background(), ReserveInput{
		OrderCode: "o-rel3", ExpiresAt: future(), Items: []ReserveItem{{TicketClassID: tc.ID, Qty: 6}},
	}))
	must(t, svc.Confirm(context.Background(), "o-rel3"))

	if err := svc.Release(context.Background(), "o-rel3"); err != ErrStateConflict {
		t.Fatalf("Release on confirmed order = %v, want ErrStateConflict", err)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run:
```bash
cd services/inventory-svc && go test ./internal/services/ -run TestRelease -v
```
Expected: FAIL — old `cancelReservationTx` returns `ErrRecordNotFound` for no rows (want no-op) and `ErrInvalidData` for a confirmed/second-call (want no-op / `ErrStateConflict`).

- [ ] **Step 3: Rewrite `cancelReservationTx`**

In `services/inventory-svc/internal/services/reservation.go`, replace `cancelReservationTx` with:

```go
func (s implReservationService) cancelReservationTx(ctx context.Context, tx *gorm.DB, oCode string) error {
	var rs []models.Reservation
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("order_code = ?", oCode).Find(&rs).Error; err != nil {
		s.l.Errorf(ctx, "service.reservation.Release.LockReservations: %v", err)
		return err
	}
	if len(rs) == 0 {
		s.l.Infof(ctx, "service.reservation.Release: no reservations for order_code=%s, no-op", oCode)
		return nil // idempotent: nothing to release
	}

	// Idempotency: all already terminal (cancelled/expired) → no-op.
	allReleased := true
	for _, r := range rs {
		if r.Status != models.ReservationStatusCancelled && r.Status != models.ReservationStatusExpired {
			allReleased = false
			break
		}
	}
	if allReleased {
		s.l.Infof(ctx, "service.reservation.Release: order_code=%s already released, no-op", oCode)
		return nil
	}

	tcUps := make(map[int64]int)
	rIDs := make([]int64, 0, len(rs))
	for _, r := range rs {
		if r.Status == models.ReservationStatusConfirmed {
			s.l.Warnf(ctx, "service.reservation.Release: conflict, reservation %d already confirmed", r.ID)
			return ErrStateConflict
		}
		if r.Status != models.ReservationStatusActive {
			continue // already cancelled/expired; leave as-is
		}
		tcUps[r.TicketClassID] += r.Qty
		rIDs = append(rIDs, r.ID)
	}

	for tcID, qty := range tcUps {
		result := tx.Model(&models.TicketClass{}).
			Where("id = ? AND reserved >= ?", tcID, qty).
			Update("reserved", gorm.Expr("reserved - ?", qty))
		if result.Error != nil {
			s.l.Errorf(ctx, "service.reservation.Release.DecrementReserved: ticket_class_id=%d: %v", tcID, result.Error)
			return result.Error
		}
		if result.RowsAffected == 0 {
			s.l.Warnf(ctx, "service.reservation.Release: insufficient reserved for ticket_class_id=%d (needed=%d)", tcID, qty)
		}
	}

	if len(rIDs) > 0 {
		if err := tx.Model(&models.Reservation{}).
			Where("id IN ?", rIDs).
			Update("status", models.ReservationStatusCancelled).Error; err != nil {
			s.l.Errorf(ctx, "service.reservation.Release.UpdateReservations: %v", err)
			return err
		}
	}

	s.l.Infof(ctx, "service.reservation.Release: cancelled %d reservations for order_code=%s", len(rIDs), oCode)
	return nil
}
```

This removes the last caller of `sumQuantities`; leave the `sumQuantities` function for Task 6 to delete (removing it now would leave an unused-function build error only if nothing references it — Go allows unused methods, so it compiles either way).

- [ ] **Step 4: Run the tests to verify they pass**

Run:
```bash
cd services/inventory-svc && go build ./... && go test ./internal/services/ -run TestRelease -v
```
Expected: PASS (all four `TestRelease_*`).

- [ ] **Step 5: Commit**

```bash
git add services/inventory-svc/internal/services/reservation.go services/inventory-svc/internal/services/reservation_release_test.go
git commit -m "fix(inventory): idempotent Release with conflict on confirmed orders"
```

---

### Task 5: Expiry worker drain loop, boundary, and index

Loops the worker until backlog is drained, standardizes the `expires_at <= now` boundary, narrows the worker's dependency to a one-method interface (so it's unit-testable without a DB), and adds the active-expiry index at boot.

**Files:**
- Modify: `services/inventory-svc/internal/workers/reservation_exp_worker.go` (drain loop + narrow interface + configurable batch/interval)
- Modify: `services/inventory-svc/internal/services/reservation.go` (`BatchExpireReservations` boundary `<` → `<=`)
- Modify: `services/inventory-svc/cmd/api/main.go` (pass interval/batch; add `CREATE INDEX IF NOT EXISTS`)
- Modify: `services/inventory-svc/config/config.go` (worker config)
- Test: `services/inventory-svc/internal/workers/reservation_exp_worker_test.go`
- Test: `services/inventory-svc/internal/services/reservation_expire_test.go`

**Interfaces:**
- Consumes: `Reserve`, `BatchExpireReservations(ctx, batchSize) (int, error)`, `newTestDB`, `seedTicketClass`, `ticketClassByID`, `must`.
- Produces: `NewReservationExpiryWorker(l, expirer, interval, batchSize)` where `expirer` is `interface { BatchExpireReservations(context.Context, int) (int, error) }`; `runJob` drains until a batch returns `< batchSize`.

- [ ] **Step 1: Write the worker drain-loop unit test (no DB)**

Create `services/inventory-svc/internal/workers/reservation_exp_worker_test.go`:

```go
package workers

import (
	"context"
	"testing"

	pkgLog "github.com/vogiaan/ticketbottle-inventory/pkg/logger"
)

type stubExpirer struct {
	returns []int
	calls   int
}

func (s *stubExpirer) BatchExpireReservations(_ context.Context, _ int) (int, error) {
	n := s.returns[s.calls]
	s.calls++
	return n, nil
}

func TestRunJob_DrainsUntilBatchNotFull(t *testing.T) {
	stub := &stubExpirer{returns: []int{2, 2, 1}} // batchSize 2 → drained on the 1
	l := pkgLog.InitializeZapLogger(pkgLog.ZapConfig{Level: "error", Mode: "development", Encoding: "console"})
	w := NewReservationExpiryWorker(l, stub, 0, 2)

	w.runJob(context.Background())

	if stub.calls != 3 {
		t.Fatalf("BatchExpireReservations called %d times, want 3", stub.calls)
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run:
```bash
cd services/inventory-svc && go test ./internal/workers/ -run TestRunJob_DrainsUntilBatchNotFull -v
```
Expected: FAIL to compile — `NewReservationExpiryWorker` currently takes `(l, svc.ReservationService)`, not `(l, expirer, interval, batchSize)`.

- [ ] **Step 3: Rewrite the worker**

Replace `services/inventory-svc/internal/workers/reservation_exp_worker.go` with:

```go
package workers

import (
	"context"
	"time"

	pkgLog "github.com/vogiaan/ticketbottle-inventory/pkg/logger"
)

// reservationExpirer is the only capability the worker needs.
type reservationExpirer interface {
	BatchExpireReservations(ctx context.Context, batchSize int) (int, error)
}

const drainMaxIterations = 100

type ReservationExpiryWorker struct {
	l         pkgLog.Logger
	tkr       *time.Ticker
	interval  time.Duration
	batchSize int
	rSvc      reservationExpirer
	doneCh    chan struct{}
}

func NewReservationExpiryWorker(l pkgLog.Logger, rSvc reservationExpirer, interval time.Duration, batchSize int) *ReservationExpiryWorker {
	if interval <= 0 {
		interval = time.Minute
	}
	if batchSize <= 0 {
		batchSize = 500
	}
	return &ReservationExpiryWorker{
		l:         l,
		rSvc:      rSvc,
		interval:  interval,
		batchSize: batchSize,
		doneCh:    make(chan struct{}),
	}
}

func (w *ReservationExpiryWorker) Start(ctx context.Context) {
	w.tkr = time.NewTicker(w.interval)
	w.l.Infof(ctx, "Starting ReservationExpiryWorker: interval=%v, batchSize=%d", w.interval, w.batchSize)

	go w.runJob(ctx)
	go func() {
		for {
			select {
			case <-w.tkr.C:
				w.runJob(ctx)
			case <-w.doneCh:
				w.l.Info(ctx, "ReservationExpiryWorker stopped")
				return
			case <-ctx.Done():
				w.l.Info(ctx, "ReservationExpiryWorker context cancelled")
				return
			}
		}
	}()
}

func (w *ReservationExpiryWorker) Stop(ctx context.Context) {
	if w.tkr != nil {
		w.tkr.Stop()
	}
	close(w.doneCh)
	w.l.Info(ctx, "ReservationExpiryWorker shutdown initiated")
}

// runJob drains expired reservations in batches until a batch comes back
// smaller than batchSize (nothing left) or the safety cap is hit.
func (w *ReservationExpiryWorker) runJob(ctx context.Context) {
	start := time.Now()
	total := 0
	for i := 0; i < drainMaxIterations; i++ {
		n, err := w.rSvc.BatchExpireReservations(ctx, w.batchSize)
		if err != nil {
			w.l.Errorf(ctx, "ReservationExpiryWorker: batch expiration failed: %v", err)
			return
		}
		total += n
		if n < w.batchSize {
			break
		}
	}
	if total > 0 {
		w.l.Infof(ctx, "ReservationExpiryWorker: expired %d reservations in %v", total, time.Since(start))
	}
}
```

- [ ] **Step 4: Run the worker unit test to verify it passes**

Run:
```bash
cd services/inventory-svc && go test ./internal/workers/ -run TestRunJob_DrainsUntilBatchNotFull -v
```
Expected: PASS.

- [ ] **Step 5: Fix the boundary and write the service-level expiry test**

In `services/inventory-svc/internal/services/reservation.go`, in `BatchExpireReservations`, change the lock query predicate from `expires_at < ?` to `expires_at <= ?`:

```go
			Where("status = ? AND expires_at <= ?", models.ReservationStatusActive, now).
```

Create `services/inventory-svc/internal/services/reservation_expire_test.go`:

```go
package service

import (
	"context"
	"testing"
	"time"

	"github.com/vogiaan/ticketbottle-inventory/internal/models"
)

func TestBatchExpire_ReleasesExpiredHolds(t *testing.T) {
	svc, repo := reserveSvc(t)
	tc := seedTicketClass(t, repo, 100, 0, 0)
	// Reserve in the past so the hold is already expired.
	must(t, svc.Reserve(context.Background(), ReserveInput{
		OrderCode: "o-expire", ExpiresAt: time.Now().UTC().Add(-1 * time.Minute),
		Items: []ReserveItem{{TicketClassID: tc.ID, Qty: 7}},
	}))
	if got := ticketClassByID(t, repo, tc.ID); got.Reserved != 7 {
		t.Fatalf("precondition reserved=%d, want 7", got.Reserved)
	}

	n, err := svc.BatchExpireReservations(context.Background(), 500)
	if err != nil {
		t.Fatalf("BatchExpireReservations: %v", err)
	}
	if n != 1 {
		t.Fatalf("expired count = %d, want 1", n)
	}
	if got := ticketClassByID(t, repo, tc.ID); got.Reserved != 0 {
		t.Fatalf("reserved = %d, want 0 after expiry", got.Reserved)
	}

	var r models.Reservation
	repo.WithContext(context.Background()).Where("order_code = ?", "o-expire").First(&r)
	if r.Status != models.ReservationStatusExpired {
		t.Fatalf("status = %s, want EXPIRED", r.Status)
	}
}
```

- [ ] **Step 6: Update wiring (main.go + config) and add the boot index**

In `services/inventory-svc/config/config.go`, add a worker config struct + defaults. Add to `Config`:

```go
	Worker   WorkerConfig
```

Add the type and its loading (place the struct near the others, and set it inside `Load()`'s `cfg := &Config{...}` literal):

```go
type WorkerConfig struct {
	ExpiryInterval  time.Duration
	ExpiryBatchSize int
}
```

Inside the `cfg := &Config{...}` literal in `Load()`, add:

```go
		Worker: WorkerConfig{
			ExpiryInterval:  getEnvAsDuration("WORKER_EXPIRY_INTERVAL", time.Minute),
			ExpiryBatchSize: getEnvAsInt("WORKER_EXPIRY_BATCH_SIZE", 500),
		},
```

In `services/inventory-svc/cmd/api/main.go`, update the worker construction (was `workers.NewReservationExpiryWorker(l, rsvSvc)`):

```go
	rsvExpWkr := workers.NewReservationExpiryWorker(l, rsvSvc, cfg.Worker.ExpiryInterval, cfg.Worker.ExpiryBatchSize)
```

And, immediately after the `db.AutoMigrate(...)` block succeeds (before `repo := pkgGorm.NewRepository(db)`), add the interim partial index (Phase C will move this into a real migration):

```go
	if err := db.Exec(
		"CREATE INDEX IF NOT EXISTS idx_reservation_active_expiry ON reservation (status, expires_at) WHERE status = 'ACTIVE'",
	).Error; err != nil {
		l.Fatalf(ctx, "Failed to create active-expiry index: %v", err)
	}
```

Add the same index creation to the test harness so tests exercise the same schema — in `services/inventory-svc/internal/services/setup_test.go`, inside `newTestDB` after `AutoMigrate`:

```go
	db.Exec("CREATE INDEX IF NOT EXISTS idx_reservation_active_expiry ON reservation (status, expires_at) WHERE status = 'ACTIVE'")
```

- [ ] **Step 7: Run the full service + worker suites**

Run:
```bash
cd services/inventory-svc && go build ./... && go test ./internal/... -race -count=1 -v
```
Expected: PASS (all Reserve/Confirm/Release/Expire/worker tests), no race warnings.

- [ ] **Step 8: Commit**

```bash
git add services/inventory-svc/internal/workers/ services/inventory-svc/internal/services/reservation.go services/inventory-svc/internal/services/reservation_expire_test.go services/inventory-svc/internal/services/setup_test.go services/inventory-svc/cmd/api/main.go services/inventory-svc/config/config.go
git commit -m "feat(inventory): expiry-worker drain loop, <= boundary, active-expiry index"
```

---

### Task 6: Dead-code sweep

Removes the unused methods this refactor exposed and other verified-dead code. This task's test cycle is `go build`, `go vet`, and the full test suite staying green.

**Files:**
- Modify: `services/inventory-svc/internal/services/reservation.go` (trim `ReservationService` interface + delete unused impl methods)
- Modify: `services/inventory-svc/internal/services/ticketclass.go` (trim `TicketClassService` interface + delete unused impl methods)
- Modify: `services/inventory-svc/internal/services/reservation_utils.go` (delete `sumQuantities` if now unused)
- Modify: `services/inventory-svc/pkg/gorm/repository.go` (remove unused helpers)
- Modify: `services/inventory-svc/config/config.go` (remove `getEnvAsSlice`, `getEnvAsBool`)

**Interfaces:**
- Consumes: nothing new.
- Produces: slimmer `ReservationService` = `{ Reserve, Confirm, Release, UpdateStatus?, BatchExpireReservations }` (see step 1 for the exact surviving set); slimmer `TicketClassService`.

- [ ] **Step 1: Verify each candidate is dead, then remove**

Run the caller check first (must print nothing but definitions):
```bash
cd services/inventory-svc && grep -rnE '\.(UpdateStatus|UpdateStatusByOrderCode|Delete|DeleteByOrderCode|GetByOrderCode|GetActiveByTicketClassID|GetExpired|GetTotalReservedQuantity|GetByEventID|IncrementReserved|DecrementReserved|IncrementSold)\(' internal cmd --include='*.go' | grep -vE 'func |_test.go'
```
Expected: no output (no non-definition callers). If a line appears, keep that method.

Then, in `services/inventory-svc/internal/services/reservation.go`:
- From the `ReservationService` interface remove: `UpdateStatus`, `UpdateStatusByOrderCode`, `Delete`, `DeleteByOrderCode`. Keep `Reserve`, `Confirm`, `Release`, `BatchExpireReservations`.
- Delete the method bodies: `GetByOrderCode`, `GetActiveByTicketClassID`, `GetExpired`, `UpdateStatus`, `UpdateStatusByOrderCode`, `Delete`, `GetTotalReservedQuantity`. (`Create` and `DeleteByOrderCode` were already removed in Task 2.)

In `services/inventory-svc/internal/services/ticketclass.go`:
- From the `TicketClassService` interface remove: `GetByEventID`, `IncrementReserved`, `DecrementReserved`, `IncrementSold`. Keep `Create`, `Update`, `GetByID`, `GetMany`, `Delete`, `GetAvailableCount`, `CheckAvailability`.
- Delete the method bodies: `GetByEventID`, `IncrementReserved`, `DecrementReserved`, `IncrementSold`.

In `services/inventory-svc/internal/services/reservation_utils.go`: if `grep -rn sumQuantities internal --include='*.go' | grep -v '_test.go'` shows only the definition, delete the file.

In `services/inventory-svc/pkg/gorm/repository.go`: run
```bash
grep -rnE 'repo\.(HardDelete|FindAll|FindWhere|Count|Exists)\(|\.(HardDelete|FindAll|Count|Exists)\(' internal cmd --include='*.go' | grep -v '_test.go'
```
Remove every helper with no caller. `FindWhere` is used by `ticketclass.go` (`GetByEventID`) only — once `GetByEventID` is deleted it becomes dead, so remove `FindWhere` too. Keep `GetDB`, `WithContext`, `Create`, `FindByID`, `Update`, `Delete` (all referenced). Remove `Exists` (unsafe: panics on empty `conditions`), and any of `HardDelete`, `FindAll`, `Count` with no caller.

In `services/inventory-svc/config/config.go`: delete `getEnvAsSlice` and `getEnvAsBool` (unused), and the now-unused `"strings"` import if `getEnvAsSlice` was its only user.

- [ ] **Step 2: Build, vet, and run the full suite**

Run:
```bash
cd services/inventory-svc && go build ./... && go vet ./... && go test ./internal/... -race -count=1
```
Expected: builds clean, `go vet` clean, all tests PASS.

- [ ] **Step 3: Commit**

```bash
git add services/inventory-svc/internal/services/ services/inventory-svc/pkg/gorm/repository.go services/inventory-svc/config/config.go
git commit -m "refactor(inventory): remove dead methods and unsafe generic repo helpers"
```

---

### Task 7: Documentation + final verification

Updates the service CLAUDE.md to match the new behavior and invariants, and runs a final end-to-end verification.

**Files:**
- Modify: `services/inventory-svc/CLAUDE.md`

**Interfaces:** none.

- [ ] **Step 1: Update the service CLAUDE.md**

In `services/inventory-svc/CLAUDE.md`:
- In "Three-step reservation flow", replace the sentence "`Reserve` fans out per item over goroutines, so each item locks independently." with: "`Reserve` runs as a **single transaction**: it locks all target `ticket_class` rows in **ascending id order** (deadlock-free), validates availability, increments `reserved`, and batch-inserts the reservation rows — all-or-nothing."
- Add a new "Idempotency" subsection under Conventions: "`Reserve`/`Confirm`/`Release` key off `order_code`. An operation that finds the order already in its target state (or nothing to do) returns success; only genuine state conflicts return `ErrStateConflict` (gRPC `FailedPrecondition`). This keeps the Temporal saga's activity retries safe."
- Add to Conventions: "When locking multiple `ticket_class` rows, always lock in ascending id order."
- Update the boot note: mention the interim `idx_reservation_active_expiry` partial index created at boot alongside `AutoMigrate` (to be replaced by versioned migrations in a follow-up).

- [ ] **Step 2: Final verification — build, vet, full race suite**

Run:
```bash
cd services/inventory-svc && go build ./... && go vet ./... && make test
```
Expected: build + vet clean; `make test` runs `go test ./internal/... -race -count=1` → all PASS.

- [ ] **Step 3: Commit**

```bash
git add services/inventory-svc/CLAUDE.md
git commit -m "docs(inventory): document atomic Reserve, idempotency, and lock-order invariant"
```

---

## Deferred to a follow-up plan (spec Phase C / S4)

Not implemented here; write a separate plan when ready:
- Replace boot-time `AutoMigrate` with versioned **goose** migrations (`migrations/`, `cmd/migrate/`, `Dockerfile.migrate`).
- Move the `idx_reservation_active_expiry` index (added at boot in Task 5) into `00002_active_expiry_index.sql`; reproduce the current schema in `00001_init.sql` and verify it via `pg_dump --schema-only` diff against an `AutoMigrate` database.
- Add an `inventory-migrate` Helm Job mirroring `deploy/helm/ticketbottle/templates/apps/migrations.yaml`, plus a `make migrate` target and `build-migrate-images.sh` entry.

## Coordination note (cross-service)

Task 3 introduces a new gRPC status code (`FailedPrecondition`) for `Confirm`/`Release` conflicts, where the Order saga previously saw `ResourceExhausted`/`Internal`. Before shipping, confirm the Order service's Temporal activities treat `FailedPrecondition` as **non-retryable** (idempotent conflicts shouldn't trigger endless retries or wrongful compensation). This is a read-only check of `services/order-svc`; adjust there if it keys on the old codes.
