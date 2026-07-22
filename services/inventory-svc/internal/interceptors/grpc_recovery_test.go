package interceptors

import (
	"context"
	"testing"

	pkgLog "github.com/vogiaan/ticketbottle-inventory/pkg/logger"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func testLogger() pkgLog.Logger {
	return pkgLog.InitializeZapLogger(pkgLog.ZapConfig{
		Level: "error", Mode: "development", Encoding: "console",
	})
}

func TestGrpcRecovery_ConvertsPanicToInternal(t *testing.T) {
	interceptor := GrpcRecoveryInterceptor(testLogger())
	info := &grpc.UnaryServerInfo{FullMethod: "/inventory.InventoryService/Reserve"}

	resp, err := interceptor(context.Background(), nil, info,
		func(ctx context.Context, req any) (any, error) {
			var m map[string]string
			m["boom"] = "nil map write" // panics
			return nil, nil
		})

	if resp != nil {
		t.Fatalf("resp = %v, want nil after a panic", resp)
	}
	if err == nil {
		t.Fatal("err = nil, want an Internal status error")
	}
	if st, _ := status.FromError(err); st.Code() != codes.Internal {
		t.Fatalf("code = %s, want Internal", st.Code())
	}
}

func TestGrpcRecovery_PassesThroughNormalCalls(t *testing.T) {
	interceptor := GrpcRecoveryInterceptor(testLogger())
	info := &grpc.UnaryServerInfo{FullMethod: "/inventory.InventoryService/Confirm"}

	resp, err := interceptor(context.Background(), "req", info,
		func(ctx context.Context, req any) (any, error) {
			return "ok", nil
		})

	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if resp != "ok" {
		t.Fatalf("resp = %v, want \"ok\"", resp)
	}
}
