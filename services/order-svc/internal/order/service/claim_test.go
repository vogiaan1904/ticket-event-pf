package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/vogiaan1904/ticketbottle-order/internal/models"
	"github.com/vogiaan1904/ticketbottle-order/internal/order"
	repo "github.com/vogiaan1904/ticketbottle-order/internal/order/repository"
	"github.com/vogiaan1904/ticketbottle-order/internal/testutil/dynamotest"
	"github.com/vogiaan1904/ticketbottle-order/pkg/grpc/payment"
	"github.com/vogiaan1904/ticketbottle-order/pkg/logger"
	"google.golang.org/grpc"
)

// testCreateTimeout stands in for ORDER_CREATE_TIMEOUT: the settle window a
// claim naming no order is given before it is treated as abandoned is this
// plus purchaseSlotSettleMargin.
const testCreateTimeout = 30 * time.Second

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
		l:             l,
		repo:          r,
		pmtSvc:        stubPaymentClient{url: "https://pay.test/checkout"},
		createTimeout: testCreateTimeout,
		clock:         time.Now,
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

// A claim naming an order that does not exist is not proof the create behind
// it died: the saga reserves inventory and starts a workflow before the order
// row is written, so a duplicate arriving inside that window sees exactly the
// same "not found" a genuinely abandoned claim would. Clearing it on sight
// would destroy the in-flight buyer's claim and hand their slot to this
// duplicate -- two orders, two inventory holds, from one slot. Within the
// settle window the claim must be left standing and the duplicate told to
// retry instead.
func TestClaimPurchaseSlot_AYoungOrphanClaimIsLeftStandingForTheInFlightCreate(t *testing.T) {
	s, r := newSlotService(t)
	ctx := context.Background()

	if _, _, err := r.ClaimPurchaseSlot(ctx, "sess-inflight", "TB-GHOST-0001"); err != nil {
		t.Fatalf("seed claim: %v", err)
	}

	out, err := s.claimPurchaseSlot(ctx, "sess-inflight", "TB-DUPLICATE-0002", testPaymentMethod)
	if !errors.Is(err, order.ErrPurchaseSlotUnsettled) {
		t.Fatalf("duplicate claim inside the settle window returned (%v, %v), want ErrPurchaseSlotUnsettled", out, err)
	}
	if errors.Is(err, repo.ErrOrderNotFound) {
		t.Fatal("duplicate claim inside the settle window surfaced ErrOrderNotFound to the buyer")
	}

	held, _, err := r.ClaimPurchaseSlot(ctx, "sess-inflight", "TB-INTRUDER-0003")
	if !errors.Is(err, order.ErrPurchaseSlotTaken) {
		t.Fatal("a fresh orphan claim was cleared before its create could finish")
	}
	if held != "TB-GHOST-0001" {
		t.Fatalf("slot is held by %q, want the in-flight create's own claim TB-GHOST-0001", held)
	}
}

// Past the settle window, no create is still running behind an orphaned
// claim: it either finished -- and this lookup would have found its order --
// or it died. The claim is genuinely abandoned, so it is cleared and the
// buyer is told to retry into the now-free slot.
func TestClaimPurchaseSlot_AnOldOrphanClaimIsClearedAndTheRetryTakesTheSlot(t *testing.T) {
	l := logger.InitializeTestZapLogger()
	db := dynamotest.NewClient(t)

	// A dedicated repo whose clock is pinned to well before the settle window
	// writes the claim, so its age does not depend on how fast the test runs.
	abandonedAt := time.Now().Add(-2 * time.Minute)
	staleRepo := repo.NewWithClock(l, db, dynamotest.TableName, func() time.Time { return abandonedAt })
	ctx := context.Background()

	if _, _, err := staleRepo.ClaimPurchaseSlot(ctx, "sess-abandoned", "TB-GHOST-0001"); err != nil {
		t.Fatalf("seed claim: %v", err)
	}

	s := &implService{
		l:             l,
		repo:          repo.New(l, db, dynamotest.TableName),
		pmtSvc:        stubPaymentClient{url: "https://pay.test/checkout"},
		createTimeout: testCreateTimeout,
		clock:         time.Now,
	}

	out, err := s.claimPurchaseSlot(ctx, "sess-abandoned", "TB-RETRY-0002", testPaymentMethod)
	if !errors.Is(err, order.ErrPurchaseSlotUnsettled) {
		t.Fatalf("claim over an abandoned orphan returned (%v, %v), want ErrPurchaseSlotUnsettled", out, err)
	}
	if errors.Is(err, repo.ErrOrderNotFound) {
		t.Fatal("claim over an abandoned orphan surfaced ErrOrderNotFound to the buyer")
	}

	existing, _, err := staleRepo.ClaimPurchaseSlot(ctx, "sess-abandoned", "TB-RETRY-0003")
	if err != nil {
		t.Fatalf("slot is still held by %q after its settle window passed: %v", existing, err)
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
			if _, _, err := r.ClaimPurchaseSlot(ctx, dedupeKey, "TB-DEAD-0001"); err != nil {
				t.Fatalf("seed claim: %v", err)
			}

			out, err := s.claimPurchaseSlot(ctx, dedupeKey, "TB-FRESH-0002", testPaymentMethod)
			if err != nil {
				t.Fatalf("claim over a %s order: %v", status, err)
			}
			if out != nil {
				t.Fatalf("claim over a %s order returned an order to resume: %+v", status, out)
			}

			held, _, err := r.ClaimPurchaseSlot(ctx, dedupeKey, "TB-INTRUDER-0003")
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
	if _, _, err := r.ClaimPurchaseSlot(ctx, "sess-done", "TB-DONE-0001"); err != nil {
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

	held, _, err := r.ClaimPurchaseSlot(ctx, "sess-done", "TB-INTRUDER-0003")
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
	if _, _, err := r.ClaimPurchaseSlot(ctx, "sess-live", "TB-LIVE-0001"); err != nil {
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
