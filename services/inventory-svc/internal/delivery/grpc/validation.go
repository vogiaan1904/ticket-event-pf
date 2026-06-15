package grpc

import (
	invpb "github.com/vogiaan/ticketbottle-inventory/pkg/grpc/inventory"
	"github.com/vogiaan/ticketbottle-inventory/pkg/util"
)

func (s *grpcService) validateCreateTicketClassRequest(req *invpb.CreateTicketClassRequest) error {
	if req.GetEventId() == "" {
		return ErrValidationFailed
	}
	if req.GetName() == "" {
		return ErrValidationFailed
	}
	if req.GetPriceCents() < 0 {
		return ErrValidationFailed
	}
	if req.GetCurrency() == "" {
		return ErrValidationFailed
	}
	if req.GetTotal() <= 0 {
		return ErrValidationFailed
	}

	if req.GetStartSaleAt() != "" {
		_, err := util.ParseISO8601(req.GetStartSaleAt())
		if err != nil {
			return ErrValidationFailed
		}
	}

	if req.GetEndSaleAt() != "" {
		_, err := util.ParseISO8601(req.GetEndSaleAt())
		if err != nil {
			return ErrValidationFailed
		}
	}

	return nil
}

func (s *grpcService) validateUpdateTicketClassRequest(req *invpb.UpdateTicketClassRequest) error {
	if req.GetId() == "" {
		return ErrValidationFailed
	}

	return nil
}

func (s *grpcService) validateFindOneTicketClassRequest(req *invpb.FindOneTicketClassRequest) error {
	if req.GetId() == "" {
		return ErrValidationFailed
	}

	return nil
}

func (s *grpcService) validateFindManyTicketClassRequest(req *invpb.FindManyTicketClassRequest) error {
	// Either event_id or ids must be provided
	if req.GetEventId() == "" && len(req.GetIds()) == 0 {
		return ErrValidationFailed
	}

	return nil
}

func (s *grpcService) validateDeleteTicketClassRequest(req *invpb.DeleteTicketClassRequest) error {
	if req.GetId() == "" {
		return ErrValidationFailed
	}

	return nil
}

func (s *grpcService) validateReserveRequest(req *invpb.ReserveRequest) error {
	if req.GetOrderCode() == "" {
		return ErrValidationFailed
	}
	if len(req.GetItems()) == 0 {
		return ErrValidationFailed
	}
	if req.GetExpiresAt() == "" {
		return ErrValidationFailed
	}

	for _, item := range req.GetItems() {
		if err := s.validateReserveItem(item); err != nil {
			return err
		}
	}

	return nil
}

func (s *grpcService) validateReserveItem(item *invpb.ReserveItem) error {
	if item.GetTicketClassId() == "" {
		return ErrValidationFailed
	}
	if item.GetQuantity() <= 0 {
		return ErrValidationFailed
	}

	return nil
}

func (s *grpcService) validateConfirmRequest(req *invpb.ConfirmRequest) error {
	if req.GetOrderCode() == "" {
		return ErrValidationFailed
	}

	return nil
}

func (s *grpcService) validateReleaseRequest(req *invpb.ReleaseRequest) error {
	if req.GetOrderCode() == "" {
		return ErrValidationFailed
	}

	return nil
}

func (s *grpcService) validateGetAvailabilityRequest(req *invpb.GetAvailabilityRequest) error {
	if req.GetTicketClassId() == "" {
		return ErrValidationFailed
	}

	return nil
}

func (s *grpcService) validateCheckAvailabilityRequest(req *invpb.CheckAvailabilityRequest) error {
	if len(req.GetItems()) == 0 {
		return ErrValidationFailed
	}

	for _, item := range req.GetItems() {
		if err := s.validateCheckAvailabilityItem(item); err != nil {
			return err
		}
	}

	return nil
}

func (s *grpcService) validateCheckAvailabilityItem(item *invpb.CheckAvailabilityItem) error {
	if item.GetTicketClassId() == "" {
		return ErrValidationFailed
	}
	if item.GetQuantity() <= 0 {
		return ErrValidationFailed
	}

	return nil
}
