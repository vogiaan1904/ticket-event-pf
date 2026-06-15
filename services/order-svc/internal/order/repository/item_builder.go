package repository

import (
	"github.com/vogiaan1904/ticketbottle-order/internal/models"
	pkgDynamo "github.com/vogiaan1904/ticketbottle-order/pkg/dynamodb"
)

func (r *implRepository) buildOrderItemModel(orderCode string, opt CreateOrderItemOption) models.OrderItem {
	now := r.clock()
	itemID := pkgDynamo.GenerateItemID()

	m := models.OrderItem{
		// DynamoDB keys
		PK:         pkgDynamo.BuildOrderPK(orderCode),
		SK:         pkgDynamo.BuildItemSK(itemID),
		EntityType: string(pkgDynamo.EntityTypeOrderItem),

		ID:              itemID,
		OrderCode:       orderCode,
		TicketClassID:   opt.TicketClassID,
		TicketClassName: opt.TicketClassName,
		PriceAtPurchase: opt.PriceAtPurchase,
		Quantity:        opt.Quantity,
		TotalAmount:     opt.TotalAmount,
		CreatedAt:       now,
		UpdatedAt:       now,
	}

	return m
}
