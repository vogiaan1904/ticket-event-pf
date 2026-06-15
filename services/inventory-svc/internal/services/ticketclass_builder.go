package service

import (
	"github.com/vogiaan/ticketbottle-inventory/internal/models"
)

func (s implTicketClassService) buildUpdate(in UpdateTicketClassInput, model *models.TicketClass) {
	if in.Name != "" {
		model.Name = in.Name
	}
	if in.PriceCents != nil {
		model.PriceCents = *in.PriceCents
	}
	model.Currency = in.Currency
	model.Total = in.Total
	model.SaleStartAt = in.SaleStartAt
	model.SaleEndAt = in.SaleEndAt
	if in.Status != nil {
		model.Status = models.TicketClassStatus(*in.Status)
	}
}

func (s implTicketClassService) buildModel(in CreateTicketClassInput) models.TicketClass {
	return models.TicketClass{
		EventID:     in.EventID,
		Name:        in.Name,
		PriceCents:  in.PriceCents,
		Currency:    in.Currency,
		Total:       in.Total,
		SaleStartAt: in.SaleStartAt,
		SaleEndAt:   in.SaleEndAt,
		Status:      models.TicketClassStatusActive,
	}
}
