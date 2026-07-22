package service

import (
	"github.com/vogiaan/ticketbottle-inventory/internal/models"
)

// updateColumns turns a partial update into the exact set of columns to write.
// Counter columns (reserved, sold) are never included: writing them outside a
// locked reservation transaction is what silently erases live holds.
func (s implTicketClassService) updateColumns(in UpdateTicketClassInput) map[string]any {
	cols := make(map[string]any, 7)
	if in.Name != nil {
		cols["name"] = *in.Name
	}
	if in.PriceCents != nil {
		cols["price_cents"] = *in.PriceCents
	}
	if in.Currency != nil {
		cols["currency"] = *in.Currency
	}
	if in.Total != nil {
		cols["total"] = *in.Total
	}
	if in.SaleStartAt != nil {
		cols["sale_start_at"] = *in.SaleStartAt
	}
	if in.SaleEndAt != nil {
		cols["sale_end_at"] = *in.SaleEndAt
	}
	if in.Status != nil {
		cols["status"] = models.TicketClassStatus(*in.Status)
	}
	return cols
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
