package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/vogiaan/ticketbottle-inventory/config"
	grpcSvc "github.com/vogiaan/ticketbottle-inventory/internal/delivery/grpc"
	"github.com/vogiaan/ticketbottle-inventory/internal/interceptors"
	"github.com/vogiaan/ticketbottle-inventory/internal/models"
	svc "github.com/vogiaan/ticketbottle-inventory/internal/services"
	"github.com/vogiaan/ticketbottle-inventory/internal/workers"
	pkgGorm "github.com/vogiaan/ticketbottle-inventory/pkg/gorm"
	invpb "github.com/vogiaan/ticketbottle-inventory/pkg/grpc/inventory"
	pkgLog "github.com/vogiaan/ticketbottle-inventory/pkg/logger"
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

	db, err := pkgGorm.New(&cfg.Postgres)
	if err != nil {
		l.Fatalf(ctx, "Failed to initialize database: %v", err)
	}

	if err := db.AutoMigrate(
		&models.TicketClass{},
		&models.Reservation{},
	); err != nil {
		l.Fatalf(ctx, "Failed to migrate database: %v", err)
	}
	l.Info(ctx, "Database tables migrated successfully")

	repo := pkgGorm.NewRepository(db)

	rsvSvc := svc.NewReservationService(l, repo)
	tcSvc := svc.NewTicketClassService(l, repo)

	rsvExpWkr := workers.NewReservationExpiryWorker(l, rsvSvc)

	wkrMng := workers.NewWorkerManager(l)
	wkrMng.Register(rsvExpWkr)
	wkrMng.StartAll(ctx)

	// gRPC server
	grpcSvc := grpcSvc.NewGrpcService(rsvSvc, tcSvc, l)
	lnr, err := net.Listen("tcp", fmt.Sprintf(":%d", cfg.Server.GRpcPort))
	if err != nil {
		l.Fatalf(ctx, "gRPC server failed to listen: %v", err)
	}

	grpcSvr := grpc.NewServer(
		grpc.UnaryInterceptor(interceptors.GrpcLoggingInterceptor(l)),
	)
	invpb.RegisterInventoryServiceServer(grpcSvr, grpcSvc)

	go func() {
		l.Infof(ctx, "gRPC server is listening on port: %d", cfg.Server.GRpcPort)
		if err := grpcSvr.Serve(lnr); err != nil {
			l.Fatalf(ctx, "Failed to serve gRPC: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	l.Info(ctx, "Server shutting down...")

	cancel()
	time.Sleep(1 * time.Second)
	grpcSvr.GracefulStop()
	wkrMng.StopAll(ctx)

	l.Info(ctx, "Server exited")
}
