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
		// Lock so the capacity check sees a stable reserved/sold and a concurrent
		// Reserve serialises behind us instead of racing the write.
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

	query := s.repo.GetDB().WithContext(ctx).Model(&models.TicketClass{})

	if in.EventID != "" {
		query = query.Where("event_id = ?", in.EventID)
	}

	if len(in.IDs) > 0 {
		query = query.Where("id IN ?", in.IDs)
	}

	if err := query.Find(&tcs).Error; err != nil {
		s.l.Errorf(ctx, "service.ticketclass.GetMany: %v", err)
		return nil, err
	}

	return tcs, nil
}

func (s *implTicketClassService) Delete(ctx context.Context, id int64) error {
	return s.repo.GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Lock the row: Reserve takes this same lock before creating a hold, so
		// none can slip in between the guard below and the delete.
		var tc models.TicketClass
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&tc, id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				s.l.Warnf(ctx, "service.ticketclass.Delete: ticket class %d not found, no-op", id)
				return nil
			}
			s.l.Errorf(ctx, "service.ticketclass.Delete.Lock: %v", err)
			return err
		}

		var liveCount int64
		if err := tx.Model(&models.Reservation{}).
			Where("ticket_class_id = ? AND status IN ?", id,
				[]models.ReservationStatus{models.ReservationStatusActive, models.ReservationStatusConfirmed}).
			Count(&liveCount).Error; err != nil {
			s.l.Errorf(ctx, "service.ticketclass.Delete.CountLiveReservations: %v", err)
			return err
		}
		if liveCount > 0 {
			s.l.Warnf(ctx, "service.ticketclass.Delete: refusing to delete ticket_class_id=%d, %d non-terminal reservation(s) remain", id, liveCount)
			return ErrStateConflict
		}

		// Anything left is terminal. The FK is RESTRICT so clearing children is an
		// explicit choice here, not a CASCADE that could take a live row with it.
		if err := deleteTerminalReservations(tx, id); err != nil {
			s.l.Errorf(ctx, "service.ticketclass.Delete.DeleteTerminalReservations: %v", err)
			return err
		}

		if err := tx.Delete(&tc).Error; err != nil {
			s.l.Errorf(ctx, "service.ticketclass.Delete: %v", err)
			return err
		}

		s.l.Infof(ctx, "service.ticketclass.Delete: deleted ticket_class_id=%d", id)
		return nil
	})
}

// deleteTerminalReservations deletes a ticket class's EXPIRED/CANCELLED rows.
// Why scoped, when the caller's liveCount guard already refuses live rows:
// it keeps FK RESTRICT load-bearing -- a hold created without the class lock
// survives this statement and trips a real FK violation on the parent delete.
func deleteTerminalReservations(tx *gorm.DB, id int64) error {
	return tx.Where("ticket_class_id = ? AND status IN ?", id,
		[]models.ReservationStatus{models.ReservationStatusExpired, models.ReservationStatusCancelled}).
		Delete(&models.Reservation{}).Error
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
		if !onSale(tc, now) {
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
