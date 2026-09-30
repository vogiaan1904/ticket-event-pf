package grpc

import (
	"context"
	"strings"
	"testing"

	"github.com/vogiaan1904/ticketbottle-waitroom/internal/models"
	"github.com/vogiaan1904/ticketbottle-waitroom/internal/service"
	"github.com/vogiaan1904/ticketbottle-waitroom/pkg/logger"
	waitroompb "github.com/vogiaan1904/ticketbottle-waitroom/protogen/waitroom"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// leaveSvc answers LeaveQueue with a fixed error; any other call panics.
type leaveSvc struct {
	service.WaitroomService
	err error
}

func (f *leaveSvc) LeaveQueue(context.Context, string, string) error { return f.err }

// A session the caller cannot see is a 404, never an INTERNAL that pages.
func TestLeavingAnUnknownSessionIsNotFound(t *testing.T) {
	s := &grpcService{
		svc: &leaveSvc{err: service.ErrSessionNotFound},
		l:   logger.InitializeZapLogger(logger.ZapConfig{Level: "error", Mode: "development", Encoding: "console"}),
	}

	_, err := s.LeaveQueue(context.Background(), &waitroompb.LeaveQueueRequest{SessionId: "ss-1", UserId: "u-1"})
	if got := status.Code(err); got != codes.NotFound {
		t.Fatalf("code = %s, want NotFound", got)
	}
}

// statusSvc answers GetQueueStatus with a fixed result; any other call panics.
type statusSvc struct {
	service.WaitroomService
	out *service.QueueStatusOutput
	err error
}

func (f *statusSvc) GetQueueStatus(context.Context, string, string) (*service.QueueStatusOutput, error) {
	return f.out, f.err
}

// Sold out is a buyer losing a race: FAILED_PRECONDITION, a 409, never a page.
func TestSoldOutIsAFailedPreconditionWithItsOwnCode(t *testing.T) {
	s := &grpcService{svc: &statusSvc{err: service.ErrSoldOut}, l: logger.InitializeZapLogger(logger.ZapConfig{Level: "error", Mode: "development", Encoding: "console"})}

	_, err := s.GetQueueStatus(context.Background(), &waitroompb.GetQueueStatusRequest{SessionId: "ss-1", UserId: "u-1"})
	if got := status.Code(err); got != codes.FailedPrecondition {
		t.Fatalf("code = %s, want FailedPrecondition", got)
	}
	if !strings.Contains(status.Convert(err).Message(), "WTR012") {
		t.Fatalf("message = %q, want WTR012", status.Convert(err).Message())
	}
}

func TestAPausedLineReachesTheWire(t *testing.T) {
	s := &grpcService{
		svc: &statusSvc{out: &service.QueueStatusOutput{SessionID: "ss-1", Status: models.SessionStatusQueued, Paused: true}},
		l:   logger.InitializeZapLogger(logger.ZapConfig{Level: "error", Mode: "development", Encoding: "console"}),
	}

	res, err := s.GetQueueStatus(context.Background(), &waitroompb.GetQueueStatusRequest{SessionId: "ss-1", UserId: "u-1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.GetPaused() {
		t.Fatal("paused did not reach the response")
	}
}

// A refusal is the caller's outcome and logs quietly; only our own failure is an error.
func TestOnlyAFaultCodeIsLoggedAsAnError(t *testing.T) {
	for code, want := range map[codes.Code]bool{
		codes.FailedPrecondition: false,
		codes.NotFound:           false,
		codes.AlreadyExists:      false,
		codes.InvalidArgument:    false,
		codes.Internal:           true,
		codes.Unknown:            true,
		codes.Unavailable:        true,
		codes.DeadlineExceeded:   true,
	} {
		if got := isFault(code); got != want {
			t.Errorf("isFault(%s) = %v, want %v", code, got, want)
		}
	}
}
