package activities

import (
	"context"
	"errors"

	"github.com/vogiaan1904/ticketbottle-order/internal/models"
	"github.com/vogiaan1904/ticketbottle-order/internal/order"
	repo "github.com/vogiaan1904/ticketbottle-order/internal/order/repository"
	"go.temporal.io/sdk/temporal"
)

type OrderActivities struct {
	Repo repo.Repository
}

func NewOrderActivities(repo repo.Repository) *OrderActivities {
	return &OrderActivities{
		Repo: repo,
	}
}

// CreateOrder must be idempotent: Temporal's retry policy guarantees a worker
// can crash after a PutItem lands but before the activity result is recorded,
// which replays this same call. repository.Create correctly refuses the
// replay with ErrOrderAlreadyExists, so the retry path reads the order back
// by code instead of failing. It is only served if it actually belongs to
// this request -- otherwise the code was reused by a different order, which
// is a code-generation bug, not a retry, and must not be served to the wrong
// buyer.
func (a *OrderActivities) CreateOrder(ctx context.Context, opt repo.CreateOrderOption) (*models.Order, error) {
	o, err := a.Repo.Create(ctx, opt)
	if err == nil {
		return &o, nil
	}
	if !errors.Is(err, order.ErrOrderAlreadyExists) {
		return nil, err
	}

	// The read-back failing (network flake, or the row not visible yet) is
	// itself a transient condition: surface it as-is so Temporal's retry
	// policy runs again, rather than masking it behind ErrOrderAlreadyExists.
	existing, getErr := a.Repo.GetByCode(ctx, opt.Code)
	if getErr != nil {
		return nil, getErr
	}

	if existing.UserID != opt.UserID || existing.EventID != opt.EventID {
		return nil, temporal.NewNonRetryableApplicationError(
			order.ErrOrderCreationFailed.Error(),
			order.ErrTypeOrderCodeCollision,
			err,
		)
	}

	return &existing, nil
}

func (a *OrderActivities) CreateOrderItems(ctx context.Context, oCode string, items []repo.CreateOrderItemOption) ([]models.OrderItem, error) {
	itms, err := a.Repo.CreateManyItems(ctx, oCode, items)
	if err != nil {
		return nil, err
	}

	return itms, nil
}

// GetOrder tags an order that does not exist as an outcome rather than a
// fault. No number of attempts makes an order appear that was never written or
// that a compensation rolled back, and whoever is driving the workflow has to
// tell that apart from an unreachable datastore, which may well answer next
// time.
func (a *OrderActivities) GetOrder(ctx context.Context, code string) (*models.Order, error) {
	o, err := a.Repo.GetOne(ctx, repo.GetOneOrderOption{
		FilterOrder: order.FilterOrder{
			Code: code,
		},
	})
	if err != nil {
		if errors.Is(err, repo.ErrOrderNotFound) {
			return nil, temporal.NewNonRetryableApplicationError(
				order.ErrOrderNotFound.Error(),
				order.ErrTypeOrderNotFound,
				err,
			)
		}

		return nil, err
	}

	return &o, nil
}

func (a *OrderActivities) UpdateOrderStatus(ctx context.Context, code string, status models.OrderStatus) error {
	_, err := a.Repo.Update(ctx, code, repo.UpdateOrderOption{
		Status: status,
	})
	if err != nil {
		return err
	}

	return nil
}

// ReleasePurchaseSlot gives a buyer's purchase slot back once their purchase
// has an outcome. The repository refuses to delete a claim that has since moved
// on to another order, so a late release cannot take a live claim away from a
// create that has already started behind it.
func (a *OrderActivities) ReleasePurchaseSlot(ctx context.Context, dedupeKey, orderCode string) error {
	return a.Repo.ReleasePurchaseSlot(ctx, dedupeKey, orderCode)
}

func (a *OrderActivities) DeleteOrder(ctx context.Context, code string) error {
	err := a.Repo.Delete(ctx, code)
	if err != nil {
		return err
	}

	return nil
}

func (a *OrderActivities) DeleteOrderItems(ctx context.Context, code string) error {
	err := a.Repo.DeleteItemByOrderCode(ctx, code)
	if err != nil {
		return err
	}

	return nil
}
