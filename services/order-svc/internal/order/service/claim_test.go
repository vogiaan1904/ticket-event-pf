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
	"github.com/vogiaan1904/ticketbottle-order/internal/workflows"
	"github.com/vogiaan1904/ticketbottle-order/pkg/grpc/payment"
	"github.com/vogiaan1904/ticketbottle-order/pkg/logger"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// testCreateTimeout stands in for ORDER_CREATE_TIMEOUT. It is only the
// caller's own leg of the settle window -- purchaseSlotSettleWindow adds the
// workflow's server-side budget on top of it.
const testCreateTimeout = 30 * time.Second

const testPaymentMethod = models.PaymentMethod("STRIPE")

// stubPaymentClient answers only the lookup resumeExistingOrder makes -- the one
// collaborator faked, since the slot tests use a real repo on DynamoDB local.
type stubPaymentClient struct {
	payment.PaymentServiceClient
	url string
	err error
}

func (c stubPaymentClient) GetPaymentUrlByIdempotencyKey(ctx context.Context, in *payment.GetPaymentUrlByIdempotencyKeyRequest, opts ...grpc.CallOption) (*payment.GetPaymentUrlByIdempotencyKeyResponse, error) {
	if c.err != nil {
		return nil, c.err
	}

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

// A claim naming no order is not proof the create died: the saga reserves and
// starts the workflow before writing the row, so a duplicate inside that window
// sees the same "not found". Clearing on sight hands the live buyer's slot to
// the duplicate -- two orders, two holds. Inside the window: leave it, retry.
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

// Past the settle window nothing is still running: it finished (this lookup
// would have found the order) or it died. Clear the claim, tell the buyer to
// retry into the now-free slot.
func TestClaimPurchaseSlot_AnOldOrphanClaimIsClearedAndTheRetryTakesTheSlot(t *testing.T) {
	l := logger.InitializeTestZapLogger()
	db := dynamotest.NewClient(t)
	ctx := context.Background()

	s := &implService{
		l:             l,
		repo:          repo.New(l, db, dynamotest.TableName),
		pmtSvc:        stubPaymentClient{url: "https://pay.test/checkout"},
		createTimeout: testCreateTimeout,
		clock:         time.Now,
	}

	// The claim is written by a repo with its clock pinned past the settle
	// window, so its age tracks the window instead of the test's wall clock.
	abandonedAt := time.Now().Add(-2 * s.purchaseSlotSettleWindow())
	staleRepo := repo.NewWithClock(l, db, dynamotest.TableName, func() time.Time { return abandonedAt })

	if _, _, err := staleRepo.ClaimPurchaseSlot(ctx, "sess-abandoned", "TB-GHOST-0001"); err != nil {
		t.Fatalf("seed claim: %v", err)
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

// A terminal order holds nothing, so its claim has no work left. This request
// must take the slot, not merely free it, or the create it runs is unguarded.
func TestClaimPurchaseSlot_ATerminalOrderReleasesTheSlotAndThisCreateTakesIt(t *testing.T) {
	for _, status := range []models.OrderStatus{
		models.OrderStatusCancelled,
		models.OrderStatusPaymentFailed,
		models.OrderStatusTimeout,
		models.OrderStatusRefundRequired,
		models.OrderStatusRefunded,
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

// The settle window has to outlive the create, and the create is not bounded by
// the caller's deadline -- wfRun.Get returning does not stop the workflow. Sized
// on the caller's timeout alone it judges a live workflow dead and lets one slot
// produce two orders and two holds.
func TestPurchaseSlotSettleWindow_OutlivesACreateStillRunningServerSide(t *testing.T) {
	s := &implService{createTimeout: testCreateTimeout}

	budget := workflows.CreateOrderSlotBudget()
	if window := s.purchaseSlotSettleWindow(); window <= budget {
		t.Fatalf("settle window is %v, but a create can legitimately spend %v server-side before an order row exists", window, budget)
	}
}

// The order row lands two saga steps before the payment intent, so inside that
// window payment-svc refuses the lookup. That is not the buyer's answer -- their
// checkout is still being set up, and a 403 stops a purchase mid-success.
func TestClaimPurchaseSlot_APendingOrderWithNoPaymentYetIsNotForbidden(t *testing.T) {
	s, r := newSlotService(t)
	s.pmtSvc = stubPaymentClient{err: status.Error(codes.PermissionDenied, "permission denied")}
	ctx := context.Background()

	seedOrder(t, r, "TB-EARLY-0001", models.OrderStatusPending)
	if _, _, err := r.ClaimPurchaseSlot(ctx, "sess-early", "TB-EARLY-0001"); err != nil {
		t.Fatalf("seed claim: %v", err)
	}

	out, err := s.claimPurchaseSlot(ctx, "sess-early", "TB-DUPLICATE-0002", testPaymentMethod)
	if !errors.Is(err, order.ErrPurchaseSlotUnsettled) {
		t.Fatalf("duplicate create on an order with no payment yet returned (%v, %v), want ErrPurchaseSlotUnsettled", out, err)
	}
	if status.Code(err) == codes.PermissionDenied {
		t.Fatal("a buyer whose checkout is still being set up was told they are forbidden")
	}

	held, _, err := r.ClaimPurchaseSlot(ctx, "sess-early", "TB-INTRUDER-0003")
	if !errors.Is(err, order.ErrPurchaseSlotTaken) || held != "TB-EARLY-0001" {
		t.Fatalf("slot is held by %q (%v), want the in-flight order TB-EARLY-0001", held, err)
	}
}
