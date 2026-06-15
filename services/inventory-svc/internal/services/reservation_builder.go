package service

import (
	"time"

	"github.com/vogiaan/ticketbottle-inventory/internal/models"
)

func (s implReservationService) buildModel(oCode string, expAt time.Time, in ReserveItem) models.Reservation {
	return models.Reservation{
		OrderCode:     oCode,
		TicketClassID: in.TicketClassID,
		Qty:           in.Qty,
		Status:        models.ReservationStatusActive,
		ExpiresAt:     expAt,
	}
}
