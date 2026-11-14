package service

import (
	"context"
	"fmt"

	"github.com/vogiaan1904/ticketbottle-order/internal/order"
	"github.com/vogiaan1904/ticketbottle-order/pkg/grpc/inventory"
	"github.com/vogiaan1904/ticketbottle-order/pkg/grpc/waitroom"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func generatePaymentIdempotencyKey(orderCode string, provider string) string {
	return fmt.Sprintf("%s:%s", orderCode, provider)
}

func (s *implService) releaseTickets(ctx context.Context, code string, reason inventory.ReleaseReason) error {
	_, err := s.invSvc.Release(ctx, &inventory.ReleaseRequest{
		OrderCode: code,
		Reason:    reason,
	})
	if err != nil {
		s.l.Errorf(ctx, "Failed to release tickets for order %s: %v", code, err)
		return err
	}

	s.l.Infof(ctx, "Successfully released tickets for order %s", code)
	return nil
}

// validateCheckoutToken asks the waitroom whether the token still admits this
// buyer to this event, and returns the session it admits (0033).
func (s *implService) validateCheckoutToken(ctx context.Context, in order.CreateOrderInput) (string, error) {
	if in.CheckoutToken == "" {
		return "", order.ErrInvalidCheckoutToken
	}

	out, err := s.wrSvc.ValidateCheckoutToken(ctx, &waitroom.ValidateCheckoutTokenRequest{
		Token:   in.CheckoutToken,
		UserId:  in.UserID,
		EventId: in.EventID,
	})
	switch status.Code(err) {
	case codes.OK:
		return out.GetSessionId(), nil
	case codes.Unauthenticated:
		return "", order.ErrInvalidCheckoutToken
	case codes.Unavailable, codes.DeadlineExceeded:
		return "", order.ErrWaitroomUnavailable
	default:
		return "", err
	}
}

// ticketsInOrder is how many tickets an order takes, across all its items.
func ticketsInOrder(items []order.OrderItemInput) int32 {
	var n int32
	for _, item := range items {
		n += item.Quantity
	}
	return n
}
