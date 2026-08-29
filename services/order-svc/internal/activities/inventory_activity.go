package activities

import (
	"context"

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
		// inventory-svc answers a sold-out reserve with ResourceExhausted. That
		// is a business rejection, not a fault: retrying it burns the default
		// five attempts with backoff before failing anyway, and reaches the
		// caller as an opaque activity error. Tag it so the gRPC layer can turn
		// it into a 4xx, and stop the retries.
		if status.Code(err) == codes.ResourceExhausted {
			return temporal.NewNonRetryableApplicationError(
				order.ErrNotEnoughTickets.Error(),
				order.ErrTypeInsufficientInventory,
				err,
			)
		}
		return err
	}

	return nil
}

func (a *InventoryActivities) ReleaseInventory(ctx context.Context, orderCode string) error {
	_, err := a.Client.Release(ctx, &inventory.ReleaseRequest{
		OrderCode: orderCode,
	})
	if err != nil {
		return err
	}

	return nil
}

func (a *InventoryActivities) ConfirmInventory(ctx context.Context, orderCode string) error {
	_, err := a.Client.Confirm(ctx, &inventory.ConfirmRequest{
		OrderCode: orderCode,
	})
	if err != nil {
		return err
	}

	return nil
}

func (a *InventoryActivities) CheckAvailability(ctx context.Context, items []*inventory.CheckAvailabilityItem) (bool, error) {
	resp, err := a.Client.CheckAvailability(ctx, &inventory.CheckAvailabilityRequest{
		Items: items,
	})
	if err != nil {
		return false, err
	}

	return resp.Accept, nil
}
