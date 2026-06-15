package grpc

import (
	"context"

	"github.com/vogiaan1904/ticketbottle-order/internal/models"
	"github.com/vogiaan1904/ticketbottle-order/internal/order"
	orderpb "github.com/vogiaan1904/ticketbottle-order/pkg/grpc/order"
	"github.com/vogiaan1904/ticketbottle-order/pkg/logger"
	"github.com/vogiaan1904/ticketbottle-order/pkg/paginator"
	"github.com/vogiaan1904/ticketbottle-order/pkg/response"
	"google.golang.org/protobuf/types/known/emptypb"
)

type grpcService struct {
	svc order.Service
	l   logger.Logger
	orderpb.UnimplementedOrderServiceServer
}

func NewGrpcService(svc order.Service, l logger.Logger) orderpb.OrderServiceServer {
	return &grpcService{
		svc: svc,
		l:   l,
	}
}

func (s *grpcService) CreateOrder(ctx context.Context, req *orderpb.CreateOrderRequest) (*orderpb.CreateOrderResponse, error) {
	if err := s.validateCreateOrderRequest(req); err != nil {
		s.l.Errorf(ctx, "internal.order.delivery.grpc.CreateOrder.validateCreateOrderRequest: %v", err)
		return nil, response.GrpcError(err)
	}

	in := order.CreateOrderInput{
		UserID:        req.UserId,
		EventID:       req.EventId,
		UserFullName:  req.UserFullname,
		Email:         req.UserEmail,
		Currency:      req.Currency,
		PaymentMethod: models.PaymentMethod(req.PaymentMethod),
		RedirectUrl:   req.RedirectUrl,
		CheckoutToken: req.CheckoutToken,
	}

	itms := make([]order.OrderItemInput, len(req.Items))
	for i, item := range req.Items {
		itms[i] = order.OrderItemInput{
			TicketClassID: item.TicketClassId,
			Quantity:      item.Quantity,
		}
	}
	in.Items = itms

	out, err := s.svc.Create(ctx, in)
	if err != nil {
		err := s.mapError(err)
		s.l.Errorf(ctx, "internal.order.delivery.grpc.service.Create: %v", err)
		return nil, response.GrpcError(err)
	}

	return s.newCreateResponses(out), nil
}

func (s *grpcService) CancelOrder(ctx context.Context, req *orderpb.CancelOrderRequest) (*emptypb.Empty, error) {
	if err := s.validateCancelOrderRequest(req); err != nil {
		s.l.Errorf(ctx, "internal.order.delivery.grpc.service.Cancel.validateCancelOrderRequest: %v", err)
		return nil, response.GrpcError(err)
	}

	err := s.svc.Cancel(ctx, req.GetCode())
	if err != nil {
		err := s.mapError(err)
		s.l.Errorf(ctx, "internal.order.delivery.grpc.service.Cancel: %v", err)
		return nil, response.GrpcError(err)
	}

	return &emptypb.Empty{}, nil
}

func (s *grpcService) GetManyOrders(ctx context.Context, req *orderpb.GetManyOrdersRequest) (*orderpb.GetManyOrdersResponse, error) {
	if err := s.validateGetManyOrdersRequest(req); err != nil {
		s.l.Errorf(ctx, "internal.order.delivery.grpc.service.GetManyOrders.validateGetManyOrdersRequest: %v", err)
		return nil, response.GrpcError(err)
	}

	pagQ := paginator.PaginatorQuery{
		Limit:  req.GetPageSize(),
		Cursor: req.GetCursor(),
	}

	in := order.GetManyOrderInput{
		Pag:         pagQ,
		FilterOrder: s.newOrderFilter(req.GetFilter()),
	}

	out, err := s.svc.GetMany(ctx, in)
	if err != nil {
		err := s.mapError(err)
		s.l.Errorf(ctx, "internal.order.delivery.grpc.service.GetManyOrders: %v", err)
		return nil, response.GrpcError(err)
	}

	return s.newGetManyOrderResponse(out), nil
}

func (s *grpcService) ListOrders(ctx context.Context, req *orderpb.ListOrdersRequest) (*orderpb.ListOrdersResponse, error) {
	if err := s.validateListOrdersRequest(req); err != nil {
		s.l.Errorf(ctx, "internal.order.delivery.grpc.service.ListOrders.validateListOrdersRequest: %v", err)
		return nil, response.GrpcError(err)
	}

	in := order.ListOrderInput{
		FilterOrder: s.newOrderFilter(req.GetFilter()),
	}

	os, err := s.svc.List(ctx, in)
	if err != nil {
		err := s.mapError(err)
		s.l.Errorf(ctx, "internal.order.delivery.grpc.service.ListOrders: %v", err)
		return nil, response.GrpcError(err)
	}

	return s.newListOrderResponse(os), nil
}

func (s *grpcService) GetOrder(ctx context.Context, req *orderpb.GetOrderRequest) (*orderpb.GetOrderResponse, error) {
	if err := s.validateGetOrderRequest(req); err != nil {
		s.l.Errorf(ctx, "internal.order.delivery.grpc.service.GetOrder.validateGetOrderRequest: %v", err)
		return nil, response.GrpcError(err)
	}

	o, err := s.svc.GetByID(ctx, req.GetCode())
	if err != nil {
		err := s.mapError(err)
		s.l.Errorf(ctx, "internal.order.delivery.grpc.service.GetOrder.GetByID: %v", err)
		return nil, response.GrpcError(err)
	}

	return s.newGetOrderResponse(o), nil
}
