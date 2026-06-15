package service

import (
	"context"

	"github.com/vogiaan1904/ticketbottle-order/internal/infra/temporal"
	"github.com/vogiaan1904/ticketbottle-order/internal/models"
	"github.com/vogiaan1904/ticketbottle-order/internal/order"
	"github.com/vogiaan1904/ticketbottle-order/internal/workflows"
	"go.temporal.io/sdk/client"
)

func (s *implService) HandlePaymentCompleted(ctx context.Context, in order.HandlePaymentCompletedInput) error {
	wfOpts := client.StartWorkflowOptions{
		ID:        workflows.GetConfirmOrderWorkflowID(in.OrderCode),
		TaskQueue: temporal.ConfirmOrderTaskQueue,
	}

	wfIn := workflows.ConfirmOrderWorkflowInput{
		OrderCode: in.OrderCode,
		Status:    models.OrderStatusCompleted,
	}

	wfRun, err := s.temporal.ExecuteWorkflow(ctx, wfOpts, workflows.ConfirmOrder, &wfIn)
	if err != nil {
		s.l.Errorf(ctx, "Failed to start confirm order workflow: %v", err)
		return err
	}

	err = wfRun.Get(ctx, nil)
	if err != nil {
		s.l.Errorf(ctx, "Confirm order workflow failed: %v", err)
		return err
	}

	return nil
}

func (s *implService) HandlePaymentFailed(ctx context.Context, in order.HandlePaymentFailedInput) error {
	err := s.handlePaymentFailure(ctx, in.OrderCode)
	if err != nil {
		s.l.Errorf(ctx, "Failed to handle payment failure: %v", err)
		return err
	}

	s.l.Infof(ctx, "Payment failure handled successfully for order: %s", in.OrderCode)
	return nil
}
