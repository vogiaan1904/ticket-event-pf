package service

import (
	"context"
	"fmt"

	"github.com/vogiaan1904/ticketbottle-order/internal/models"
	"github.com/vogiaan1904/ticketbottle-order/internal/order"
	"github.com/vogiaan1904/ticketbottle-order/pkg/grpc/inventory"
)

func generatePaymentIdempotencyKey(orderCode string, provider string) string {
	return fmt.Sprintf("%s:%s", orderCode, provider)
}

func (s *implService) releaseTickets(ctx context.Context, code string) error {
	_, err := s.invSvc.Release(ctx, &inventory.ReleaseRequest{
		OrderCode: code,
	})
	if err != nil {
		s.l.Errorf(ctx, "Failed to release tickets for order %s: %v", code, err)
		return err
	}

	s.l.Infof(ctx, "Successfully released tickets for order %s", code)
	return nil
}

func (s *implService) validateCheckoutToken(ctx context.Context, in order.CreateOrderInput) (models.CheckoutTokenClaim, error) {
	if in.CheckoutToken == "" {
		return models.CheckoutTokenClaim{}, order.ErrInvalidCheckoutToken
	}

	p, err := s.jwt.Verify(ctx, in.CheckoutToken)
	if err != nil {
		return models.CheckoutTokenClaim{}, order.ErrInvalidCheckoutToken
	}

	if p.UserID != in.UserID || p.EventID != in.EventID {
		return models.CheckoutTokenClaim{}, order.ErrInvalidCheckoutToken
	}

	return p.CheckoutTokenClaim, nil
}
