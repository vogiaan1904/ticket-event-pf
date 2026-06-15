package grpc

import (
	orderpb "github.com/vogiaan1904/ticketbottle-order/pkg/grpc/order"
)

func (s *grpcService) validateCreateOrderRequest(req *orderpb.CreateOrderRequest) error {
	if req.GetEventId() == "" {
		return ErrValidationFailed
	}
	if req.GetUserId() == "" {
		return ErrValidationFailed
	}
	if req.GetUserFullname() == "" {
		return ErrValidationFailed
	}
	if req.GetUserEmail() == "" {
		return ErrValidationFailed
	}
	if req.GetUserPhone() == "" {
		return ErrValidationFailed
	}
	if req.GetPaymentMethod() == "" {
		return ErrValidationFailed
	}
	if req.GetCurrency() == "" {
		return ErrValidationFailed
	}
	if len(req.GetItems()) == 0 {
		return ErrValidationFailed
	}
	if req.GetRedirectUrl() == "" {
		return ErrValidationFailed
	}

	for _, item := range req.GetItems() {
		if err := validateCreateOrderItem(item); err != nil {
			return err
		}
	}

	return nil
}

func validateCreateOrderItem(item *orderpb.CreateOrderItem) error {
	if item.GetTicketClassId() == "" {
		return ErrValidationFailed
	}
	if item.GetQuantity() <= 0 {
		return ErrValidationFailed
	}

	return nil
}

func (s *grpcService) validateGetManyOrdersRequest(req *orderpb.GetManyOrdersRequest) error {
	if req.GetCursor() == "" {
		return ErrValidationFailed
	}
	if req.GetPageSize() <= 0 {
		return ErrValidationFailed
	}
	if req.GetFilter() != nil {
		if err := s.validateOrderFilter(req.GetFilter()); err != nil {
			return err
		}
	}

	return nil
}

func (s *grpcService) validateOrderFilter(fil *orderpb.OrderFilter) error {
	if fil.Status != nil {
		if _, ok := OrderStatus[fil.GetStatus()]; !ok {
			return ErrValidationFailed
		}
	}

	return nil
}

func (s *grpcService) validateGetOrderRequest(req *orderpb.GetOrderRequest) error {
	if req.GetCode() == "" {
		return ErrValidationFailed
	}
	return nil
}

func (s *grpcService) validateListOrdersRequest(req *orderpb.ListOrdersRequest) error {
	if req.GetFilter() != nil {
		if err := s.validateOrderFilter(req.GetFilter()); err != nil {
			return err
		}
	}
	return nil
}

func (s *grpcService) validateCancelOrderRequest(req *orderpb.CancelOrderRequest) error {
	if req.GetCode() == "" {
		return ErrValidationFailed
	}
	return nil
}
