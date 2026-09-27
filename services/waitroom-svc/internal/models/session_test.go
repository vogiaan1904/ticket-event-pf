package models

import (
	"math"
	"testing"
	"time"
)

var saleStart = time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)

// The invariant the draw exists to create: gathering early buys you a place in
// the lottery, not a place at the front of it.
func TestPreOpenJoinersAreOrderedByLotNotByArrival(t *testing.T) {
	// Two joiners an hour apart, both before the doors open.
	early := saleStart.Add(-2 * time.Hour)
	late := saleStart.Add(-1 * time.Hour)

	earlyWins := 0
	const draws = 400
	for range draws {
		if DrawQueueScore(early, saleStart) < DrawQueueScore(late, saleStart) {
			earlyWins++
		}
	}

	// A draw is fair; arrival order would make this 400 or 0.
	if earlyWins < draws/4 || earlyWins > 3*draws/4 {
		t.Errorf("the earlier joiner won %d/%d draws; arrival order is still deciding", earlyWins, draws)
	}
}

// The band must be closed: no amount of luck lets a latecomer overtake someone
// who was waiting before the sale opened.
func TestNoLatecomerEverOutdrawsAPreOpenJoiner(t *testing.T) {
	preOpen := saleStart.Add(-30 * time.Minute)
	firstLatecomer := DrawQueueScore(saleStart, saleStart)

	for range 1000 {
		if got := DrawQueueScore(preOpen, saleStart); got >= firstLatecomer {
			t.Fatalf("pre-open score %v reached the first post-open score %v", got, firstLatecomer)
		}
	}
}

func TestPostOpenJoinersKeepArrivalOrder(t *testing.T) {
	first := saleStart.Add(time.Second)
	second := saleStart.Add(2 * time.Second)

	if DrawQueueScore(first, saleStart) >= DrawQueueScore(second, saleStart) {
		t.Error("once the sale is open, arriving first must still mean queueing first")
	}
}

// An event whose sale start could not be parsed leaves the zero time. Falling
// back to arrival is the behaviour that predates the draw; drawing on a zero
// sale start would put every joiner in a band around 1970.
func TestAnAbsentSaleStartFallsBackToArrival(t *testing.T) {
	at := time.Date(2026, 5, 4, 12, 0, 0, 0, time.UTC)

	if got := DrawQueueScore(at, time.Time{}); got != float64(at.Unix()) {
		t.Errorf("got %v, want the arrival timestamp %v", got, float64(at.Unix()))
	}
}

// The score is drawn once and carried. Re-deriving it on read would shuffle
// people who are already standing in line.
func TestGetQueueScoreReturnsTheDrawnValue(t *testing.T) {
	s := &Session{QueuedAt: saleStart.Add(-time.Hour), QueueScore: 1234.5}

	for range 5 {
		if got := s.GetQueueScore(); got != 1234.5 {
			t.Fatalf("got %v, want the stored 1234.5", got)
		}
	}
}

// Sessions written before the draw existed carry no score. They must keep the
// order they were given, not jump to the head with a score of zero.
func TestASessionStoredBeforeTheDrawKeepsArrivalOrder(t *testing.T) {
	at := time.Date(2026, 5, 4, 12, 0, 0, 0, time.UTC)
	s := &Session{QueuedAt: at}

	if got := s.GetQueueScore(); math.Abs(got-float64(at.Unix())) > 0.0001 {
		t.Errorf("got %v, want the arrival timestamp %v", got, float64(at.Unix()))
	}
}
