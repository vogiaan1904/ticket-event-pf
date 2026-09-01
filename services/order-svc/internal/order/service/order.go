package service

import (
	"context"
	"errors"
	"fmt"
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
)

// Cancellation has to outlive the deadline that triggered it, but the handler
// only has to tell the workflow, not watch it stop.
const cancelWorkflowTimeout = 5 * time.Second

// Releasing a stranded purchase slot has to outlive the deadline that stranded
// it, for the same reason cancellation does.
const releaseSlotTimeout = 5 * time.Second

// claimAttempts bounds the claim loop. Each pass costs one round trip and only
// happens when the slot was held by an order that had already ended, so a
// second pass is normal and a fourth means the slot is churning rather than
// settling.
const claimAttempts = 3

// errPurchaseSlotReleased is internal to the claim loop: the slot was held by
// an order that can no longer hold inventory or money, the claim has been
// dropped, and this request should take it. It never reaches a caller.
var errPurchaseSlotReleased = errors.New("purchase slot released")

// mapWorkflowError translates a Temporal failure into a domain error. wfRun.Get
// returns the workflow's error rebuilt from a serialised failure proto, so the
// original sentinel is gone; the ApplicationError type string is what survives,
// and it is what separates a business rejection from a fault.
func mapWorkflowError(err error) error {
	var appErr *sdktemporal.ApplicationError
	if !errors.As(err, &appErr) {
		return err
	}

	switch appErr.Type() {
	case order.ErrTypeInsufficientInventory:
		return order.ErrNotEnoughTickets
	case order.ErrTypeOrderCodeCollision:
		// A code collision means two different orders landed on the same
		// code -- a code-generation bug, not a business outcome the buyer
		// caused. ErrOrderCreationFailed already maps to codes.Internal.
		return order.ErrOrderCreationFailed
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

	// The order's code is generated here rather than at the workflow call
	// because the purchase-slot claim records it: the claim is what a duplicate
	// create is answered with, so it has to name the order it is protecting.
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

	// One in-flight order per buyer. With a waiting room the slot is the
	// session, because that is what admission handed out; without one it is the
	// buyer and the event, so an event that skips the queue is not left with no
	// suppression at all -- which is what happened when this check lived inside
	// the AllowWaitRoom branch.
	dedupeKey := ssID
	if dedupeKey == "" {
		dedupeKey = fmt.Sprintf("user#%s:event#%s", in.UserID, in.EventID)
	}

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
		return order.CreateOrderOutput{}, err
	}

	if tcResp == nil || tcResp.GetTicketClasses() == nil || len(tcResp.GetTicketClasses()) == 0 {
		s.l.Errorf(ctx, "internal.order.service.Create: %v", in.EventID)
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

		// The saga reserves inventory before it writes the order row, so a
		// create that fails -- sold out, or a caller who gave up and had the
		// saga compensated away underneath them -- can leave the slot claimed
		// with no order behind it. Nothing would ever answer for that claim,
		// and the buyer's next attempt would be refused on behalf of an order
		// that does not exist. Sold out has to stay sold out.
		s.releasePurchaseSlot(ctx, dedupeKey, code)

		// Get only stops waiting: the saga runs server-side and would go on
		// reserving inventory for a caller that has already given up. Cancel it
		// so CreateOrder's deferred compensation releases the hold.
		if ctxErr := ctx.Err(); ctxErr != nil {
			cancelCtx, cancelDone := context.WithTimeout(context.WithoutCancel(ctx), cancelWorkflowTimeout)
			defer cancelDone()
			if cErr := s.temporal.CancelWorkflow(cancelCtx, wfRun.GetID(), wfRun.GetRunID()); cErr != nil {
				s.l.Errorf(ctx, "failed to cancel abandoned create order workflow %s: %v", wfRun.GetID(), cErr)
			}
			return order.CreateOrderOutput{}, order.ErrRequestTimeout
		}

		return order.CreateOrderOutput{}, mapWorkflowError(err)
	}

	return order.CreateOrderOutput{
		Order:      wfRes.Order,
		OrderItems: wfRes.OrderItems,
		PaymentUrl: wfRes.PaymentUrl,
	}, nil
}

// claimPurchaseSlot takes the buyer's slot for this request. It returns a
// non-nil order when the slot is held by one the buyer should be sent back to,
// and nil when this request won the slot and should go on to create one.
//
// The loop is here because a slot held by an order that has already ended is
// released rather than honoured, and a release has to be followed by a fresh
// claim: freeing the slot and then proceeding without retaking it would leave
// this create unguarded and hand the slot to whoever asked next.
func (s *implService) claimPurchaseSlot(ctx context.Context, dedupeKey, code string, method models.PaymentMethod) (*order.CreateOrderOutput, error) {
	for range claimAttempts {
		heldBy, err := s.repo.ClaimPurchaseSlot(ctx, dedupeKey, code)
		if err == nil {
			return nil, nil
		}
		if !errors.Is(err, order.ErrPurchaseSlotTaken) {
			s.l.Errorf(ctx, "internal.order.service.claimPurchaseSlot.ClaimPurchaseSlot: %v", err)
			return nil, err
		}

		out, err := s.resumeExistingOrder(ctx, dedupeKey, heldBy, method)
		if err == nil {
			return &out, nil
		}
		if !errors.Is(err, errPurchaseSlotReleased) {
			return nil, err
		}
	}

	// Every pass found the slot held and then released it, so it is changing
	// hands faster than this request can take it. No order was created and
	// nothing is broken, so the buyer retries rather than being handed a fault.
	s.l.Warnf(ctx, "internal.order.service.claimPurchaseSlot: slot %s changed hands %d times", dedupeKey, claimAttempts)
	return nil, order.ErrPurchaseSlotUnsettled
}

// releasePurchaseSlot drops the buyer's claim after a create that produced no
// order it could point at.
//
// It runs on a context detached from the request because the case that needs
// it most is the one where the caller already gave up and the request context
// is dead. A failure is logged and swallowed: the claim carries a TTL behind
// it, the next request to find it stranded clears it, and the caller is owed
// the error that actually failed their create, not this one.
func (s *implService) releasePurchaseSlot(ctx context.Context, dedupeKey, code string) {
	relCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), releaseSlotTimeout)
	defer cancel()

	if err := s.repo.ReleasePurchaseSlot(relCtx, dedupeKey, code); err != nil {
		s.l.Errorf(ctx, "internal.order.service.releasePurchaseSlot: %v", err)
	}
}

// resumeExistingOrder answers a duplicate create with the order that already
// holds the buyer's slot.
//
// A live or completed order is returned with its payment URL, so a retrying
// client lands back on the same checkout and a buyer who has bought does not
// buy twice. An order that ended without buying anything holds neither
// inventory nor money, so its claim has no work left to do: it is released and
// errPurchaseSlotReleased tells the caller to take the slot for itself.
//
// A claim naming an order that was never written is the same situation with a
// worse cause -- a create died mid-flight -- and is cleared the same way, but
// the buyer is asked to retry instead: whatever killed that attempt is more
// recent than a status transition and is worth a fresh request to find out.
func (s *implService) resumeExistingOrder(ctx context.Context, dedupeKey, code string, method models.PaymentMethod) (order.CreateOrderOutput, error) {
	o, err := s.repo.GetByCode(ctx, code)
	if errors.Is(err, repo.ErrOrderNotFound) {
		s.l.Warnf(ctx, "internal.order.service.resumeExistingOrder: slot %s names order %s, which was never written", dedupeKey, code)
		s.releasePurchaseSlot(ctx, dedupeKey, code)
		return order.CreateOrderOutput{}, order.ErrPurchaseSlotUnsettled
	}
	if err != nil {
		s.l.Errorf(ctx, "internal.order.service.resumeExistingOrder.GetByCode: %v", err)
		return order.CreateOrderOutput{}, err
	}

	switch o.Status {
	case models.OrderStatusPending, models.OrderStatusCompleted:
	case models.OrderStatusCancelled, models.OrderStatusPaymentFailed, models.OrderStatusTimeout:
		s.releasePurchaseSlot(ctx, dedupeKey, code)
		return order.CreateOrderOutput{}, errPurchaseSlotReleased
	default:
		// A status nobody here recognises. Guessing would either sell the
		// buyer a second order or strand them, so the slot is left alone and
		// the request is refused.
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
		s.l.Errorf(ctx, "internal.order.service.resumeExistingOrder.GetPaymentUrlByIdempotencyKey: %v", err)
		return order.CreateOrderOutput{}, err
	}

	return order.CreateOrderOutput{
		Order:      &o,
		OrderItems: itms,
		PaymentUrl: pmtResp.PaymentUrl,
	}, nil
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
