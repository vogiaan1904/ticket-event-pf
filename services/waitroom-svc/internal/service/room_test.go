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
