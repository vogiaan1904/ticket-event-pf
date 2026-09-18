package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/vogiaan1904/ticketbottle-order/config"
	acts "github.com/vogiaan1904/ticketbottle-order/internal/activities"
	"github.com/vogiaan1904/ticketbottle-order/internal/infra/dynamodb"
	"github.com/vogiaan1904/ticketbottle-order/internal/infra/kafka"
	"github.com/vogiaan1904/ticketbottle-order/internal/infra/temporal"
	"github.com/vogiaan1904/ticketbottle-order/internal/interceptors"
	"github.com/vogiaan1904/ticketbottle-order/internal/metrics"
	oGrpc "github.com/vogiaan1904/ticketbottle-order/internal/order/delivery/grpc"
	oKafka "github.com/vogiaan1904/ticketbottle-order/internal/order/delivery/kafka/producer"
	oRepo "github.com/vogiaan1904/ticketbottle-order/internal/order/repository"
	oSvc "github.com/vogiaan1904/ticketbottle-order/internal/order/service"
	"github.com/vogiaan1904/ticketbottle-order/internal/workflows"
	eSvc "github.com/vogiaan1904/ticketbottle-order/pkg/grpc/event"
	iSvc "github.com/vogiaan1904/ticketbottle-order/pkg/grpc/inventory"
	opb "github.com/vogiaan1904/ticketbottle-order/pkg/grpc/order"
	pSvc "github.com/vogiaan1904/ticketbottle-order/pkg/grpc/payment"
	pkgJwt "github.com/vogiaan1904/ticketbottle-order/pkg/jwt"
	pkgLog "github.com/vogiaan1904/ticketbottle-order/pkg/logger"
	pkgTemporal "github.com/vogiaan1904/ticketbottle-order/pkg/temporal"
	"google.golang.org/grpc"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load config: %v\n", err)
		os.Exit(1)
	}

	l := pkgLog.InitializeZapLogger(pkgLog.ZapConfig{
		Level:    cfg.Log.Level,
		Mode:     cfg.Log.Mode,
		Encoding: cfg.Log.Encoding,
	})

	ddbClient, err := dynamodb.Connect(cfg.DynamoDB)
	if err != nil {
		l.Fatalf(ctx, "Failed to connect to DynamoDB: %v", err)
		os.Exit(1)
	}
	defer dynamodb.Disconnect(ddbClient)

	iSvc, iClose, err := iSvc.NewInventoryClient(cfg.Microservice.Inventory)
	if err != nil {
		l.Fatalf(ctx, "Failed to create inventory service client: %v", err)
		os.Exit(1)
	}
	defer iClose()

	eSvc, eClose, err := eSvc.NewEventClient(cfg.Microservice.Event)
	if err != nil {
		l.Fatalf(ctx, "Failed to create event service client: %v", err)
		os.Exit(1)
	}
	defer eClose()

	pSvc, pClose, err := pSvc.NewPaymentClient(cfg.Microservice.Payment)
	if err != nil {
		l.Fatalf(ctx, "Failed to create payment service client: %v", err)
		os.Exit(1)
	}
	defer pClose()

	kProd, err := kafka.NewProducer(cfg.Kafka)
	if err != nil {
		l.Fatalf(ctx, "Failed to create Kafka producer: %v", err)
		os.Exit(1)
	}

	oProd := oKafka.NewProducer(kProd, l)

	oRepo := oRepo.New(l, ddbClient.DB(), ddbClient.TableName())

	jwtMgr := pkgJwt.NewManager(cfg.JWT.Secret, l)

	tCli, err := pkgTemporal.NewClient(cfg.Temporal)
	if err != nil {
		l.Fatalf(ctx, "Failed to create Temporal client: %v", err)
		os.Exit(1)
	}
	defer tCli.Close()

	oActs := acts.NewOrderActivities(oRepo)
	pActs := acts.NewPaymentActivities(pSvc)
	iActs := acts.NewInventoryActivities(iSvc)

	w := temporal.NewOrderWorker(tCli, temporal.CreateOrderTaskQueue)

	w.RegisterWorkflow(workflows.CreateOrder)
	w.RegisterActivity(oActs)
	w.RegisterActivity(pActs)
	w.RegisterActivity(iActs)

	go func() {
		l.Infof(ctx, "Starting Temporal worker on task queue: %s", temporal.CreateOrderTaskQueue)
		if err := w.Run(nil); err != nil {
			l.Fatalf(ctx, "Temporal worker failed: %v", err)
		}
	}()

	oSvc := oSvc.New(l, oRepo, jwtMgr, iSvc, eSvc, pSvc, oProd, tCli, cfg.Server.CreateOrderTimeout)

	oGrpc := oGrpc.NewGrpcService(oSvc, l)

	lnr, err := net.Listen("tcp", fmt.Sprintf(":%d", cfg.Server.GRpcPort))
	if err != nil {
		l.Fatalf(ctx, "gRPC server failed to listen: %v", err)
	}

	gRpcSrv := grpc.NewServer(
		grpc.ChainUnaryInterceptor(
			interceptors.GrpcLoggingInterceptor(l),
			interceptors.GrpcMetricsInterceptor(),
		),
	)
	opb.RegisterOrderServiceServer(gRpcSrv, oGrpc)

	go func() {
		l.Infof(ctx, "gRPC server is listening on port: %d", cfg.Server.GRpcPort)
		if err := gRpcSrv.Serve(lnr); err != nil {
			l.Fatalf(ctx, "Failed to serve gRPC: %v", err)
		}
	}()

	metricsSrv := metrics.NewServer(cfg.Server.MetricsPort)
	go func() {
		l.Infof(ctx, "metrics server is listening on port: %d", cfg.Server.MetricsPort)
		if err := metricsSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			l.Fatalf(ctx, "Failed to serve metrics: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	l.Info(ctx, "Server shutting down...")

	w.Stop()

	cancel()
	time.Sleep(1 * time.Second)
	gRpcSrv.GracefulStop()

	if err := metricsSrv.Shutdown(context.Background()); err != nil {
		l.Errorf(ctx, "Error shutting down metrics server: %v", err)
	}

	if err := kProd.Close(); err != nil {
		l.Errorf(ctx, "Error closing Kafka producer: %v", err)
	}

	l.Info(ctx, "Server exited")
}
