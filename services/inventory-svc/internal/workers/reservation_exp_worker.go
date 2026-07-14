package workers

import (
	"context"
	"time"

	pkgLog "github.com/vogiaan/ticketbottle-inventory/pkg/logger"
)

// reservationExpirer is the only capability the worker needs.
type reservationExpirer interface {
	BatchExpireReservations(ctx context.Context, batchSize int) (int, error)
}

const drainMaxIterations = 100

type ReservationExpiryWorker struct {
	l         pkgLog.Logger
	tkr       *time.Ticker
	interval  time.Duration
	batchSize int
	rSvc      reservationExpirer
	doneCh    chan struct{}
}

func NewReservationExpiryWorker(l pkgLog.Logger, rSvc reservationExpirer, interval time.Duration, batchSize int) *ReservationExpiryWorker {
	if interval <= 0 {
		interval = time.Minute
	}
	if batchSize <= 0 {
		batchSize = 500
	}
	return &ReservationExpiryWorker{
		l:         l,
		rSvc:      rSvc,
		interval:  interval,
		batchSize: batchSize,
		doneCh:    make(chan struct{}),
	}
}

func (w *ReservationExpiryWorker) Start(ctx context.Context) {
	w.tkr = time.NewTicker(w.interval)
	w.l.Infof(ctx, "Starting ReservationExpiryWorker: interval=%v, batchSize=%d", w.interval, w.batchSize)

	go w.runJob(ctx)
	go func() {
		for {
			select {
			case <-w.tkr.C:
				w.runJob(ctx)
			case <-w.doneCh:
				w.l.Info(ctx, "ReservationExpiryWorker stopped")
				return
			case <-ctx.Done():
				w.l.Info(ctx, "ReservationExpiryWorker context cancelled")
				return
			}
		}
	}()
}

func (w *ReservationExpiryWorker) Stop(ctx context.Context) {
	if w.tkr != nil {
		w.tkr.Stop()
	}
	close(w.doneCh)
	w.l.Info(ctx, "ReservationExpiryWorker shutdown initiated")
}

// runJob drains expired reservations in batches until a batch comes back
// smaller than batchSize (nothing left) or the safety cap is hit.
func (w *ReservationExpiryWorker) runJob(ctx context.Context) {
	start := time.Now()
	total := 0
	drained := false
	for i := 0; i < drainMaxIterations; i++ {
		n, err := w.rSvc.BatchExpireReservations(ctx, w.batchSize)
		if err != nil {
			w.l.Errorf(ctx, "ReservationExpiryWorker: batch expiration failed after expiring %d this run: %v", total, err)
			return
		}
		total += n
		if n < w.batchSize {
			drained = true
			break
		}
	}
	if total > 0 {
		w.l.Infof(ctx, "ReservationExpiryWorker: expired %d reservations in %v", total, time.Since(start))
	}
	if !drained {
		w.l.Warnf(ctx, "ReservationExpiryWorker: hit drain cap (%d iterations), backlog may remain", drainMaxIterations)
	}
}
