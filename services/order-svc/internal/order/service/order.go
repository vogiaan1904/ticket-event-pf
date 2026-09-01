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
)

// Cancellation has to outlive the deadline that triggered it, but the handler
// only has to tell the workflow, not watch it stop.
const cancelWorkflowTimeout = 5 * time.Second

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

	var ssID string
	if eCfg.AllowWaitRoom {
		claim, err := s.validateCheckoutToken(ctx, in)
		if err != nil {
			s.l.Errorf(ctx, "internal.order.service.Create: %v", err)
			return order.CreateOrderOutput{}, err
		}

		ssID = claim.SessionID

		existingOrder, err := s.repo.GetOne(ctx, repo.GetOneOrderOption{
			FilterOrder: order.FilterOrder{
				SessionID: ssID,
			},
		})
		if err == nil {
			switch existingOrder.Status {
			case models.OrderStatusPending, models.OrderStatusCompleted:
				itms, err := s.repo.ListItemByOrderCode(ctx, existingOrder.Code)
				if err != nil {
					s.l.Errorf(ctx, "internal.order.service.Create.repo.ListItemByOrderCode: %v", err)
					return order.CreateOrderOutput{}, err
				}

				pmtResp, err := s.pmtSvc.GetPaymentUrlByIdempotencyKey(ctx, &payment.GetPaymentUrlByIdempotencyKeyRequest{
					IdempotencyKey: generatePaymentIdempotencyKey(existingOrder.Code, string(in.PaymentMethod)),
				})
				if err != nil {
					s.l.Errorf(ctx, "internal.order.service.Create.pmtSvc.GetPaymentUrlByIdempotencyKey: %v", err)
					return order.CreateOrderOutput{}, err
				}

				return order.CreateOrderOutput{
					Order:      &existingOrder,
					OrderItems: itms,
					PaymentUrl: pmtResp.PaymentUrl,
				}, nil
			case models.OrderStatusCancelled, models.OrderStatusPaymentFailed, models.OrderStatusTimeout:

			}
		} else if err != repo.ErrOrderNotFound {
			s.l.Errorf(ctx, "internal.order.service.Create.repo.GetOne: %v", err)
			return order.CreateOrderOutput{}, err
		}
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

	code := util.GenerateOrderCodeWithEventPrefix(e.Name)

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
		return order.CreateOrderOutput{}, err
	}

	var wfRes workflows.CreateOrderWorkflowResult
	err = wfRun.Get(ctx, &wfRes)
	if err != nil {
		s.l.Errorf(ctx, "create order workflow failed: %v", err)

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
