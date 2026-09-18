package activities

import (
	"context"

	"github.com/vogiaan1904/ticketbottle-order/internal/metrics"
	"github.com/vogiaan1904/ticketbottle-order/internal/order"
	"github.com/vogiaan1904/ticketbottle-order/pkg/grpc/inventory"
	"go.temporal.io/sdk/temporal"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type InventoryActivities struct {
	Client inventory.InventoryServiceClient
}

func NewInventoryActivities(client inventory.InventoryServiceClient) *InventoryActivities {
	return &InventoryActivities{
		Client: client,
	}
}

func (a *InventoryActivities) ReserveInventory(ctx context.Context, orderCode string, expiresAt string, items []*inventory.ReserveItem) error {
	_, err := a.Client.Reserve(ctx, &inventory.ReserveRequest{
		OrderCode: orderCode,
		ExpiresAt: expiresAt,
		Items:     items,
	})
	if err != nil {
		// FailedPrecondition = sold out / sale closed / state conflict. Retrying
		// burns five attempts to fail anyway, so tag it non-retryable and let
		// the gRPC layer turn it into a 4xx.
		if status.Code(err) == codes.FailedPrecondition {
			soldOutErr := temporal.NewNonRetryableApplicationError(
				order.ErrNotEnoughTickets.Error(),
				order.ErrTypeInsufficientInventory,
				err,
			)
			metrics.RecordActivityFailure("ReserveInventory", soldOutErr)
			return soldOutErr
		}
		metrics.RecordActivityFailure("ReserveInventory", err)
		return err
	}

	return nil
}

func (a *InventoryActivities) ReleaseInventory(ctx context.Context, orderCode string) error {
	_, err := a.Client.Release(ctx, &inventory.ReleaseRequest{
		OrderCode: orderCode,
	})
	if err != nil {
		metrics.RecordActivityFailure("ReleaseInventory", err)
		return err
	}

	return nil
}

func (a *InventoryActivities) ConfirmInventory(ctx context.Context, orderCode string) error {
	_, err := a.Client.Confirm(ctx, &inventory.ConfirmRequest{
		OrderCode: orderCode,
	})
	if err != nil {
		// Unfulfillable, not a fault to retry:
		//	FailedPrecondition -> hold expired and resold, or state conflict
		//	NotFound           -> rows hard-deleted, or reserve never ran
		// Internal is excluded on purpose: counter drift is our bug, and pages
		// someone rather than silently refunding the buyer.
		code := status.Code(err)
		if code == codes.FailedPrecondition || code == codes.NotFound {
			cannotConfirmErr := temporal.NewNonRetryableApplicationError(
				order.ErrInventoryCannotConfirm.Error(),
				order.ErrTypeInventoryCannotConfirm,
				err,
			)
			metrics.RecordActivityFailure("ConfirmInventory", cannotConfirmErr)
			return cannotConfirmErr
		}
		metrics.RecordActivityFailure("ConfirmInventory", err)
		return err
	}

	return nil
}

func (a *InventoryActivities) CheckAvailability(ctx context.Context, items []*inventory.CheckAvailabilityItem) (bool, error) {
	resp, err := a.Client.CheckAvailability(ctx, &inventory.CheckAvailabilityRequest{
		Items: items,
	})
	if err != nil {
		metrics.RecordActivityFailure("CheckAvailability", err)
		return false, err
	}

	return resp.Accept, nil
}
