package service

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/vogiaan1904/ticketbottle-order/internal/infra/temporal"
	"github.com/vogiaan1904/ticketbottle-order/internal/models"
	"github.com/vogiaan1904/ticketbottle-order/internal/order"
	repo "github.com/vogiaan1904/ticketbottle-order/internal/order/repository"
	"github.com/vogiaan1904/ticketbottle-order/internal/workflows"
	"github.com/vogiaan1904/ticketbottle-order/pkg/grpc/event"
	"github.com/vogiaan1904/ticketbottle-order/pkg/grpc/inventory"
	"github.com/vogiaan1904/ticketbottle-order/pkg/grpc/payment"
	"github.com/vogiaan1904/ticketbottle-order/pkg/util"
	"go.temporal.io/sdk/client"
	sdktemporal "go.temporal.io/sdk/temporal"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Cancellation has to outlive the deadline that triggered it, but the handler
// only has to tell the workflow, not watch it stop.
const cancelWorkflowTimeout = 5 * time.Second

// Releasing a stranded purchase slot has to outlive the deadline that stranded
// it, for the same reason cancellation does.
const releaseSlotTimeout = 5 * time.Second

// claimAttempts bounds the retake loop: a second pass is normal, a fourth means
// the slot is churning rather than settling.
const claimAttempts = 3

// purchaseSlotSettleMargin is slack on top of purchaseSlotSettleWindow's real
// budget: the DynamoDB round trips on either end, plus clock skew between the
// process that wrote a claim and the one reading it back.
const purchaseSlotSettleMargin = 30 * time.Second

// errPurchaseSlotReleased tells the claim loop the slot's order is terminal and
// the claim is dropped, so this request should retake it. Never reaches a caller.
var errPurchaseSlotReleased = errors.New("purchase slot released")

// mapWorkflowError translates a Temporal failure into a domain error.
// Why match on the type string: wfRun.Get rebuilds the error from a serialised
// failure proto, so the sentinel is gone and only ApplicationError.Type survives.
func mapWorkflowError(err error) error {
	var appErr *sdktemporal.ApplicationError
	if !errors.As(err, &appErr) {
		return err
	}

	switch appErr.Type() {
	case order.ErrTypeInsufficientInventory:
		return order.ErrNotEnoughTickets
	case order.ErrTypeOrderCodeCollision:
		// Two orders on one code is a code-generation bug, not a buyer outcome.
		return order.ErrOrderCreationFailed
	case order.ErrTypeOrderAlreadyProcessed:
		return order.ErrOrderAlreadyProcessed
	case order.ErrTypeOrderNotFound:
		return order.ErrOrderNotFound
	default:
		return err
	}
}

func (s *implService) Create(ctx context.Context, in order.CreateOrderInput) (order.CreateOrderOutput, error) {
	var e *event.Event
	var eCfg *event.EventConfig

	wg := sync.WaitGroup{}
	var wgErr error

	wg.Go(func() {
		resp, err := s.evSvc.FindOne(ctx, &event.FindOneEventRequest{
			Id: in.EventID,
		})
		if err != nil {
			s.l.Errorf(ctx, "internal.order.service.Create.evSvc.FindOne: %v", err)
			wgErr = err
			return
		}

		if resp.Event == nil {
			s.l.Errorf(ctx, "internal.order.service.Create: %v", in.EventID)
			wgErr = order.ErrEventNotFound
			return

		}

		e = resp.Event
	})

	wg.Go(func() {
		resp, err := s.evSvc.GetConfig(ctx, &event.GetEventConfigRequest{
			EventId: in.EventID,
		})
		if err != nil {
			s.l.Errorf(ctx, "internal.order.service.Create.evSvc.GetConfig: %v", err)
			wgErr = err
			return
		}

		if resp.EventConfig == nil {
			s.l.Errorf(ctx, "internal.order.service.Create: %v", in.EventID)
			wgErr = order.ErrEventConfigNotFound
			return
		}

		eCfg = resp.EventConfig
	})

	wg.Wait()
	if wgErr != nil {
		return order.CreateOrderOutput{}, wgErr
	}

	if e.Status != event.EventStatus_EVENT_STATUS_PUBLISHED {
		s.l.Errorf(ctx, "internal.order.service.Create: %v", in.EventID)
		return order.CreateOrderOutput{}, order.ErrEventNotReadyForSale
	}

	code := util.GenerateOrderCodeWithEventPrefix(e.Name)

	var ssID string
	if eCfg.AllowWaitRoom {
		claim, err := s.validateCheckoutToken(ctx, in)
		if err != nil {
			s.l.Errorf(ctx, "internal.order.service.Create: %v", err)
			return order.CreateOrderOutput{}, err
		}

		ssID = claim.SessionID
	}

	// One in-flight order per buyer; the workflow gives it back. See
	// docs/PURCHASE_SLOT.md.
	dedupeKey := order.PurchaseSlotKey(ssID, in.UserID, in.EventID)

	existing, err := s.claimPurchaseSlot(ctx, dedupeKey, code, in.PaymentMethod)
	if err != nil {
		return order.CreateOrderOutput{}, err
	}
	if existing != nil {
		return *existing, nil
	}

	tcMap := make(map[string]*inventory.TicketClass)

	ctx, cancel := context.WithTimeout(ctx, s.createTimeout)
	defer cancel()

	tcIds := make([]string, len(in.Items))
	for i, item := range in.Items {
		tcIds[i] = item.TicketClassID
	}

	tcResp, err := s.invSvc.FindManyTicketClass(ctx, &inventory.FindManyTicketClassRequest{
		EventId: in.EventID,
		Ids:     tcIds,
	})
	if err != nil {
		s.l.Errorf(ctx, "internal.order.service.Create.invSvc.FindManyTicketClass: %v", err)
		s.releasePurchaseSlot(ctx, dedupeKey, code)
		return order.CreateOrderOutput{}, err
	}

	if tcResp == nil || tcResp.GetTicketClasses() == nil || len(tcResp.GetTicketClasses()) == 0 {
		s.l.Errorf(ctx, "internal.order.service.Create: %v", in.EventID)
		s.releasePurchaseSlot(ctx, dedupeKey, code)
		return order.CreateOrderOutput{}, order.ErrTicketClassNotFound
	}

	for _, tc := range tcResp.GetTicketClasses() {
		tcMap[tc.GetId()] = tc
	}

	amt := int64(0)
	itmIns := make([]workflows.CreateOrderItemInput, len(in.Items))

	for i, itm := range in.Items {
		tc := tcMap[itm.TicketClassID]
		tt := tc.PriceCents * int64(itm.Quantity)
		amt += tt
		itmIns[i] = workflows.CreateOrderItemInput{
			TicketClassID:   itm.TicketClassID,
			TicketClassName: tc.Name,
			PriceAtPurchase: tc.PriceCents,
			Quantity:        itm.Quantity,
			TotalAmount:     tt,
		}
	}

	wfOpts := client.StartWorkflowOptions{
		ID:        workflows.GetCreateOrderWorkflowID(code),
		TaskQueue: temporal.CreateOrderTaskQueue,
	}

	wfIn := workflows.CreateOrderWorkflowInput{
		OrderCode:       code,
		SessionID:       ssID,
		UserID:          in.UserID,
		Email:           in.Email,
		Phone:           in.Phone,
		UserFullName:    in.UserFullName,
		EventID:         in.EventID,
		EventName:       e.Name,
		Currency:        "VND",
		TotalAmount:     amt,
		Items:           itmIns,
		PaymentProvider: string(in.PaymentMethod),
		RedirectUrl:     in.RedirectUrl,
		IdempotencyKey:  generatePaymentIdempotencyKey(code, string(in.PaymentMethod)),
	}

	wfRun, err := s.temporal.ExecuteWorkflow(ctx, wfOpts, workflows.CreateOrder, &wfIn)
	if err != nil {
		s.l.Errorf(ctx, "failed to start create order workflow: %v", err)
		s.releasePurchaseSlot(ctx, dedupeKey, code)
		return order.CreateOrderOutput{}, err
	}

	var wfRes workflows.CreateOrderWorkflowResult
	err = wfRun.Get(ctx, &wfRes)
	if err != nil {
		s.l.Errorf(ctx, "create order workflow failed: %v", err)

		// Get only stops waiting; the saga runs on and keeps reserving inventory
		// for a caller that gave up. See docs/PURCHASE_SLOT.md#caller-timeout.
		if ctxErr := ctx.Err(); ctxErr != nil {
			cancelCtx, cancelDone := context.WithTimeout(context.WithoutCancel(ctx), cancelWorkflowTimeout)
			defer cancelDone()
			if cErr := s.temporal.CancelWorkflow(cancelCtx, wfRun.GetID(), wfRun.GetRunID()); cErr != nil {
				// Cancel failed -> keep the claim. The workflow may still write a
				// live order, and releasing would let a retry mint a second one
				// behind its back.
				s.l.Errorf(ctx, "failed to cancel abandoned create order workflow %s: %v", wfRun.GetID(), cErr)
				return order.CreateOrderOutput{}, order.ErrRequestTimeout
			}

			// Cancel landed -> no order row will be written, so the claim guards
			// nothing and would refuse every retry.
			s.releasePurchaseSlot(ctx, dedupeKey, code)
			return order.CreateOrderOutput{}, order.ErrRequestTimeout
		}

		// Ran to completion and lost on a business outcome such as sold out: no
		// inventory held, no order written, so the claim guards nothing.
		s.releasePurchaseSlot(ctx, dedupeKey, code)
		return order.CreateOrderOutput{}, mapWorkflowError(err)
	}

	return order.CreateOrderOutput{
		Order:      wfRes.Order,
		OrderItems: wfRes.OrderItems,
		PaymentUrl: wfRes.PaymentUrl,
	}, nil
}

// claimPurchaseSlot takes the buyer's slot for this request: a non-nil order
// means the slot is held and the buyer is sent back to it, nil means this
// request won it. It loops because releasing a terminal order's slot has to be
// followed by a fresh claim. See docs/PURCHASE_SLOT.md#lifecycle.
func (s *implService) claimPurchaseSlot(ctx context.Context, dedupeKey, code string, method models.PaymentMethod) (*order.CreateOrderOutput, error) {
	for range claimAttempts {
		heldBy, claimedAt, err := s.repo.ClaimPurchaseSlot(ctx, dedupeKey, code)
		if err == nil {
			return nil, nil
		}
		if !errors.Is(err, order.ErrPurchaseSlotTaken) {
			s.l.Errorf(ctx, "internal.order.service.claimPurchaseSlot.ClaimPurchaseSlot: %v", err)
			return nil, err
		}

		out, err := s.resumeExistingOrder(ctx, dedupeKey, heldBy, claimedAt, method)
		if err == nil {
			return &out, nil
		}
		if !errors.Is(err, errPurchaseSlotReleased) {
			return nil, err
		}
	}

	// The slot is changing hands faster than this request can take it. Nothing
	// is broken and no order exists, so the buyer retries rather than faults.
	s.l.Warnf(ctx, "internal.order.service.claimPurchaseSlot: slot %s changed hands %d times", dedupeKey, claimAttempts)
	return nil, order.ErrPurchaseSlotUnsettled
}

// purchaseSlotSettleWindow is how old a claim naming no order has to be before
// it counts as abandoned rather than a create still in flight. Two budgets run
// back to back -- this caller's leg, then the saga's -- and sizing on the first
// alone hands an in-flight buyer's slot to their own retry.
// See docs/PURCHASE_SLOT.md#settle-window.
func (s *implService) purchaseSlotSettleWindow() time.Duration {
	return s.createTimeout + workflows.CreateOrderSlotBudget() + purchaseSlotSettleMargin
}

// releasePurchaseSlot drops the buyer's claim after a create that produced no
// order to point at. The ctx is detached because the case that needs it most is
// a caller that already gave up; a failure is swallowed, since the claim has a
// TTL and the caller is owed the error that failed their create, not this one.
func (s *implService) releasePurchaseSlot(ctx context.Context, dedupeKey, code string) {
	relCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), releaseSlotTimeout)
	defer cancel()

	if err := s.repo.ReleasePurchaseSlot(relCtx, dedupeKey, code); err != nil {
		s.l.Errorf(ctx, "internal.order.service.releasePurchaseSlot: %v", err)
	}
}

// resumeExistingOrder answers a duplicate create with the order already holding
// the buyer's slot: a live one comes back with its payment URL, a terminal one
// releases the slot (errPurchaseSlotReleased), and a claim naming no order is
// judged against purchaseSlotSettleWindow rather than assumed stale.
// See docs/PURCHASE_SLOT.md#lifecycle.
func (s *implService) resumeExistingOrder(ctx context.Context, dedupeKey, code string, claimedAt time.Time, method models.PaymentMethod) (order.CreateOrderOutput, error) {
	o, err := s.repo.GetByCode(ctx, code)
	if errors.Is(err, repo.ErrOrderNotFound) {
		age := s.clock().Sub(claimedAt)
		if age < s.purchaseSlotSettleWindow() {
			s.l.Warnf(ctx, "internal.order.service.resumeExistingOrder: slot %s names order %s, which has not been written yet (claim age %s)", dedupeKey, code, age)
			return order.CreateOrderOutput{}, order.ErrPurchaseSlotUnsettled
		}

		s.l.Warnf(ctx, "internal.order.service.resumeExistingOrder: slot %s names order %s, abandoned after %s with no order written", dedupeKey, code, age)
		s.releasePurchaseSlot(ctx, dedupeKey, code)
		return order.CreateOrderOutput{}, order.ErrPurchaseSlotUnsettled
	}
	if err != nil {
		s.l.Errorf(ctx, "internal.order.service.resumeExistingOrder.GetByCode: %v", err)
		return order.CreateOrderOutput{}, err
	}

	// pending | completed -> resume; the buyer lands back on the same checkout
	// terminal             -> release the slot, let this request retake it
	// unknown              -> refuse; guessing double-sells or strands
	switch o.Status {
	case models.OrderStatusPending, models.OrderStatusCompleted:
	case models.OrderStatusCancelled, models.OrderStatusPaymentFailed, models.OrderStatusTimeout,
		models.OrderStatusRefundRequired, models.OrderStatusRefunded:
		// No ticket is held in any of these, and a refund owed is tracked
		// separately from the ability to buy again.
		s.releasePurchaseSlot(ctx, dedupeKey, code)
		return order.CreateOrderOutput{}, errPurchaseSlotReleased
	default:
		// Leave the slot alone: guessing double-sells or strands the buyer.
		s.l.Warnf(ctx, "internal.order.service.resumeExistingOrder: order %s holds slot %s in unhandled status %s", code, dedupeKey, o.Status)
		return order.CreateOrderOutput{}, order.ErrOrderAlreadyProcessed
	}

	itms, err := s.repo.ListItemByOrderCode(ctx, o.Code)
	if err != nil {
		s.l.Errorf(ctx, "internal.order.service.resumeExistingOrder.ListItemByOrderCode: %v", err)
		return order.CreateOrderOutput{}, err
	}

	pmtResp, err := s.pmtSvc.GetPaymentUrlByIdempotencyKey(ctx, &payment.GetPaymentUrlByIdempotencyKeyRequest{
		IdempotencyKey: generatePaymentIdempotencyKey(o.Code, string(method)),
	})
	if err != nil {
		// The order row lands two steps before the payment intent, so pending
		// with no payment is a checkout still being set up, not a forbidden one.
		if o.Status == models.OrderStatusPending && isPaymentRecordMissing(err) {
			s.l.Warnf(ctx, "internal.order.service.resumeExistingOrder: slot %s names order %s, whose payment intent does not exist yet", dedupeKey, code)
			return order.CreateOrderOutput{}, order.ErrPurchaseSlotUnsettled
		}

		s.l.Errorf(ctx, "internal.order.service.resumeExistingOrder.GetPaymentUrlByIdempotencyKey: %v", err)
		return order.CreateOrderOutput{}, err
	}

	return order.CreateOrderOutput{
		Order:      &o,
		OrderItems: itms,
		PaymentUrl: pmtResp.PaymentUrl,
	}, nil
}

// isPaymentRecordMissing reports whether a payment lookup found nothing rather
// than failing. Why two codes: payment-svc answers an unknown idempotency key
// with PermissionDenied, not NotFound.
func isPaymentRecordMissing(err error) bool {
	switch status.Code(err) {
	case codes.PermissionDenied, codes.NotFound:
		return true
	default:
		return false
	}
}

func (s *implService) handlePaymentFailure(ctx context.Context, code string) error {
	err := s.releaseTickets(ctx, code)
	if err != nil {
		s.l.Errorf(ctx, "internal.order.service.handlePaymentFailure.releaseTickets: %v", err)
	}

	o, err := s.repo.GetOne(ctx, repo.GetOneOrderOption{
		FilterOrder: order.FilterOrder{
			Code: code,
		},
	})
	if err != nil {
		if err == repo.ErrOrderNotFound {
			s.l.Warnf(ctx, "internal.order.service.handlePaymentFailure.repo.GetByCode: %v", order.ErrOrderNotFound)
			return order.ErrOrderNotFound
		}
		s.l.Errorf(ctx, "internal.order.service.handlePaymentFailure.repo.GetByCode: %v", err)
		return err
	}

	_, err = s.repo.Update(ctx, o.Code, repo.UpdateOrderOption{
		Status: models.OrderStatusPaymentFailed,
	})
	if err != nil {
		s.l.Errorf(ctx, "internal.order.service.handlePaymentFailure.repo.Update: %v", err)
		return err
	}

	err = s.publishCheckoutFailedEvent(ctx, order.PubCheckoutFailedEventInput{
		SessionID: o.SessionID,
		UserID:    o.UserID,
		EventID:   o.EventID,
	})
	if err != nil {
		s.l.Errorf(ctx, "internal.order.service.handlePaymentFailure.publishCheckoutFailedEvent: %v", err)
	}

	return nil
}

func (s *implService) Cancel(ctx context.Context, code string) error {
	o, err := s.repo.GetByCode(ctx, code)
	if err != nil {
		if err == repo.ErrOrderNotFound {
			s.l.Warnf(ctx, "internal.order.service.Cancel: %v", order.ErrOrderNotFound)
			return order.ErrOrderNotFound
		}
		s.l.Errorf(ctx, "internal.order.service.Cancel.repo.GetByCode:%v", err)
		return err
	}

	if o.Status != models.OrderStatusPending {
		s.l.Errorf(ctx, "internal.order.service.Cancel: %v", order.ErrOrderNotPending)
		return order.ErrOrderNotPending
	}

	if err := s.releaseTickets(ctx, o.Code); err != nil {
		s.l.Errorf(ctx, "internal.order.service.Cancel.releaseTickets: %v", err)
	}

	_, err = s.repo.Update(ctx, o.Code, repo.UpdateOrderOption{
		Status: models.OrderStatusCancelled,
	})
	if err != nil {
		s.l.Errorf(ctx, "Failed to update order status to cancelled for %s: %v", o.Code, err)
		return order.ErrOrderCancellationFailed
	}

	if o.SessionID != "" {
		if err := s.publishCheckoutFailedEvent(ctx, order.PubCheckoutFailedEventInput{
			SessionID: o.SessionID,
			UserID:    o.UserID,
			EventID:   o.EventID,
		}); err != nil {
			s.l.Warnf(ctx, "Failed to publish checkout cancelled event for order %s: %v", o.Code, err)
		}
	}

	return nil
}

func (s *implService) GetMany(ctx context.Context, in order.GetManyOrderInput) (order.GetManyOrderOutput, error) {
	os, pag, err := s.repo.GetMany(ctx, repo.GetManyOrderOption(in))
	if err != nil {
		s.l.Errorf(ctx, "internal.order.service.GetMany.repo.GetMany: %v", err)
		return order.GetManyOrderOutput{}, err
	}

	return order.GetManyOrderOutput{
		Orders: os,
		Pag:    pag,
	}, nil
}

func (s *implService) GetByID(ctx context.Context, code string) (models.Order, error) {
	o, err := s.repo.GetByCode(ctx, code)
	if err != nil {
		if err == repo.ErrOrderNotFound {
			s.l.Warnf(ctx, "internal.order.service.GetByID: %v", order.ErrOrderNotFound)
			return models.Order{}, order.ErrOrderNotFound
		}
		s.l.Errorf(ctx, "internal.order.service.GetByID.repo.GetByCode:%v", err)
		return models.Order{}, err
	}

	return o, nil
}

func (s *implService) GetOne(ctx context.Context, in order.GetOneOrderInput) (models.Order, error) {
	o, err := s.repo.GetOne(ctx, repo.GetOneOrderOption(in))
	if err != nil {
		if err == repo.ErrOrderNotFound {
			s.l.Warnf(ctx, "internal.order.service.GetOne: %v", order.ErrOrderNotFound)
			return models.Order{}, order.ErrOrderNotFound
		}
		s.l.Errorf(ctx, "internal.order.service.GetOne.repo.GetOne:%v", err)
		return models.Order{}, err
	}

	return o, nil
}

func (s *implService) List(ctx context.Context, in order.ListOrderInput) ([]models.Order, error) {
	os, err := s.repo.List(ctx, repo.ListOrderOption(in))
	if err != nil {
		s.l.Errorf(ctx, "internal.order.service.List.repo.List: %v", err)
		return nil, err
	}

	return os, nil
}
