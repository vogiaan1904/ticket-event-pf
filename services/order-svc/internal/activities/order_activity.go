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

// CreateOrder is idempotent: a worker can crash between a landed PutItem and
// the recorded result, replaying this call.
//
//	already exists, same buyer+event -> serve the written order
//	already exists, different one    -> code collision, fail non-retryably
func (a *OrderActivities) CreateOrder(ctx context.Context, opt repo.CreateOrderOption) (*models.Order, error) {
	o, err := a.Repo.Create(ctx, opt)
	if err == nil {
		return &o, nil
	}
	if !errors.Is(err, order.ErrOrderAlreadyExists) {
		return nil, err
	}

	// A failed read-back is transient (flake, row not yet visible): surface it
	// so Temporal retries, rather than masking it as ErrOrderAlreadyExists.
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

// GetOrder tags a missing order as an outcome, not a fault: retrying never
// materialises an order that was never written or was rolled back, and the
// caller has to tell that from an unreachable datastore.
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

// ReleasePurchaseSlot gives a buyer's slot back once their purchase has an
// outcome. The repository refuses to delete a claim that has moved on to another
// order, so a late release cannot strip a create that started behind it.
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
