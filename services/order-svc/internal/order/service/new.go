package service

import (
	"github.com/vogiaan1904/ticketbottle-order/internal/order"
	"github.com/vogiaan1904/ticketbottle-order/internal/order/delivery/kafka/producer"
	repo "github.com/vogiaan1904/ticketbottle-order/internal/order/repository"
	"github.com/vogiaan1904/ticketbottle-order/pkg/grpc/event"
	"github.com/vogiaan1904/ticketbottle-order/pkg/grpc/inventory"
	"github.com/vogiaan1904/ticketbottle-order/pkg/grpc/payment"
	pkgJwt "github.com/vogiaan1904/ticketbottle-order/pkg/jwt"
	"github.com/vogiaan1904/ticketbottle-order/pkg/logger"
	temporalCli "go.temporal.io/sdk/client"
)

type implService struct {
	l        logger.Logger
	repo     repo.Repository
	jwt      pkgJwt.Manager
	prod     producer.Producer
	invSvc   inventory.InventoryServiceClient
	evSvc    event.EventServiceClient
	pmtSvc   payment.PaymentServiceClient
	temporal temporalCli.Client
}

func New(l logger.Logger, repo repo.Repository, jwt pkgJwt.Manager, invSvc inventory.InventoryServiceClient, evSvc event.EventServiceClient, pmtSvc payment.PaymentServiceClient, prod producer.Producer, tprCli temporalCli.Client) order.Service {
	return &implService{
		l:        l,
		repo:     repo,
		jwt:      jwt,
		invSvc:   invSvc,
		evSvc:    evSvc,
		pmtSvc:   pmtSvc,
		prod:     prod,
		temporal: tprCli,
	}
}
