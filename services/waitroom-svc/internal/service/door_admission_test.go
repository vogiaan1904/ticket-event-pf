package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/vogiaan1904/ticketbottle-waitroom/internal/models"
)

func TestAJoinToASoldOutEventIsRefused(t *testing.T) {
	r := newAdmissionRig(t, time.Now().Add(-time.Hour), &interleavingProducer{})
	r.inv.set(10, 10, 0)

	_, err := r.svc.JoinQueue(context.Background(), &JoinQueueInput{UserID: "u-1", EventID: r.eID})
	if !errors.Is(err, ErrSoldOut) {
		t.Fatalf("join error = %v, want ErrSoldOut", err)
	}
}

func TestAPausedLineKeepsTheWaiterQueuedAndSaysSo(t *testing.T) {
	r := newAdmissionRig(t, time.Now().Add(-time.Hour), &interleavingProducer{})
	r.inv.set(10, 7, 0)

	ssID := r.join(t, "u-1")
	sleepPastSecond()
	r.tick(t)

	st := r.status(t, ssID)
	if st.Status != models.SessionStatusQueued || !st.Paused || st.Position < 1 {
		t.Fatalf("status = %s, paused = %v, position = %d; want queued, paused, in place", st.Status, st.Paused, st.Position)
	}
}

// Ended is final for the session, even once tickets come back.
func TestAWaiterOnALineThatSoldOutStaysSoldOut(t *testing.T) {
	r := newAdmissionRig(t, time.Now().Add(-time.Hour), &interleavingProducer{})
	r.inv.set(10, 7, 0)
	ssID := r.join(t, "u-1")

	r.inv.set(10, 10, 0)
	sleepPastSecond()
	r.tick(t)

	r.inv.set(20, 10, 10)
	time.Sleep(5 * time.Millisecond)

	_, err := r.svc.GetQueueStatus(context.Background(), ssID, r.owners[ssID])
	if !errors.Is(err, ErrSoldOut) {
		t.Fatalf("status error = %v, want ErrSoldOut", err)
	}
}
