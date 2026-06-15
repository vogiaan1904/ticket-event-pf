package service

import (
	"context"
	"sync"
	"time"

	"github.com/vogiaan/ticketbottle-inventory/internal/models"
	pkgGorm "github.com/vogiaan/ticketbottle-inventory/pkg/gorm"
	pkgLog "github.com/vogiaan/ticketbottle-inventory/pkg/logger"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type implReservationService struct {
	l    pkgLog.Logger
	repo *pkgGorm.Repository
}

type ReservationService interface {
	Reserve(ctx context.Context, in ReserveInput) error
	Confirm(ctx context.Context, oCode string) error
	Release(ctx context.Context, oCode string) error
	UpdateStatus(ctx context.Context, id uint, status models.ReservationStatus) error
	UpdateStatusByOrderCode(ctx context.Context, oCode string, status models.ReservationStatus) error
	BatchExpireReservations(ctx context.Context, batchSize int) (int, error)
	Delete(ctx context.Context, id uint) error
	DeleteByOrderCode(ctx context.Context, oCode string) error
}

func NewReservationService(l pkgLog.Logger, repo *pkgGorm.Repository) ReservationService {
	return &implReservationService{
		l:    l,
		repo: repo,
	}
}

func (s implReservationService) Reserve(ctx context.Context, in ReserveInput) error {

	wg := sync.WaitGroup{}
	var wgErr error

	for _, item := range in.Items {
		wg.Add(1)
		go func(item ReserveItem) {
			defer wg.Done()
			_, err := s.Create(ctx, in.OrderCode, in.ExpiresAt, item)
			if err != nil {
				s.l.Errorf(ctx, "service.reservation.Reserve: %v", err)
				wgErr = err
			}
		}(item)
	}

	wg.Wait()
	if wgErr != nil {
		s.l.Errorf(ctx, "service.reservation.Reserve: %v", wgErr)
		s.DeleteByOrderCode(ctx, in.OrderCode)
		return wgErr
	}

	return nil
}

func (s implReservationService) Create(ctx context.Context, oCode string, expAt time.Time, item ReserveItem) (models.Reservation, error) {
	var r models.Reservation

	err := s.repo.GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Step 1: Lock the ticket class row for update
		var ticketClass models.TicketClass
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			First(&ticketClass, item.TicketClassID).Error; err != nil {
			s.l.Errorf(ctx, "service.reservation.Create.LockTicketClass: %v", err)
			return err
		}

		// Step 2: Check if enough stock available
		requestedQty := item.Qty
		availableQty := ticketClass.Total - ticketClass.Reserved - ticketClass.Sold

		if availableQty < requestedQty {
			s.l.Warnf(ctx, "service.reservation.Create: insufficient stock for ticket_class_id=%d (available=%d, requested=%d)",
				item.TicketClassID, availableQty, requestedQty)
			return gorm.ErrInvalidData
		}

		// Alternative check: reserved + sold + qty <= total
		if ticketClass.Reserved+ticketClass.Sold+requestedQty > ticketClass.Total {
			s.l.Warnf(ctx, "service.reservation.Create: would exceed total capacity for ticket_class_id=%d", item.TicketClassID)
			return gorm.ErrInvalidData
		}

		// Step 3: Update ticket class counters (increment reserved)
		result := tx.Model(&ticketClass).
			Where("id = ?", ticketClass.ID).
			Update("reserved", gorm.Expr("reserved + ?", requestedQty))

		if result.Error != nil {
			s.l.Errorf(ctx, "service.reservation.Create.IncrementReserved: %v", result.Error)
			return result.Error
		}

		if result.RowsAffected == 0 {
			s.l.Errorf(ctx, "service.reservation.Create: failed to update ticket_class_id=%d", item.TicketClassID)
			return gorm.ErrInvalidData
		}

		// Step 4: Build and insert the reservation record
		r = s.buildModel(oCode, expAt, item)
		if err := tx.Create(&r).Error; err != nil {
			s.l.Errorf(ctx, "service.reservation.Create.InsertReservation: %v", err)
			return err
		}

		s.l.Infof(ctx, "service.reservation.Create: created reservation %d for order %s (ticket_class_id=%d, qty=%d, expires_at=%s)",
			r.ID, r.OrderCode, r.TicketClassID, r.Qty, r.ExpiresAt.Format(time.RFC3339))

		return nil
	})

	if err != nil {
		return models.Reservation{}, err
	}

	return r, nil
}

func (s implReservationService) GetByOrderCode(ctx context.Context, oCode string) ([]models.Reservation, error) {
	var rs []models.Reservation
	if err := s.repo.WithContext(ctx).Where("order_code = ?", oCode).Find(&rs).Error; err != nil {
		s.l.Errorf(ctx, "service.reservation.GetByOrderCode: %v", err)
		return nil, err
	}

	return rs, nil
}

func (s implReservationService) GetActiveByTicketClassID(ctx context.Context, ticketClassID uint) ([]models.Reservation, error) {
	var rs []models.Reservation
	now := time.Now().UTC()
	if err := s.repo.WithContext(ctx).
		Where("ticket_class_id = ? AND status = ?", ticketClassID, models.ReservationStatusActive).
		Where("expires_at > ?", now).
		Find(&rs).Error; err != nil {
		s.l.Errorf(ctx, "service.reservation.GetActiveByTicketClassID: %v", err)
		return nil, err
	}

	return rs, nil
}

func (s implReservationService) GetExpired(ctx context.Context, limit int) ([]models.Reservation, error) {
	var rs []models.Reservation
	now := time.Now().UTC()
	if err := s.repo.WithContext(ctx).
		Where("status = ? AND expires_at <= ?", models.ReservationStatusActive, now).
		Limit(limit).
		Find(&rs).Error; err != nil {
		s.l.Errorf(ctx, "service.reservation.GetExpired: %v", err)
		return nil, err
	}

	return rs, nil
}

func (s implReservationService) UpdateStatus(ctx context.Context, id uint, status models.ReservationStatus) error {
	var r models.Reservation
	if err := s.repo.FindByID(ctx, &r, id); err != nil {
		if err == gorm.ErrRecordNotFound {
			s.l.Warnf(ctx, "service.reservation.UpdateStatus: %v", err)
		}
		s.l.Errorf(ctx, "service.reservation.UpdateStatus: %v", err)
		return err
	}

	r.Status = status
	if err := s.repo.Update(ctx, &r); err != nil {
		s.l.Errorf(ctx, "service.reservation.UpdateStatus: %v", err)
		return err
	}

	return nil
}

func (s implReservationService) UpdateStatusByOrderCode(ctx context.Context, oCode string, status models.ReservationStatus) error {
	if err := s.repo.WithContext(ctx).Model(&models.Reservation{}).
		Where("order_code = ?", oCode).
		Update("status", status).Error; err != nil {
		s.l.Errorf(ctx, "service.reservation.UpdateStatusByOrderCode: %v", err)
		return err
	}

	return nil
}

func (s implReservationService) Confirm(ctx context.Context, oCode string) error {
	err := s.repo.GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return s.confirmReservationTx(ctx, tx, oCode)
	})
	return err
}

func (s implReservationService) confirmReservationTx(ctx context.Context, tx *gorm.DB, oCode string) error {
	// Step 1: Lock and fetch all reservations for this order
	var rs []models.Reservation
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("order_code = ?", oCode).
		Find(&rs).Error; err != nil {
		s.l.Errorf(ctx, "service.reservation.ConfirmReservation.LockReservations: %v", err)
		return err
	}

	if len(rs) == 0 {
		s.l.Warnf(ctx, "service.reservation.ConfirmReservation: no reservations found for order_code=%s", oCode)
		return gorm.ErrRecordNotFound
	}

	now := time.Now().UTC()
	tcUps := make(map[int64]int) // ticket_class_id -> qty to move from reserved to sold
	rIDs := make([]int64, 0, len(rs))

	// Step 2: Validate all reservations and group by ticket_class_id
	for _, r := range rs {
		// Validate reservation is active
		if r.Status != models.ReservationStatusActive {
			s.l.Warnf(ctx, "service.reservation.ConfirmReservation: reservation %d is not active (status=%s)", r.ID, r.Status)
			return gorm.ErrInvalidData
		}

		// Check if reservation has expired
		if now.After(r.ExpiresAt) {
			s.l.Warnf(ctx, "service.reservation.ConfirmReservation: reservation %d has expired", r.ID)
			return gorm.ErrInvalidData
		}

		tcUps[r.TicketClassID] += r.Qty
		rIDs = append(rIDs, r.ID)
	}

	// Step 3: Update ticket class counters (reserved → sold) grouped by ticket_class_id
	for tcID, qty := range tcUps {
		result := tx.Model(&models.TicketClass{}).
			Where("id = ?", tcID).
			Where("reserved >= ?", qty).
			Updates(map[string]any{
				"reserved": gorm.Expr("reserved - ?", qty),
				"sold":     gorm.Expr("sold + ?", qty),
			})

		if result.Error != nil {
			s.l.Errorf(ctx, "service.reservation.ConfirmReservation.UpdateTicketClass: ticket_class_id=%d, error=%v", tcID, result.Error)
			return result.Error
		}

		// Check if the ticket class was actually updated (reserved >= qty condition)
		if result.RowsAffected == 0 {
			s.l.Errorf(ctx, "service.reservation.ConfirmReservation: insufficient reserved tickets for ticket_class_id=%d (needed=%d)", tcID, qty)
			return gorm.ErrInvalidData
		}

		s.l.Infof(ctx, "service.reservation.ConfirmReservation: moved %d tickets from reserved to sold for ticket_class_id=%d", qty, tcID)
	}

	// Step 4: Update all reservation statuses to CONFIRMED
	result := tx.Model(&models.Reservation{}).
		Where("id IN ?", rIDs).
		Update("status", models.ReservationStatusConfirmed)

	if result.Error != nil {
		s.l.Errorf(ctx, "service.reservation.ConfirmReservation.UpdateReservations: %v", result.Error)
		return result.Error
	}

	s.l.Infof(ctx, "successfully confirmed %d reservations for order_code=%s", len(rs), oCode)
	return nil
}

func (s implReservationService) Release(ctx context.Context, oCode string) error {
	err := s.repo.GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return s.cancelReservationTx(ctx, tx, oCode)
	})
	return err
}

func (s implReservationService) cancelReservationTx(ctx context.Context, tx *gorm.DB, oCode string) error {
	// Step 1: Lock and fetch all reservations for this order
	var rs []models.Reservation
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("order_code = ?", oCode).
		Find(&rs).Error; err != nil {
		s.l.Errorf(ctx, "service.reservation.CancelReservation.LockReservations: %v", err)
		return err
	}

	if len(rs) == 0 {
		s.l.Warnf(ctx, "service.reservation.CancelReservation: no reservations found for order_code=%s", oCode)
		return gorm.ErrRecordNotFound
	}

	tcUps := make(map[int64]int) // ticket_class_id -> qty to release from reserved
	rIDs := make([]int64, 0, len(rs))

	// Step 2: Validate all reservations can be cancelled and group by ticket_class_id
	for _, r := range rs {
		// Validate reservation can be cancelled (must be ACTIVE)
		if r.Status != models.ReservationStatusActive {
			s.l.Warnf(ctx, "service.reservation.CancelReservation: reservation %d is not active (status=%s)", r.ID, r.Status)
			return gorm.ErrInvalidData
		}

		tcUps[r.TicketClassID] += r.Qty
		rIDs = append(rIDs, r.ID)
	}

	// Step 3: Update ticket class counters (decrement reserved) grouped by ticket_class_id
	for tcID, qty := range tcUps {
		result := tx.Model(&models.TicketClass{}).
			Where("id = ?", tcID).
			Where("reserved >= ?", qty). // Safety check
			Update("reserved", gorm.Expr("reserved - ?", qty))

		if result.Error != nil {
			s.l.Errorf(ctx, "service.reservation.CancelReservation.DecrementReserved: ticket_class_id=%d, error=%v", tcID, result.Error)
			return result.Error
		}

		// Check if the ticket class was actually updated
		if result.RowsAffected == 0 {
			s.l.Errorf(ctx, "service.reservation.CancelReservation: insufficient reserved tickets for ticket_class_id=%d (needed=%d)", tcID, qty)
			return gorm.ErrInvalidData
		}

		s.l.Infof(ctx, "service.reservation.CancelReservation: released %d reserved tickets for ticket_class_id=%d", qty, tcID)
	}

	// Step 4: Update all reservation statuses to CANCELLED
	result := tx.Model(&models.Reservation{}).
		Where("id IN ?", rIDs).
		Update("status", models.ReservationStatusCancelled)

	if result.Error != nil {
		s.l.Errorf(ctx, "service.reservation.CancelReservation.UpdateReservations: %v", result.Error)
		return result.Error
	}

	// Step 5: Publish Kafka event (TODO: implement Kafka producer)
	// TODO: Publish reservation.cancelled event
	// Event payload: {order_code, reservation_ids, ticket_class_summary, cancelled_at, total_count}

	s.l.Infof(ctx, "service.reservation.CancelReservation: successfully cancelled %d reservations for order_code=%s (total qty released: %d)",
		len(rs), oCode, s.sumQuantities(tcUps))
	return nil
}

func (s implReservationService) BatchExpireReservations(ctx context.Context, batchSize int) (int, error) {
	if batchSize <= 0 || batchSize > 1000 {
		batchSize = 500 // Default batch size
	}

	totalExpired := 0

	return totalExpired, s.repo.GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		now := time.Now().UTC()
		s.l.Infof(ctx, "service.reservation.BatchExpireReservations: checking for expired reservations (now=%s, timezone=%s)",
			now.Format(time.RFC3339), now.Location())

		var rs []models.Reservation
		err := tx.Clauses(clause.Locking{
			Strength: "UPDATE",
			Options:  "SKIP LOCKED",
		}).
			Select("id", "ticket_class_id", "qty", "expires_at").
			Where("status = ? AND expires_at < ?", models.ReservationStatusActive, now).
			Order("expires_at").
			Limit(batchSize).
			Find(&rs).Error

		if err != nil {
			s.l.Errorf(ctx, "service.reservation.BatchExpireReservations.LockReservations: %v", err)
			return err
		}

		if len(rs) == 0 {
			s.l.Infof(ctx, "service.reservation.BatchExpireReservations: no expired reservations found")
			return nil
		}

		s.l.Infof(ctx, "service.reservation.BatchExpireReservations: found %d expired reservations (first expires_at=%s)",
			len(rs), rs[0].ExpiresAt.Format(time.RFC3339))

		tsQtyMap := make(map[int64]int)
		rIDs := make([]int64, 0, len(rs))

		for _, r := range rs {
			tsQtyMap[r.TicketClassID] += r.Qty
			rIDs = append(rIDs, r.ID)
		}

		for tcID, totalQty := range tsQtyMap {
			result := tx.Model(&models.TicketClass{}).
				Where("id = ?", tcID).
				Where("reserved >= ?", totalQty). // Safety check
				Update("reserved", gorm.Expr("reserved - ?", totalQty))

			if result.Error != nil {
				s.l.Errorf(ctx, "service.reservation.BatchExpireReservations.DecrementReserved: ticket_class_id=%d, qty=%d, error=%v",
					tcID, totalQty, result.Error)
				return result.Error
			}

			if result.RowsAffected == 0 {
				s.l.Warnf(ctx, "service.reservation.BatchExpireReservations: insufficient reserved for ticket_class_id=%d (needed=%d)",
					tcID, totalQty)
			}

			s.l.Infof(ctx, "service.reservation.BatchExpireReservations: released %d tickets for ticket_class_id=%d",
				totalQty, tcID)
		}

		result := tx.Model(&models.Reservation{}).
			Where("id IN ?", rIDs).
			Update("status", models.ReservationStatusExpired)

		if result.Error != nil {
			s.l.Errorf(ctx, "service.reservation.BatchExpireReservations.UpdateStatus: %v", result.Error)
			return result.Error
		}

		totalExpired = len(rIDs)

		// Step 5: Publish Kafka events (TODO: implement Kafka producer)
		// TODO: Publish batch reservation.expired event
		// Event payload: {reservation_ids, ticket_class_summary, expired_at, total_count}

		s.l.Infof(ctx, "service.reservation.BatchExpireReservations: successfully expired %d reservations across %d ticket classes",
			totalExpired, len(tsQtyMap))

		return nil
	})
}

func (s implReservationService) Delete(ctx context.Context, id uint) error {
	var r models.Reservation
	if err := s.repo.FindByID(ctx, &r, id); err != nil {
		if err == gorm.ErrRecordNotFound {
			s.l.Warnf(ctx, "service.reservation.Delete: %v", err)
		}
		s.l.Errorf(ctx, "service.reservation.Delete: %v", err)
		return err
	}

	if err := s.repo.Delete(ctx, &r); err != nil {
		s.l.Errorf(ctx, "service.reservation.Delete: %v", err)
		return err
	}

	return nil
}

func (s implReservationService) DeleteByOrderCode(ctx context.Context, oCode string) error {
	if err := s.repo.WithContext(ctx).Where("order_code = ?", oCode).Delete(&models.Reservation{}).Error; err != nil {
		s.l.Errorf(ctx, "service.reservation.DeleteByOrderID: %v", err)
		return err
	}

	return nil
}

func (s implReservationService) GetTotalReservedQuantity(ctx context.Context, ticketClassID uint) (int, error) {
	var result struct {
		Total int
	}

	now := time.Now().UTC()
	if err := s.repo.WithContext(ctx).Model(&models.Reservation{}).
		Select("COALESCE(SUM(qty), 0) as total").
		Where("ticket_class_id = ? AND status = ? AND expires_at > ?",
			ticketClassID, models.ReservationStatusActive, now).
		Scan(&result).Error; err != nil {
		s.l.Errorf(ctx, "service.reservation.GetTotalReservedQuantity: %v", err)
		return 0, err
	}
	return result.Total, nil
}
