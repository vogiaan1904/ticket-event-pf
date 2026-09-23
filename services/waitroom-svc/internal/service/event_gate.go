package service

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/vogiaan1904/ticketbottle-waitroom/pkg/util"
	"github.com/vogiaan1904/ticketbottle-waitroom/protogen/event"
	"golang.org/x/sync/singleflight"
)

// EventInfo is everything the waitroom needs to know about an event to admit
// someone to its queue.
type EventInfo struct {
	AllowWaitRoom bool
	SaleStartAt   time.Time
}

// eventGate answers whether an event accepts queuers, from cache.
//
// Every joiner asks the same question about the same handful of events, and an
// on-sale asks it hundreds of thousands of times in seconds. Without this,
// JoinQueue is a load generator pointed at event-svc.
type eventGate struct {
	eSvc event.EventServiceClient
	ttl  time.Duration

	mu      sync.RWMutex
	entries map[string]eventGateEntry
	group   singleflight.Group
}

// A cached verdict. `err` holds a decision about the event itself -- not found,
// no config -- which is as cacheable as a success. Dependency failures are not
// stored: the next caller must be free to retry.
type eventGateEntry struct {
	info      *EventInfo
	err       error
	expiresAt time.Time
}

func newEventGate(eSvc event.EventServiceClient, ttl time.Duration) *eventGate {
	return &eventGate{
		eSvc:    eSvc,
		ttl:     ttl,
		entries: make(map[string]eventGateEntry),
	}
}

// Get returns the event's queueing rules, fetching them at most once per TTL.
func (g *eventGate) Get(ctx context.Context, eID string) (*EventInfo, error) {
	if e, ok := g.lookup(eID); ok {
		return e.info, e.err
	}

	// Concurrent misses collapse into one call. A TTL cache alone does not help
	// at an on-sale: the first instant is a cold miss for every joiner at once,
	// which is the stampede it was meant to prevent.
	v, err, _ := g.group.Do(eID, func() (any, error) {
		// Detached: every waiter collapsed behind this call shares its result, so
		// one joiner hanging up must not fail the rest.
		fetchCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), g.fetchTimeout())
		defer cancel()

		info, verdict := g.fetch(fetchCtx, eID)
		if !isEventVerdict(verdict) {
			// The dependency failed; say so without remembering it.
			return nil, verdict
		}
		g.store(eID, eventGateEntry{info: info, err: verdict, expiresAt: time.Now().Add(g.ttl)})
		return info, verdict
	})
	if err != nil {
		return nil, err
	}

	info, _ := v.(*EventInfo)
	return info, nil
}

func (g *eventGate) lookup(eID string) (eventGateEntry, bool) {
	g.mu.RLock()
	defer g.mu.RUnlock()

	e, ok := g.entries[eID]
	if !ok || time.Now().After(e.expiresAt) {
		return eventGateEntry{}, false
	}
	return e, true
}

func (g *eventGate) store(eID string, e eventGateEntry) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.entries[eID] = e
}

// fetch asks event-svc the two questions JoinQueue has always asked.
func (g *eventGate) fetch(ctx context.Context, eID string) (*EventInfo, error) {
	out, err := g.eSvc.FindOne(ctx, &event.FindOneEventRequest{Id: eID})
	if err != nil {
		return nil, eventServiceError(err, ErrEventNotFound)
	}
	if out.Event == nil {
		return nil, ErrEventNotFound
	}

	cfgOut, err := g.eSvc.GetConfig(ctx, &event.GetEventConfigRequest{EventId: eID})
	if err != nil {
		return nil, eventServiceError(err, ErrEventConfigNotFound)
	}
	if cfgOut.EventConfig == nil {
		return nil, ErrEventConfigNotFound
	}

	// An unparseable sale start leaves the zero time, which orders every joiner
	// by arrival -- the behaviour before a draw existed.
	saleStart, _ := util.ParseISO8601(cfgOut.EventConfig.TicketSaleStartDate)

	return &EventInfo{
		AllowWaitRoom: cfgOut.EventConfig.AllowWaitRoom,
		SaleStartAt:   saleStart,
	}, nil
}

// isEventVerdict reports whether err is a decision about the event rather than a
// failure to reach the service that holds it. Only the former may be cached.
func isEventVerdict(err error) bool {
	return err == nil ||
		errors.Is(err, ErrEventNotFound) ||
		errors.Is(err, ErrEventConfigNotFound)
}

// fetchTimeout bounds the shared fetch. It is not the caller's deadline: the
// call outlives whichever joiner happened to trigger it.
func (g *eventGate) fetchTimeout() time.Duration {
	return 5 * time.Second
}
