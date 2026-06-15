package service

import (
	"context"

	"github.com/vogiaan/ticketbottle-inventory/internal/models"
	pkgGorm "github.com/vogiaan/ticketbottle-inventory/pkg/gorm"
	pkgLog "github.com/vogiaan/ticketbottle-inventory/pkg/logger"
	"gorm.io/gorm"
)

type TicketClassService interface {
	Create(ctx context.Context, in CreateTicketClassInput) (models.TicketClass, error)
	Update(ctx context.Context, id int64, in UpdateTicketClassInput) (models.TicketClass, error)
	GetByID(ctx context.Context, id int64) (models.TicketClass, error)
	GetByEventID(ctx context.Context, eventID string) ([]models.TicketClass, error)
	GetMany(ctx context.Context, in GetManyTicketClassInput) ([]models.TicketClass, error)
	Delete(ctx context.Context, id int64) error
	IncrementReserved(ctx context.Context, id int64, quantity int) error
	DecrementReserved(ctx context.Context, id int64, quantity int) error
	IncrementSold(ctx context.Context, id int64, quantity int) error
	GetAvailableCount(ctx context.Context, id int64) (int, error)
	CheckAvailability(ctx context.Context, ins []CheckAvailabilityInput) (bool, error)
}

type implTicketClassService struct {
	l    pkgLog.Logger
	repo *pkgGorm.Repository
}

func NewTicketClassService(l pkgLog.Logger, repo *pkgGorm.Repository) TicketClassService {
	return &implTicketClassService{
		l:    l,
		repo: repo,
	}
}

func (s implTicketClassService) Create(ctx context.Context, in CreateTicketClassInput) (models.TicketClass, error) {
	tc := s.buildModel(in)
	if err := s.repo.Create(ctx, &tc); err != nil {
		s.l.Errorf(ctx, "service.ticketclass.Create: %v", err)
		return models.TicketClass{}, err
	}

	return tc, nil
}

func (s implTicketClassService) Update(ctx context.Context, id int64, in UpdateTicketClassInput) (models.TicketClass, error) {
	var tc models.TicketClass
	if err := s.repo.FindByID(ctx, &tc, id); err != nil {
		if err == gorm.ErrRecordNotFound {
			s.l.Warnf(ctx, "service.ticketclass.Update: %v", err)
		}
		s.l.Errorf(ctx, "service.ticketclass.Update: %v", err)
		return models.TicketClass{}, err
	}

	s.buildUpdate(in, &tc)
	if err := s.repo.Update(ctx, &tc); err != nil {
		s.l.Errorf(ctx, "service.ticketclass.Update: %v", err)
		return models.TicketClass{}, err
	}

	return tc, nil
}

func (s implTicketClassService) GetByID(ctx context.Context, id int64) (models.TicketClass, error) {
	var tc models.TicketClass
	if err := s.repo.FindByID(ctx, &tc, id); err != nil {
		if err == gorm.ErrRecordNotFound {
			s.l.Warnf(ctx, "service.ticketclass.GetByID: %v", err)
		}
		s.l.Errorf(ctx, "service.ticketclass.GetByID: %v", err)
		return models.TicketClass{}, err
	}

	return tc, nil
}

func (s implTicketClassService) GetByEventID(ctx context.Context, eventID string) ([]models.TicketClass, error) {
	var tcs []models.TicketClass
	if err := s.repo.FindWhere(ctx, &tcs, "event_id = ?", eventID); err != nil {
		s.l.Errorf(ctx, "service.ticketclass.GetByEventID: %v", err)
		return nil, err
	}

	return tcs, nil
}

func (s implTicketClassService) GetMany(ctx context.Context, in GetManyTicketClassInput) ([]models.TicketClass, error) {
	var tcs []models.TicketClass

	// Build dynamic query
	query := s.repo.GetDB().WithContext(ctx).Model(&models.TicketClass{})

	// Add event_id filter if provided
	if in.EventID != "" {
		query = query.Where("event_id = ?", in.EventID)
	}

	// Add ids filter if provided
	if len(in.IDs) > 0 {
		query = query.Where("id IN ?", in.IDs)
	}

	// Execute the query
	if err := query.Find(&tcs).Error; err != nil {
		s.l.Errorf(ctx, "service.ticketclass.GetMany: %v", err)
		return nil, err
	}

	return tcs, nil
}

func (s *implTicketClassService) Delete(ctx context.Context, id int64) error {
	var tc models.TicketClass
	if err := s.repo.FindByID(ctx, &tc, id); err != nil {
		if err == gorm.ErrRecordNotFound {
			s.l.Warnf(ctx, "service.ticketclass.Delete: %v", err)
			return nil
		}
		s.l.Errorf(ctx, "service.ticketclass.Delete: %v", err)
		return err
	}

	if err := s.repo.Delete(ctx, &tc); err != nil {
		s.l.Errorf(ctx, "service.ticketclass.Delete: %v", err)
		return err
	}

	return nil
}

func (s *implTicketClassService) IncrementReserved(ctx context.Context, id int64, quantity int) error {
	return s.repo.WithContext(ctx).
		Model(&models.TicketClass{}).
		Where("id = ?", id).
		Where("total >= reserved + sold + ?", quantity). // Check availability
		Update("reserved", gorm.Expr("reserved + ?", quantity)).Error
}

func (s *implTicketClassService) DecrementReserved(ctx context.Context, id int64, quantity int) error {
	return s.repo.WithContext(ctx).
		Model(&models.TicketClass{}).
		Where("id = ?", id).
		Update("reserved", gorm.Expr("GREATEST(0, reserved - ?)", quantity)).Error
}

func (s *implTicketClassService) IncrementSold(ctx context.Context, id int64, quantity int) error {
	return s.repo.WithContext(ctx).
		Model(&models.TicketClass{}).
		Where("id = ?", id).
		Updates(map[string]interface{}{
			"sold":     gorm.Expr("sold + ?", quantity),
			"reserved": gorm.Expr("GREATEST(0, reserved - ?)", quantity),
		}).Error
}

func (s *implTicketClassService) GetAvailableCount(ctx context.Context, id int64) (int, error) {
	var tc models.TicketClass
	if err := s.repo.FindByID(ctx, &tc, id); err != nil {
		if err == gorm.ErrRecordNotFound {
			s.l.Warnf(ctx, "service.ticketclass.GetAvailableCount: %v", err)
		}
		s.l.Errorf(ctx, "service.ticketclass.GetAvailableCount: %v", err)
		return 0, err
	}

	return tc.Total - tc.Reserved - tc.Sold, nil
}

func (s *implTicketClassService) CheckAvailability(ctx context.Context, ins []CheckAvailabilityInput) (bool, error) {
	if len(ins) == 0 {
		return true, nil
	}

	// Extract all ticket class IDs
	ids := make([]int64, 0, len(ins))
	qtyMap := make(map[int64]int)

	for _, in := range ins {
		ids = append(ids, in.TicketClassID)
		qtyMap[in.TicketClassID] = in.Qty
	}

	// Fetch all ticket classes at once
	var ticketClasses []models.TicketClass
	if err := s.repo.WithContext(ctx).
		Model(&models.TicketClass{}).
		Where("id IN ?", ids).
		Find(&ticketClasses).Error; err != nil {
		s.l.Errorf(ctx, "service.ticketclass.CheckAvailability: %v", err)
		return false, err
	}

	// Check if we found all requested ticket classes
	if len(ticketClasses) != len(ins) {
		s.l.Warnf(ctx, "service.ticketclass.CheckAvailability: requested %d ticket classes, found %d", len(ins), len(ticketClasses))
		return false, nil
	}

	// Check availability for each ticket class
	for _, tc := range ticketClasses {
		requestedQty := qtyMap[tc.ID]
		availableQty := tc.Total - tc.Reserved - tc.Sold

		if availableQty < requestedQty {
			s.l.Warnf(ctx, "service.ticketclass.CheckAvailability: insufficient stock for ticket_class_id=%d (available=%d, requested=%d)",
				tc.ID, availableQty, requestedQty)
			return false, nil
		}
	}

	return true, nil
}
