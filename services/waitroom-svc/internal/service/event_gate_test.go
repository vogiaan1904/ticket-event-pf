package service

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/vogiaan1904/ticketbottle-waitroom/protogen/event"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Only the two calls the gate makes are implemented; the embedded interface
// turns any other call into a nil-pointer panic, which is the report we want.
type fakeEventClient struct {
	event.EventServiceClient

	findOnes atomic.Int32
	configs  atomic.Int32

	findOneErr error
	configErr  error
	nilEvent   bool
	nilConfig  bool

	allowWaitRoom bool
	saleStart     string

	// block, when set, holds every call until it is closed.
	block chan struct{}
	// ctxErrSeen records what the fetch's own context reported once released.
	ctxErrSeen error
	mu         sync.Mutex
}

func (f *fakeEventClient) FindOne(ctx context.Context, _ *event.FindOneEventRequest, _ ...grpc.CallOption) (*event.FindOneEventResponse, error) {
	f.findOnes.Add(1)
	if f.block != nil {
		<-f.block
		f.mu.Lock()
		f.ctxErrSeen = ctx.Err()
		f.mu.Unlock()
	}
	if f.findOneErr != nil {
		return nil, f.findOneErr
	}
	if f.nilEvent {
		return &event.FindOneEventResponse{}, nil
	}
	return &event.FindOneEventResponse{Event: &event.Event{Id: "e-1"}}, nil
}

func (f *fakeEventClient) GetConfig(_ context.Context, _ *event.GetEventConfigRequest, _ ...grpc.CallOption) (*event.GetEventConfigResponse, error) {
	f.configs.Add(1)
	if f.configErr != nil {
		return nil, f.configErr
	}
	if f.nilConfig {
		return &event.GetEventConfigResponse{}, nil
	}
	return &event.GetEventConfigResponse{EventConfig: &event.EventConfig{
		AllowWaitRoom:       f.allowWaitRoom,
		TicketSaleStartDate: f.saleStart,
	}}, nil
}

func TestEventGateAsksEventServiceOncePerTTL(t *testing.T) {
	c := &fakeEventClient{allowWaitRoom: true}
	g := newEventGate(c, time.Minute)

	for range 50 {
		if _, err := g.Get(context.Background(), "e-1"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}

	if got := c.findOnes.Load(); got != 1 {
		t.Errorf("FindOne called %d times, want 1", got)
	}
	if got := c.configs.Load(); got != 1 {
		t.Errorf("GetConfig called %d times, want 1", got)
	}
}

// The property that makes this worth having. A TTL cache alone is cold at the
// instant an on-sale opens, so every joiner would miss at once -- which is the
// stampede, not a fix for it.
func TestEventGateCollapsesConcurrentMisses(t *testing.T) {
	c := &fakeEventClient{allowWaitRoom: true, block: make(chan struct{})}
	g := newEventGate(c, time.Minute)

	const joiners = 200
	var wg sync.WaitGroup
	errs := make([]error, joiners)
	for i := range joiners {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = g.Get(context.Background(), "e-1")
		}()
	}

	// Let them all pile onto the same cold key before the fetch returns.
	time.Sleep(50 * time.Millisecond)
	close(c.block)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("joiner %d: %v", i, err)
		}
	}
	if got := c.findOnes.Load(); got != 1 {
		t.Errorf("%d joiners caused %d FindOne calls, want 1", joiners, got)
	}
}

// A failure to reach event-svc is not a verdict about the event. Caching it
// would keep a queue closed for the whole TTL after the dependency recovered.
func TestEventGateDoesNotCacheADependencyFailure(t *testing.T) {
	c := &fakeEventClient{
		allowWaitRoom: true,
		findOneErr:    status.Error(codes.Unavailable, "event-svc is down"),
	}
	g := newEventGate(c, time.Minute)

	if _, err := g.Get(context.Background(), "e-1"); !errors.Is(err, ErrEventServiceUnavailable) {
		t.Fatalf("got %v, want ErrEventServiceUnavailable", err)
	}

	c.findOneErr = nil
	info, err := g.Get(context.Background(), "e-1")
	if err != nil {
		t.Fatalf("the retry after recovery must succeed, got %v", err)
	}
	if !info.AllowWaitRoom {
		t.Error("expected the recovered config")
	}
}

// A missing event is a verdict, so it is cached: a flood aimed at an event id
// that does not exist must not become a flood aimed at event-svc.
func TestEventGateCachesAMissingEvent(t *testing.T) {
	c := &fakeEventClient{nilEvent: true}
	g := newEventGate(c, time.Minute)

	for range 20 {
		if _, err := g.Get(context.Background(), "e-nope"); !errors.Is(err, ErrEventNotFound) {
			t.Fatalf("got %v, want ErrEventNotFound", err)
		}
	}

	if got := c.findOnes.Load(); got != 1 {
		t.Errorf("FindOne called %d times for a missing event, want 1", got)
	}
}

// Every joiner collapsed behind one fetch shares its result, so the fetch must
// not run on the context of whichever joiner happened to trigger it.
func TestEventGateFetchOutlivesTheCallerThatTriggeredIt(t *testing.T) {
	c := &fakeEventClient{allowWaitRoom: true, block: make(chan struct{})}
	g := newEventGate(c, time.Minute)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := g.Get(ctx, "e-1")
		done <- err
	}()

	time.Sleep(50 * time.Millisecond)
	cancel()
	close(c.block)
	<-done

	c.mu.Lock()
	seen := c.ctxErrSeen
	c.mu.Unlock()
	if seen != nil {
		t.Errorf("the fetch saw %v; a caller hanging up must not cancel the shared call", seen)
	}
}

func TestEventGateParsesTheSaleStart(t *testing.T) {
	c := &fakeEventClient{allowWaitRoom: true, saleStart: "2026-10-01T09:00:00Z"}
	g := newEventGate(c, time.Minute)

	info, err := g.Get(context.Background(), "e-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if want := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC); !info.SaleStartAt.Equal(want) {
		t.Errorf("sale start %v, want %v", info.SaleStartAt, want)
	}
}
