package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Measures Reserve against a single hot ticket class, the shape the oversell
// guard serialises on. Numbers and method:
// docs/plans/2026-09-22-inventory-contention-benchmark.md.
const (
	contentionOps      = 2000      // reserve attempts per run
	contentionCapacity = 1_000_000 // large enough that a run never sells out
	soldOutCapacity    = 500       // small enough that most attempts lose
)

func contentionWorkers() []int { return []int{1, 4, 16} }

type runResult struct {
	workers   int
	elapsed   time.Duration
	succeeded int64
	soldOut   int64
}

func (r runResult) perSec() float64 {
	return float64(r.succeeded) / r.elapsed.Seconds()
}

// driveReserve runs `ops` single-ticket reserves across `workers` goroutines
// against one ticket class, and returns how long they took.
func driveReserve(t testing.TB, svc ReservationService, tcID int64, workers, ops int, tag string) runResult {
	t.Helper()

	var next, succeeded, soldOut int64
	var wg sync.WaitGroup
	expires := time.Now().UTC().Add(15 * time.Minute)

	start := time.Now()
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				i := atomic.AddInt64(&next, 1) - 1
				if i >= int64(ops) {
					return
				}
				// Unique per attempt: a repeated order_code takes the
				// idempotent no-op path and would not touch the hot row.
				err := svc.Reserve(context.Background(), ReserveInput{
					OrderCode: fmt.Sprintf("bench-%s-%d-%d", tag, workers, i),
					ExpiresAt: expires,
					Items:     []ReserveItem{{TicketClassID: tcID, Qty: 1}},
				})
				switch {
				case err == nil:
					atomic.AddInt64(&succeeded, 1)
				case errors.Is(err, ErrInsufficientStock):
					atomic.AddInt64(&soldOut, 1)
				default:
					t.Errorf("Reserve: unexpected error: %v", err)
					return
				}
			}
		}()
	}
	wg.Wait()

	return runResult{
		workers:   workers,
		elapsed:   time.Since(start),
		succeeded: atomic.LoadInt64(&succeeded),
		soldOut:   atomic.LoadInt64(&soldOut),
	}
}

// TestReserveThroughput_OneHotClass reports reserves/sec at fixed worker counts.
//
// Gated behind INVENTORY_BENCH: it is a measurement, not an assertion, and CI
// runs the suite under -race, where the numbers would mean nothing.
func TestReserveThroughput_OneHotClass(t *testing.T) {
	if os.Getenv("INVENTORY_BENCH") == "" {
		t.Skip("set INVENTORY_BENCH=1 to run the contention measurement")
	}

	repo := newTestDB(t)
	svc := NewReservationService(newTestLogger(), repo)

	results := make([]runResult, 0, len(contentionWorkers()))
	for _, workers := range contentionWorkers() {
		tc := seedTicketClass(t, repo, contentionCapacity, 0, 0)
		r := driveReserve(t, svc, tc.ID, workers, contentionOps, "tput")

		// The invariant, asserted under the contention just measured: every
		// attempt that reported success is on the row exactly once, and the
		// row never exceeds its own capacity.
		got := ticketClassByID(t, repo, tc.ID)
		if int64(got.Reserved) != r.succeeded {
			t.Fatalf("workers=%d: reserved=%d but %d calls reported success", workers, got.Reserved, r.succeeded)
		}
		if got.Reserved+got.Sold > got.Total {
			t.Fatalf("workers=%d: OVERSELL reserved=%d sold=%d total=%d", workers, got.Reserved, got.Sold, got.Total)
		}
		results = append(results, r)
	}

	t.Log("")
	t.Logf("  reserve throughput, one hot ticket class, %d ops per run", contentionOps)
	t.Logf("  %-9s %-12s %-14s %s", "workers", "elapsed", "reserves/sec", "vs 1 worker")
	base := results[0].perSec()
	for _, r := range results {
		t.Logf("  %-9d %-12s %-14.0f %.2fx",
			r.workers, r.elapsed.Round(time.Millisecond), r.perSec(), r.perSec()/base)
	}
	t.Log("")
}

// TestReserveUnderContention_SellsOutExactly drives more attempts than there is
// stock and asserts the row lands exactly on its capacity: no oversell, and no
// capacity stranded by a loser that decremented nothing.
func TestReserveUnderContention_SellsOutExactly(t *testing.T) {
	if os.Getenv("INVENTORY_BENCH") == "" {
		t.Skip("set INVENTORY_BENCH=1 to run the contention measurement")
	}

	repo := newTestDB(t)
	svc := NewReservationService(newTestLogger(), repo)
	tc := seedTicketClass(t, repo, soldOutCapacity, 0, 0)

	r := driveReserve(t, svc, tc.ID, 16, soldOutCapacity*3, "soldout")

	got := ticketClassByID(t, repo, tc.ID)
	if got.Reserved != soldOutCapacity {
		t.Fatalf("reserved = %d, want exactly %d", got.Reserved, soldOutCapacity)
	}
	if int64(got.Reserved) != r.succeeded {
		t.Fatalf("reserved=%d but %d calls reported success", got.Reserved, r.succeeded)
	}
	t.Logf("  %d of %d attempts won, %d correctly refused, capacity landed exactly on %d",
		r.succeeded, soldOutCapacity*3, r.soldOut, got.Total)
}

// TestReserveThroughput_SpreadAcrossClasses answers whether more rows buys more
// throughput. Same worker count, same total ops, spread over N ticket classes
// instead of one.
//
// scales with N -> the row lock is the wall, and sharding a hot class would work
// flat in N     -> the wall is per-transaction (WAL/fsync), which every shard
//
//	pays too, and sharding buys nothing
func TestReserveThroughput_SpreadAcrossClasses(t *testing.T) {
	if os.Getenv("INVENTORY_BENCH") == "" {
		t.Skip("set INVENTORY_BENCH=1 to run the contention measurement")
	}

	repo := newTestDB(t)
	svc := NewReservationService(newTestLogger(), repo)
	const workers = 16

	t.Log("")
	t.Logf("  %d workers, %d ops, spread over N ticket classes", workers, contentionOps)
	t.Logf("  %-9s %-12s %-14s %s", "classes", "elapsed", "reserves/sec", "vs 1 class")
	var base float64
	for _, classes := range []int{1, 2, 4, 8} {
		ids := make([]int64, classes)
		for i := range ids {
			ids[i] = seedTicketClass(t, repo, contentionCapacity, 0, 0).ID
		}

		var next, ok int64
		var wg sync.WaitGroup
		expires := time.Now().UTC().Add(15 * time.Minute)
		start := time.Now()
		for w := 0; w < workers; w++ {
			wg.Add(1)
			go func(w int) {
				defer wg.Done()
				for {
					i := atomic.AddInt64(&next, 1) - 1
					if i >= int64(contentionOps) {
						return
					}
					// Worker-pinned, not per-op round robin: a shard scheme
					// hands one buyer one row, and that is what this measures.
					err := svc.Reserve(context.Background(), ReserveInput{
						OrderCode: fmt.Sprintf("spread-%d-%d", classes, i),
						ExpiresAt: expires,
						Items:     []ReserveItem{{TicketClassID: ids[w%classes], Qty: 1}},
					})
					if err != nil {
						t.Errorf("Reserve: %v", err)
						return
					}
					atomic.AddInt64(&ok, 1)
				}
			}(w)
		}
		wg.Wait()
		elapsed := time.Since(start)

		var total int64
		for _, id := range ids {
			tc := ticketClassByID(t, repo, id)
			if tc.Reserved+tc.Sold > tc.Total {
				t.Fatalf("classes=%d: OVERSELL on %d", classes, id)
			}
			total += int64(tc.Reserved)
		}
		if total != ok {
			t.Fatalf("classes=%d: rows hold %d but %d calls succeeded", classes, total, ok)
		}

		rate := float64(ok) / elapsed.Seconds()
		if classes == 1 {
			base = rate
		}
		t.Logf("  %-9d %-12s %-14.0f %.2fx", classes, elapsed.Round(time.Millisecond), rate, rate/base)
	}
	t.Log("")
}
