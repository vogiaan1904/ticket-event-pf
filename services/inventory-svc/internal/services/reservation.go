package service

import (
	"context"
	"sort"
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
}

func NewReservationService(l pkgLog.Logger, repo *pkgGorm.Repository) ReservationService {
	return &implReservationService{
		l:    l,
		repo: repo,
	}
}

func (s implReservationService) Reserve(ctx context.Context, in ReserveInput) error {
	ids, qtyByID := aggregateDemand(in.Items)

	return s.repo.GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Idempotency: reservations already exist for this order → no-op.
		var existing int64
		if err := tx.Model(&models.Reservation{}).
			Where("order_code = ?", in.OrderCode).Count(&existing).Error; err != nil {
			s.l.Errorf(ctx, "service.reservation.Reserve.CountExisting: %v", err)
			return err
		}
		if existing > 0 {
			s.l.Infof(ctx, "service.reservation.Reserve: reservations already exist for order_code=%s, no-op", in.OrderCode)
			return nil
		}

		// Lock all target ticket classes in ascending id order (deadlock-free).
		var tcs []models.TicketClass
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id IN ?", ids).Order("id").Find(&tcs).Error; err != nil {
			s.l.Errorf(ctx, "service.reservation.Reserve.LockTicketClasses: %v", err)
			return err
		}
		if len(tcs) != len(ids) {
			s.l.Warnf(ctx, "service.reservation.Reserve: ticket classes not found (want=%d got=%d)", len(ids), len(tcs))
			return gorm.ErrRecordNotFound
		}
		byID := indexByID(tcs)

		// Validate availability against the locked rows.
		for _, id := range ids {
			tc := byID[id]
			q := qtyByID[id]
			if tc.Total-tc.Reserved-tc.Sold < q {
				s.l.Warnf(ctx, "service.reservation.Reserve: insufficient stock for ticket_class_id=%d (available=%d, requested=%d)",
					id, tc.Total-tc.Reserved-tc.Sold, q)
				return gorm.ErrInvalidData
			}
		}

		// Increment reserved counters (guarded) and build reservation rows.
		rs := make([]models.Reservation, 0, len(ids))
		for _, id := range ids {
			q := qtyByID[id]
			res := tx.Model(&models.TicketClass{}).
				Where("id = ? AND reserved + sold + ? <= total", id, q).
				Update("reserved", gorm.Expr("reserved + ?", q))
			if res.Error != nil {
				s.l.Errorf(ctx, "service.reservation.Reserve.IncrementReserved: ticket_class_id=%d: %v", id, res.Error)
				return res.Error
			}
			if res.RowsAffected == 0 {
				s.l.Warnf(ctx, "service.reservation.Reserve: availability guard failed for ticket_class_id=%d", id)
				return gorm.ErrInvalidData
			}
			rs = append(rs, s.buildModel(in.OrderCode, in.ExpiresAt, ReserveItem{TicketClassID: id, Qty: q}))
		}

		if err := tx.Create(&rs).Error; err != nil {
			s.l.Errorf(ctx, "service.reservation.Reserve.InsertReservations: %v", err)
			return err
		}

		s.l.Infof(ctx, "service.reservation.Reserve: created %d reservations for order_code=%s", len(rs), in.OrderCode)
		return nil
	})
}

// aggregateDemand collapses items to one entry per ticket class (summing qty)
// and returns the ticket-class ids sorted ascending for deterministic locking.
func aggregateDemand(items []ReserveItem) (ids []int64, qtyByID map[int64]int) {
	qtyByID = make(map[int64]int, len(items))
	for _, it := range items {
		if _, ok := qtyByID[it.TicketClassID]; !ok {
			ids = append(ids, it.TicketClassID)
		}
		qtyByID[it.TicketClassID] += it.Qty
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids, qtyByID
}

func indexByID(tcs []models.TicketClass) map[int64]models.TicketClass {
	m := make(map[int64]models.TicketClass, len(tcs))
	for _, tc := range tcs {
		m[tc.ID] = tc
	}
	return m
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
	var rs []models.Reservation
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("order_code = ?", oCode).Find(&rs).Error; err != nil {
		s.l.Errorf(ctx, "service.reservation.Confirm.LockReservations: %v", err)
		return err
	}
	if len(rs) == 0 {
		s.l.Warnf(ctx, "service.reservation.Confirm: no reservations for order_code=%s", oCode)
		return gorm.ErrRecordNotFound
	}

	// Idempotency: all already confirmed → no-op.
	allConfirmed := true
	for _, r := range rs {
		if r.Status != models.ReservationStatusConfirmed {
			allConfirmed = false
			break
		}
	}
	if allConfirmed {
		s.l.Infof(ctx, "service.reservation.Confirm: order_code=%s already confirmed, no-op", oCode)
		return nil
	}

	now := time.Now().UTC()
	tcUps := make(map[int64]int)
	rIDs := make([]int64, 0, len(rs))
	for _, r := range rs {
		if r.Status != models.ReservationStatusActive || now.After(r.ExpiresAt) {
			s.l.Warnf(ctx, "service.reservation.Confirm: conflict for reservation %d (status=%s, timeExpired=%v)",
				r.ID, r.Status, now.After(r.ExpiresAt))
			return ErrStateConflict
		}
		tcUps[r.TicketClassID] += r.Qty
		rIDs = append(rIDs, r.ID)
	}

	for tcID, qty := range tcUps {
		result := tx.Model(&models.TicketClass{}).
			Where("id = ? AND reserved >= ?", tcID, qty).
			Updates(map[string]any{
				"reserved": gorm.Expr("reserved - ?", qty),
				"sold":     gorm.Expr("sold + ?", qty),
			})
		if result.Error != nil {
			s.l.Errorf(ctx, "service.reservation.Confirm.UpdateTicketClass: ticket_class_id=%d: %v", tcID, result.Error)
			return result.Error
		}
		if result.RowsAffected == 0 {
			s.l.Errorf(ctx, "service.reservation.Confirm: insufficient reserved for ticket_class_id=%d (needed=%d)", tcID, qty)
			return gorm.ErrInvalidData
		}
	}

	if err := tx.Model(&models.Reservation{}).
		Where("id IN ?", rIDs).
		Update("status", models.ReservationStatusConfirmed).Error; err != nil {
		s.l.Errorf(ctx, "service.reservation.Confirm.UpdateReservations: %v", err)
		return err
	}

	s.l.Infof(ctx, "service.reservation.Confirm: confirmed %d reservations for order_code=%s", len(rs), oCode)
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
