package service

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/vogiaan1904/ticketbottle-waitroom/internal/models"
)

func newDoorProcessor(q *fakeQueue, s *fakeSessions, inv *fakeInventoryClient) (*queueProcessor, *fakeProducer) {
	p := &fakeProducer{}
	qp := newTestProcessor(q, s, p)
	qp.stock = NewStockGate(inv, time.Minute, qp.l)
	return qp, p
}

func tick(t *testing.T, qp *queueProcessor) {
	t.Helper()
	if err := qp.ProcessEventQueue(context.Background(), "e-1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestAPausedDoorAdmitsNobodyAndKeepsEveryPlace(t *testing.T) {
	inv := &fakeInventoryClient{}
	inv.set(10, 7, 0)
	q := newFakeQueue("ss-1", "ss-2")
	s := &fakeSessions{sessions: map[string]*models.Session{
		"ss-1": queuedSession("ss-1"),
		"ss-2": queuedSession("ss-2"),
	}}
	qp, p := newDoorProcessor(q, s, inv)

	tick(t, qp)

	if want := []string{"ss-1", "ss-2"}; !slices.Equal(q.queued, want) {
		t.Errorf("queue = %v, want %v", q.queued, want)
	}
	if len(p.published) != 0 || len(s.statusUpdatesSeen) != 0 {
		t.Errorf("a paused door touched sessions: published %v, status updates %v", p.published, s.statusUpdatesSeen)
	}
}

func TestASoldOutLineEndsItsQueuedSessions(t *testing.T) {
	inv := &fakeInventoryClient{}
	inv.set(10, 10, 0)
	q := newFakeQueue("ss-1", "ss-2")
	s := &fakeSessions{sessions: map[string]*models.Session{
		"ss-1": queuedSession("ss-1"),
		"ss-2": queuedSession("ss-2"),
	}}
	qp, p := newDoorProcessor(q, s, inv)

	tick(t, qp)

	if len(q.queued) != 0 {
		t.Errorf("queue = %v, want empty", q.queued)
	}
	for _, id := range []string{"ss-1", "ss-2"} {
		if got := s.sessions[id].Status; got != models.SessionStatusSoldOut {
			t.Errorf("%s status = %s, want sold_out", id, got)
		}
	}
	if len(p.published) != 0 {
		t.Errorf("a sold-out line admitted %v", p.published)
	}
}

// An admitted session holds a token; its checkout gets inventory's own answer.
func TestClosingNeverRemovesAnAdmittedSession(t *testing.T) {
	inv := &fakeInventoryClient{}
	inv.set(10, 10, 0)
	admitted := queuedSession("ss-1")
	admitted.Status = models.SessionStatusAdmitted
	q := newFakeQueue("ss-1", "ss-2")
	s := &fakeSessions{sessions: map[string]*models.Session{"ss-1": admitted, "ss-2": queuedSession("ss-2")}}
	qp, _ := newDoorProcessor(q, s, inv)

	tick(t, qp)

	if want := []string{"ss-1"}; !slices.Equal(q.queued, want) {
		t.Errorf("queue = %v, want %v", q.queued, want)
	}
	if got := s.sessions["ss-1"].Status; got != models.SessionStatusAdmitted {
		t.Errorf("admitted session became %s", got)
	}
}

func TestClosingDropsAnEntryWhoseSessionIsGone(t *testing.T) {
	inv := &fakeInventoryClient{}
	inv.set(10, 10, 0)
	q := newFakeQueue("ss-gone")
	qp, _ := newDoorProcessor(q, &fakeSessions{sessions: map[string]*models.Session{}}, inv)

	tick(t, qp)

	if len(q.queued) != 0 {
		t.Errorf("queue = %v, want empty", q.queued)
	}
}

func TestClosingKeepsASessionItCouldNotRead(t *testing.T) {
	inv := &fakeInventoryClient{}
	inv.set(10, 10, 0)
	q := newFakeQueue("ss-1")
	s := &fakeSessions{sessions: map[string]*models.Session{"ss-1": queuedSession("ss-1")}, getErr: errors.New("redis unavailable")}
	qp, _ := newDoorProcessor(q, s, inv)

	tick(t, qp)

	if want := []string{"ss-1"}; !slices.Equal(q.queued, want) {
		t.Errorf("queue = %v, want %v", q.queued, want)
	}
}

// Sold out closes the line whatever the room: nobody is getting in either way.
func TestASoldOutLineClosesWithBuyersStillInside(t *testing.T) {
	inv := &fakeInventoryClient{}
	inv.set(10, 10, 0)
	q := newFakeQueue("ss-1")
	q.processing["ss-0"] = true
	s := &fakeSessions{sessions: map[string]*models.Session{"ss-1": queuedSession("ss-1")}}
	qp, _ := newDoorProcessor(q, s, inv)

	tick(t, qp)

	if len(q.queued) != 0 {
		t.Errorf("queue = %v, want empty", q.queued)
	}
}

func TestAnEmptyLineAsksInventoryNothing(t *testing.T) {
	inv := plentyOfStock()
	qp, _ := newDoorProcessor(newFakeQueue(), &fakeSessions{sessions: map[string]*models.Session{}}, inv)

	tick(t, qp)

	if n := inv.calls.Load(); n != 0 {
		t.Fatalf("inventory asked %d times for an empty line", n)
	}
}

func expiredSession(id string) *models.Session {
	ss := queuedSession(id)
	ss.ExpiresAt = time.Now().Add(-time.Minute)
	return ss
}

// A paused door admits nobody, so nothing else would ever drop these.
func TestAPausedDoorDropsDeadEntriesUpToTheFirstLiveOne(t *testing.T) {
	inv := &fakeInventoryClient{}
	inv.set(10, 7, 0)
	q := newFakeQueue("ss-gone", "ss-expired", "ss-live", "ss-gone-2")
	s := &fakeSessions{sessions: map[string]*models.Session{
		"ss-expired": expiredSession("ss-expired"),
		"ss-live":    queuedSession("ss-live"),
	}}
	qp, _ := newDoorProcessor(q, s, inv)

	tick(t, qp)

	if want := []string{"ss-live", "ss-gone-2"}; !slices.Equal(q.queued, want) {
		t.Errorf("queue = %v, want %v", q.queued, want)
	}
	if len(s.statusUpdatesSeen) != 0 {
		t.Errorf("dropping dead entries touched sessions: %v", s.statusUpdatesSeen)
	}
}

// Once only dead entries are left, the line empties and inventory is asked no more.
func TestALineOfOnlyDeadEntriesStopsAskingInventory(t *testing.T) {
	inv := &fakeInventoryClient{}
	inv.set(10, 7, 0)
	q := newFakeQueue("ss-gone-1", "ss-gone-2")
	qp, _ := newDoorProcessor(q, &fakeSessions{sessions: map[string]*models.Session{}}, inv)
	qp.stock = NewStockGate(inv, 0, qp.l)

	tick(t, qp)
	tick(t, qp)

	if len(q.queued) != 0 {
		t.Errorf("queue = %v, want empty", q.queued)
	}
	if n := inv.calls.Load(); n != 1 {
		t.Errorf("inventory asked %d times, want 1", n)
	}
}
