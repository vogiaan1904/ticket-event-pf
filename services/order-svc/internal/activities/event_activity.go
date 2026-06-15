package activities

import (
	"context"

	"github.com/vogiaan1904/ticketbottle-order/internal/order"
	"github.com/vogiaan1904/ticketbottle-order/pkg/grpc/event"
)

type EventActivities struct {
	Client event.EventServiceClient
}

func NewEventActivities(client event.EventServiceClient) *EventActivities {
	return &EventActivities{
		Client: client,
	}
}

func (a *EventActivities) GetEvent(ctx context.Context, eventID string) (*event.Event, error) {
	resp, err := a.Client.FindOne(ctx, &event.FindOneEventRequest{
		Id: eventID,
	})
	if err != nil {
		return nil, err
	}

	if resp.Event == nil {
		return nil, order.ErrEventNotFound
	}

	return resp.Event, nil
}

func (a *EventActivities) GetEventConfig(ctx context.Context, eventID string) (*event.EventConfig, error) {
	resp, err := a.Client.GetConfig(ctx, &event.GetEventConfigRequest{
		EventId: eventID,
	})
	if err != nil {
		return nil, err
	}

	if resp.EventConfig == nil {
		return nil, order.ErrEventConfigNotFound
	}

	return resp.EventConfig, nil
}
