package grpc

import (
	"strconv"
	"time"

	"github.com/vogiaan/ticketbottle-inventory/internal/models"
	svc "github.com/vogiaan/ticketbottle-inventory/internal/services"
	invpb "github.com/vogiaan/ticketbottle-inventory/pkg/grpc/inventory"
	"github.com/vogiaan/ticketbottle-inventory/pkg/util"
)

func (s *grpcService) newTicketClassResponse(tc models.TicketClass) *invpb.TicketClass {
	pbTC := &invpb.TicketClass{
		Id:         strconv.FormatInt(tc.ID, 10),
		EventId:    tc.EventID,
		Name:       tc.Name,
		PriceCents: tc.PriceCents,
		Currency:   tc.Currency,
		Total:      int32(tc.Total),
		CreatedAt:  util.TimeToISO8601Str(tc.CreatedAt),
		UpdatedAt:  util.TimeToISO8601Str(tc.UpdatedAt),
	}

	if tc.SaleStartAt != nil {
		pbTC.StartSaleAt = util.TimeToISO8601Str(*tc.SaleStartAt)
	}
	if tc.SaleEndAt != nil {
		pbTC.EndSaleAt = util.TimeToISO8601Str(*tc.SaleEndAt)
	}

	return pbTC
}

func (s *grpcService) newCreateTicketClassResponse(tc models.TicketClass) *invpb.CreateTicketClassResponse {
	return &invpb.CreateTicketClassResponse{
		TicketClass: s.newTicketClassResponse(tc),
	}
}

func (s *grpcService) newUpdateTicketClassResponse(tc models.TicketClass) *invpb.UpdateTicketClassResponse {
	return &invpb.UpdateTicketClassResponse{
		TicketClass: s.newTicketClassResponse(tc),
	}
}

func (s *grpcService) newFindOneTicketClassResponse(tc models.TicketClass) *invpb.FindOneTicketClassResponse {
	return &invpb.FindOneTicketClassResponse{
		TicketClass: s.newTicketClassResponse(tc),
	}
}

func (s *grpcService) newFindManyTicketClassResponse(tcs []models.TicketClass) *invpb.FindManyTicketClassResponse {
	pbTCs := make([]*invpb.TicketClass, len(tcs))
	for i, tc := range tcs {
		pbTCs[i] = s.newTicketClassResponse(tc)
	}

	return &invpb.FindManyTicketClassResponse{
		TicketClasses: pbTCs,
	}
}

// parseTime parses ISO8601 time string to *time.Time, returns nil if empty
func parseTime(s string) (*time.Time, error) {
	if s == "" {
		return nil, nil
	}
	t, err := util.ParseISO8601(s)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func (s *grpcService) newCreateTicketClassInput(req *invpb.CreateTicketClassRequest) (svc.CreateTicketClassInput, error) {
	startSaleAt, err := parseTime(req.GetStartSaleAt())
	if err != nil {
		return svc.CreateTicketClassInput{}, err
	}

	endSaleAt, err := parseTime(req.GetEndSaleAt())
	if err != nil {
		return svc.CreateTicketClassInput{}, err
	}

	return svc.CreateTicketClassInput{
		EventID:     req.GetEventId(),
		Name:        req.GetName(),
		PriceCents:  req.GetPriceCents(),
		Currency:    req.GetCurrency(),
		Total:       int(req.GetTotal()),
		SaleStartAt: startSaleAt,
		SaleEndAt:   endSaleAt,
	}, nil
}

// newUpdateTicketClassInput converts the proto request to a service input.
// The fields are non-optional scalars, so "" / 0 is the only absent signal:
// this RPC cannot set a price of 0 or an empty name until the contract gains
// `optional` fields.
func (s *grpcService) newUpdateTicketClassInput(req *invpb.UpdateTicketClassRequest) (svc.UpdateTicketClassInput, error) {
	startSaleAt, err := parseTime(req.GetStartSaleAt())
	if err != nil {
		return svc.UpdateTicketClassInput{}, err
	}

	endSaleAt, err := parseTime(req.GetEndSaleAt())
	if err != nil {
		return svc.UpdateTicketClassInput{}, err
	}

	in := svc.UpdateTicketClassInput{
		SaleStartAt: startSaleAt,
		SaleEndAt:   endSaleAt,
	}
	if v := req.GetName(); v != "" {
		in.Name = &v
	}
	if v := req.GetCurrency(); v != "" {
		in.Currency = &v
	}
	if v := req.GetPriceCents(); v != 0 {
		in.PriceCents = &v
	}
	if v := req.GetTotal(); v != 0 {
		total := int(v)
		in.Total = &total
	}

	return in, nil
}

func (s *grpcService) newGetManyTicketClassInput(req *invpb.FindManyTicketClassRequest) (svc.GetManyTicketClassInput, error) {
	in := svc.GetManyTicketClassInput{
		EventID: req.GetEventId(),
	}

	if len(req.GetIds()) > 0 {
		ids := make([]int64, len(req.GetIds()))
		for i, idStr := range req.GetIds() {
			id, err := strconv.ParseInt(idStr, 10, 64)
			if err != nil {
				return svc.GetManyTicketClassInput{}, err
			}
			ids[i] = id
		}
		in.IDs = ids
	}

	return in, nil
}

func (s *grpcService) newReserveInput(req *invpb.ReserveRequest) (svc.ReserveInput, error) {
	expiresAt, err := util.ParseISO8601(req.GetExpiresAt())
	if err != nil {
		return svc.ReserveInput{}, err
	}

	items := make([]svc.ReserveItem, len(req.GetItems()))
	for i, pbItem := range req.GetItems() {
		ticketClassID, err := strconv.ParseInt(pbItem.GetTicketClassId(), 10, 64)
		if err != nil {
			return svc.ReserveInput{}, err
		}
		items[i] = svc.ReserveItem{
			TicketClassID: ticketClassID,
			Qty:           int(pbItem.GetQuantity()),
		}
	}

	return svc.ReserveInput{
		OrderCode: req.GetOrderCode(),
		Items:     items,
		ExpiresAt: expiresAt,
	}, nil
}

func (s *grpcService) newCheckAvailabilityInput(req *invpb.CheckAvailabilityRequest) ([]svc.CheckAvailabilityInput, error) {
	inputs := make([]svc.CheckAvailabilityInput, len(req.GetItems()))
	for i, pbItem := range req.GetItems() {
		ticketClassID, err := strconv.ParseInt(pbItem.GetTicketClassId(), 10, 64)
		if err != nil {
			return nil, err
		}
		inputs[i] = svc.CheckAvailabilityInput{
			TicketClassID: ticketClassID,
			Qty:           int(pbItem.GetQuantity()),
		}
	}
	return inputs, nil
}
