# Inventory Service Correctness Fixes Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Close every P0 and P1 correctness defect in `services/inventory-svc` — the oversell vectors in the ticket-class update path, the payment-captured-but-hold-expired race, status-blind idempotency, silent inventory drift, and unenforced sale windows — so the service's stated no-oversell invariant is enforced by code and by the database, not by convention in one file.

**Architecture:** All quantity mutations move behind either a `SELECT ... FOR UPDATE` transaction or a guarded conditional `UPDATE`, and two Postgres `CHECK` constraints become the final backstop so any future violation fails loudly instead of overselling. Domain errors replace GORM sentinels at the service boundary. The confirm path gains the ability to accept a hold that is past its expiry (and to re-acquire stock for one the worker already released), while order-svc extends the hold to strictly outlive the payment window — together closing the window where money is captured but no seat exists.

**Tech Stack:** Go 1.25, GORM 1.31 (`gorm.io/driver/postgres`), gRPC 1.76, zap (custom wrapper in `pkg/logger`), PostgreSQL 15, Temporal Go SDK (order-svc only).

## Global Constraints

- **No new Go module dependencies.** `services/inventory-svc/vendor/` and `services/order-svc/vendor/` are committed; Go auto-selects `-mod=vendor`. Adding a dependency would require `go mod vendor` and a large vendor diff. Every fix in this plan uses only what is already in `go.mod`.
- **Go version:** 1.25 (both services). Generics are available and used.
- **Logging:** the zap wrapper in `pkg/logger`, ctx-first `f`-suffixed methods only — `s.l.Errorf(ctx, "...", err)`. Message prefix is `package.type.Method`, e.g. `service.reservation.Reserve:`. Never `fmt.Errorf` for logging, never `log.Printf`.
- **The invariant:** never read-modify-write `reserved`, `sold`, or `total` outside a `FOR UPDATE` transaction or a guarded conditional `UPDATE`.
- **Lock ordering:** when a single transaction updates multiple `ticket_class` rows, always touch them in ascending `id` order. Deadlock-freedom depends on this.
- **Postgres for tests:** `postgresql://root:root@localhost:5435/ticketbottle_inventory_test?sslmode=disable`, overridable with `TEST_POSTGRES_URL`. Started via `services/inventory-svc/docker-compose.dev.yml`.
- **Schema changes** go into `internal/models/ddl.go` as idempotent statements applied after `AutoMigrate`. Versioned migrations are a separate, later plan — do not introduce a migration tool here.
- **Branch:** all work lands on `fix/inventory-correctness`, branched from the current `docs/aws-mac-offload-plan`.
- **Existing test files are load-bearing.** `internal/services/*_test.go` currently pass. Only Task 8 is permitted to delete an existing test, and only the one named there.

---

## File Structure

**`services/inventory-svc` — created:**

| Path | Responsibility |
|---|---|
| `internal/models/ddl.go` | The single source of post-`AutoMigrate` DDL (partial index + CHECK constraints), shared by `main.go` and the test harness. |
| `internal/interceptors/grpc_recovery.go` | Unary interceptor turning handler panics into `codes.Internal` instead of a process crash. |
| `internal/interceptors/grpc_recovery_test.go` | Unit test for the above. |
| `internal/services/ticketclass_update_test.go` | Tests for the rewritten `Update` — partial semantics, counter preservation under concurrency, capacity guard. |
| `internal/services/ticketclass_availability_test.go` | Tests for `CheckAvailability` duplicate-id aggregation. |
| `internal/services/reservation_sale_window_test.go` | Tests for ticket-class status and sale-window enforcement in `Reserve`. |
| `internal/services/errors_test.go` | Asserts the service layer returns domain errors, not GORM sentinels. |

**`services/inventory-svc` — modified:**

| Path | Change |
|---|---|
| `internal/services/errors.go` | Add `ErrInsufficientStock`, `ErrNotFound`, `ErrInventoryDrift`, `ErrSaleClosed`. |
| `internal/services/reservation.go` | Status-scoped idempotency; sale-window checks; confirm accepts past-expiry and re-acquires; drift-tolerant batch expiry; domain errors; generic `sortedInt64Keys`. |
| `internal/services/ticketclass.go` | `Update` rewritten to a locked, column-targeted write; `CheckAvailability` aggregates duplicates; domain errors; double-logging removed. |
| `internal/services/ticketclass_types.go` | `UpdateTicketClassInput` fully pointerized. |
| `internal/services/ticketclass_builder.go` | `buildUpdate` → `updateColumns` returning the exact column set. |
| `internal/services/setup_test.go` | Apply shared DDL; fail (not skip) when `CI` is set. |
| `internal/delivery/grpc/presenter.go` | `newUpdateTicketClassInput` only populates fields the request actually carries. |
| `internal/delivery/grpc/errors.go` | `mapError` switches on domain errors. |
| `pkg/errors/errors.go` | Add `ErrSaleClosed`. |
| `cmd/api/main.go` | Apply `models.PostMigrateStatements()`; chain the recovery interceptor. |
| `Makefile` | Fix the broken `test-db` target. |

**`services/order-svc` — modified:**

| Path | Change |
|---|---|
| `internal/workflows/shared.go` | Add `ReservationHoldGrace` and the `reservationExpiry` helper. |
| `internal/workflows/create_order.go` | Use `reservationExpiry` so the hold outlives the payment window. |
| `internal/workflows/shared_test.go` | **Created** — regression guard that the hold strictly outlives `PaymentTimeout`. |

---

## Task 1: Test harness — make the suite actually run

Right now `make test-db` targets a container name and database that do not exist (`ticketbottle-postgres-inventory` / `-d ticketbottle_inventory`; the compose file declares `ticketbottle-inventory` / `ticketbottle`), it swallows the failure with `|| true`, and `setup_test.go` then calls `t.Skipf`. The result is a suite that reports PASS having asserted nothing. Nothing else in this plan is trustworthy until this is fixed.

**Files:**
- Create: `services/inventory-svc/internal/models/ddl.go`
- Modify: `services/inventory-svc/Makefile`
- Modify: `services/inventory-svc/internal/services/setup_test.go:19-46`
- Modify: `services/inventory-svc/cmd/api/main.go:45-57`

**Interfaces:**
- Consumes: nothing.
- Produces: `models.PostMigrateStatements() []string` — idempotent DDL applied after `AutoMigrate`, used by `cmd/api/main.go` and by `newTestDB` in the test harness. Tasks 2 and 7 append to it.

- [ ] **Step 1: Create the branch and start Postgres**

```bash
cd /Users/vogiaan/coding/projects/TicketEventPF
git checkout -b fix/inventory-correctness
cd services/inventory-svc
docker compose -f docker-compose.dev.yml up -d
```

Wait for readiness, then confirm:

```bash
until docker exec ticketbottle-inventory pg_isready -U root >/dev/null 2>&1; do sleep 1; done
docker exec ticketbottle-inventory psql -U root -d ticketbottle -c "SELECT 1"
```

Expected: a `?column? / 1` result table.

- [ ] **Step 2: Fix the `test-db` target**

Replace the `test-db` and `test` targets in `services/inventory-svc/Makefile` with:

```makefile
test-db:
	@docker exec ticketbottle-inventory psql -U root -d ticketbottle \
		-c "CREATE DATABASE ticketbottle_inventory_test" 2>/dev/null || true
	@docker exec ticketbottle-inventory psql -U root -d ticketbottle_inventory_test \
		-c "SELECT 1" >/dev/null

test: test-db
	go test ./internal/... -race -count=1
```

The second `psql` has no `|| true`, so a database that failed to materialise now fails the target instead of silently skipping the suite.

- [ ] **Step 3: Run it to verify the test database is created**

```bash
cd /Users/vogiaan/coding/projects/TicketEventPF/services/inventory-svc && make test-db
```

Expected: exits 0, no output. Verify:

```bash
docker exec ticketbottle-inventory psql -U root -lqt | grep ticketbottle_inventory_test
```

Expected: a line containing `ticketbottle_inventory_test`.

- [ ] **Step 4: Create the shared DDL file**

Create `services/inventory-svc/internal/models/ddl.go`:

```go
package models

// PostMigrateStatements are the DDL statements applied immediately after
// AutoMigrate, in order. Every statement must be idempotent: both the service
// boot path and the test harness run them unconditionally on every start.
//
// This is the single source of truth for schema that AutoMigrate cannot
// express (partial indexes, CHECK constraints). Versioned migrations will
// eventually replace both this and AutoMigrate.
func PostMigrateStatements() []string {
	return []string{
		`CREATE INDEX IF NOT EXISTS idx_reservation_active_expiry
		   ON reservation (status, expires_at) WHERE status = 'ACTIVE'`,
	}
}
```

- [ ] **Step 5: Apply the shared DDL from `main.go`**

In `services/inventory-svc/cmd/api/main.go`, replace the single `db.Exec(...)` index block (currently lines 53-57) with:

```go
	for _, stmt := range models.PostMigrateStatements() {
		if err := db.Exec(stmt).Error; err != nil {
			l.Fatalf(ctx, "Failed to apply post-migrate statement: %v", err)
		}
	}
	l.Info(ctx, "Post-migrate statements applied successfully")
```

`models` is already imported at `cmd/api/main.go:15`.

- [ ] **Step 6: Apply the shared DDL from the test harness and fail-fast in CI**

In `services/inventory-svc/internal/services/setup_test.go`, replace the body of `newTestDB` from the `dsn` lookup through the `db.Exec(...)` index line with:

```go
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
		// A missing database must never let the suite report success in CI --
		// these tests are the only thing standing between a refactor and an
		// oversell. Locally, skipping keeps `go test ./...` usable.
		if os.Getenv("CI") != "" {
			t.Fatalf("CI requires a reachable test postgres (%s): %v", dsn, err)
		}
		t.Skipf("skipping: cannot reach test postgres (%s): %v", dsn, err)
	}
	if err := db.AutoMigrate(&models.TicketClass{}, &models.Reservation{}); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	for _, stmt := range models.PostMigrateStatements() {
		if err := db.Exec(stmt).Error; err != nil {
			t.Fatalf("post-migrate statement %q: %v", stmt, err)
		}
	}
```

The `t.Cleanup` block and the `return pkgGorm.NewRepository(db)` line below it stay exactly as they are.

- [ ] **Step 7: Run the full suite — it must now execute, not skip**

```bash
cd /Users/vogiaan/coding/projects/TicketEventPF/services/inventory-svc && make test 2>&1 | tail -20
```

Expected: `ok github.com/vogiaan/ticketbottle-inventory/internal/services` with a non-trivial duration, and `ok .../internal/workers`. **There must be no `[no tests to run]` or `SKIP` lines.** If you see `--- SKIP: TestHarness_Connects`, the database is not reachable — stop and fix that before continuing.

- [ ] **Step 8: Verify the build still passes**

```bash
cd /Users/vogiaan/coding/projects/TicketEventPF/services/inventory-svc && go build ./... && go vet ./internal/... ./pkg/... ./config/...
```

Expected: no output, exit 0.

- [ ] **Step 9: Commit**

```bash
cd /Users/vogiaan/coding/projects/TicketEventPF
git add services/inventory-svc/Makefile \
        services/inventory-svc/internal/models/ddl.go \
        services/inventory-svc/internal/services/setup_test.go \
        services/inventory-svc/cmd/api/main.go
git commit -m "test(inventory): repair the test-db target and centralise post-migrate DDL

make test-db pointed at a container and database that never existed and
swallowed the failure, so the suite skipped itself and reported PASS with
zero assertions run. Fix the target, fail instead of skip under CI, and
move the partial-index DDL into models.PostMigrateStatements() so boot and
the test harness apply exactly the same schema."
```

---

## Task 2: Database CHECK constraints (finding #3)

The last line of defense against oversell. Added `NOT VALID` so they enforce every future write immediately without scanning existing rows — a boot-time full-table validation could brick a deploy on pre-existing drift. Validating historical rows is a deliberate, separate operational step.

**Files:**
- Modify: `services/inventory-svc/internal/models/ddl.go`
- Test: `services/inventory-svc/internal/services/errors_test.go` (created here, extended in Task 3)

**Interfaces:**
- Consumes: `models.PostMigrateStatements()` from Task 1.
- Produces: constraints `chk_ticket_class_capacity`, `chk_ticket_class_total_nonneg`, `chk_reservation_qty_positive`. Task 8's re-acquire guard and Task 4's capacity guard both rely on `chk_ticket_class_capacity` as their backstop.

- [ ] **Step 1: Write the failing test**

Create `services/inventory-svc/internal/services/errors_test.go`:

```go
package service

import (
	"context"
	"testing"

	"github.com/vogiaan/ticketbottle-inventory/internal/models"
)

// The CHECK constraints are the backstop for every quantity bug in this
// service: if application logic ever lets reserved + sold exceed total, the
// database must refuse the write rather than oversell the event.
func TestCapacityConstraint_RejectsOversell(t *testing.T) {
	repo := newTestDB(t)
	tc := seedTicketClass(t, repo, 10, 0, 0)

	err := repo.WithContext(context.Background()).
		Model(&models.TicketClass{}).
		Where("id = ?", tc.ID).
		Update("reserved", 11).Error
	if err == nil {
		t.Fatal("expected the capacity CHECK constraint to reject reserved=11 on total=10")
	}
}

func TestCapacityConstraint_RejectsNegativeReserved(t *testing.T) {
	repo := newTestDB(t)
	tc := seedTicketClass(t, repo, 10, 0, 0)

	err := repo.WithContext(context.Background()).
		Model(&models.TicketClass{}).
		Where("id = ?", tc.ID).
		Update("reserved", -1).Error
	if err == nil {
		t.Fatal("expected the capacity CHECK constraint to reject reserved=-1")
	}
}

func TestReservationQtyConstraint_RejectsZero(t *testing.T) {
	repo := newTestDB(t)
	tc := seedTicketClass(t, repo, 10, 0, 0)

	r := models.Reservation{
		OrderCode:     "o-qty-zero",
		TicketClassID: tc.ID,
		Qty:           0,
		Status:        models.ReservationStatusActive,
		ExpiresAt:     future(),
	}
	if err := repo.Create(context.Background(), &r); err == nil {
		t.Fatal("expected the qty CHECK constraint to reject qty=0")
	}
}
```

- [ ] **Step 2: Run to verify it fails**

```bash
cd /Users/vogiaan/coding/projects/TicketEventPF/services/inventory-svc && go test ./internal/services/ -run 'Constraint' -count=1 -v 2>&1 | tail -20
```

Expected: all three FAIL with messages like `expected the capacity CHECK constraint to reject reserved=11 on total=10` — the writes currently succeed.

- [ ] **Step 3: Add the constraints**

Replace the returned slice in `services/inventory-svc/internal/models/ddl.go` with:

```go
	return []string{
		`CREATE INDEX IF NOT EXISTS idx_reservation_active_expiry
		   ON reservation (status, expires_at) WHERE status = 'ACTIVE'`,

		// The oversell backstop. NOT VALID means the constraint enforces every
		// new write immediately but does not scan pre-existing rows -- a boot
		// that fails on historical drift would take the service down rather
		// than surface the drift. Validate deliberately, out of band:
		//   ALTER TABLE ticket_class VALIDATE CONSTRAINT chk_ticket_class_capacity;
		`DO $$ BEGIN
		   ALTER TABLE ticket_class ADD CONSTRAINT chk_ticket_class_capacity
		     CHECK (reserved >= 0 AND sold >= 0 AND reserved + sold <= total) NOT VALID;
		 EXCEPTION WHEN duplicate_object THEN NULL; END $$`,

		`DO $$ BEGIN
		   ALTER TABLE ticket_class ADD CONSTRAINT chk_ticket_class_total_nonneg
		     CHECK (total >= 0) NOT VALID;
		 EXCEPTION WHEN duplicate_object THEN NULL; END $$`,

		`DO $$ BEGIN
		   ALTER TABLE reservation ADD CONSTRAINT chk_reservation_qty_positive
		     CHECK (qty > 0) NOT VALID;
		 EXCEPTION WHEN duplicate_object THEN NULL; END $$`,
	}
```

- [ ] **Step 4: Run to verify the tests pass**

```bash
cd /Users/vogiaan/coding/projects/TicketEventPF/services/inventory-svc && go test ./internal/services/ -run 'Constraint' -count=1 -v 2>&1 | tail -20
```

Expected: three PASS lines.

- [ ] **Step 5: Run the whole suite to confirm nothing regressed**

```bash
cd /Users/vogiaan/coding/projects/TicketEventPF/services/inventory-svc && make test 2>&1 | tail -20
```

Expected: `ok` for both packages, no FAIL. In particular `TestRelease_GuardFailure_ErrorsAndRollsBack` (which deliberately seeds a reservation whose qty exceeds `reserved`) must still pass — it never writes an invalid `ticket_class` row, so the constraints do not touch it.

- [ ] **Step 6: Commit**

```bash
cd /Users/vogiaan/coding/projects/TicketEventPF
git add services/inventory-svc/internal/models/ddl.go \
        services/inventory-svc/internal/services/errors_test.go
git commit -m "feat(inventory): add CHECK constraints as the oversell backstop

reserved + sold <= total, non-negative counters, and qty > 0 are now
enforced by Postgres. Added NOT VALID so they guard every new write without
scanning historical rows at boot; validate out of band once drift is known
to be clean."
```

---

## Task 3: Domain errors replace GORM sentinels (finding #10)

`reservation.go` currently returns `gorm.ErrInvalidData` to mean "insufficient stock" and `gorm.ErrRecordNotFound` to mean "unknown ticket class", and `mapError` translates them back. That is a persistence-layer error carrying business meaning across the service boundary: any GORM internal that happens to return `ErrInvalidData` becomes a `ResourceExhausted` to the caller. This task must land before Tasks 4, 7, and 8, which all introduce new error paths.

**Files:**
- Modify: `services/inventory-svc/internal/services/errors.go`
- Modify: `services/inventory-svc/internal/services/reservation.go:61,72,89,154,197,269`
- Modify: `services/inventory-svc/internal/services/ticketclass.go:44-131`
- Modify: `services/inventory-svc/internal/delivery/grpc/errors.go:16-27`
- Test: `services/inventory-svc/internal/services/errors_test.go`

**Interfaces:**
- Consumes: nothing from earlier tasks.
- Produces: `service.ErrInsufficientStock`, `service.ErrNotFound`, `service.ErrInventoryDrift` (all `error`, alongside the existing `service.ErrStateConflict`). Task 4 returns `ErrNotFound` and `ErrStateConflict`; Task 8 returns `ErrInventoryDrift` and `ErrStateConflict`; Task 10 returns `ErrInventoryDrift`.

- [ ] **Step 1: Write the failing test**

Append to `services/inventory-svc/internal/services/errors_test.go`:

```go
func TestReserve_InsufficientStock_ReturnsDomainError(t *testing.T) {
	svc, repo := reserveSvc(t)
	tc := seedTicketClass(t, repo, 2, 0, 0)

	err := svc.Reserve(context.Background(), ReserveInput{
		OrderCode: "o-domain-stock", ExpiresAt: future(),
		Items: []ReserveItem{{TicketClassID: tc.ID, Qty: 5}},
	})
	if !errors.Is(err, ErrInsufficientStock) {
		t.Fatalf("Reserve over capacity = %v, want ErrInsufficientStock", err)
	}
}

func TestReserve_UnknownTicketClass_ReturnsDomainError(t *testing.T) {
	svc, _ := reserveSvc(t)

	err := svc.Reserve(context.Background(), ReserveInput{
		OrderCode: "o-domain-missing", ExpiresAt: future(),
		Items: []ReserveItem{{TicketClassID: 999999999, Qty: 1}},
	})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("Reserve on unknown ticket class = %v, want ErrNotFound", err)
	}
}

func TestConfirm_UnknownOrder_ReturnsDomainError(t *testing.T) {
	svc, _ := reserveSvc(t)

	if err := svc.Confirm(context.Background(), "o-domain-ghost"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Confirm on unknown order = %v, want ErrNotFound", err)
	}
}

func TestGetByID_Unknown_ReturnsDomainError(t *testing.T) {
	repo := newTestDB(t)
	tcSvc := NewTicketClassService(newTestLogger(), repo)

	_, err := tcSvc.GetByID(context.Background(), 999999999)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetByID on unknown id = %v, want ErrNotFound", err)
	}
}
```

Add `"errors"` to that file's import block.

- [ ] **Step 2: Run to verify it fails**

```bash
cd /Users/vogiaan/coding/projects/TicketEventPF/services/inventory-svc && go test ./internal/services/ -run 'DomainError' -count=1 2>&1 | tail -20
```

Expected: FAIL to compile with `undefined: ErrInsufficientStock` and `undefined: ErrNotFound`.

- [ ] **Step 3: Define the domain errors**

Replace `services/inventory-svc/internal/services/errors.go` entirely:

```go
package service

import "errors"

var (
	// ErrStateConflict signals a reservation is in a state that forbids the
	// requested transition (e.g. releasing a confirmed hold). Distinct from
	// insufficient-stock and not-found.
	ErrStateConflict = errors.New("reservation state conflict")

	// ErrInsufficientStock signals the requested quantity exceeds what is
	// available (total - reserved - sold) on at least one ticket class.
	ErrInsufficientStock = errors.New("insufficient stock")

	// ErrNotFound signals a referenced ticket class or order code does not
	// exist. Callers map this to gRPC NotFound.
	ErrNotFound = errors.New("resource not found")

	// ErrInventoryDrift signals the counters disagree with the reservation
	// rows -- a reserved counter lower than the holds that claim it. This is
	// corruption, not a user error: it means something wrote a quantity
	// outside a locked transaction. Always alarm-worthy.
	ErrInventoryDrift = errors.New("inventory counter drift detected")
)
```

- [ ] **Step 4: Return domain errors from `reservation.go`**

Six replacements in `services/inventory-svc/internal/services/reservation.go`. The surrounding log lines stay as they are:

| Line | Was | Now |
|---|---|---|
| 61 | `return gorm.ErrRecordNotFound` | `return ErrNotFound` |
| 72 | `return gorm.ErrInvalidData` | `return ErrInsufficientStock` |
| 89 | `return gorm.ErrInvalidData` | `return ErrInsufficientStock` |
| 154 | `return gorm.ErrRecordNotFound` | `return ErrNotFound` |
| 197 | `return gorm.ErrInvalidData` | `return ErrInventoryDrift` |
| 269 | `return gorm.ErrInvalidData` | `return ErrInventoryDrift` |

Lines 197 and 269 fire when `reserved` is lower than the holds claiming it — that is drift, not a shortage, and it must be distinguishable in logs and metrics.

- [ ] **Step 5: Return domain errors from `ticketclass.go` and stop double-logging**

Four methods in `services/inventory-svc/internal/services/ticketclass.go` currently do `if err == gorm.ErrRecordNotFound { Warnf }` and then **fall through** to `Errorf` the same error — every not-found logs twice, and they use `==` rather than `errors.Is`. Replace each of the four not-found blocks with this shape.

In `Update` (currently lines 45-52), `GetByID` (63-71), and `GetAvailableCount` (120-128), the `FindByID` error block becomes:

```go
		if errors.Is(err, gorm.ErrRecordNotFound) {
			s.l.Warnf(ctx, "service.ticketclass.<Method>: ticket class %d not found", id)
			return models.TicketClass{}, ErrNotFound
		}
		s.l.Errorf(ctx, "service.ticketclass.<Method>: %v", err)
		return models.TicketClass{}, err
```

substituting the real method name for `<Method>`, and `return 0, ErrNotFound` / `return 0, err` in `GetAvailableCount`.

In `Delete` (101-110), not-found stays an idempotent success — only the double-log goes:

```go
		if errors.Is(err, gorm.ErrRecordNotFound) {
			s.l.Warnf(ctx, "service.ticketclass.Delete: ticket class %d not found, no-op", id)
			return nil
		}
		s.l.Errorf(ctx, "service.ticketclass.Delete: %v", err)
		return err
```

Add `"errors"` to the file's import block.

> Note: `Update`'s body is rewritten wholesale in Task 4. Making it consistent here keeps this task's diff self-contained and reviewable.

- [ ] **Step 6: Map domain errors at the gRPC boundary**

Replace `mapError` in `services/inventory-svc/internal/delivery/grpc/errors.go`:

```go
func (s *grpcService) mapError(err error) error {
	switch {
	case errors.Is(err, svc.ErrNotFound), errors.Is(err, gorm.ErrRecordNotFound):
		return pkgErrors.ErrNotFound
	case errors.Is(err, svc.ErrInsufficientStock):
		return pkgErrors.ErrInsufficientStock
	case errors.Is(err, svc.ErrStateConflict):
		return pkgErrors.ErrConflict
	case errors.Is(err, svc.ErrInventoryDrift):
		return pkgErrors.ErrInternal
	default:
		return pkgErrors.ErrInternal
	}
}
```

`gorm.ErrRecordNotFound` stays as a fallback because `pkg/gorm/repository.go` still surfaces it raw from paths this plan does not touch.

- [ ] **Step 7: Run the tests**

```bash
cd /Users/vogiaan/coding/projects/TicketEventPF/services/inventory-svc && make test 2>&1 | tail -20
```

Expected: `ok` for both packages. The four new `DomainError` tests pass, and every pre-existing test still passes — they assert on `ErrStateConflict` or on `err != nil`, neither of which changes here.

- [ ] **Step 8: Verify build and vet**

```bash
cd /Users/vogiaan/coding/projects/TicketEventPF/services/inventory-svc && go build ./... && go vet ./internal/... ./pkg/... ./config/...
```

Expected: no output, exit 0. If `vet` reports `gorm` imported and not used in `reservation.go`, keep the import — `gorm.Expr` and `gorm.DB` are still used throughout.

- [ ] **Step 9: Commit**

```bash
cd /Users/vogiaan/coding/projects/TicketEventPF
git add services/inventory-svc/internal/services/errors.go \
        services/inventory-svc/internal/services/errors_test.go \
        services/inventory-svc/internal/services/reservation.go \
        services/inventory-svc/internal/services/ticketclass.go \
        services/inventory-svc/internal/delivery/grpc/errors.go
git commit -m "refactor(inventory): return domain errors instead of GORM sentinels

gorm.ErrInvalidData meant 'insufficient stock' and gorm.ErrRecordNotFound
meant 'unknown ticket class', so any GORM internal returning those codes
was mistranslated at the gRPC boundary. Introduces ErrInsufficientStock,
ErrNotFound and ErrInventoryDrift, and drops the duplicate not-found
logging in ticketclass.go."
```

---

## Task 4: `UpdateTicketClass` — stop clobbering the counters (findings #1, #2)

**This is the P0.** `Update` does `FindByID` → mutate → `repo.Update`, and `repo.Update` is GORM `Save(model)`, which writes *every* column including `reserved` and `sold`, with no lock and no transaction. A concurrent burst of reservations between the read and the write is silently erased, and those orders' `ACTIVE` reservation rows then let the same seats be sold twice.

Compounding it, `validateUpdateTicketClassRequest` only checks that `id` is non-empty while `buildUpdate` assigns `Total`, `Currency` and both sale windows unconditionally — so a rename, `{id: "1", name: "VIP"}`, sets `total = 0` and `currency = ""`.

**Files:**
- Modify: `services/inventory-svc/internal/services/ticketclass_types.go:16-24`
- Modify: `services/inventory-svc/internal/services/ticketclass_builder.go:7-20`
- Modify: `services/inventory-svc/internal/services/ticketclass.go:44-61`
- Modify: `services/inventory-svc/internal/delivery/grpc/presenter.go:108-127`
- Test: `services/inventory-svc/internal/services/ticketclass_update_test.go`

**Interfaces:**
- Consumes: `service.ErrNotFound`, `service.ErrStateConflict` (Task 3); `chk_ticket_class_capacity` (Task 2).
- Produces: `UpdateTicketClassInput` with every field a pointer — `Name *string`, `PriceCents *int64`, `Currency *string`, `Total *int`, `SaleStartAt *time.Time`, `SaleEndAt *time.Time`, `Status *string`. `nil` means "leave unchanged". `updateColumns(in UpdateTicketClassInput) map[string]any` on `implTicketClassService` replaces `buildUpdate`.

- [ ] **Step 1: Write the failing tests**

Create `services/inventory-svc/internal/services/ticketclass_update_test.go`:

```go
package service

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	pkgGorm "github.com/vogiaan/ticketbottle-inventory/pkg/gorm"
)

func updateSvc(t *testing.T) (TicketClassService, ReservationService, *pkgGorm.Repository) {
	t.Helper()
	repo := newTestDB(t)
	return NewTicketClassService(newTestLogger(), repo),
		NewReservationService(newTestLogger(), repo),
		repo
}

// A rename must not touch total, currency, or the sale window. The old
// buildUpdate assigned every field unconditionally, so {id, name} wiped
// total to 0 and currency to "".
func TestUpdate_PartialUpdate_PreservesUnsetFields(t *testing.T) {
	tcSvc, _, repo := updateSvc(t)
	tc := seedTicketClass(t, repo, 100, 0, 0)

	name := "VIP"
	got, err := tcSvc.Update(context.Background(), tc.ID, UpdateTicketClassInput{Name: &name})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if got.Name != "VIP" {
		t.Fatalf("name = %q, want VIP", got.Name)
	}
	if got.Total != 100 {
		t.Fatalf("total = %d, want 100 (must not be reset by a partial update)", got.Total)
	}
	if got.Currency != "USD" {
		t.Fatalf("currency = %q, want USD (must not be reset by a partial update)", got.Currency)
	}
}

// The P0 regression guard. Under the old Save()-based Update, reservations
// committing between the read and the write were erased from the counter.
func TestUpdate_ConcurrentReserve_DoesNotLoseHolds(t *testing.T) {
	tcSvc, rSvc, repo := updateSvc(t)
	tc := seedTicketClass(t, repo, 500, 0, 0)

	const reserves = 40
	var wg sync.WaitGroup
	var mu sync.Mutex
	okCount := 0

	for i := 0; i < reserves; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			err := rSvc.Reserve(context.Background(), ReserveInput{
				OrderCode: fmt.Sprintf("o-upd-race-%d", n),
				ExpiresAt: future(),
				Items:     []ReserveItem{{TicketClassID: tc.ID, Qty: 1}},
			})
			if err == nil {
				mu.Lock()
				okCount++
				mu.Unlock()
			}
		}(i)
	}
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			name := fmt.Sprintf("rename-%d", n)
			_, _ = tcSvc.Update(context.Background(), tc.ID, UpdateTicketClassInput{Name: &name})
		}(i)
	}
	wg.Wait()

	got := ticketClassByID(t, repo, tc.ID)
	if got.Reserved != okCount {
		t.Fatalf("reserved = %d but %d reserves succeeded -- holds were lost by a concurrent Update", got.Reserved, okCount)
	}
}

func TestUpdate_TotalBelowCommitted_ReturnsConflict(t *testing.T) {
	tcSvc, _, repo := updateSvc(t)
	tc := seedTicketClass(t, repo, 100, 5, 3)

	newTotal := 4
	_, err := tcSvc.Update(context.Background(), tc.ID, UpdateTicketClassInput{Total: &newTotal})
	if !errors.Is(err, ErrStateConflict) {
		t.Fatalf("shrinking total below reserved+sold = %v, want ErrStateConflict", err)
	}
	if got := ticketClassByID(t, repo, tc.ID); got.Total != 100 {
		t.Fatalf("total = %d, want 100 (rejected update must not apply)", got.Total)
	}
}

func TestUpdate_TotalAtCommitted_Succeeds(t *testing.T) {
	tcSvc, _, repo := updateSvc(t)
	tc := seedTicketClass(t, repo, 100, 5, 3)

	newTotal := 8 // exactly reserved + sold
	got, err := tcSvc.Update(context.Background(), tc.ID, UpdateTicketClassInput{Total: &newTotal})
	if err != nil {
		t.Fatalf("Update to exactly reserved+sold: %v", err)
	}
	if got.Total != 8 {
		t.Fatalf("total = %d, want 8", got.Total)
	}
}

func TestUpdate_Unknown_ReturnsNotFound(t *testing.T) {
	tcSvc, _, _ := updateSvc(t)
	name := "ghost"
	if _, err := tcSvc.Update(context.Background(), 999999999, UpdateTicketClassInput{Name: &name}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Update on unknown id = %v, want ErrNotFound", err)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

```bash
cd /Users/vogiaan/coding/projects/TicketEventPF/services/inventory-svc && go test ./internal/services/ -run 'TestUpdate_' -count=1 2>&1 | tail -20
```

Expected: FAIL to compile — `cannot use &name (value of type *string) as string value in struct literal`, because `UpdateTicketClassInput.Name` is still a plain `string`.

- [ ] **Step 3: Pointerize the input type**

Replace `UpdateTicketClassInput` in `services/inventory-svc/internal/services/ticketclass_types.go`:

```go
// UpdateTicketClassInput is a partial update: a nil field means "leave this
// column alone". There is deliberately no way to express reserved or sold --
// those belong exclusively to the reservation flow's locked transactions.
type UpdateTicketClassInput struct {
	Name        *string
	PriceCents  *int64
	Currency    *string
	Total       *int
	SaleStartAt *time.Time
	SaleEndAt   *time.Time
	Status      *string
}
```

- [ ] **Step 4: Replace `buildUpdate` with `updateColumns`**

In `services/inventory-svc/internal/services/ticketclass_builder.go`, delete `buildUpdate` entirely and put this in its place (leave `buildModel` untouched):

```go
// updateColumns turns a partial update into the exact set of columns to write.
// Counter columns (reserved, sold) are never included: writing them outside a
// locked reservation transaction is what silently erases live holds.
func (s implTicketClassService) updateColumns(in UpdateTicketClassInput) map[string]any {
	cols := make(map[string]any, 7)
	if in.Name != nil {
		cols["name"] = *in.Name
	}
	if in.PriceCents != nil {
		cols["price_cents"] = *in.PriceCents
	}
	if in.Currency != nil {
		cols["currency"] = *in.Currency
	}
	if in.Total != nil {
		cols["total"] = *in.Total
	}
	if in.SaleStartAt != nil {
		cols["sale_start_at"] = *in.SaleStartAt
	}
	if in.SaleEndAt != nil {
		cols["sale_end_at"] = *in.SaleEndAt
	}
	if in.Status != nil {
		cols["status"] = models.TicketClassStatus(*in.Status)
	}
	return cols
}
```

- [ ] **Step 5: Rewrite `Update` as a locked, column-targeted write**

Replace the whole `Update` method in `services/inventory-svc/internal/services/ticketclass.go`:

```go
func (s implTicketClassService) Update(ctx context.Context, id int64, in UpdateTicketClassInput) (models.TicketClass, error) {
	cols := s.updateColumns(in)

	var tc models.TicketClass
	err := s.repo.GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Lock the row so the capacity check below sees a stable
		// reserved/sold, and so a concurrent Reserve on this ticket class
		// serialises behind us instead of racing the write.
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			First(&tc, id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				s.l.Warnf(ctx, "service.ticketclass.Update: ticket class %d not found", id)
				return ErrNotFound
			}
			s.l.Errorf(ctx, "service.ticketclass.Update.Lock: %v", err)
			return err
		}

		if in.Total != nil && *in.Total < tc.Reserved+tc.Sold {
			s.l.Warnf(ctx, "service.ticketclass.Update: refusing total=%d below committed reserved=%d sold=%d for ticket_class_id=%d",
				*in.Total, tc.Reserved, tc.Sold, id)
			return ErrStateConflict
		}

		if len(cols) == 0 {
			return nil
		}

		if err := tx.Model(&models.TicketClass{}).Where("id = ?", id).
			Updates(cols).Error; err != nil {
			s.l.Errorf(ctx, "service.ticketclass.Update.Write: %v", err)
			return err
		}

		// Re-read inside the transaction so the caller gets the row as
		// committed, counters included.
		if err := tx.First(&tc, id).Error; err != nil {
			s.l.Errorf(ctx, "service.ticketclass.Update.Reload: %v", err)
			return err
		}
		return nil
	})
	if err != nil {
		return models.TicketClass{}, err
	}

	return tc, nil
}
```

Add `"gorm.io/gorm/clause"` to the file's import block (`"errors"` and `"gorm.io/gorm"` were added in Task 3).

- [ ] **Step 6: Only populate fields the request actually carries**

Replace `newUpdateTicketClassInput` in `services/inventory-svc/internal/delivery/grpc/presenter.go`:

```go
// newUpdateTicketClassInput converts protobuf request to service input.
//
// The proto uses plain (non-optional) scalars, so the zero value is the only
// available "absent" signal: an empty string or a 0 means "leave unchanged".
// That makes it impossible to set a price of 0 or an empty name through this
// RPC -- an accepted limitation until the contract gains `optional` fields.
func (s *grpcService) newUpdateTicketClassInput(req *invpb.UpdateTicketClassRequest) (svc.UpdateTicketClassInput, error) {
	startSaleAt, err := parseTime(req.GetStartSaleAt())
	if err != nil {
		return svc.UpdateTicketClassInput{}, err
	}

	endSaleAt, err := parseTime(req.GetEndSaleAt())
	if err != nil {
		return svc.UpdateTicketClassInput{}, err
	}

	in := svc.UpdateTicketClassInput{
		SaleStartAt: startSaleAt,
		SaleEndAt:   endSaleAt,
	}
	if v := req.GetName(); v != "" {
		in.Name = &v
	}
	if v := req.GetCurrency(); v != "" {
		in.Currency = &v
	}
	if v := req.GetPriceCents(); v != 0 {
		in.PriceCents = &v
	}
	if v := req.GetTotal(); v != 0 {
		total := int(v)
		in.Total = &total
	}

	return in, nil
}
```

- [ ] **Step 7: Run the tests**

```bash
cd /Users/vogiaan/coding/projects/TicketEventPF/services/inventory-svc && go test ./internal/services/ -run 'TestUpdate_' -count=1 -race -v 2>&1 | tail -30
```

Expected: five PASS lines. `TestUpdate_ConcurrentReserve_DoesNotLoseHolds` is the one that matters — it must report `reserved` exactly equal to the number of successful reserves.

- [ ] **Step 8: Run the whole suite and the build**

```bash
cd /Users/vogiaan/coding/projects/TicketEventPF/services/inventory-svc && go build ./... && go vet ./internal/... ./pkg/... ./config/... && make test 2>&1 | tail -20
```

Expected: no build or vet output; `ok` for both test packages.

- [ ] **Step 9: Commit**

```bash
cd /Users/vogiaan/coding/projects/TicketEventPF
git add services/inventory-svc/internal/services/ticketclass_types.go \
        services/inventory-svc/internal/services/ticketclass_builder.go \
        services/inventory-svc/internal/services/ticketclass.go \
        services/inventory-svc/internal/services/ticketclass_update_test.go \
        services/inventory-svc/internal/delivery/grpc/presenter.go
git commit -m "fix(inventory): stop UpdateTicketClass from clobbering reserved/sold

Update did FindByID -> mutate -> Save(), and Save writes every column, so
reservations committing between the read and the write were silently erased
from the counter while their ACTIVE rows survived -- letting the same seats
sell twice. Update now locks the row FOR UPDATE and writes only the columns
the request actually carries, and refuses to shrink total below
reserved+sold. UpdateTicketClassInput is fully pointerized so a rename no
longer resets total to 0 and currency to an empty string."
```

---

## Task 5: `CheckAvailability` must aggregate duplicate ticket classes (finding #8)

`ticketclass.go:144` does `qtyMap[in.TicketClassID] = in.Qty`, **overwriting** where `aggregateDemand` in `reservation.go` correctly sums. And `len(ticketClasses) != len(ins)` compares distinct rows against input entries, so two line items for the same ticket class produce a false "not found". `Reserve` handles duplicates correctly; only this pre-check does not.

**Files:**
- Modify: `services/inventory-svc/internal/services/ticketclass.go:133-176`
- Test: `services/inventory-svc/internal/services/ticketclass_availability_test.go`

**Interfaces:**
- Consumes: `aggregateDemand(items []ReserveItem) (ids []int64, qtyByID map[int64]int)` — already defined at `reservation.go:106`, same package.
- Produces: nothing new.

- [ ] **Step 1: Write the failing tests**

Create `services/inventory-svc/internal/services/ticketclass_availability_test.go`:

```go
package service

import (
	"context"
	"testing"
)

// Two line items for the same ticket class must be summed. Overwriting means
// a request for 3 + 4 of a class with 5 left is wrongly accepted.
func TestCheckAvailability_DuplicateIDs_SumsQuantities(t *testing.T) {
	repo := newTestDB(t)
	tcSvc := NewTicketClassService(newTestLogger(), repo)
	tc := seedTicketClass(t, repo, 5, 0, 0)

	ok, err := tcSvc.CheckAvailability(context.Background(), []CheckAvailabilityInput{
		{TicketClassID: tc.ID, Qty: 3},
		{TicketClassID: tc.ID, Qty: 4}, // 7 total against 5 available
	})
	if err != nil {
		t.Fatalf("CheckAvailability: %v", err)
	}
	if ok {
		t.Fatal("accept = true, want false: 3 + 4 exceeds the 5 available")
	}
}

// The mirror case: duplicates that fit must not trip the row-count check.
func TestCheckAvailability_DuplicateIDsWithinCapacity_Accepts(t *testing.T) {
	repo := newTestDB(t)
	tcSvc := NewTicketClassService(newTestLogger(), repo)
	tc := seedTicketClass(t, repo, 10, 0, 0)

	ok, err := tcSvc.CheckAvailability(context.Background(), []CheckAvailabilityInput{
		{TicketClassID: tc.ID, Qty: 3},
		{TicketClassID: tc.ID, Qty: 4}, // 7 against 10 available
	})
	if err != nil {
		t.Fatalf("CheckAvailability: %v", err)
	}
	if !ok {
		t.Fatal("accept = false, want true: 3 + 4 fits within the 10 available")
	}
}

func TestCheckAvailability_UnknownID_Rejects(t *testing.T) {
	repo := newTestDB(t)
	tcSvc := NewTicketClassService(newTestLogger(), repo)

	ok, err := tcSvc.CheckAvailability(context.Background(), []CheckAvailabilityInput{
		{TicketClassID: 999999999, Qty: 1},
	})
	if err != nil {
		t.Fatalf("CheckAvailability: %v", err)
	}
	if ok {
		t.Fatal("accept = true, want false for an unknown ticket class")
	}
}
```

- [ ] **Step 2: Run to verify it fails**

```bash
cd /Users/vogiaan/coding/projects/TicketEventPF/services/inventory-svc && go test ./internal/services/ -run 'TestCheckAvailability_' -count=1 -v 2>&1 | tail -20
```

Expected: `TestCheckAvailability_DuplicateIDsWithinCapacity_Accepts` FAILs with `accept = false, want true` — the row-count check misfires. **That test is the only RED-phase discriminator, and this is inherent to the bug's shape.** Both defects fire on the same input, and the row-count check short-circuits first: with `ids = [5, 5]`, `Where("id IN ?", ids)` returns one row, so `len(ticketClasses)=1 != len(ins)=2` returns `(false, nil)` before the qty loop is ever reached. Any duplicate-id test that *expects rejection* therefore passes on the broken code by accident. `TestCheckAvailability_DuplicateIDs_SumsQuantities` passes both before and after the fix; keep it as a regression guard on the summing behaviour, but do not read its RED-phase pass as evidence of anything.

- [ ] **Step 3: Reuse `aggregateDemand`**

Replace the whole `CheckAvailability` method in `services/inventory-svc/internal/services/ticketclass.go`:

```go
func (s *implTicketClassService) CheckAvailability(ctx context.Context, ins []CheckAvailabilityInput) (bool, error) {
	if len(ins) == 0 {
		return true, nil
	}

	// Collapse duplicate line items exactly the way Reserve does -- a
	// pre-check that disagrees with the real gate is worse than no pre-check.
	items := make([]ReserveItem, len(ins))
	for i, in := range ins {
		items[i] = ReserveItem{TicketClassID: in.TicketClassID, Qty: in.Qty}
	}
	ids, qtyByID := aggregateDemand(items)

	var tcs []models.TicketClass
	if err := s.repo.WithContext(ctx).
		Model(&models.TicketClass{}).
		Where("id IN ?", ids).
		Find(&tcs).Error; err != nil {
		s.l.Errorf(ctx, "service.ticketclass.CheckAvailability: %v", err)
		return false, err
	}

	if len(tcs) != len(ids) {
		s.l.Warnf(ctx, "service.ticketclass.CheckAvailability: requested %d distinct ticket classes, found %d", len(ids), len(tcs))
		return false, nil
	}

	for _, tc := range tcs {
		requestedQty := qtyByID[tc.ID]
		availableQty := tc.Total - tc.Reserved - tc.Sold
		if availableQty < requestedQty {
			s.l.Warnf(ctx, "service.ticketclass.CheckAvailability: insufficient stock for ticket_class_id=%d (available=%d, requested=%d)",
				tc.ID, availableQty, requestedQty)
			return false, nil
		}
	}

	return true, nil
}
```

- [ ] **Step 4: Run to verify the tests pass**

```bash
cd /Users/vogiaan/coding/projects/TicketEventPF/services/inventory-svc && go test ./internal/services/ -run 'TestCheckAvailability_' -count=1 -v 2>&1 | tail -20
```

Expected: three PASS lines.

- [ ] **Step 5: Run the whole suite**

```bash
cd /Users/vogiaan/coding/projects/TicketEventPF/services/inventory-svc && go build ./... && make test 2>&1 | tail -20
```

Expected: `ok` for both packages.

- [ ] **Step 6: Commit**

```bash
cd /Users/vogiaan/coding/projects/TicketEventPF
git add services/inventory-svc/internal/services/ticketclass.go \
        services/inventory-svc/internal/services/ticketclass_availability_test.go
git commit -m "fix(inventory): sum duplicate ticket classes in CheckAvailability

The qty map overwrote instead of summing, and the found-row count was
compared against the input-entry count, so two line items for the same
ticket class both under-counted demand and produced a false not-found.
Now reuses aggregateDemand, the same collapse Reserve performs."
```

---

## Task 6: `Reserve` idempotency must be status-scoped (finding #6)

`reservation.go:41-50` short-circuits on `COUNT(*) WHERE order_code = ?` regardless of status. Because `Release` leaves `CANCELLED` rows behind (and the expiry worker leaves `EXPIRED` rows), a retried `Reserve` for that order code returns **success while holding zero inventory**. The saga usually dodges this because compensation is followed by a fresh order code, but the RPC contract is wrong and one workflow change away from being a live bug.

**Files:**
- Modify: `services/inventory-svc/internal/services/reservation.go:37-50`
- Test: `services/inventory-svc/internal/services/reservation_reserve_test.go`

**Interfaces:**
- Consumes: `service.ErrStateConflict` (pre-existing).
- Produces: nothing new. `Reserve` gains one behaviour: an order code whose rows are all terminal (`EXPIRED`/`CANCELLED`) now returns `ErrStateConflict` instead of a false success.

- [ ] **Step 1: Write the failing tests**

Append to `services/inventory-svc/internal/services/reservation_reserve_test.go`:

```go
// Reserve -> Release -> Reserve must not report success while holding
// nothing. The old count-any-status short-circuit saw the CANCELLED rows and
// no-oped, handing back an order with zero inventory behind it.
func TestReserve_AfterRelease_ReturnsConflict(t *testing.T) {
	svc, repo := reserveSvc(t)
	tc := seedTicketClass(t, repo, 100, 0, 0)
	in := ReserveInput{
		OrderCode: "o-rereserve",
		ExpiresAt: future(),
		Items:     []ReserveItem{{TicketClassID: tc.ID, Qty: 4}},
	}
	must(t, svc.Reserve(context.Background(), in))
	must(t, svc.Release(context.Background(), "o-rereserve"))

	if err := svc.Reserve(context.Background(), in); !errors.Is(err, ErrStateConflict) {
		t.Fatalf("Reserve after Release = %v, want ErrStateConflict", err)
	}
	if got := ticketClassByID(t, repo, tc.ID); got.Reserved != 0 {
		t.Fatalf("reserved = %d, want 0 (the rejected re-reserve must hold nothing)", got.Reserved)
	}
}

func TestReserve_AfterExpiry_ReturnsConflict(t *testing.T) {
	svc, repo := reserveSvc(t)
	tc := seedTicketClass(t, repo, 100, 0, 0)
	in := ReserveInput{
		OrderCode: "o-reexpire",
		ExpiresAt: time.Now().UTC().Add(-1 * time.Minute),
		Items:     []ReserveItem{{TicketClassID: tc.ID, Qty: 4}},
	}
	must(t, svc.Reserve(context.Background(), in))
	if _, err := svc.BatchExpireReservations(context.Background(), 500); err != nil {
		t.Fatalf("BatchExpireReservations: %v", err)
	}

	if err := svc.Reserve(context.Background(), in); !errors.Is(err, ErrStateConflict) {
		t.Fatalf("Reserve after expiry = %v, want ErrStateConflict", err)
	}
}
```

Add `"errors"` to that file's import block.

- [ ] **Step 2: Run to verify it fails**

```bash
cd /Users/vogiaan/coding/projects/TicketEventPF/services/inventory-svc && go test ./internal/services/ -run 'TestReserve_After' -count=1 -v 2>&1 | tail -20
```

Expected: both FAIL with `Reserve after Release = <nil>, want ErrStateConflict`.

- [ ] **Step 3: Scope the idempotency check by status**

In `services/inventory-svc/internal/services/reservation.go`, replace the existing idempotency block (the `var existing int64` count through the `if existing > 0 { ... }` close) with:

```go
		// Idempotency, scoped by status. A live hold (ACTIVE or CONFIRMED)
		// means this is a retry of a call that already succeeded -- no-op. But
		// rows that are all terminal mean the hold was released or expired,
		// and silently returning success there would hand the caller an order
		// with zero inventory behind it. A concurrent duplicate that races
		// past this unlocked read is still caught by the
		// (order_code, ticket_class_id) unique index on insert.
		var statuses []models.ReservationStatus
		if err := tx.Model(&models.Reservation{}).
			Where("order_code = ?", in.OrderCode).
			Pluck("status", &statuses).Error; err != nil {
			s.l.Errorf(ctx, "service.reservation.Reserve.LoadExistingStatuses: %v", err)
			return err
		}
		if len(statuses) > 0 {
			for _, st := range statuses {
				if st == models.ReservationStatusActive || st == models.ReservationStatusConfirmed {
					s.l.Infof(ctx, "service.reservation.Reserve: live reservations already exist for order_code=%s, no-op", in.OrderCode)
					return nil
				}
			}
			s.l.Warnf(ctx, "service.reservation.Reserve: order_code=%s has only terminal reservations (%v), refusing to re-reserve",
				in.OrderCode, statuses)
			return ErrStateConflict
		}
```

- [ ] **Step 4: Run to verify the tests pass**

```bash
cd /Users/vogiaan/coding/projects/TicketEventPF/services/inventory-svc && go test ./internal/services/ -run 'TestReserve_' -count=1 -v 2>&1 | tail -30
```

Expected: all `TestReserve_*` PASS, including the pre-existing `TestReserve_Idempotent_SecondCallNoOp` — its second call still sees `ACTIVE` rows and still no-ops.

- [ ] **Step 5: Run the whole suite**

```bash
cd /Users/vogiaan/coding/projects/TicketEventPF/services/inventory-svc && go build ./... && make test 2>&1 | tail -20
```

Expected: `ok` for both packages.

- [ ] **Step 6: Commit**

```bash
cd /Users/vogiaan/coding/projects/TicketEventPF
git add services/inventory-svc/internal/services/reservation.go \
        services/inventory-svc/internal/services/reservation_reserve_test.go
git commit -m "fix(inventory): scope Reserve idempotency to live reservations

The short-circuit counted rows of any status, so a Reserve retried after a
Release or an expiry saw the terminal rows and returned success while
holding no inventory at all. Live rows (ACTIVE/CONFIRMED) still no-op;
all-terminal now returns ErrStateConflict."
```

---

## Task 7: Enforce ticket-class status and sale window (finding #9)

`TicketClassStatus`, `SaleStartAt` and `SaleEndAt` are persisted, exposed in the proto, and checked **nowhere**. You can reserve tickets for an `INACTIVE` class, or hours before the sale opens. The check belongs inside `Reserve`'s locked transaction, next to the availability validation, so it sees the same consistent snapshot.

**Files:**
- Modify: `services/inventory-svc/internal/services/errors.go`
- Modify: `services/inventory-svc/internal/services/reservation.go` (the validation loop, currently lines 65-74)
- Modify: `services/inventory-svc/internal/services/ticketclass.go` (`CheckAvailability` loop)
- Modify: `services/inventory-svc/pkg/errors/errors.go`
- Modify: `services/inventory-svc/internal/delivery/grpc/errors.go`
- Test: `services/inventory-svc/internal/services/reservation_sale_window_test.go`

**Interfaces:**
- Consumes: `service.ErrSaleClosed` (defined in this task); `models.TicketClassStatusActive`, `models.TicketClassStatusInactive` (pre-existing).
- Produces: `service.ErrSaleClosed` and `pkgErrors.ErrSaleClosed` (gRPC `FailedPrecondition`). `seedTicketClassWindow` test helper.

- [ ] **Step 1: Write the failing tests**

Create `services/inventory-svc/internal/services/reservation_sale_window_test.go`:

```go
package service

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/vogiaan/ticketbottle-inventory/internal/models"
	pkgGorm "github.com/vogiaan/ticketbottle-inventory/pkg/gorm"
)

// seedTicketClassWindow seeds a ticket class with an explicit status and sale
// window. Passing nil for a bound means "unbounded on that side".
func seedTicketClassWindow(t *testing.T, repo *pkgGorm.Repository, status models.TicketClassStatus, start, end *time.Time) models.TicketClass {
	t.Helper()
	n := seedCounter.Add(1)
	tc := models.TicketClass{
		EventID:     fmt.Sprintf("evt-%s-%d", t.Name(), n),
		Name:        fmt.Sprintf("GA-%s-%d", t.Name(), n),
		PriceCents:  1000,
		Currency:    "USD",
		Total:       100,
		Status:      status,
		SaleStartAt: start,
		SaleEndAt:   end,
	}
	if err := repo.Create(context.Background(), &tc); err != nil {
		t.Fatalf("seed ticket class: %v", err)
	}
	return tc
}

func TestReserve_InactiveTicketClass_ReturnsSaleClosed(t *testing.T) {
	svc, repo := reserveSvc(t)
	tc := seedTicketClassWindow(t, repo, models.TicketClassStatusInactive, nil, nil)

	err := svc.Reserve(context.Background(), ReserveInput{
		OrderCode: "o-inactive", ExpiresAt: future(),
		Items: []ReserveItem{{TicketClassID: tc.ID, Qty: 1}},
	})
	if !errors.Is(err, ErrSaleClosed) {
		t.Fatalf("Reserve on INACTIVE class = %v, want ErrSaleClosed", err)
	}
	if got := ticketClassByID(t, repo, tc.ID); got.Reserved != 0 {
		t.Fatalf("reserved = %d, want 0", got.Reserved)
	}
}

func TestReserve_BeforeSaleStart_ReturnsSaleClosed(t *testing.T) {
	svc, repo := reserveSvc(t)
	start := time.Now().UTC().Add(1 * time.Hour)
	tc := seedTicketClassWindow(t, repo, models.TicketClassStatusActive, &start, nil)

	err := svc.Reserve(context.Background(), ReserveInput{
		OrderCode: "o-early", ExpiresAt: future(),
		Items: []ReserveItem{{TicketClassID: tc.ID, Qty: 1}},
	})
	if !errors.Is(err, ErrSaleClosed) {
		t.Fatalf("Reserve before sale start = %v, want ErrSaleClosed", err)
	}
}

func TestReserve_AfterSaleEnd_ReturnsSaleClosed(t *testing.T) {
	svc, repo := reserveSvc(t)
	end := time.Now().UTC().Add(-1 * time.Hour)
	tc := seedTicketClassWindow(t, repo, models.TicketClassStatusActive, nil, &end)

	err := svc.Reserve(context.Background(), ReserveInput{
		OrderCode: "o-late", ExpiresAt: future(),
		Items: []ReserveItem{{TicketClassID: tc.ID, Qty: 1}},
	})
	if !errors.Is(err, ErrSaleClosed) {
		t.Fatalf("Reserve after sale end = %v, want ErrSaleClosed", err)
	}
}

func TestReserve_WithinSaleWindow_Succeeds(t *testing.T) {
	svc, repo := reserveSvc(t)
	start := time.Now().UTC().Add(-1 * time.Hour)
	end := time.Now().UTC().Add(1 * time.Hour)
	tc := seedTicketClassWindow(t, repo, models.TicketClassStatusActive, &start, &end)

	if err := svc.Reserve(context.Background(), ReserveInput{
		OrderCode: "o-inwindow", ExpiresAt: future(),
		Items: []ReserveItem{{TicketClassID: tc.ID, Qty: 2}},
	}); err != nil {
		t.Fatalf("Reserve within the sale window: %v", err)
	}
	if got := ticketClassByID(t, repo, tc.ID); got.Reserved != 2 {
		t.Fatalf("reserved = %d, want 2", got.Reserved)
	}
}

func TestCheckAvailability_InactiveTicketClass_Rejects(t *testing.T) {
	repo := newTestDB(t)
	tcSvc := NewTicketClassService(newTestLogger(), repo)
	tc := seedTicketClassWindow(t, repo, models.TicketClassStatusInactive, nil, nil)

	ok, err := tcSvc.CheckAvailability(context.Background(), []CheckAvailabilityInput{
		{TicketClassID: tc.ID, Qty: 1},
	})
	if err != nil {
		t.Fatalf("CheckAvailability: %v", err)
	}
	if ok {
		t.Fatal("accept = true, want false for an INACTIVE ticket class")
	}
}
```

- [ ] **Step 2: Run to verify it fails**

```bash
cd /Users/vogiaan/coding/projects/TicketEventPF/services/inventory-svc && go test ./internal/services/ -run 'SaleClosed|SaleWindow|InactiveTicketClass' -count=1 2>&1 | tail -20
```

Expected: FAIL to compile with `undefined: ErrSaleClosed`.

- [ ] **Step 3: Define `ErrSaleClosed` in both error layers**

Append to the `var` block in `services/inventory-svc/internal/services/errors.go`:

```go
	// ErrSaleClosed signals the ticket class is not currently on sale --
	// INACTIVE, or outside its [sale_start_at, sale_end_at] window.
	ErrSaleClosed = errors.New("ticket class not on sale")
```

Append to the `var` block in `services/inventory-svc/pkg/errors/errors.go`:

```go
	ErrSaleClosed = NewGRPCError(codes.FailedPrecondition, "ticket class not on sale")
```

Add this case to `mapError` in `services/inventory-svc/internal/delivery/grpc/errors.go`, immediately before the `ErrStateConflict` case:

```go
	case errors.Is(err, svc.ErrSaleClosed):
		return pkgErrors.ErrSaleClosed
```

- [ ] **Step 4: Enforce the window inside `Reserve`'s locked transaction**

In `services/inventory-svc/internal/services/reservation.go`, replace the availability validation loop (currently lines 65-74, the `// Validate availability against the locked rows.` block) with:

```go
		// Validate sale eligibility and availability against the locked rows.
		now := time.Now().UTC()
		for _, id := range ids {
			tc := byID[id]
			if tc.Status != models.TicketClassStatusActive {
				s.l.Warnf(ctx, "service.reservation.Reserve: ticket_class_id=%d is %s, not on sale", id, tc.Status)
				return ErrSaleClosed
			}
			if tc.SaleStartAt != nil && now.Before(*tc.SaleStartAt) {
				s.l.Warnf(ctx, "service.reservation.Reserve: ticket_class_id=%d sale opens at %s", id, tc.SaleStartAt.Format(time.RFC3339))
				return ErrSaleClosed
			}
			if tc.SaleEndAt != nil && now.After(*tc.SaleEndAt) {
				s.l.Warnf(ctx, "service.reservation.Reserve: ticket_class_id=%d sale closed at %s", id, tc.SaleEndAt.Format(time.RFC3339))
				return ErrSaleClosed
			}

			q := qtyByID[id]
			if tc.Total-tc.Reserved-tc.Sold < q {
				s.l.Warnf(ctx, "service.reservation.Reserve: insufficient stock for ticket_class_id=%d (available=%d, requested=%d)",
					id, tc.Total-tc.Reserved-tc.Sold, q)
				return ErrInsufficientStock
			}
		}
```

- [ ] **Step 5: Apply the same rule in `CheckAvailability`**

In `services/inventory-svc/internal/services/ticketclass.go`, replace the final validation loop of `CheckAvailability` with:

```go
	now := time.Now().UTC()
	for _, tc := range tcs {
		if tc.Status != models.TicketClassStatusActive ||
			(tc.SaleStartAt != nil && now.Before(*tc.SaleStartAt)) ||
			(tc.SaleEndAt != nil && now.After(*tc.SaleEndAt)) {
			s.l.Warnf(ctx, "service.ticketclass.CheckAvailability: ticket_class_id=%d is not on sale (status=%s)", tc.ID, tc.Status)
			return false, nil
		}

		requestedQty := qtyByID[tc.ID]
		availableQty := tc.Total - tc.Reserved - tc.Sold
		if availableQty < requestedQty {
			s.l.Warnf(ctx, "service.ticketclass.CheckAvailability: insufficient stock for ticket_class_id=%d (available=%d, requested=%d)",
				tc.ID, availableQty, requestedQty)
			return false, nil
		}
	}
```

Add `"time"` to that file's import block.

- [ ] **Step 6: Run to verify the tests pass**

```bash
cd /Users/vogiaan/coding/projects/TicketEventPF/services/inventory-svc && go test ./internal/services/ -run 'SaleClosed|SaleWindow|InactiveTicketClass' -count=1 -v 2>&1 | tail -20
```

Expected: five PASS lines.

- [ ] **Step 7: Run the whole suite**

```bash
cd /Users/vogiaan/coding/projects/TicketEventPF/services/inventory-svc && go build ./... && go vet ./internal/... ./pkg/... ./config/... && make test 2>&1 | tail -20
```

Expected: `ok` for both packages. Every pre-existing test seeds `Status: models.TicketClassStatusActive` with nil sale bounds via `seedTicketClass`, so none of them trip the new gate.

- [ ] **Step 8: Commit**

```bash
cd /Users/vogiaan/coding/projects/TicketEventPF
git add services/inventory-svc/internal/services/errors.go \
        services/inventory-svc/internal/services/reservation.go \
        services/inventory-svc/internal/services/ticketclass.go \
        services/inventory-svc/internal/services/reservation_sale_window_test.go \
        services/inventory-svc/pkg/errors/errors.go \
        services/inventory-svc/internal/delivery/grpc/errors.go
git commit -m "feat(inventory): enforce ticket class status and sale window

status, sale_start_at and sale_end_at were persisted and exposed but never
checked, so an INACTIVE class or one whose sale had not opened could still
be reserved. Reserve now validates them inside the locked transaction
(same snapshot as the availability check) and CheckAvailability mirrors it.
New ErrSaleClosed maps to gRPC FailedPrecondition."
```

---

## Task 8: `Confirm` accepts past-expiry holds and re-acquires released ones (finding #4, inventory half)

The money bug. `reservation.go:174` rejects a confirm whose reservation is past `expires_at`, returning `ErrStateConflict`. Order-svc sets the hold to exactly `PaymentTimeout` with zero slack, and the expiry worker sweeps every 60s — so a payment landing in the last seconds of the window produces a captured payment, a released seat, and a `// TODO: Implement manual intervention` in `confirm_order.go:41-45`.

Two distinct states need distinct handling:

- **`ACTIVE` but past `expires_at`** — the worker has not swept it yet, so the row *still holds* its `reserved` quantity. Moving reserved → sold is exactly correct and always safe. This alone closes the common case.
- **`EXPIRED`** — the worker already gave the stock back. Take it again from free stock if it is there; only if the seat has genuinely been resold does the caller get a conflict (and must refund).

**Files:**
- Modify: `services/inventory-svc/internal/services/reservation.go:126-136` (generic `sortedInt64Keys`) and `145-210` (`confirmReservationTx`)
- Modify: `services/inventory-svc/internal/services/reservation_confirm_test.go`
- Test: `services/inventory-svc/internal/services/reservation_confirm_test.go`

**Interfaces:**
- Consumes: `service.ErrStateConflict`, `service.ErrInventoryDrift`, `service.ErrNotFound` (Task 3); `chk_ticket_class_capacity` (Task 2).
- Produces: `sortedInt64Keys[V any](m map[int64]V) []int64` — generic over the value type. Existing `map[int64]int` call sites in `Release` and `BatchExpireReservations` infer `V = int` and need no change. Task 10 relies on this signature.

- [ ] **Step 1: Delete the test that asserts the old behaviour**

`TestConfirm_Expired_ReturnsConflict` in `services/inventory-svc/internal/services/reservation_confirm_test.go` encodes exactly the bug being fixed: it reserves with a past `expires_at` (leaving the row `ACTIVE`) and asserts `Confirm` returns `ErrStateConflict`. **Delete that test function entirely.** It is replaced by the three tests in Step 2. This is the only existing test this plan removes.

- [ ] **Step 2: Write the failing tests**

Append to `services/inventory-svc/internal/services/reservation_confirm_test.go`:

```go
// An ACTIVE hold past its expires_at still holds its reserved quantity --
// the worker has not swept it yet. Confirming it is exactly correct, and
// refusing was the bug that left payments captured with no seat.
func TestConfirm_ActivePastExpiry_Succeeds(t *testing.T) {
	svc, repo := reserveSvc(t)
	tc := seedTicketClass(t, repo, 100, 0, 0)
	must(t, svc.Reserve(context.Background(), ReserveInput{
		OrderCode: "o-past-exp", ExpiresAt: time.Now().UTC().Add(-1 * time.Minute),
		Items: []ReserveItem{{TicketClassID: tc.ID, Qty: 5}},
	}))

	if err := svc.Confirm(context.Background(), "o-past-exp"); err != nil {
		t.Fatalf("Confirm on an unswept past-expiry hold: %v", err)
	}
	got := ticketClassByID(t, repo, tc.ID)
	if got.Reserved != 0 || got.Sold != 5 {
		t.Fatalf("reserved=%d sold=%d, want 0/5", got.Reserved, got.Sold)
	}
}

// The worker already released the hold, but the seats are still there:
// re-acquire them rather than fail a paid order.
func TestConfirm_ExpiredByWorker_ReacquiresWhenStockAvailable(t *testing.T) {
	svc, repo := reserveSvc(t)
	tc := seedTicketClass(t, repo, 100, 0, 0)
	must(t, svc.Reserve(context.Background(), ReserveInput{
		OrderCode: "o-reacq", ExpiresAt: time.Now().UTC().Add(-1 * time.Minute),
		Items: []ReserveItem{{TicketClassID: tc.ID, Qty: 5}},
	}))
	if _, err := svc.BatchExpireReservations(context.Background(), 500); err != nil {
		t.Fatalf("BatchExpireReservations: %v", err)
	}
	if got := ticketClassByID(t, repo, tc.ID); got.Reserved != 0 {
		t.Fatalf("precondition reserved=%d, want 0 after the worker swept", got.Reserved)
	}

	if err := svc.Confirm(context.Background(), "o-reacq"); err != nil {
		t.Fatalf("Confirm on a swept hold with stock still free: %v", err)
	}
	got := ticketClassByID(t, repo, tc.ID)
	if got.Reserved != 0 || got.Sold != 5 {
		t.Fatalf("reserved=%d sold=%d, want 0/5 (stock re-acquired)", got.Reserved, got.Sold)
	}

	var r models.Reservation
	repo.WithContext(context.Background()).Where("order_code = ?", "o-reacq").First(&r)
	if r.Status != models.ReservationStatusConfirmed {
		t.Fatalf("status = %s, want CONFIRMED", r.Status)
	}
}

// The seats really are gone -- the caller must learn that and refund.
func TestConfirm_ExpiredByWorker_ConflictsWhenSoldOut(t *testing.T) {
	svc, repo := reserveSvc(t)
	tc := seedTicketClass(t, repo, 5, 0, 0)
	must(t, svc.Reserve(context.Background(), ReserveInput{
		OrderCode: "o-soldout", ExpiresAt: time.Now().UTC().Add(-1 * time.Minute),
		Items: []ReserveItem{{TicketClassID: tc.ID, Qty: 5}},
	}))
	if _, err := svc.BatchExpireReservations(context.Background(), 500); err != nil {
		t.Fatalf("BatchExpireReservations: %v", err)
	}
	// Somebody else took every seat in the meantime.
	must(t, svc.Reserve(context.Background(), ReserveInput{
		OrderCode: "o-winner", ExpiresAt: future(),
		Items: []ReserveItem{{TicketClassID: tc.ID, Qty: 5}},
	}))
	must(t, svc.Confirm(context.Background(), "o-winner"))

	if err := svc.Confirm(context.Background(), "o-soldout"); !errors.Is(err, ErrStateConflict) {
		t.Fatalf("Confirm on a swept hold with no stock left = %v, want ErrStateConflict", err)
	}
	got := ticketClassByID(t, repo, tc.ID)
	if got.Sold != 5 {
		t.Fatalf("sold = %d, want 5 (the failed re-acquire must not oversell)", got.Sold)
	}
}
```

Add `"errors"` to that file's import block.

- [ ] **Step 3: Run to verify it fails**

```bash
cd /Users/vogiaan/coding/projects/TicketEventPF/services/inventory-svc && go test ./internal/services/ -run 'TestConfirm_' -count=1 -v 2>&1 | tail -30
```

Expected: `TestConfirm_ActivePastExpiry_Succeeds` FAILs with `reservation state conflict`; `TestConfirm_ExpiredByWorker_ReacquiresWhenStockAvailable` FAILs the same way; `TestConfirm_ExpiredByWorker_ConflictsWhenSoldOut` may pass for the wrong reason (the old code conflicts on everything) — it becomes meaningful once the others pass.

- [ ] **Step 4: Make `sortedInt64Keys` generic**

Replace `sortedInt64Keys` in `services/inventory-svc/internal/services/reservation.go`:

```go
// sortedInt64Keys returns the map keys in ascending order, so every operation
// that updates multiple ticket_class rows locks them in the same (ascending
// id) order -- preserving the deadlock-freedom invariant.
func sortedInt64Keys[V any](m map[int64]V) []int64 {
	keys := make([]int64, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	return keys
}
```

Existing call sites in `cancelReservationTx` and `BatchExpireReservations` pass `map[int64]int` and infer `V = int` — no change needed at those lines.

- [ ] **Step 5: Rewrite `confirmReservationTx`**

Replace the entire `confirmReservationTx` method in `services/inventory-svc/internal/services/reservation.go`:

```go
// confirmDelta is how much of a ticket class's confirm comes from the hold
// this order still owns, versus how much has to be taken back out of free
// stock because the expiry worker already released it.
type confirmDelta struct {
	fromReserved int
	fromFree     int
}

func (s implReservationService) confirmReservationTx(ctx context.Context, tx *gorm.DB, oCode string) error {
	var rs []models.Reservation
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("order_code = ?", oCode).Find(&rs).Error; err != nil {
		s.l.Errorf(ctx, "service.reservation.Confirm.LockReservations: %v", err)
		return err
	}
	if len(rs) == 0 {
		s.l.Warnf(ctx, "service.reservation.Confirm: no reservations for order_code=%s", oCode)
		return ErrNotFound
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

	deltas := make(map[int64]*confirmDelta, len(rs))
	rIDs := make([]int64, 0, len(rs))
	for _, r := range rs {
		d := deltas[r.TicketClassID]
		if d == nil {
			d = &confirmDelta{}
			deltas[r.TicketClassID] = d
		}

		switch r.Status {
		case models.ReservationStatusActive:
			// Still holding its quantity in `reserved`, even if past
			// expires_at -- the worker just has not swept it yet. Confirming
			// is a pure reserved -> sold move and is always safe.
			d.fromReserved += r.Qty

		case models.ReservationStatusExpired:
			// The worker already handed the stock back. Payment succeeded
			// anyway, so try to take it again out of what is free.
			s.l.Warnf(ctx, "service.reservation.Confirm: reservation %d for order_code=%s was already expired; attempting re-acquire of qty=%d",
				r.ID, oCode, r.Qty)
			d.fromFree += r.Qty

		default:
			// CONFIRMED mixed with unconfirmed, or CANCELLED: an order half
			// applied or explicitly released. Never guess -- surface it.
			s.l.Warnf(ctx, "service.reservation.Confirm: conflict for reservation %d (status=%s)", r.ID, r.Status)
			return ErrStateConflict
		}
		rIDs = append(rIDs, r.ID)
	}

	for _, tcID := range sortedInt64Keys(deltas) {
		d := deltas[tcID]

		if d.fromReserved > 0 {
			result := tx.Model(&models.TicketClass{}).
				Where("id = ? AND reserved >= ?", tcID, d.fromReserved).
				Updates(map[string]any{
					"reserved": gorm.Expr("reserved - ?", d.fromReserved),
					"sold":     gorm.Expr("sold + ?", d.fromReserved),
				})
			if result.Error != nil {
				s.l.Errorf(ctx, "service.reservation.Confirm.UpdateTicketClass: ticket_class_id=%d: %v", tcID, result.Error)
				return result.Error
			}
			if result.RowsAffected == 0 {
				s.l.Errorf(ctx, "service.reservation.Confirm: insufficient reserved for ticket_class_id=%d (needed=%d)", tcID, d.fromReserved)
				return ErrInventoryDrift
			}
		}

		if d.fromFree > 0 {
			result := tx.Model(&models.TicketClass{}).
				Where("id = ? AND total - reserved - sold >= ?", tcID, d.fromFree).
				Update("sold", gorm.Expr("sold + ?", d.fromFree))
			if result.Error != nil {
				s.l.Errorf(ctx, "service.reservation.Confirm.ReacquireTicketClass: ticket_class_id=%d: %v", tcID, result.Error)
				return result.Error
			}
			if result.RowsAffected == 0 {
				s.l.Errorf(ctx, "service.reservation.Confirm: cannot re-acquire %d for ticket_class_id=%d on order_code=%s -- stock is gone, order needs a refund",
					d.fromFree, tcID, oCode)
				return ErrStateConflict
			}
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

`time` is no longer referenced by this method, but it is still used by `BatchExpireReservations` and (after Task 7) by `Reserve` — keep the import.

- [ ] **Step 6: Run to verify the tests pass**

```bash
cd /Users/vogiaan/coding/projects/TicketEventPF/services/inventory-svc && go test ./internal/services/ -run 'TestConfirm_' -count=1 -race -v 2>&1 | tail -30
```

Expected: all `TestConfirm_*` PASS — the three new ones plus the surviving `TestConfirm_MovesReservedToSold`, `TestConfirm_Idempotent_SecondCallNoOp`, `TestConfirm_NoReservations_ReturnsNotFound`, and `TestConfirm_MixedState_ReturnsConflict` (a `CONFIRMED` row mixed with an `ACTIVE` one still hits the `default` branch).

- [ ] **Step 7: Run the whole suite**

```bash
cd /Users/vogiaan/coding/projects/TicketEventPF/services/inventory-svc && go build ./... && go vet ./internal/... ./pkg/... ./config/... && make test 2>&1 | tail -20
```

Expected: `ok` for both packages.

- [ ] **Step 8: Commit**

```bash
cd /Users/vogiaan/coding/projects/TicketEventPF
git add services/inventory-svc/internal/services/reservation.go \
        services/inventory-svc/internal/services/reservation_confirm_test.go
git commit -m "fix(inventory): confirm past-expiry holds instead of losing paid orders

Confirm rejected any reservation past expires_at, so a payment landing in
the last seconds of the window produced a captured payment with no seat and
a manual-intervention TODO in order-svc. An ACTIVE row past its expiry still
holds its reserved quantity, so confirming it is a pure reserved -> sold
move and is now accepted. A row the worker already swept is re-acquired out
of free stock, and only genuinely-resold stock returns ErrStateConflict.

Replaces TestConfirm_Expired_ReturnsConflict, which encoded the old
behaviour, with three tests covering unswept, re-acquired, and sold-out."
```

---

## Task 9: Extend the hold past the payment window (finding #4, order-svc half)

`create_order.go:97` sets the inventory hold to `workflow.Now(ctx).Add(PaymentTimeout)` — the hold expires at exactly the moment the payment window closes, leaving zero room for the webhook → outbox → Kafka → `ConfirmOrder` chain. Task 8 makes the inventory side survive that race; this makes the race not happen.

Note `PaymentTimeout` is also passed to the payment provider as `TimeoutSeconds` at `steps.go:103`. That usage must **not** change — only the hold gets the grace.

**Files:**
- Modify: `services/order-svc/internal/workflows/shared.go:10-19`
- Modify: `services/order-svc/internal/workflows/create_order.go:97`
- Test: `services/order-svc/internal/workflows/shared_test.go`

**Interfaces:**
- Consumes: `PaymentTimeout` (pre-existing, `6 * time.Minute`).
- Produces: `ReservationHoldGrace time.Duration` and `reservationExpiry(now time.Time) time.Time` in package `workflows`.

- [ ] **Step 1: Write the failing test**

Create `services/order-svc/internal/workflows/shared_test.go`:

```go
package workflows

import (
	"testing"
	"time"
)

// The inventory hold must outlive the payment window. If it does not, a
// payment completing at the edge of the window races the inventory expiry
// worker, and losing that race means a captured payment with no seat.
func TestReservationExpiry_OutlivesPaymentWindow(t *testing.T) {
	now := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)

	expiry := reservationExpiry(now)
	paymentDeadline := now.Add(PaymentTimeout)

	if !expiry.After(paymentDeadline) {
		t.Fatalf("hold expires at %s but the payment window closes at %s -- the hold must strictly outlive it",
			expiry.Format(time.RFC3339), paymentDeadline.Format(time.RFC3339))
	}
}

// The grace has to be big enough to cover webhook -> outbox -> Kafka ->
// ConfirmOrder, plus one full sweep of the inventory expiry worker (60s).
func TestReservationHoldGrace_CoversWorkerInterval(t *testing.T) {
	const workerInterval = time.Minute

	if ReservationHoldGrace <= workerInterval {
		t.Fatalf("ReservationHoldGrace = %v, must exceed the inventory expiry worker interval of %v",
			ReservationHoldGrace, workerInterval)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

```bash
cd /Users/vogiaan/coding/projects/TicketEventPF/services/order-svc && go test ./internal/workflows/ -run 'TestReservation' -count=1 2>&1 | tail -20
```

Expected: FAIL to compile with `undefined: reservationExpiry` and `undefined: ReservationHoldGrace`.

- [ ] **Step 3: Add the grace constant and the helper**

In `services/order-svc/internal/workflows/shared.go`, add to the existing `const` block, immediately after `PaymentTimeout`:

```go
	// ReservationHoldGrace extends the inventory hold beyond PaymentTimeout.
	// A payment completing at the very edge of the window still has to travel
	// webhook -> outbox -> Kafka -> ConfirmOrder, and inventory's expiry
	// worker sweeps every 60s. Without this slack the worker wins that race
	// and the order ends up paid with no seat behind it.
	ReservationHoldGrace = 3 * time.Minute
```

Then add below the `const` block:

```go
// reservationExpiry returns the instant the inventory hold for an order must
// live until: the full payment window plus the grace that covers the
// post-payment confirmation chain.
func reservationExpiry(now time.Time) time.Time {
	return now.Add(PaymentTimeout + ReservationHoldGrace)
}
```

`time` is already imported in this file.

- [ ] **Step 4: Use the helper in the workflow**

In `services/order-svc/internal/workflows/create_order.go`, replace line 97:

```go
	expAt := util.TimeToISO8601Str(reservationExpiry(workflow.Now(ctx)))
```

- [ ] **Step 5: Run to verify the tests pass**

```bash
cd /Users/vogiaan/coding/projects/TicketEventPF/services/order-svc && go test ./internal/workflows/ -run 'TestReservation' -count=1 -v 2>&1 | tail -20
```

Expected: two PASS lines.

- [ ] **Step 6: Verify order-svc builds and its suite is green**

```bash
cd /Users/vogiaan/coding/projects/TicketEventPF/services/order-svc && go build ./... && go vet ./internal/... && go test ./internal/... ./pkg/... -count=1 2>&1 | tail -20
```

Expected: no build or vet output. Test output shows `ok` or `no test files` per package, no FAIL.

- [ ] **Step 7: Confirm the payment-provider timeout was not changed**

```bash
cd /Users/vogiaan/coding/projects/TicketEventPF/services/order-svc && grep -n "TimeoutSeconds" internal/workflows/steps.go
```

Expected: `TimeoutSeconds: int32(PaymentTimeout.Seconds()),` — still bare `PaymentTimeout`, with no grace added. The user's payment window is unchanged; only the inventory hold got longer.

- [ ] **Step 8: Commit**

```bash
cd /Users/vogiaan/coding/projects/TicketEventPF
git add services/order-svc/internal/workflows/shared.go \
        services/order-svc/internal/workflows/create_order.go \
        services/order-svc/internal/workflows/shared_test.go
git commit -m "fix(order): hold inventory past the payment window

The reserve expiry was set to exactly PaymentTimeout, leaving no room for
webhook -> outbox -> Kafka -> ConfirmOrder while inventory's expiry worker
sweeps every 60s. Adds ReservationHoldGrace (3m) so the hold strictly
outlives the payment window. The payment provider's own TimeoutSeconds is
deliberately unchanged."
```

---

## Task 10: Inventory drift must not be swept under the rug (finding #7)

`reservation.go:343-346` logs a `Warnf` when the guarded decrement matches no rows and then **marks the reservation EXPIRED anyway** — so the one unattended path is also the only one that silently permits `reserved` to drift. `Confirm` (:195) and `Release` (:267) both correctly error on the same condition.

Failing the whole batch would be worse: a single poison row would block every healthy reservation behind it forever. Instead, skip the affected ticket class, leave its reservations `ACTIVE` for investigation, and log at `Error` — which re-surfaces every tick, exactly the repeating alarm you want.

This task also removes a latent fragility: `return totalExpired, s.repo...Transaction(func...)` reads a variable the closure mutates, and the Go spec does not order those two operands. It works under gc today; a named return removes the ambiguity.

**Files:**
- Modify: `services/inventory-svc/internal/services/reservation.go:286-372`
- Test: `services/inventory-svc/internal/services/reservation_expire_test.go`

**Interfaces:**
- Consumes: `service.ErrInventoryDrift` (Task 3); `sortedInt64Keys[V any]` (Task 8).
- Produces: `BatchExpireReservations(ctx, batchSize) (expired int, err error)` — same signature, now with named returns. `expired` counts only reservations actually transitioned to `EXPIRED`.

- [ ] **Step 1: Write the failing tests**

Append to `services/inventory-svc/internal/services/reservation_expire_test.go`:

```go
// A reservation whose ticket class cannot absorb the decrement is corruption.
// Marking it EXPIRED anyway (the old behaviour) destroys the only evidence,
// so leave it ACTIVE and let the error log repeat every tick.
func TestBatchExpire_Drift_LeavesReservationActive(t *testing.T) {
	svc, repo := reserveSvc(t)
	tc := seedTicketClass(t, repo, 100, 0, 0) // reserved = 0

	// Bypass Reserve so the reservation claims stock the counter never got.
	r := models.Reservation{
		OrderCode:     "o-drift",
		TicketClassID: tc.ID,
		Qty:           5,
		Status:        models.ReservationStatusActive,
		ExpiresAt:     time.Now().UTC().Add(-1 * time.Minute),
	}
	if err := repo.Create(context.Background(), &r); err != nil {
		t.Fatalf("seed reservation: %v", err)
	}

	n, err := svc.BatchExpireReservations(context.Background(), 500)
	if err != nil {
		t.Fatalf("BatchExpireReservations should skip drift, not fail the batch: %v", err)
	}
	if n != 0 {
		t.Fatalf("expired count = %d, want 0", n)
	}

	var got models.Reservation
	repo.WithContext(context.Background()).Where("order_code = ?", "o-drift").First(&got)
	if got.Status != models.ReservationStatusActive {
		t.Fatalf("status = %s, want ACTIVE (drift must stay visible)", got.Status)
	}
	if tcNow := ticketClassByID(t, repo, tc.ID); tcNow.Reserved != 0 {
		t.Fatalf("reserved = %d, want 0 (unchanged)", tcNow.Reserved)
	}
}

// One poison ticket class must not block healthy reservations in the same
// batch -- otherwise the worker stalls forever behind a single bad row.
func TestBatchExpire_Drift_DoesNotBlockHealthyRows(t *testing.T) {
	svc, repo := reserveSvc(t)
	bad := seedTicketClass(t, repo, 100, 0, 0)
	good := seedTicketClass(t, repo, 100, 0, 0)

	poison := models.Reservation{
		OrderCode:     "o-drift-2",
		TicketClassID: bad.ID,
		Qty:           5,
		Status:        models.ReservationStatusActive,
		ExpiresAt:     time.Now().UTC().Add(-1 * time.Minute),
	}
	if err := repo.Create(context.Background(), &poison); err != nil {
		t.Fatalf("seed poison reservation: %v", err)
	}
	must(t, svc.Reserve(context.Background(), ReserveInput{
		OrderCode: "o-healthy", ExpiresAt: time.Now().UTC().Add(-1 * time.Minute),
		Items: []ReserveItem{{TicketClassID: good.ID, Qty: 3}},
	}))

	n, err := svc.BatchExpireReservations(context.Background(), 500)
	if err != nil {
		t.Fatalf("BatchExpireReservations: %v", err)
	}
	if n != 1 {
		t.Fatalf("expired count = %d, want 1 (the healthy row only)", n)
	}
	if got := ticketClassByID(t, repo, good.ID); got.Reserved != 0 {
		t.Fatalf("healthy reserved = %d, want 0", got.Reserved)
	}
	if got := ticketClassByID(t, repo, bad.ID); got.Reserved != 0 {
		t.Fatalf("poison reserved = %d, want 0 (untouched)", got.Reserved)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

```bash
cd /Users/vogiaan/coding/projects/TicketEventPF/services/inventory-svc && go test ./internal/services/ -run 'TestBatchExpire_Drift' -count=1 -v 2>&1 | tail -20
```

Expected: `TestBatchExpire_Drift_LeavesReservationActive` FAILs with `expired count = 1, want 0` and `status = EXPIRED, want ACTIVE`.

- [ ] **Step 3: Rewrite `BatchExpireReservations`**

Replace the entire method in `services/inventory-svc/internal/services/reservation.go`:

```go
func (s implReservationService) BatchExpireReservations(ctx context.Context, batchSize int) (expired int, err error) {
	if batchSize <= 0 || batchSize > 1000 {
		batchSize = 500 // Default batch size
	}

	err = s.repo.GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		now := time.Now().UTC()

		var rs []models.Reservation
		if err := tx.Clauses(clause.Locking{
			Strength: "UPDATE",
			Options:  "SKIP LOCKED",
		}).
			Select("id", "ticket_class_id", "qty", "expires_at").
			Where("status = ? AND expires_at <= ?", models.ReservationStatusActive, now).
			Order("expires_at").
			Limit(batchSize).
			Find(&rs).Error; err != nil {
			s.l.Errorf(ctx, "service.reservation.BatchExpireReservations.LockReservations: %v", err)
			return err
		}

		if len(rs) == 0 {
			return nil
		}

		qtyByTC := make(map[int64]int, len(rs))
		for _, r := range rs {
			qtyByTC[r.TicketClassID] += r.Qty
		}

		// Ticket classes whose counter could not absorb the decrement. Their
		// reservations stay ACTIVE so the drift stays visible instead of
		// being erased, and so one bad row cannot stall the whole batch.
		drifted := make(map[int64]bool)

		for _, tcID := range sortedInt64Keys(qtyByTC) {
			totalQty := qtyByTC[tcID]
			result := tx.Model(&models.TicketClass{}).
				Where("id = ? AND reserved >= ?", tcID, totalQty).
				Update("reserved", gorm.Expr("reserved - ?", totalQty))

			if result.Error != nil {
				s.l.Errorf(ctx, "service.reservation.BatchExpireReservations.DecrementReserved: ticket_class_id=%d, qty=%d: %v",
					tcID, totalQty, result.Error)
				return result.Error
			}
			if result.RowsAffected == 0 {
				s.l.Errorf(ctx, "service.reservation.BatchExpireReservations: %v: reserved is below the %d held by expiring reservations on ticket_class_id=%d; leaving them ACTIVE for investigation",
					ErrInventoryDrift, totalQty, tcID)
				drifted[tcID] = true
				continue
			}
		}

		expiredIDs := make([]int64, 0, len(rs))
		for _, r := range rs {
			if drifted[r.TicketClassID] {
				continue
			}
			expiredIDs = append(expiredIDs, r.ID)
		}
		if len(expiredIDs) == 0 {
			return nil
		}

		if err := tx.Model(&models.Reservation{}).
			Where("id IN ?", expiredIDs).
			Update("status", models.ReservationStatusExpired).Error; err != nil {
			s.l.Errorf(ctx, "service.reservation.BatchExpireReservations.UpdateStatus: %v", err)
			return err
		}

		expired = len(expiredIDs)
		s.l.Infof(ctx, "service.reservation.BatchExpireReservations: expired %d reservations across %d ticket classes (%d skipped for drift)",
			expired, len(qtyByTC)-len(drifted), len(drifted))
		return nil
	})

	return expired, err
}
```

Two behaviour notes beyond the drift handling: the per-tick `no expired reservations found` and `found N expired` info logs are gone (they fired every 60s forever and said nothing when idle), and the dead `// Step 5: Publish Kafka events (TODO)` comment block is removed — no Kafka producer exists in this service and nothing in the plan of record calls for one here.

- [ ] **Step 4: Run to verify the tests pass**

```bash
cd /Users/vogiaan/coding/projects/TicketEventPF/services/inventory-svc && go test ./internal/services/ -run 'TestBatchExpire' -count=1 -v 2>&1 | tail -20
```

Expected: three PASS lines — the two new drift tests plus the pre-existing `TestBatchExpire_ReleasesExpiredHolds`.

- [ ] **Step 5: Run the whole suite, including the worker package**

```bash
cd /Users/vogiaan/coding/projects/TicketEventPF/services/inventory-svc && go build ./... && go vet ./internal/... ./pkg/... ./config/... && make test 2>&1 | tail -20
```

Expected: `ok` for both packages. `TestRunJob_DrainsUntilBatchNotFull` in `internal/workers` uses a stub and is unaffected by the signature's named returns.

- [ ] **Step 6: Commit**

```bash
cd /Users/vogiaan/coding/projects/TicketEventPF
git add services/inventory-svc/internal/services/reservation.go \
        services/inventory-svc/internal/services/reservation_expire_test.go
git commit -m "fix(inventory): stop the expiry worker from erasing counter drift

When the guarded decrement matched no rows the worker logged a warning and
marked the reservation EXPIRED anyway -- the one unattended path was also
the only one that silently permitted drift. Affected ticket classes are now
skipped, their reservations stay ACTIVE, and the error log repeats each
tick as a standing alarm; healthy rows in the same batch still expire.

Also converts the return to named results: the old
'return totalExpired, Transaction(func...)' read a variable the closure
mutates, an operand ordering the Go spec does not define."
```

---

## Task 11: Panic recovery interceptor (finding #5)

`main.go:77-79` registers only the logging interceptor. grpc-go does **not** recover handler panics — one nil-map dereference in a presenter takes down the whole inventory service and every in-flight reservation with it. There is no dependency for this in `go.mod` and adding one would churn `vendor/`, so it is hand-rolled in ~25 lines.

**Files:**
- Create: `services/inventory-svc/internal/interceptors/grpc_recovery.go`
- Create: `services/inventory-svc/internal/interceptors/grpc_recovery_test.go`
- Modify: `services/inventory-svc/cmd/api/main.go:77-79`

**Interfaces:**
- Consumes: `logger.Logger` (pre-existing).
- Produces: `interceptors.GrpcRecoveryInterceptor(l logger.Logger) grpc.UnaryServerInterceptor`.

- [ ] **Step 1: Write the failing test**

Create `services/inventory-svc/internal/interceptors/grpc_recovery_test.go`:

```go
package interceptors

import (
	"context"
	"testing"

	pkgLog "github.com/vogiaan/ticketbottle-inventory/pkg/logger"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func testLogger() pkgLog.Logger {
	return pkgLog.InitializeZapLogger(pkgLog.ZapConfig{
		Level: "error", Mode: "development", Encoding: "console",
	})
}

func TestGrpcRecovery_ConvertsPanicToInternal(t *testing.T) {
	interceptor := GrpcRecoveryInterceptor(testLogger())
	info := &grpc.UnaryServerInfo{FullMethod: "/inventory.InventoryService/Reserve"}

	resp, err := interceptor(context.Background(), nil, info,
		func(ctx context.Context, req any) (any, error) {
			var m map[string]string
			m["boom"] = "nil map write" // panics
			return nil, nil
		})

	if resp != nil {
		t.Fatalf("resp = %v, want nil after a panic", resp)
	}
	if err == nil {
		t.Fatal("err = nil, want an Internal status error")
	}
	if st, _ := status.FromError(err); st.Code() != codes.Internal {
		t.Fatalf("code = %s, want Internal", st.Code())
	}
}

func TestGrpcRecovery_PassesThroughNormalCalls(t *testing.T) {
	interceptor := GrpcRecoveryInterceptor(testLogger())
	info := &grpc.UnaryServerInfo{FullMethod: "/inventory.InventoryService/Confirm"}

	resp, err := interceptor(context.Background(), "req", info,
		func(ctx context.Context, req any) (any, error) {
			return "ok", nil
		})

	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if resp != "ok" {
		t.Fatalf("resp = %v, want \"ok\"", resp)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

```bash
cd /Users/vogiaan/coding/projects/TicketEventPF/services/inventory-svc && go test ./internal/interceptors/ -count=1 2>&1 | tail -20
```

Expected: FAIL to compile with `undefined: GrpcRecoveryInterceptor`.

- [ ] **Step 3: Write the interceptor**

Create `services/inventory-svc/internal/interceptors/grpc_recovery.go`:

```go
package interceptors

import (
	"context"
	"runtime/debug"

	"github.com/vogiaan/ticketbottle-inventory/pkg/logger"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// GrpcRecoveryInterceptor turns a panic inside a handler into an Internal
// status error instead of letting it unwind into the runtime.
//
// grpc-go does not recover handler panics on its own: without this, a single
// nil dereference in one RPC kills the process and takes every in-flight
// reservation transaction with it. This must be the outermost interceptor so
// it also covers panics raised by the ones inside it.
func GrpcRecoveryInterceptor(l logger.Logger) grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		req any,
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (resp any, err error) {
		defer func() {
			if r := recover(); r != nil {
				l.Errorf(ctx, "interceptors.GrpcRecovery: panic in %s: %v\n%s", info.FullMethod, r, debug.Stack())
				resp = nil
				err = status.Error(codes.Internal, "internal server error")
			}
		}()

		return handler(ctx, req)
	}
}
```

- [ ] **Step 4: Run to verify the tests pass**

```bash
cd /Users/vogiaan/coding/projects/TicketEventPF/services/inventory-svc && go test ./internal/interceptors/ -count=1 -v 2>&1 | tail -20
```

Expected: two PASS lines.

- [ ] **Step 5: Chain it in `main.go`**

In `services/inventory-svc/cmd/api/main.go`, replace the `grpc.NewServer(...)` call:

```go
	grpcSvr := grpc.NewServer(
		// Recovery must be outermost so it also catches panics raised inside
		// the interceptors that follow it.
		grpc.ChainUnaryInterceptor(
			interceptors.GrpcRecoveryInterceptor(l),
			interceptors.GrpcLoggingInterceptor(l),
		),
	)
```

- [ ] **Step 6: Verify build and the full suite**

```bash
cd /Users/vogiaan/coding/projects/TicketEventPF/services/inventory-svc && go build ./... && go vet ./internal/... ./pkg/... ./config/... && go test ./internal/... -race -count=1 2>&1 | tail -20
```

Expected: no build or vet output; `ok` for `internal/interceptors`, `internal/services`, and `internal/workers`.

- [ ] **Step 7: Commit**

```bash
cd /Users/vogiaan/coding/projects/TicketEventPF
git add services/inventory-svc/internal/interceptors/grpc_recovery.go \
        services/inventory-svc/internal/interceptors/grpc_recovery_test.go \
        services/inventory-svc/cmd/api/main.go
git commit -m "feat(inventory): recover handler panics instead of crashing the process

grpc-go does not recover panics in handlers, so one nil dereference took
down the whole service and every in-flight reservation with it. Hand-rolled
to avoid a new module dependency (vendor/ is committed); chained outermost
so it also covers the logging interceptor."
```

---

## Task 12: Update service documentation and verify end to end

Ten behaviours changed. `services/inventory-svc/CLAUDE.md` documents several of them as they used to be — notably the idempotency contract and the confirm rules — and a stale guidance file is worse than none.

**Files:**
- Modify: `services/inventory-svc/CLAUDE.md`

**Interfaces:**
- Consumes: everything from Tasks 1-11.
- Produces: nothing.

- [ ] **Step 1: Update the Role and flow sections**

In `services/inventory-svc/CLAUDE.md`, replace the "Three-step reservation flow" section's trailing paragraph (from "Keyed by **order code**." to the end of that section) with:

```markdown
Keyed by **order code**. A background `ReservationExpiryWorker` (`internal/workers/`) auto-releases holds that expire, so the Order saga's compensation and the worker can both free inventory safely. `Reserve` runs as a **single transaction**: it locks all target `ticket_class` rows in **ascending id order** (deadlock-free), validates sale eligibility and availability, increments `reserved`, and batch-inserts the reservation rows — all-or-nothing.

**Sale eligibility** is enforced inside that locked transaction: the ticket class must be `ACTIVE` and `now` must fall within `[sale_start_at, sale_end_at]` (either bound may be null). Violations return `ErrSaleClosed` → gRPC `FailedPrecondition`. `CheckAvailability` applies the same rule.
```

- [ ] **Step 2: Rewrite the Idempotency section**

Replace the whole `### Idempotency` section with:

```markdown
### Idempotency

`Reserve`/`Confirm`/`Release` key off `order_code`, and idempotency is scoped **by reservation status** — an order code alone is not enough to decide.

- **`Reserve`** — an order with any `ACTIVE` or `CONFIRMED` row is a retry of a call that already succeeded: no-op, success. An order whose rows are **all terminal** (`EXPIRED`/`CANCELLED`) returns `ErrStateConflict`; returning success there would hand the caller an order holding zero inventory.
- **`Confirm`** — all-`CONFIRMED` is a no-op. An `ACTIVE` row **past its `expires_at` is still confirmed**: it continues to hold its `reserved` quantity until the worker sweeps it, so the reserved→sold move is safe and refusing it stranded paid orders. An `EXPIRED` row (already swept) is **re-acquired** from free stock; only if the stock is genuinely gone does it return `ErrStateConflict`, which the caller must treat as refund-required.
- **`Release`** — an unknown order code or all-terminal rows are a no-op; a `CONFIRMED` row returns `ErrStateConflict`.

Order-svc sets the hold to `PaymentTimeout + ReservationHoldGrace` (`internal/workflows/shared.go`) so the hold strictly outlives the payment window — the expiry worker must never win that race.
```

- [ ] **Step 3: Document the constraints and error contract**

Replace the paragraph beginning "On boot `main.go` runs GORM `AutoMigrate`" with:

```markdown
On boot `main.go` runs GORM `AutoMigrate` for `TicketClass` and `Reservation`, then applies `models.PostMigrateStatements()` (`internal/models/ddl.go`) — the single source of DDL that AutoMigrate cannot express. That is the partial index `idx_reservation_active_expiry` plus three `CHECK` constraints:

- `chk_ticket_class_capacity` — `reserved >= 0 AND sold >= 0 AND reserved + sold <= total`
- `chk_ticket_class_total_nonneg` — `total >= 0`
- `chk_reservation_qty_positive` — `qty > 0`

They are added `NOT VALID`: enforced on every new write, but historical rows are not scanned, so pre-existing drift cannot fail a boot. Validate deliberately, out of band, once drift is known clean: `ALTER TABLE ticket_class VALIDATE CONSTRAINT chk_ticket_class_capacity;`

All of this is still interim — versioned migrations are the target. Default Postgres is on **5435** (see `config/config.go`).
```

Then append to the `## Conventions` section:

```markdown
- **Errors crossing the service boundary are domain errors**, never GORM sentinels: `ErrInsufficientStock`, `ErrNotFound`, `ErrStateConflict`, `ErrSaleClosed`, `ErrInventoryDrift` (`internal/services/errors.go`). `internal/delivery/grpc/errors.go` maps them to gRPC codes. Returning `gorm.ErrInvalidData` to mean "sold out" is how this service used to mistranslate unrelated driver failures into `ResourceExhausted`.
- **`ErrInventoryDrift` means corruption, not a user error** — `reserved` is lower than the holds claiming it, which can only happen if something wrote a quantity outside a locked transaction. The expiry worker skips drifted ticket classes and leaves their reservations `ACTIVE` so the evidence survives and the error log repeats every tick.
- **Never write `reserved` or `sold` from the ticket-class CRUD path.** `updateColumns` deliberately cannot express them; `Update` locks `FOR UPDATE` and refuses to shrink `total` below `reserved + sold`.
- **Tests need a live Postgres.** `make test` starts from `docker-compose.dev.yml` (container `ticketbottle-inventory`, port 5435) and creates `ticketbottle_inventory_test`. The harness skips locally when the DB is unreachable but **fails** when `CI` is set — a suite that skips itself reports PASS having asserted nothing.
```

- [ ] **Step 4: Run the complete verification sweep**

```bash
cd /Users/vogiaan/coding/projects/TicketEventPF/services/inventory-svc && \
  go build ./... && \
  go vet ./internal/... ./pkg/... ./config/... && \
  make test 2>&1 | tail -20
```

Expected: no build or vet output; `ok` for `internal/interceptors`, `internal/services`, `internal/workers`. Zero SKIP lines.

```bash
cd /Users/vogiaan/coding/projects/TicketEventPF/services/order-svc && \
  go build ./... && \
  go vet ./internal/... && \
  go test ./internal/... ./pkg/... -count=1 2>&1 | tail -20
```

Expected: no build or vet output; no FAIL.

- [ ] **Step 5: Commit**

```bash
cd /Users/vogiaan/coding/projects/TicketEventPF
git add services/inventory-svc/CLAUDE.md
git commit -m "docs(inventory): document the new idempotency, sale-window and drift rules

Ten behaviours changed across this branch and CLAUDE.md still described the
old ones -- notably that Confirm rejects any past-expiry hold, which is the
bug the branch fixes."
```

- [ ] **Step 6: End-to-end acceptance (gate1)**

The purchase flow now spans changes in two services, so the local acceptance test is the real check. This is slow — it builds and kind-loads eight images, budget **20-30 minutes**.

```bash
cd /Users/vogiaan/coding/projects/TicketEventPF
make -C deploy cluster-up
make -C deploy infra-up
make -C deploy apps-up
make -C deploy gate1
```

Expected: `gate1` reports the full purchase flow passing.

**If the cluster cannot come up** (Docker resources, an image build failure, an unrelated pre-existing break): **report that plainly, with the actual command output, and do not claim the plan is verified.** Unit tests passing is not the same as gate1 passing, and saying otherwise is worse than saying it did not run. Note what failed and stop.

- [ ] **Step 7: Tear down and report**

```bash
make -C deploy cluster-down
cd /Users/vogiaan/coding/projects/TicketEventPF && git log --oneline docs/aws-mac-offload-plan..fix/inventory-correctness
```

Expected: 12 commits, one per task.

Report: which findings are closed, the final test output, and whether gate1 ran and passed.

---

## Findings Coverage

| # | Finding | Task |
|---|---|---|
| 1 | `UpdateTicketClass` lost update on `reserved`/`sold` | 4 |
| 2 | `UpdateTicketClass` full-overwrite semantics | 4 |
| 3 | No DB CHECK constraints | 2 |
| 4 | Confirm-after-expiry: payment captured, no tickets | 8 (inventory) + 9 (order-svc) |
| 5 | No panic-recovery interceptor | 11 |
| 6 | `Reserve` idempotency is status-blind | 6 |
| 7 | `BatchExpire` silently marks EXPIRED on guard failure | 10 |
| 8 | `CheckAvailability` duplicate-id mishandling | 5 |
| 9 | `status` / sale window never enforced | 7 |
| 10 | GORM sentinels used as domain errors | 3 |
| — | Test suite skips itself into a green run (#17, pulled forward) | 1 |
| — | `BatchExpireReservations` return-operand ordering | 10 |
| — | Double-logging + `==` instead of `errors.Is` in `ticketclass.go` (#19) | 3 |

**Explicitly out of scope** (P2, deferred to a follow-up plan): hot-row throughput ceiling (#11), versioned migrations (#12), health checks / metrics / tracing (#13), shutdown sequencing (#14), worker per-batch timeout (#15), Dockerfile hardening (#16), testcontainers (#17), proto `package event` rename and Timestamp fields (#18), dead code removal in `pkg/gorm` (#19), inventory ledger and reconciliation job (#20).
