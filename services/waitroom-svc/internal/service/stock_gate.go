package service

import (
	"context"
	"sync"
	"time"

	"github.com/vogiaan1904/ticketbottle-waitroom/internal/metrics"
	"github.com/vogiaan1904/ticketbottle-waitroom/pkg/logger"
	"github.com/vogiaan1904/ticketbottle-waitroom/protogen/inventory"
	"golang.org/x/sync/singleflight"
)

// Door is what an event's door does until the next answer from inventory.
type Door int

const (
	DoorOpen Door = iota
	DoorPaused
	DoorSoldOut
)

func (d Door) String() string {
	switch d {
	case DoorPaused:
		return "paused"
	case DoorSoldOut:
		return "sold_out"
	default:
		return "open"
	}
}

// Stock is one answer about an event's tickets. Counted is false when inventory gave
// nothing to judge by: it did not answer, or no class can still sell.
type Stock struct {
	Door      Door
	Available int64
	Counted   bool
}

// stockFetchTimeout bounds one question to inventory; past it the door fails open.
const stockFetchTimeout = time.Second

// StockGate answers whether an event has tickets left, from a short cache.
//
// The tick, every join and every status poll ask the same question; one answer per
// event per TTL serves them all. It fails open: an unanswered question reads as
// DoorOpen. See docs/design/admission-sizing.md#when-tickets-run-out.
type StockGate struct {
	inv inventory.InventoryServiceClient
	ttl time.Duration
	l   logger.Logger

	mu      sync.RWMutex
	entries map[string]stockGateEntry
	group   singleflight.Group
}

type stockGateEntry struct {
	stock     Stock
	expiresAt time.Time
}

func NewStockGate(inv inventory.InventoryServiceClient, ttl time.Duration, l logger.Logger) *StockGate {
	return &StockGate{inv: inv, ttl: ttl, l: l, entries: make(map[string]stockGateEntry)}
}

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

// doorFor turns an event's counts into what its door does.
//
//	no class can still sell -> open: nothing to judge by
//	sold == total           -> sold out
//	nothing available now   -> paused: all held, or the rest not on sale yet
//	otherwise               -> open
func doorFor(total, sold, available int64) Door {
	switch {
	case total <= 0:
		return DoorOpen
	case sold >= total:
		return DoorSoldOut
	case available <= 0:
		return DoorPaused
	default:
		return DoorOpen
	}
}
