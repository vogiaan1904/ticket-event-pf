package service

import "github.com/vogiaan1904/ticketbottle-order/internal/models"

type SagaCompensation struct {
	CreatedOrder    *models.Order
	ItemsCreated    bool
	TicketsReserved bool
}
