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
func TestASoldOutLineClosesEvenWithEveryChairTaken(t *testing.T) {
	inv := &fakeInventoryClient{}
	inv.set(10, 10, 0)
	q := newFakeQueue("ss-1")
	q.processing["ss-0"] = true
	s := &fakeSessions{sessions: map[string]*models.Session{"ss-1": queuedSession("ss-1")}}
	qp, _ := newDoorProcessor(q, s, inv)
	qp.cfg.MaxConcurrentPerEvent = 1

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
