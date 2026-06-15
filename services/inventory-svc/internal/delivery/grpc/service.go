package grpc

import (
	"context"
	"strconv"

	svc "github.com/vogiaan/ticketbottle-inventory/internal/services"
	invpb "github.com/vogiaan/ticketbottle-inventory/pkg/grpc/inventory"
	"github.com/vogiaan/ticketbottle-inventory/pkg/logger"
	"github.com/vogiaan/ticketbottle-inventory/pkg/response"
	"google.golang.org/protobuf/types/known/emptypb"
)

type grpcService struct {
	rSvc  svc.ReservationService
	tcSvc svc.TicketClassService
	l     logger.Logger
	invpb.UnimplementedInventoryServiceServer
}

func NewGrpcService(rSvc svc.ReservationService, tcSvc svc.TicketClassService, l logger.Logger) invpb.InventoryServiceServer {
	return &grpcService{
		rSvc:  rSvc,
		tcSvc: tcSvc,
		l:     l,
	}
}

func (s *grpcService) CreateTicketClass(ctx context.Context, req *invpb.CreateTicketClassRequest) (*invpb.CreateTicketClassResponse, error) {
	if err := s.validateCreateTicketClassRequest(req); err != nil {
		s.l.Errorf(ctx, "internal.delivery.grpc.CreateTicketClass.validateCreateTicketClassRequest: %v", err)
		return nil, response.GrpcError(err)
	}

	in, err := s.newCreateTicketClassInput(req)
	if err != nil {
		s.l.Errorf(ctx, "internal.delivery.grpc.CreateTicketClass.newCreateTicketClassInput: %v", err)
		return nil, response.GrpcError(err)
	}

	tc, err := s.tcSvc.Create(ctx, in)
	if err != nil {
		err = s.mapError(err)
		s.l.Errorf(ctx, "internal.delivery.grpc.CreateTicketClass.Create: %v", err)
		return nil, response.GrpcError(err)
	}

	return s.newCreateTicketClassResponse(tc), nil
}

func (s *grpcService) UpdateTicketClass(ctx context.Context, req *invpb.UpdateTicketClassRequest) (*invpb.UpdateTicketClassResponse, error) {
	if err := s.validateUpdateTicketClassRequest(req); err != nil {
		s.l.Errorf(ctx, "internal.delivery.grpc.UpdateTicketClass.validateUpdateTicketClassRequest: %v", err)
		return nil, response.GrpcError(err)
	}

	id, err := strconv.ParseInt(req.GetId(), 10, 64)
	if err != nil {
		s.l.Errorf(ctx, "internal.delivery.grpc.UpdateTicketClass.ParseInt: %v", err)
		return nil, response.GrpcError(ErrValidationFailed)
	}

	in, err := s.newUpdateTicketClassInput(req)
	if err != nil {
		s.l.Errorf(ctx, "internal.delivery.grpc.UpdateTicketClass.newUpdateTicketClassInput: %v", err)
		return nil, response.GrpcError(err)
	}

	tc, err := s.tcSvc.Update(ctx, id, in)
	if err != nil {
		err = s.mapError(err)
		s.l.Errorf(ctx, "internal.delivery.grpc.UpdateTicketClass.Update: %v", err)
		return nil, response.GrpcError(err)
	}

	return s.newUpdateTicketClassResponse(tc), nil
}

func (s *grpcService) FindOneTicketClass(ctx context.Context, req *invpb.FindOneTicketClassRequest) (*invpb.FindOneTicketClassResponse, error) {
	if err := s.validateFindOneTicketClassRequest(req); err != nil {
		s.l.Errorf(ctx, "internal.delivery.grpc.FindOneTicketClass.validateFindOneTicketClassRequest: %v", err)
		return nil, response.GrpcError(err)
	}

	id, err := strconv.ParseInt(req.GetId(), 10, 64)
	if err != nil {
		s.l.Errorf(ctx, "internal.delivery.grpc.FindOneTicketClass.ParseInt: %v", err)
		return nil, response.GrpcError(ErrValidationFailed)
	}

	tc, err := s.tcSvc.GetByID(ctx, id)
	if err != nil {
		err = s.mapError(err)
		s.l.Errorf(ctx, "internal.delivery.grpc.FindOneTicketClass.GetByID: %v", err)
		return nil, response.GrpcError(err)
	}

	return s.newFindOneTicketClassResponse(tc), nil
}

func (s *grpcService) FindManyTicketClass(ctx context.Context, req *invpb.FindManyTicketClassRequest) (*invpb.FindManyTicketClassResponse, error) {
	if err := s.validateFindManyTicketClassRequest(req); err != nil {
		s.l.Errorf(ctx, "internal.delivery.grpc.FindManyTicketClass.validateFindManyTicketClassRequest: %v", err)
		return nil, response.GrpcError(err)
	}

	in, err := s.newGetManyTicketClassInput(req)
	if err != nil {
		s.l.Errorf(ctx, "internal.delivery.grpc.FindManyTicketClass.newGetManyTicketClassInput: %v", err)
		return nil, response.GrpcError(err)
	}

	tcs, err := s.tcSvc.GetMany(ctx, in)
	if err != nil {
		err = s.mapError(err)
		s.l.Errorf(ctx, "internal.delivery.grpc.FindManyTicketClass.GetMany: %v", err)
		return nil, response.GrpcError(err)
	}

	return s.newFindManyTicketClassResponse(tcs), nil
}

func (s *grpcService) DeleteTicketClass(ctx context.Context, req *invpb.DeleteTicketClassRequest) (*emptypb.Empty, error) {
	if err := s.validateDeleteTicketClassRequest(req); err != nil {
		s.l.Errorf(ctx, "internal.delivery.grpc.DeleteTicketClass.validateDeleteTicketClassRequest: %v", err)
		return nil, response.GrpcError(err)
	}

	id, err := strconv.ParseInt(req.GetId(), 10, 64)
	if err != nil {
		s.l.Errorf(ctx, "internal.delivery.grpc.DeleteTicketClass.ParseInt: %v", err)
		return nil, response.GrpcError(ErrValidationFailed)
	}

	err = s.tcSvc.Delete(ctx, id)
	if err != nil {
		err = s.mapError(err)
		s.l.Errorf(ctx, "internal.delivery.grpc.DeleteTicketClass.Delete: %v", err)
		return nil, response.GrpcError(err)
	}

	return &emptypb.Empty{}, nil
}

func (s *grpcService) CheckAvailability(ctx context.Context, req *invpb.CheckAvailabilityRequest) (*invpb.CheckAvailabilityResponse, error) {
	if err := s.validateCheckAvailabilityRequest(req); err != nil {
		s.l.Errorf(ctx, "internal.delivery.grpc.CheckAvailability.validateCheckAvailabilityRequest: %v", err)
		return nil, response.GrpcError(err)
	}

	inputs, err := s.newCheckAvailabilityInput(req)
	if err != nil {
		s.l.Errorf(ctx, "internal.delivery.grpc.CheckAvailability.newCheckAvailabilityInput: %v", err)
		return nil, response.GrpcError(err)
	}

	accept, err := s.tcSvc.CheckAvailability(ctx, inputs)
	if err != nil {
		err = s.mapError(err)
		s.l.Errorf(ctx, "internal.delivery.grpc.CheckAvailability.CheckAvailability: %v", err)
		return nil, response.GrpcError(err)
	}

	return &invpb.CheckAvailabilityResponse{
		Accept: accept,
	}, nil
}

func (s *grpcService) GetAvailability(ctx context.Context, req *invpb.GetAvailabilityRequest) (*invpb.GetAvailabilityResponse, error) {
	if err := s.validateGetAvailabilityRequest(req); err != nil {
		s.l.Errorf(ctx, "internal.delivery.grpc.GetAvailability.validateGetAvailabilityRequest: %v", err)
		return nil, response.GrpcError(err)
	}

	id, err := strconv.ParseInt(req.GetTicketClassId(), 10, 64)
	if err != nil {
		s.l.Errorf(ctx, "internal.delivery.grpc.GetAvailability.ParseInt: %v", err)
		return nil, response.GrpcError(ErrValidationFailed)
	}

	count, err := s.tcSvc.GetAvailableCount(ctx, id)
	if err != nil {
		err = s.mapError(err)
		s.l.Errorf(ctx, "internal.delivery.grpc.GetAvailability.GetAvailableCount: %v", err)
		return nil, response.GrpcError(err)
	}

	return &invpb.GetAvailabilityResponse{
		AvailableQuantity: int32(count),
	}, nil
}

func (s *grpcService) Reserve(ctx context.Context, req *invpb.ReserveRequest) (*emptypb.Empty, error) {
	if err := s.validateReserveRequest(req); err != nil {
		s.l.Errorf(ctx, "internal.delivery.grpc.Reserve.validateReserveRequest: %v", err)
		return nil, response.GrpcError(err)
	}

	in, err := s.newReserveInput(req)
	if err != nil {
		s.l.Errorf(ctx, "internal.delivery.grpc.Reserve.newReserveInput: %v", err)
		return nil, response.GrpcError(err)
	}

	err = s.rSvc.Reserve(ctx, in)
	if err != nil {
		err = s.mapError(err)
		s.l.Errorf(ctx, "internal.delivery.grpc.Reserve.Reserve: %v", err)
		return nil, response.GrpcError(err)
	}

	return &emptypb.Empty{}, nil
}

func (s *grpcService) Confirm(ctx context.Context, req *invpb.ConfirmRequest) (*emptypb.Empty, error) {
	if err := s.validateConfirmRequest(req); err != nil {
		s.l.Errorf(ctx, "internal.delivery.grpc.Confirm.validateConfirmRequest: %v", err)
		return nil, response.GrpcError(err)
	}

	err := s.rSvc.Confirm(ctx, req.GetOrderCode())
	if err != nil {
		err = s.mapError(err)
		s.l.Errorf(ctx, "internal.delivery.grpc.Confirm.Confirm: %v", err)
		return nil, response.GrpcError(err)
	}

	return &emptypb.Empty{}, nil
}

func (s *grpcService) Release(ctx context.Context, req *invpb.ReleaseRequest) (*emptypb.Empty, error) {
	if err := s.validateReleaseRequest(req); err != nil {
		s.l.Errorf(ctx, "internal.delivery.grpc.Release.validateReleaseRequest: %v", err)
		return nil, response.GrpcError(err)
	}

	err := s.rSvc.Release(ctx, req.GetOrderCode())
	if err != nil {
		err = s.mapError(err)
		s.l.Errorf(ctx, "internal.delivery.grpc.Release.Release: %v", err)
		return nil, response.GrpcError(err)
	}

	return &emptypb.Empty{}, nil
}
