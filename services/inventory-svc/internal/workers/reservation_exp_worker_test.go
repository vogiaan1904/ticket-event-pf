package workers

import (
	"context"
	"testing"

	pkgLog "github.com/vogiaan/ticketbottle-inventory/pkg/logger"
)

type stubExpirer struct {
	returns []int
	calls   int
}

func (s *stubExpirer) BatchExpireReservations(_ context.Context, _ int) (int, error) {
	n := s.returns[s.calls]
	s.calls++
	return n, nil
}

func TestRunJob_DrainsUntilBatchNotFull(t *testing.T) {
	stub := &stubExpirer{returns: []int{2, 2, 1}} // batchSize 2 → drained on the 1
	l := pkgLog.InitializeZapLogger(pkgLog.ZapConfig{Level: "error", Mode: "development", Encoding: "console"})
	w := NewReservationExpiryWorker(l, stub, 0, 2)

	w.runJob(context.Background())

	if stub.calls != 3 {
		t.Fatalf("BatchExpireReservations called %d times, want 3", stub.calls)
	}
}
