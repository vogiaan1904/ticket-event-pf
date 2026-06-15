package grpc

import (
	"github.com/vogiaan1904/ticketbottle-order/internal/order"
	pkgErrors "github.com/vogiaan1904/ticketbottle-order/pkg/errors"
)

var (
	ErrValidationFailed = pkgErrors.NewGRPCError("ORD400", "Validation failed")
	// Order errors
	ErrGRPCOrderNotFound           = pkgErrors.NewGRPCError("ORD001", "Order not found")
	ErrGRPCOrderAlreadyExists      = pkgErrors.NewGRPCError("ORD002", "Order already exists")
	ErrGRPCInvalidOrderStatus      = pkgErrors.NewGRPCError("ORD003", "Invalid order status")
	ErrGRPCOrderCreationFailed     = pkgErrors.NewGRPCError("ORD004", "Order creation failed")
	ErrGRPCOrderUpdateFailed       = pkgErrors.NewGRPCError("ORD005", "Order update failed")
	ErrGRPCOrderCancellationFailed = pkgErrors.NewGRPCError("ORD006", "Order cancellation failed")
	ErrGRPCOrderNotPending         = pkgErrors.NewGRPCError("ORD007", "Order is not in pending status")
	ErrGRPCPaymentAmountMismatch   = pkgErrors.NewGRPCError("ORD008", "Payment amount does not match order amount")

	// Event errors
	ErrGRPCEventNotFound        = pkgErrors.NewGRPCError("ORD009", "Event not found")
	ErrGRPCEventNotReadyForSale = pkgErrors.NewGRPCError("ORD010", "Event not ready for sale")
	ErrGRPCTicketClassNotFound  = pkgErrors.NewGRPCError("ORD011", "Ticket class not found")
	ErrGRPCTicketSoldOut        = pkgErrors.NewGRPCError("ORD012", "Ticket sold out")
	ErrGRPCNotEnoughTickets     = pkgErrors.NewGRPCError("ORD013", "Not enough tickets available")
	ErrGRPCEventConfigNotFound  = pkgErrors.NewGRPCError("ORD014", "Event config not found")

	// Checkout errors
	ErrGRPCInvalidCheckoutToken = pkgErrors.NewGRPCError("ORD016", "Invalid checkout token")
)

func (s *grpcService) mapError(err error) error {
	switch err {
	case order.ErrOrderNotFound:
		return ErrGRPCOrderNotFound
	case order.ErrOrderAlreadyExists:
		return ErrGRPCOrderAlreadyExists
	case order.ErrInvalidOrderStatus:
		return ErrGRPCInvalidOrderStatus
	case order.ErrOrderCreationFailed:
		return ErrGRPCOrderCreationFailed
	case order.ErrOrderUpdateFailed:
		return ErrGRPCOrderUpdateFailed
	case order.ErrOrderCancellationFailed:
		return ErrGRPCOrderCancellationFailed
	case order.ErrOrderNotPending:
		return ErrGRPCOrderNotPending
	case order.ErrPaymentAmountMismatch:
		return ErrGRPCPaymentAmountMismatch
	case order.ErrEventNotFound:
		return ErrGRPCEventNotFound
	case order.ErrEventNotReadyForSale:
		return ErrGRPCEventNotReadyForSale
	case order.ErrTicketClassNotFound:
		return ErrGRPCTicketClassNotFound
	case order.ErrTicketSoldOut:
		return ErrGRPCTicketSoldOut
	case order.ErrNotEnoughTickets:
		return ErrGRPCNotEnoughTickets
	case order.ErrEventConfigNotFound:
		return ErrGRPCEventConfigNotFound
	case order.ErrInvalidCheckoutToken:
		return ErrGRPCInvalidCheckoutToken
	default:
		return err
	}
}
