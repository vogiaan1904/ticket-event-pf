package grpc

import (
	"errors"

	"github.com/vogiaan1904/ticketbottle-order/internal/order"
	pkgErrors "github.com/vogiaan1904/ticketbottle-order/pkg/errors"
	"google.golang.org/grpc/codes"
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

	ErrGRPCRequestTimeout = pkgErrors.NewGRPCErrorWithStatus(codes.DeadlineExceeded, "ORD017", "Order creation timed out")
)

// mapError turns a domain error into its wire equivalent. Matching uses
// errors.Is rather than == so a sentinel still resolves after it has been
// wrapped on the way up; a bare switch on err sends every wrapped error to
// default, which response.GrpcError renders as codes.Internal — a 500 for what
// may well be an ordinary business rejection.
func (s *grpcService) mapError(err error) error {
	switch {
	case errors.Is(err, order.ErrOrderNotFound):
		return ErrGRPCOrderNotFound
	case errors.Is(err, order.ErrOrderAlreadyExists):
		return ErrGRPCOrderAlreadyExists
	case errors.Is(err, order.ErrInvalidOrderStatus):
		return ErrGRPCInvalidOrderStatus
	case errors.Is(err, order.ErrOrderCreationFailed):
		return ErrGRPCOrderCreationFailed
	case errors.Is(err, order.ErrOrderUpdateFailed):
		return ErrGRPCOrderUpdateFailed
	case errors.Is(err, order.ErrOrderCancellationFailed):
		return ErrGRPCOrderCancellationFailed
	case errors.Is(err, order.ErrOrderNotPending):
		return ErrGRPCOrderNotPending
	case errors.Is(err, order.ErrPaymentAmountMismatch):
		return ErrGRPCPaymentAmountMismatch
	case errors.Is(err, order.ErrEventNotFound):
		return ErrGRPCEventNotFound
	case errors.Is(err, order.ErrEventNotReadyForSale):
		return ErrGRPCEventNotReadyForSale
	case errors.Is(err, order.ErrTicketClassNotFound):
		return ErrGRPCTicketClassNotFound
	case errors.Is(err, order.ErrTicketSoldOut):
		return ErrGRPCTicketSoldOut
	case errors.Is(err, order.ErrNotEnoughTickets):
		return ErrGRPCNotEnoughTickets
	case errors.Is(err, order.ErrEventConfigNotFound):
		return ErrGRPCEventConfigNotFound
	case errors.Is(err, order.ErrInvalidCheckoutToken):
		return ErrGRPCInvalidCheckoutToken
	case errors.Is(err, order.ErrRequestTimeout):
		return ErrGRPCRequestTimeout
	default:
		return err
	}
}
