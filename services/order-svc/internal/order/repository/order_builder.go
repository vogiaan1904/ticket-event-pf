package repository

import (
	"github.com/vogiaan1904/ticketbottle-order/internal/models"
	pkgDynamo "github.com/vogiaan1904/ticketbottle-order/pkg/dynamodb"
)

func (r *implRepository) buildOrderModel(opt CreateOrderOption) models.Order {
	now := r.clock()

	m := models.Order{
		// DynamoDB keys
		PK:         pkgDynamo.BuildOrderPK(opt.Code),
		SK:         pkgDynamo.BuildOrderSK(opt.Code),
		GSI1PK:     pkgDynamo.BuildUserGSI1PK(opt.UserID),
		GSI1SK:     pkgDynamo.BuildOrderGSI1SK(now, opt.Code),
		GSI2PK:     pkgDynamo.BuildEventGSI2PK(opt.EventID),
		GSI2SK:     pkgDynamo.BuildOrderGSI2SK(now, opt.Code),
		EntityType: string(pkgDynamo.EntityTypeOrder),

		// Business fields
		Code:          opt.Code,
		SessionID:     opt.SessionID,
		UserID:        opt.UserID,
		UserFullName:  opt.UserFullName,
		Email:         opt.Email,
		Phone:         opt.Phone,
		EventID:       opt.EventID,
		TotalAmount:   opt.TotalAmount,
		Currency:      opt.Currency,
		PaymentMethod: opt.PaymentMethod,
		Status:        opt.Status,
		CreatedAt:     now,
		UpdatedAt:     now,
	}

	return m
}
