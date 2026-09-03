package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/vogiaan1904/ticketbottle-order/config"
	acts "github.com/vogiaan1904/ticketbottle-order/internal/activities"
	"github.com/vogiaan1904/ticketbottle-order/internal/infra/dynamodb"
	"github.com/vogiaan1904/ticketbottle-order/internal/infra/kafka"
	"github.com/vogiaan1904/ticketbottle-order/internal/infra/temporal"
	oCons "github.com/vogiaan1904/ticketbottle-order/internal/order/delivery/kafka/consumer"
	oProd "github.com/vogiaan1904/ticketbottle-order/internal/order/delivery/kafka/producer"
	oRepo "github.com/vogiaan1904/ticketbottle-order/internal/order/repository"
	oSvc "github.com/vogiaan1904/ticketbottle-order/internal/order/service"
	"github.com/vogiaan1904/ticketbottle-order/internal/workflows"
	eSvc "github.com/vogiaan1904/ticketbottle-order/pkg/grpc/event"
	iSvc "github.com/vogiaan1904/ticketbottle-order/pkg/grpc/inventory"
	pSvc "github.com/vogiaan1904/ticketbottle-order/pkg/grpc/payment"
	pkgJwt "github.com/vogiaan1904/ticketbottle-order/pkg/jwt"
	pkgLog "github.com/vogiaan1904/ticketbottle-order/pkg/logger"
	pkgTemporal "github.com/vogiaan1904/ticketbottle-order/pkg/temporal"
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

	kConsGr, err := kafka.NewConsumerGroup(cfg.Kafka)
	if err != nil {
		l.Fatalf(ctx, "Failed to create Kafka consumer group: %v", err)
		os.Exit(1)
	}

	oProd := oProd.NewProducer(kProd, l)

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
	epActs := acts.NewEventPublishingActivities(oProd)

	w := temporal.NewOrderWorker(tCli, temporal.ConfirmOrderTaskQueue)

	w.RegisterWorkflow(workflows.ConfirmOrder)
	w.RegisterActivity(oActs)
	w.RegisterActivity(pActs)
	w.RegisterActivity(iActs)
	w.RegisterActivity(epActs)

	go func() {
		l.Infof(ctx, "Starting Temporal worker on task queue: %s", temporal.ConfirmOrderTaskQueue)
		if err := w.Run(nil); err != nil {
			l.Fatalf(ctx, "Temporal worker failed: %v", err)
		}
	}()

	oSvc := oSvc.New(l, oRepo, jwtMgr, iSvc, eSvc, pSvc, oProd, tCli, cfg.Server.CreateOrderTimeout)

	cons := oCons.NewConsumer(kConsGr, oSvc, l)

	if err := cons.Start(ctx); err != nil {
		l.Fatalf(ctx, "Failed to start consumer: %v", err)
		os.Exit(1)
	}

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	l.Info(ctx, "Consumer Server shutting down...")

	w.Stop()

	cancel()

	if err := cons.Close(); err != nil {
		l.Errorf(ctx, "Error closing consumer: %v", err)
	}

	if err := kProd.Close(); err != nil {
		l.Errorf(ctx, "Error closing Kafka producer: %v", err)
	}

	l.Info(ctx, "Consumer server exited")
}
