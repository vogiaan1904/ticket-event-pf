package service

import (
	"time"

	"github.com/vogiaan1904/ticketbottle-order/internal/order"
	"github.com/vogiaan1904/ticketbottle-order/internal/order/delivery/kafka/producer"
	repo "github.com/vogiaan1904/ticketbottle-order/internal/order/repository"
	"github.com/vogiaan1904/ticketbottle-order/pkg/grpc/event"
	"github.com/vogiaan1904/ticketbottle-order/pkg/grpc/inventory"
	"github.com/vogiaan1904/ticketbottle-order/pkg/grpc/payment"
	"github.com/vogiaan1904/ticketbottle-order/pkg/grpc/waitroom"
	"github.com/vogiaan1904/ticketbottle-order/pkg/logger"
	temporalCli "go.temporal.io/sdk/client"
)

type implService struct {
	l        logger.Logger
	repo     repo.Repository
	wrSvc    waitroom.WaitroomServiceClient
	prod     producer.Producer
	invSvc   inventory.InventoryServiceClient
	evSvc    event.EventServiceClient
	pmtSvc   payment.PaymentServiceClient
	temporal temporalCli.Client

	createTimeout time.Duration
	clock         func() time.Time
}

func New(l logger.Logger, repo repo.Repository, wrSvc waitroom.WaitroomServiceClient, invSvc inventory.InventoryServiceClient, evSvc event.EventServiceClient, pmtSvc payment.PaymentServiceClient, prod producer.Producer, tprCli temporalCli.Client, createTimeout time.Duration) order.Service {
	return &implService{
		l:        l,
		repo:     repo,
		wrSvc:    wrSvc,
		invSvc:   invSvc,
		evSvc:    evSvc,
		pmtSvc:   pmtSvc,
		prod:     prod,
		temporal: tprCli,

		createTimeout: createTimeout,
		clock:         time.Now,
	}
}
