package workers

import (
	"context"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	pkgLog "github.com/vogiaan/ticketbottle-inventory/pkg/logger"
)

type Worker interface {
	Start(ctx context.Context)
	Stop(ctx context.Context)
}

type WorkerManager struct {
	l    pkgLog.Logger
	wkrs []Worker
	wg   sync.WaitGroup
}

func NewWorkerManager(l pkgLog.Logger) *WorkerManager {
	return &WorkerManager{
		l:    l,
		wkrs: make([]Worker, 0),
	}
}

func (wm *WorkerManager) Register(worker Worker) {
	wm.wkrs = append(wm.wkrs, worker)
}

func (wm *WorkerManager) StartAll(ctx context.Context) {
	wm.l.Infof(ctx, "Starting %d workers", len(wm.wkrs))

	for _, worker := range wm.wkrs {
		wm.wg.Add(1)
		go func(w Worker) {
			defer wm.wg.Done()
			w.Start(ctx)
		}(worker)
	}

	wm.l.Info(ctx, "All workers started")
}

func (wm *WorkerManager) StopAll(ctx context.Context) {
	wm.l.Infof(ctx, "Stopping %d workers", len(wm.wkrs))

	for _, worker := range wm.wkrs {
		worker.Stop(ctx)
	}

	done := make(chan struct{})
	go func() {
		wm.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		wm.l.Info(ctx, "All workers stopped gracefully")
	case <-time.After(10 * time.Second):
		wm.l.Warn(ctx, "Worker shutdown timeout reached")
	}
}

func (wm *WorkerManager) WaitForShutdownSignal(ctx context.Context) {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM, syscall.SIGINT)

	sig := <-sigChan
	wm.l.Infof(ctx, "Received shutdown signal: %v", sig)

	wm.StopAll(ctx)
}
