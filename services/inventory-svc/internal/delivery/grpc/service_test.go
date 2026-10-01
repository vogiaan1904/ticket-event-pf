package grpc

import (
	"context"
	"testing"

	svc "github.com/vogiaan/ticketbottle-inventory/internal/services"
	invpb "github.com/vogiaan/ticketbottle-inventory/pkg/grpc/inventory"
	"github.com/vogiaan/ticketbottle-inventory/pkg/logger"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// stockSvc answers GetEventStock with fixed counts; any other call panics.
type stockSvc struct {
	svc.TicketClassService
	st svc.EventStock
}

func (f *stockSvc) GetEventStock(context.Context, string) (svc.EventStock, error) { return f.st, nil }

func quietLogger() logger.Logger {
	return logger.InitializeZapLogger(logger.ZapConfig{Level: "error", Mode: "development", Encoding: "console"})
}

func TestGetEventStock_RequiresAnEventID(t *testing.T) {
	s := &grpcService{tcSvc: &stockSvc{}, l: quietLogger()}

	_, err := s.GetEventStock(context.Background(), &invpb.GetEventStockRequest{})
	if got := status.Code(err); got != codes.InvalidArgument {
		t.Fatalf("code = %s, want InvalidArgument", got)
	}
}

func TestGetEventStock_ReturnsTheCounts(t *testing.T) {
	s := &grpcService{tcSvc: &stockSvc{st: svc.EventStock{Total: 10, Sold: 4, Available: 3}}, l: quietLogger()}

	res, err := s.GetEventStock(context.Background(), &invpb.GetEventStockRequest{EventId: "e-1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.GetTotal() != 10 || res.GetSold() != 4 || res.GetAvailable() != 3 {
		t.Fatalf("response = %+v, want total 10, sold 4, available 3", res)
	}
}
