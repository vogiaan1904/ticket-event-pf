package grpc

import (
	"context"
	"testing"

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
