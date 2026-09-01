package service

import (
	"context"
	"errors"
	"testing"

	"github.com/vogiaan1904/ticketbottle-order/internal/models"
	"github.com/vogiaan1904/ticketbottle-order/internal/order"
	repo "github.com/vogiaan1904/ticketbottle-order/internal/order/repository"
	"github.com/vogiaan1904/ticketbottle-order/internal/testutil/dynamotest"
	"github.com/vogiaan1904/ticketbottle-order/pkg/grpc/payment"
	"github.com/vogiaan1904/ticketbottle-order/pkg/logger"
	"google.golang.org/grpc"
)

const testPaymentMethod = models.PaymentMethod("STRIPE")

// stubPaymentClient answers only the lookup resumeExistingOrder makes. The
// slot tests run against a real repository on DynamoDB local, so this is the
// one collaborator that has to be faked.
type stubPaymentClient struct {
	payment.PaymentServiceClient
	url string
}

func (c stubPaymentClient) GetPaymentUrlByIdempotencyKey(ctx context.Context, in *payment.GetPaymentUrlByIdempotencyKeyRequest, opts ...grpc.CallOption) (*payment.GetPaymentUrlByIdempotencyKeyResponse, error) {
	return &payment.GetPaymentUrlByIdempotencyKeyResponse{PaymentUrl: c.url}, nil
}

func newSlotService(t *testing.T) (*implService, repo.Repository) {
	t.Helper()

	l := logger.InitializeTestZapLogger()
	r := repo.New(l, dynamotest.NewClient(t), dynamotest.TableName)

	return &implService{
		l:      l,
		repo:   r,
		pmtSvc: stubPaymentClient{url: "https://pay.test/checkout"},
	}, r
}

func seedOrder(t *testing.T, r repo.Repository, code string, status models.OrderStatus) {
	t.Helper()

	_, err := r.Create(context.Background(), repo.CreateOrderOption{
		Code: code, UserID: "u1", EventID: "e1",
		Currency: "VND", TotalAmount: 1000, Status: status,
	})
	if err != nil {
		t.Fatalf("seed %s order %s: %v", status, code, err)
	}
}

// A create that dies before writing its order leaves the slot claimed for an
// order that does not exist. The buyer's next attempt must not be refused on
// behalf of it: the stale claim is dropped and the buyer is told to retry --
// never that their order was not found, which is not a thing they can act on.
func TestClaimPurchaseSlot_AClaimNamingNoOrderIsClearedAndTheBuyerRetries(t *testing.T) {
	s, r := newSlotService(t)
	ctx := context.Background()

	if _, err := r.ClaimPurchaseSlot(ctx, "sess-orphan", "TB-GHOST-0001"); err != nil {
		t.Fatalf("seed claim: %v", err)
	}

	out, err := s.claimPurchaseSlot(ctx, "sess-orphan", "TB-NEW-0002", testPaymentMethod)
	if !errors.Is(err, order.ErrPurchaseSlotUnsettled) {
		t.Fatalf("claim over an orphan returned (%v, %v), want ErrPurchaseSlotUnsettled", out, err)
	}
	if errors.Is(err, repo.ErrOrderNotFound) {
		t.Fatal("claim over an orphan surfaced ErrOrderNotFound to the buyer")
	}

	existing, err := r.ClaimPurchaseSlot(ctx, "sess-orphan", "TB-RETRY-0003")
	if err != nil {
		t.Fatalf("slot is still held by %q after the orphan was found: %v", existing, err)
	}
}

// An order that ended without buying anything holds nothing, so its claim has
// no work left to do. The buyer must be able to start again -- and this
// request has to take the slot itself, not merely free it, or the create it
// goes on to run would be unguarded.
func TestClaimPurchaseSlot_ATerminalOrderReleasesTheSlotAndThisCreateTakesIt(t *testing.T) {
	for _, status := range []models.OrderStatus{
		models.OrderStatusCancelled,
		models.OrderStatusPaymentFailed,
		models.OrderStatusTimeout,
	} {
		t.Run(string(status), func(t *testing.T) {
			s, r := newSlotService(t)
			ctx := context.Background()

			dedupeKey := "sess-" + string(status)
			seedOrder(t, r, "TB-DEAD-0001", status)
			if _, err := r.ClaimPurchaseSlot(ctx, dedupeKey, "TB-DEAD-0001"); err != nil {
				t.Fatalf("seed claim: %v", err)
			}

			out, err := s.claimPurchaseSlot(ctx, dedupeKey, "TB-FRESH-0002", testPaymentMethod)
			if err != nil {
				t.Fatalf("claim over a %s order: %v", status, err)
			}
			if out != nil {
				t.Fatalf("claim over a %s order returned an order to resume: %+v", status, out)
			}

			held, err := r.ClaimPurchaseSlot(ctx, dedupeKey, "TB-INTRUDER-0003")
			if !errors.Is(err, order.ErrPurchaseSlotTaken) {
				t.Fatalf("slot was left free after a %s order released it", status)
			}
			if held != "TB-FRESH-0002" {
				t.Fatalf("slot is held by %q, want the retrying create TB-FRESH-0002", held)
			}
		})
	}
}

// One order per buyer is the point of the slot, so a completed order keeps it.
// The duplicate create is answered with that order and its payment URL.
func TestClaimPurchaseSlot_ACompletedOrderKeepsTheSlot(t *testing.T) {
	s, r := newSlotService(t)
	ctx := context.Background()

	seedOrder(t, r, "TB-DONE-0001", models.OrderStatusCompleted)
	if _, err := r.ClaimPurchaseSlot(ctx, "sess-done", "TB-DONE-0001"); err != nil {
		t.Fatalf("seed claim: %v", err)
	}

	out, err := s.claimPurchaseSlot(ctx, "sess-done", "TB-SECOND-0002", testPaymentMethod)
	if err != nil {
		t.Fatalf("claim over a completed order: %v", err)
	}
	if out == nil {
		t.Fatal("claim over a completed order let a second create proceed")
	}
	if out.Order.Code != "TB-DONE-0001" {
		t.Fatalf("resumed order %q, want TB-DONE-0001", out.Order.Code)
	}
	if out.PaymentUrl != "https://pay.test/checkout" {
		t.Fatalf("resumed order has payment url %q", out.PaymentUrl)
	}

	held, err := r.ClaimPurchaseSlot(ctx, "sess-done", "TB-INTRUDER-0003")
	if !errors.Is(err, order.ErrPurchaseSlotTaken) {
		t.Fatal("a completed order gave up its slot")
	}
	if held != "TB-DONE-0001" {
		t.Fatalf("slot is held by %q, want TB-DONE-0001", held)
	}
}

// A pending order is a checkout still in progress: the retrying client is sent
// back to it rather than being given a second one.
func TestClaimPurchaseSlot_APendingOrderIsResumed(t *testing.T) {
	s, r := newSlotService(t)
	ctx := context.Background()

	seedOrder(t, r, "TB-LIVE-0001", models.OrderStatusPending)
	if _, err := r.ClaimPurchaseSlot(ctx, "sess-live", "TB-LIVE-0001"); err != nil {
		t.Fatalf("seed claim: %v", err)
	}

	out, err := s.claimPurchaseSlot(ctx, "sess-live", "TB-SECOND-0002", testPaymentMethod)
	if err != nil {
		t.Fatalf("claim over a pending order: %v", err)
	}
	if out == nil || out.Order.Code != "TB-LIVE-0001" {
		t.Fatalf("claim over a pending order returned %+v, want the live order", out)
	}
}

// A free slot is taken outright and the create goes ahead.
func TestClaimPurchaseSlot_AFreeSlotIsWonOutright(t *testing.T) {
	s, _ := newSlotService(t)

	out, err := s.claimPurchaseSlot(context.Background(), "sess-free", "TB-ONLY-0001", testPaymentMethod)
	if err != nil {
		t.Fatalf("claim a free slot: %v", err)
	}
	if out != nil {
		t.Fatalf("claim a free slot returned an order to resume: %+v", out)
	}
}
