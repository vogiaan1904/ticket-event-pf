package workers

import (
	"context"
	"time"

	svc "github.com/vogiaan/ticketbottle-inventory/internal/services"
	pkgLog "github.com/vogiaan/ticketbottle-inventory/pkg/logger"
)

type ReservationExpiryWorker struct {
	l         pkgLog.Logger
	tkr       *time.Ticker
	interval  time.Duration
	batchSize int
	rSvc      svc.ReservationService
	doneCh    chan struct{}
}

func NewReservationExpiryWorker(
	l pkgLog.Logger,
	rSvc svc.ReservationService,
) *ReservationExpiryWorker {
	return &ReservationExpiryWorker{
		l:         l,
		rSvc:      rSvc,
		interval:  1 * time.Minute,
		batchSize: 500,
		doneCh:    make(chan struct{}),
	}
}

func (w *ReservationExpiryWorker) Start(ctx context.Context) {
	w.tkr = time.NewTicker(w.interval)
	w.l.Infof(ctx, "Starting ReservationExpiryWorker: interval=%v, batchSize=%d",
		w.interval, w.batchSize)

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

func (w *ReservationExpiryWorker) runJob(ctx context.Context) {
	startTime := time.Now()

	w.l.Debug(ctx, "ReservationExpiryWorker: starting batch expiration job")

	expCnt, err := w.rSvc.BatchExpireReservations(ctx, w.batchSize)
	if err != nil {
		w.l.Errorf(ctx, "ReservationExpiryWorker: batch expiration failed: %v", err)
		return
	}

	duration := time.Since(startTime)

	if expCnt > 0 {
		w.l.Infof(ctx, "ReservationExpiryWorker: expired %d reservations in %v",
			expCnt, duration)
	}
}
