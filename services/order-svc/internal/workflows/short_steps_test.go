package workflows

import (
	"context"
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/vogiaan1904/ticketbottle-order/internal/activities"
	"github.com/vogiaan1904/ticketbottle-order/internal/models"
	"github.com/vogiaan1904/ticketbottle-order/internal/order/delivery/kafka"
	"github.com/vogiaan1904/ticketbottle-order/internal/order/delivery/kafka/producer"
	repo "github.com/vogiaan1904/ticketbottle-order/internal/order/repository"
	"github.com/vogiaan1904/ticketbottle-order/pkg/grpc/payment"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"
)

// A local activity runs the registered struct, so these tests register real
// activities over fakes; the SDK's test suite cannot match a mock to a local
// activity registered on a struct. Steps that stay remote are still mocked.
type fakeRepo struct {
	repo.Repository
	calls []string
}

func (f *fakeRepo) Create(_ context.Context, opt repo.CreateOrderOption) (models.Order, error) {
	f.calls = append(f.calls, "Create")
	return models.Order{Code: opt.Code}, nil
}

func (f *fakeRepo) CreateManyItems(_ context.Context, _ string, _ []repo.CreateOrderItemOption) ([]models.OrderItem, error) {
	f.calls = append(f.calls, "CreateManyItems")
	return []models.OrderItem{}, nil
}

func (f *fakeRepo) GetOne(_ context.Context, opt repo.GetOneOrderOption) (models.Order, error) {
	f.calls = append(f.calls, "GetOne")
	return models.Order{Code: opt.Code, Status: models.OrderStatusPending, SessionID: "sess-1", UserID: "u1", EventID: "e1"}, nil
}

func (f *fakeRepo) Update(_ context.Context, code string, _ repo.UpdateOrderOption) (models.Order, error) {
	f.calls = append(f.calls, "Update")
	return models.Order{Code: code}, nil
}

func (f *fakeRepo) ReleasePurchaseSlot(_ context.Context, _, _ string) error {
	f.calls = append(f.calls, "ReleasePurchaseSlot")
	return nil
}

type fakeProducer struct {
	producer.Producer
	published int
}

func (f *fakeProducer) PublishCheckoutCompleted(_ context.Context, _ kafka.CheckoutCompletedEvent) error {
	f.published++
	return nil
}

func newShortStepEnv(t *testing.T, r *fakeRepo, p *fakeProducer) (*testsuite.TestWorkflowEnvironment, *int) {
	t.Helper()
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()
	env.RegisterActivity(&activities.InventoryActivities{})
	env.RegisterActivity(&activities.OrderActivities{Repo: r})
	env.RegisterActivity(&activities.PaymentActivities{})
	env.RegisterActivity(&activities.EventPublishingActivities{Prod: p})
	local := new(int)
	env.SetOnLocalActivityStartedListener(func(*activity.Info, context.Context, []interface{}) { *local++ })
	return env, local
}

func TestCreateOrder_ShortStepsRunAsLocalActivities(t *testing.T) {
	r := &fakeRepo{}
	env, local := newShortStepEnv(t, r, &fakeProducer{})
	env.OnActivity("ReserveInventory", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)
	env.OnActivity("CreatePaymentIntent", mock.Anything, mock.Anything).
		Return(&payment.CreatePaymentIntentResponse{PaymentUrl: "https://pay"}, nil)

	env.ExecuteWorkflow(CreateOrder, testCreateInput())

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow failed: %v", err)
	}
	if *local != 2 {
		t.Fatalf("%d local activities ran, want 2 (order, items)", *local)
	}
	if len(r.calls) != 2 || r.calls[0] != "Create" || r.calls[1] != "CreateManyItems" {
		t.Fatalf("repository calls %v, want [Create CreateManyItems]", r.calls)
	}
}

func TestConfirmOrder_ShortStepsRunAsLocalActivities(t *testing.T) {
	r, p := &fakeRepo{}, &fakeProducer{}
	env, local := newShortStepEnv(t, r, p)
	env.OnActivity("ConfirmInventory", mock.Anything, mock.Anything).Return(nil)

	env.ExecuteWorkflow(ConfirmOrder, &ConfirmOrderWorkflowInput{
		OrderCode: "TB-TEST-0001", Status: models.OrderStatusCompleted,
	})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow failed: %v", err)
	}
	if *local != 4 {
		t.Fatalf("%d local activities ran, want 4 (get, update, release, publish)", *local)
	}
	if p.published != 1 {
		t.Fatalf("checkout.completed published %d times, want 1", p.published)
	}
}
