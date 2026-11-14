package service

import (
	"context"
	"errors"
	"testing"

	"github.com/vogiaan1904/ticketbottle-order/internal/models"
	"github.com/vogiaan1904/ticketbottle-order/internal/order"
	"github.com/vogiaan1904/ticketbottle-order/internal/order/delivery/kafka/producer"
	"github.com/vogiaan1904/ticketbottle-order/internal/order/delivery/kafka"
	repo "github.com/vogiaan1904/ticketbottle-order/internal/order/repository"
	"github.com/vogiaan1904/ticketbottle-order/pkg/grpc/inventory"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/emptypb"
)

type releaseRecorder struct {
	inventory.InventoryServiceClient
	released []string
	reasons  []inventory.ReleaseReason
}

func (c *releaseRecorder) Release(ctx context.Context, in *inventory.ReleaseRequest, opts ...grpc.CallOption) (*emptypb.Empty, error) {
	c.released = append(c.released, in.GetOrderCode())
	c.reasons = append(c.reasons, in.GetReason())
	return &emptypb.Empty{}, nil
}

// payingRepo confirms the order's payment just after Cancel has read it.
type payingRepo struct {
	repo.Repository
}

func (r payingRepo) GetByCode(ctx context.Context, code string) (models.Order, error) {
	o, err := r.Repository.GetByCode(ctx, code)
	if err == nil {
		_, err = r.Repository.Update(ctx, code, repo.UpdateOrderOption{Status: models.OrderStatusCompleted})
	}
	return o, err
}

func TestCancel_TheOwnerCancelsAPendingOrder(t *testing.T) {
	svc, r := newSlotService(t)
	inv := &releaseRecorder{}
	svc.invSvc = inv
	seedOrder(t, r, "TB-CAN-0001", models.OrderStatusPending)

	if err := svc.Cancel(context.Background(), "TB-CAN-0001", "u1"); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	o, _ := r.GetByCode(context.Background(), "TB-CAN-0001")
	if o.Status != models.OrderStatusCancelled || len(inv.released) != 1 {
		t.Fatalf("status = %s, released %v; want CANCELLED and one release", o.Status, inv.released)
	}
}

// A payment that confirms between Cancel's read and its write keeps its order
// and its tickets.
func TestCancel_APaymentThatLandedFirstWins(t *testing.T) {
	svc, r := newSlotService(t)
	inv := &releaseRecorder{}
	svc.invSvc = inv
	svc.repo = payingRepo{r}
	seedOrder(t, r, "TB-CAN-0002", models.OrderStatusPending)

	err := svc.Cancel(context.Background(), "TB-CAN-0002", "u1")

	o, _ := r.GetByCode(context.Background(), "TB-CAN-0002")
	if !errors.Is(err, order.ErrOrderNotPending) || o.Status != models.OrderStatusCompleted || len(inv.released) != 0 {
		t.Fatalf("err = %v, status = %s, released %v; want ErrOrderNotPending, COMPLETED, none", err, o.Status, inv.released)
	}
}

// A cancelled checkout's link may still be paid; its hold must stay re-acquirable.
func TestCancel_ReleasesAsCheckoutEnded(t *testing.T) {
	svc, r := newSlotService(t)
	inv := &releaseRecorder{}
	svc.invSvc = inv
	seedOrder(t, r, "TB-CAN-0003", models.OrderStatusPending)

	if err := svc.Cancel(context.Background(), "TB-CAN-0003", "u1"); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if len(inv.reasons) != 1 || inv.reasons[0] != inventory.ReleaseReason_RELEASE_REASON_CHECKOUT_ENDED {
		t.Fatalf("release reasons = %v, want one CHECKOUT_ENDED", inv.reasons)
	}
}

type silentProducer struct{ producer.Producer }

func (silentProducer) PublishCheckoutFailed(context.Context, kafka.CheckoutFailedEvent) error {
	return nil
}

// A failed payment is final: its hold must not be re-taken by a later Confirm.
func TestHandlePaymentFailed_ReleasesAsUndone(t *testing.T) {
	svc, r := newSlotService(t)
	inv := &releaseRecorder{}
	svc.invSvc = inv
	svc.prod = silentProducer{}
	seedOrder(t, r, "TB-CAN-0004", models.OrderStatusPending)

	if err := svc.HandlePaymentFailed(context.Background(), order.HandlePaymentFailedInput{OrderCode: "TB-CAN-0004"}); err != nil {
		t.Fatalf("handle payment failed: %v", err)
	}
	if len(inv.reasons) != 1 || inv.reasons[0] != inventory.ReleaseReason_RELEASE_REASON_UNSPECIFIED {
		t.Fatalf("release reasons = %v, want one UNSPECIFIED", inv.reasons)
	}
}
