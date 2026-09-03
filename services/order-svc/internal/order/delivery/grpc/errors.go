package grpc

import (
	"errors"

	"github.com/vogiaan1904/ticketbottle-order/internal/order"
	pkgErrors "github.com/vogiaan1904/ticketbottle-order/pkg/errors"
	"google.golang.org/grpc/codes"
)

var (
	ErrValidationFailed = pkgErrors.NewGRPCError(codes.InvalidArgument, "ORD400", "Validation failed")
	// Order errors
	ErrGRPCOrderNotFound           = pkgErrors.NewGRPCError(codes.NotFound, "ORD001", "Order not found")
	ErrGRPCOrderAlreadyExists      = pkgErrors.NewGRPCError(codes.AlreadyExists, "ORD002", "Order already exists")
	ErrGRPCInvalidOrderStatus      = pkgErrors.NewGRPCError(codes.FailedPrecondition, "ORD003", "Invalid order status")
	ErrGRPCOrderCreationFailed     = pkgErrors.NewGRPCError(codes.Internal, "ORD004", "Order creation failed")
	ErrGRPCOrderUpdateFailed       = pkgErrors.NewGRPCError(codes.Internal, "ORD005", "Order update failed")
	ErrGRPCOrderCancellationFailed = pkgErrors.NewGRPCError(codes.Internal, "ORD006", "Order cancellation failed")
	ErrGRPCOrderNotPending         = pkgErrors.NewGRPCError(codes.FailedPrecondition, "ORD007", "Order is not in pending status")
	ErrGRPCPaymentAmountMismatch   = pkgErrors.NewGRPCError(codes.InvalidArgument, "ORD008", "Payment amount does not match order amount")

	// Event errors
	ErrGRPCEventNotFound        = pkgErrors.NewGRPCError(codes.NotFound, "ORD009", "Event not found")
	ErrGRPCEventNotReadyForSale = pkgErrors.NewGRPCError(codes.FailedPrecondition, "ORD010", "Event not ready for sale")
	ErrGRPCTicketClassNotFound  = pkgErrors.NewGRPCError(codes.NotFound, "ORD011", "Ticket class not found")
	ErrGRPCTicketSoldOut        = pkgErrors.NewGRPCError(codes.FailedPrecondition, "ORD012", "Ticket sold out")
	ErrGRPCNotEnoughTickets     = pkgErrors.NewGRPCError(codes.FailedPrecondition, "ORD013", "Not enough tickets available")
	ErrGRPCEventConfigNotFound  = pkgErrors.NewGRPCError(codes.NotFound, "ORD014", "Event config not found")

	// Checkout errors
	ErrGRPCInvalidCheckoutToken = pkgErrors.NewGRPCError(codes.Unauthenticated, "ORD016", "Invalid checkout token")

	ErrGRPCRequestTimeout = pkgErrors.NewGRPCError(codes.DeadlineExceeded, "ORD017", "Order creation timed out")

	// FailedPrecondition, not Internal: the slot is held by an order that has
	// already finished. Nothing is broken; the buyer starts a new checkout.
	ErrGRPCOrderAlreadyProcessed = pkgErrors.NewGRPCError(codes.FailedPrecondition, "ORD018", "Order already processed")

	// FailedPrecondition, not NotFound: the slot was held by a create that died
	// before writing an order, and NotFound would describe our leftover state as
	// the buyer's mistake. The slot is cleared, so the retry can succeed.
	ErrGRPCPurchaseSlotUnsettled = pkgErrors.NewGRPCError(codes.FailedPrecondition, "ORD019", "Could not start checkout, please try again")
)

// mapError turns a domain error into its wire equivalent. Matching is by
// errors.Is, not ==, so a sentinel still resolves after it has been wrapped on
// the way up; anything reaching default is rendered as codes.Internal by
// response.GrpcError.
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
	case errors.Is(err, order.ErrOrderAlreadyProcessed):
		return ErrGRPCOrderAlreadyProcessed
	case errors.Is(err, order.ErrPurchaseSlotUnsettled):
		return ErrGRPCPurchaseSlotUnsettled
	default:
		return err
	}
}
