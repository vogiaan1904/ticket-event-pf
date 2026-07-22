package service

import (
	"context"
	"errors"
	"time"

	"github.com/vogiaan/ticketbottle-inventory/internal/models"
	pkgGorm "github.com/vogiaan/ticketbottle-inventory/pkg/gorm"
	pkgLog "github.com/vogiaan/ticketbottle-inventory/pkg/logger"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type TicketClassService interface {
	Create(ctx context.Context, in CreateTicketClassInput) (models.TicketClass, error)
	Update(ctx context.Context, id int64, in UpdateTicketClassInput) (models.TicketClass, error)
	GetByID(ctx context.Context, id int64) (models.TicketClass, error)
	GetMany(ctx context.Context, in GetManyTicketClassInput) ([]models.TicketClass, error)
	Delete(ctx context.Context, id int64) error
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
	cols := s.updateColumns(in)

	var tc models.TicketClass
	err := s.repo.GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Lock the row so the capacity check below sees a stable
		// reserved/sold, and so a concurrent Reserve on this ticket class
		// serialises behind us instead of racing the write.
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			First(&tc, id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				s.l.Warnf(ctx, "service.ticketclass.Update: ticket class %d not found", id)
				return ErrNotFound
			}
			s.l.Errorf(ctx, "service.ticketclass.Update.Lock: %v", err)
			return err
		}

		if in.Total != nil && *in.Total < tc.Reserved+tc.Sold {
			s.l.Warnf(ctx, "service.ticketclass.Update: refusing total=%d below committed reserved=%d sold=%d for ticket_class_id=%d",
				*in.Total, tc.Reserved, tc.Sold, id)
			return ErrStateConflict
		}

		if len(cols) == 0 {
			return nil
		}

		if err := tx.Model(&models.TicketClass{}).Where("id = ?", id).
			Updates(cols).Error; err != nil {
			s.l.Errorf(ctx, "service.ticketclass.Update.Write: %v", err)
			return err
		}

		// Re-read inside the transaction so the caller gets the row as
		// committed, counters included.
		if err := tx.First(&tc, id).Error; err != nil {
			s.l.Errorf(ctx, "service.ticketclass.Update.Reload: %v", err)
			return err
		}
		return nil
	})
	if err != nil {
		return models.TicketClass{}, err
	}

	return tc, nil
}

func (s implTicketClassService) GetByID(ctx context.Context, id int64) (models.TicketClass, error) {
	var tc models.TicketClass
	if err := s.repo.FindByID(ctx, &tc, id); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			s.l.Warnf(ctx, "service.ticketclass.GetByID: ticket class %d not found", id)
			return models.TicketClass{}, ErrNotFound
		}
		s.l.Errorf(ctx, "service.ticketclass.GetByID: %v", err)
		return models.TicketClass{}, err
	}

	return tc, nil
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
		if errors.Is(err, gorm.ErrRecordNotFound) {
			s.l.Warnf(ctx, "service.ticketclass.Delete: ticket class %d not found, no-op", id)
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

func (s *implTicketClassService) GetAvailableCount(ctx context.Context, id int64) (int, error) {
	var tc models.TicketClass
	if err := s.repo.FindByID(ctx, &tc, id); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			s.l.Warnf(ctx, "service.ticketclass.GetAvailableCount: ticket class %d not found", id)
			return 0, ErrNotFound
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

	// Collapse duplicate line items exactly the way Reserve does -- a
	// pre-check that disagrees with the real gate is worse than no pre-check.
	items := make([]ReserveItem, len(ins))
	for i, in := range ins {
		items[i] = ReserveItem{TicketClassID: in.TicketClassID, Qty: in.Qty}
	}
	ids, qtyByID := aggregateDemand(items)

	var tcs []models.TicketClass
	if err := s.repo.WithContext(ctx).
		Model(&models.TicketClass{}).
		Where("id IN ?", ids).
		Find(&tcs).Error; err != nil {
		s.l.Errorf(ctx, "service.ticketclass.CheckAvailability: %v", err)
		return false, err
	}

	if len(tcs) != len(ids) {
		s.l.Warnf(ctx, "service.ticketclass.CheckAvailability: requested %d distinct ticket classes, found %d", len(ids), len(tcs))
		return false, nil
	}

	now := time.Now().UTC()
	for _, tc := range tcs {
		if tc.Status != models.TicketClassStatusActive ||
			(tc.SaleStartAt != nil && now.Before(*tc.SaleStartAt)) ||
			(tc.SaleEndAt != nil && now.After(*tc.SaleEndAt)) {
			s.l.Warnf(ctx, "service.ticketclass.CheckAvailability: ticket_class_id=%d is not on sale (status=%s)", tc.ID, tc.Status)
			return false, nil
		}

		requestedQty := qtyByID[tc.ID]
		availableQty := tc.Total - tc.Reserved - tc.Sold
		if availableQty < requestedQty {
			s.l.Warnf(ctx, "service.ticketclass.CheckAvailability: insufficient stock for ticket_class_id=%d (available=%d, requested=%d)",
				tc.ID, availableQty, requestedQty)
			return false, nil
		}
	}

	return true, nil
}
