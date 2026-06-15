package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/vogiaan1904/ticketbottle-waitroom/config"
	wrGrpc "github.com/vogiaan1904/ticketbottle-waitroom/internal/delivery/grpc"
	"github.com/vogiaan1904/ticketbottle-waitroom/internal/delivery/kafka/consumer"
	"github.com/vogiaan1904/ticketbottle-waitroom/internal/delivery/kafka/producer"
	"github.com/vogiaan1904/ticketbottle-waitroom/internal/infra/redis"
	repo "github.com/vogiaan1904/ticketbottle-waitroom/internal/repository/redis"
	"github.com/vogiaan1904/ticketbottle-waitroom/internal/service"
	pkgGrpc "github.com/vogiaan1904/ticketbottle-waitroom/pkg/grpc"
	pkgKafka "github.com/vogiaan1904/ticketbottle-waitroom/pkg/kafka"
	pkgLog "github.com/vogiaan1904/ticketbottle-waitroom/pkg/logger"
	waitroompb "github.com/vogiaan1904/ticketbottle-waitroom/protogen/waitroom"
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

	redisCli, err := redis.Connect(ctx, cfg.Redis)
	if err != nil {
		l.Fatalf(ctx, "Failed to connect to Redis: %v", err)
	}
	defer redis.Disconnect(redisCli)

	ssRepo := repo.NewRedisSessionRepository(redisCli, l)
	qRepo := repo.NewRedisQueueRepository(redisCli, l)

	// Initialize Kafka producer
	kSyncProd, err := pkgKafka.NewProducer(pkgKafka.ProducerConfig{
		Brokers:      cfg.Kafka.Brokers,
		RetryMax:     cfg.Kafka.ProducerRetryMax,
		RequiredAcks: cfg.Kafka.ProducerRequiredAcks,
	})
	if err != nil {
		l.Fatalf(ctx, "Failed to initialize Kafka producer: %v", err)
	}
	defer func() {
		if kSyncProd != nil {
			kSyncProd.Close()
		}
	}()

	// Initialize Kafka consumer
	kConsGrCli, err := pkgKafka.NewConsumer(pkgKafka.ConsumerConfig{
		Brokers: cfg.Kafka.Brokers,
		GroupID: cfg.Kafka.ConsumerGroupID,
	})
	if err != nil {
		l.Fatalf(ctx, "Failed to initialize Kafka consumer: %v", err)
	}
	defer func() {
		if kConsGrCli != nil {
			kConsGrCli.Close()
		}
	}()

	// Queue Producer
	prod := producer.NewProducer(kSyncProd, l)

	// Initialize gRpc Service Clients
	eventSvc, eventSvcClose, err := pkgGrpc.NewEventClient(cfg.Microservice.Event)
	if err != nil {
		l.Fatalf(ctx, "Failed to initialize gRpc event service client: %v", err)
	}
	defer eventSvcClose()

	// Initialize services
	ssSvc := service.NewSessionService(ssRepo, cfg.JWT, l)
	qSvc := service.NewQueueService(qRepo, l)

	// Initialize queue processor
	queueProcessor := service.NewQueueProcessor(qSvc, ssSvc, eventSvc, prod, l, cfg.Queue)

	// Initialize waitroom service with processor
	wrSvc := service.NewWaitroomService(qSvc, ssSvc, eventSvc, prod, l, queueProcessor)

	// Waitroom Consumer
	cons := consumer.NewConsumer(kConsGrCli, wrSvc, l)
	cons.Start(ctx)

	// Start Queue Processor
	go func() {
		if err := wrSvc.StartQueueProcessor(ctx); err != nil {
			l.Errorf(ctx, "Failed to start queue processor: %v", err)
		}
	}()

	// gRPC server
	lnr, err := net.Listen("tcp", fmt.Sprintf(":%d", cfg.Server.GRpcPort))
	if err != nil {
		l.Fatalf(ctx, "gRPC server failed to listen: %v", err)
	}

	wrGrpc := wrGrpc.NewGrpcService(wrSvc, l)
	gRpcSrv := grpc.NewServer()
	waitroompb.RegisterWaitroomServiceServer(gRpcSrv, wrGrpc)

	go func() {
		l.Infof(ctx, "gRPC server is listening on port: %d", cfg.Server.GRpcPort)
		if err := gRpcSrv.Serve(lnr); err != nil {
			l.Fatalf(ctx, "Failed to serve gRPC: %v", err)
		}
	}()

	// http server
	// ...

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	l.Info(ctx, "Server shutting down...")

	// Stop queue processor gracefully
	if err := wrSvc.StopQueueProcessor(); err != nil {
		l.Errorf(ctx, "Failed to stop queue processor: %v", err)
	}

	cancel()
	time.Sleep(1 * time.Second)
	gRpcSrv.GracefulStop()

	l.Info(ctx, "Server exited")
}
