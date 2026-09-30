package service

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/vogiaan1904/ticketbottle-waitroom/pkg/logger"
	"github.com/vogiaan1904/ticketbottle-waitroom/protogen/inventory"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Only GetEventStock is implemented; any other call is a nil-pointer panic.
type fakeInventoryClient struct {
	inventory.InventoryServiceClient

	calls atomic.Int32

	mu    sync.Mutex
	stock *inventory.GetEventStockResponse
	err   error
}

func (f *fakeInventoryClient) GetEventStock(context.Context, *inventory.GetEventStockRequest, ...grpc.CallOption) (*inventory.GetEventStockResponse, error) {
	f.calls.Add(1)
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return nil, f.err
	}
	return f.stock, nil
}

func (f *fakeInventoryClient) set(total, sold, available int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stock = &inventory.GetEventStockResponse{Total: total, Sold: sold, Available: available}
	f.err = nil
}

func plentyOfStock() *fakeInventoryClient {
	f := &fakeInventoryClient{}
	f.set(1000, 0, 1000)
	return f
}

func quietLogger() logger.Logger {
	return logger.InitializeZapLogger(logger.ZapConfig{Level: "error", Mode: "development", Encoding: "console"})
}

func TestDoorFor(t *testing.T) {
	cases := []struct {
		name                   string
		total, sold, available int64
		want                   Door
	}{
		{"no class can still sell", 0, 0, 0, DoorOpen},
		{"every ticket sold", 10, 10, 0, DoorSoldOut},
		{"every ticket left is held", 10, 7, 0, DoorPaused},
		{"some on sale now", 10, 7, 1, DoorOpen},
	}
	for _, c := range cases {
		if got := doorFor(c.total, c.sold, c.available); got != c.want {
			t.Errorf("%s: door = %s, want %s", c.name, got, c.want)
		}
	}
}

// Not knowing must admit as before: inventory's UPDATE, not the door, stops an oversell.
func TestStockGateFailsOpen(t *testing.T) {
	inv := &fakeInventoryClient{err: status.Error(codes.Unimplemented, "no such method")}
	g := NewStockGate(inv, time.Minute, quietLogger())

	if got := g.Get(context.Background(), "e-1"); got != DoorOpen {
		t.Fatalf("door = %s, want open", got)
	}
}

func TestStockGateAsksOncePerTTL(t *testing.T) {
	inv := &fakeInventoryClient{}
	inv.set(10, 7, 0)
	g := NewStockGate(inv, time.Minute, quietLogger())

	g.Get(context.Background(), "e-1")
	if got := g.Get(context.Background(), "e-1"); got != DoorPaused {
		t.Fatalf("door = %s, want paused", got)
	}
	if n := inv.calls.Load(); n != 1 {
		t.Fatalf("inventory asked %d times, want 1", n)
	}
}

// A failure is cached too, so a down inventory is asked once per TTL, not per join.
func TestStockGateCachesAFailure(t *testing.T) {
	inv := &fakeInventoryClient{err: status.Error(codes.Unavailable, "down")}
	g := NewStockGate(inv, time.Minute, quietLogger())

	g.Get(context.Background(), "e-1")
	g.Get(context.Background(), "e-1")
	if n := inv.calls.Load(); n != 1 {
		t.Fatalf("inventory asked %d times, want 1", n)
	}
}

func TestStockGateAsksAgainOnceTheAnswerIsStale(t *testing.T) {
	inv := &fakeInventoryClient{}
	inv.set(10, 0, 10)
	g := NewStockGate(inv, 10*time.Millisecond, quietLogger())

	g.Get(context.Background(), "e-1")
	inv.set(10, 10, 0)
	time.Sleep(20 * time.Millisecond)

	if got := g.Get(context.Background(), "e-1"); got != DoorSoldOut {
		t.Fatalf("door = %s, want sold out", got)
	}
}
