package grpc

import (
	"github.com/vogiaan1904/ticketbottle-order/internal/models"
	"github.com/vogiaan1904/ticketbottle-order/internal/order"
	orderpb "github.com/vogiaan1904/ticketbottle-order/pkg/grpc/order"
	"github.com/vogiaan1904/ticketbottle-order/pkg/util"
)

var GrpcOrderStatusValue = map[models.OrderStatus]orderpb.OrderStatus{
	models.OrderStatusPending:       orderpb.OrderStatus_ORDER_STATUS_PENDING,
	models.OrderStatusCompleted:     orderpb.OrderStatus_ORDER_STATUS_COMPLETED,
	models.OrderStatusCancelled:     orderpb.OrderStatus_ORDER_STATUS_CANCELED,
	models.OrderStatusPaymentFailed: orderpb.OrderStatus_ORDER_STATUS_FAILED,
}

var OrderStatus = map[orderpb.OrderStatus]models.OrderStatus{
	orderpb.OrderStatus_ORDER_STATUS_PENDING:   models.OrderStatusPending,
	orderpb.OrderStatus_ORDER_STATUS_COMPLETED: models.OrderStatusCompleted,
	orderpb.OrderStatus_ORDER_STATUS_CANCELED:  models.OrderStatusCancelled,
	orderpb.OrderStatus_ORDER_STATUS_FAILED:    models.OrderStatusPaymentFailed,
}

func (s *grpcService) newOrderItems(itms []models.OrderItem) []*orderpb.OrderItem {
	pbItems := make([]*orderpb.OrderItem, len(itms))
	for i, itm := range itms {
		pbItems[i] = &orderpb.OrderItem{
			TicketClassId: itm.TicketClassID,
			Quantity:      itm.Quantity,
			PriceCents:    itm.TotalAmount,
		}
	}

	return pbItems
}

func (s *grpcService) newCreateResponses(out order.CreateOrderOutput) *orderpb.CreateOrderResponse {
	ord := &orderpb.Order{
		Code:             out.Order.Code,
		UserId:           out.Order.UserID,
		EventId:          out.Order.EventID,
		UserFullname:     out.Order.UserFullName,
		UserEmail:        out.Order.Email,
		TotalAmountCents: out.Order.TotalAmount,
		Currency:         out.Order.Currency,
		PaymentMethod:    string(out.Order.PaymentMethod),
		Status:           GrpcOrderStatusValue[out.Order.Status],
		CreatedAt:        util.TimeToISO8601Str(out.Order.CreatedAt),
		UpdatedAt:        util.TimeToISO8601Str(out.Order.UpdatedAt),
		Items:            s.newOrderItems(out.OrderItems),
	}

	return &orderpb.CreateOrderResponse{
		Order:      ord,
		PaymentUrl: out.PaymentUrl,
	}
}

func (s *grpcService) newGetManyOrderResponse(out order.GetManyOrderOutput) *orderpb.GetManyOrdersResponse {
	os := make([]*orderpb.Order, len(out.Orders))
	for i, o := range out.Orders {
		os[i] = s.newOrderResponse(o)
	}

	pagResp := out.Pag.ToResponse()
	return &orderpb.GetManyOrdersResponse{
		Orders: os,
		Pagination: &orderpb.PaginationInfo{
			PageSize:   pagResp.PageSize,
			Count:      pagResp.Count,
			NextCursor: pagResp.NextCursor,
			HasNext:    pagResp.HasMore,
		},
	}
}

func (s *grpcService) newOrderResponse(o models.Order) *orderpb.Order {
	return &orderpb.Order{
		Code:             o.Code,
		UserId:           o.UserID,
		EventId:          o.EventID,
		UserFullname:     o.UserFullName,
		UserEmail:        o.Email,
		TotalAmountCents: o.TotalAmount,
		Currency:         o.Currency,
		PaymentMethod:    string(o.PaymentMethod),
		Status:           GrpcOrderStatusValue[o.Status],
		CreatedAt:        util.TimeToISO8601Str(o.CreatedAt),
		UpdatedAt:        util.TimeToISO8601Str(o.UpdatedAt),
	}
}

func (s *grpcService) newListOrderResponse(os []models.Order) *orderpb.ListOrdersResponse {
	pbos := make([]*orderpb.Order, len(os))
	for i, o := range os {
		pbos[i] = s.newOrderResponse(o)
	}

	return &orderpb.ListOrdersResponse{
		Orders: pbos,
	}
}

func (s *grpcService) newGetOrderResponse(o models.Order) *orderpb.GetOrderResponse {
	return &orderpb.GetOrderResponse{
		Order: s.newOrderResponse(o),
	}
}

func (s *grpcService) newOrderFilter(reqFil *orderpb.OrderFilter) order.FilterOrder {
	fil := order.FilterOrder{}

	if reqFil != nil {
		fil.UserID = reqFil.GetUserId()
		fil.EventID = reqFil.GetEventId()
		if reqFil.GetStatus() != 0 {
			stt := OrderStatus[reqFil.GetStatus()]
			fil.Status = &stt
		}
	}

	return fil
}
