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
	BatchExpireReservations(ctx context.Context, batchSize int) (int, error)
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
		// Idempotency, scoped by status. A live hold (ACTIVE or CONFIRMED)
		// means this is a retry of a call that already succeeded -- no-op. But
		// rows that are all terminal mean the hold was released or expired,
		// and silently returning success there would hand the caller an order
		// with zero inventory behind it. A concurrent duplicate that races
		// past this unlocked read is still caught by the
		// (order_code, ticket_class_id) unique index on insert.
		var statuses []models.ReservationStatus
		if err := tx.Model(&models.Reservation{}).
			Where("order_code = ?", in.OrderCode).
			Pluck("status", &statuses).Error; err != nil {
			s.l.Errorf(ctx, "service.reservation.Reserve.LoadExistingStatuses: %v", err)
			return err
		}
		if len(statuses) > 0 {
			for _, st := range statuses {
				if st == models.ReservationStatusActive || st == models.ReservationStatusConfirmed {
					s.l.Infof(ctx, "service.reservation.Reserve: live reservations already exist for order_code=%s, no-op", in.OrderCode)
					return nil
				}
			}
			s.l.Warnf(ctx, "service.reservation.Reserve: order_code=%s has only terminal reservations (%v), refusing to re-reserve",
				in.OrderCode, statuses)
			return ErrStateConflict
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
			return ErrNotFound
		}
		byID := indexByID(tcs)

		// Validate sale eligibility and availability against the locked rows.
		now := time.Now().UTC()
		for _, id := range ids {
			tc := byID[id]
			if tc.Status != models.TicketClassStatusActive {
				s.l.Warnf(ctx, "service.reservation.Reserve: ticket_class_id=%d is %s, not on sale", id, tc.Status)
				return ErrSaleClosed
			}
			if tc.SaleStartAt != nil && now.Before(*tc.SaleStartAt) {
				s.l.Warnf(ctx, "service.reservation.Reserve: ticket_class_id=%d sale opens at %s", id, tc.SaleStartAt.Format(time.RFC3339))
				return ErrSaleClosed
			}
			if tc.SaleEndAt != nil && now.After(*tc.SaleEndAt) {
				s.l.Warnf(ctx, "service.reservation.Reserve: ticket_class_id=%d sale closed at %s", id, tc.SaleEndAt.Format(time.RFC3339))
				return ErrSaleClosed
			}

			q := qtyByID[id]
			if tc.Total-tc.Reserved-tc.Sold < q {
				s.l.Warnf(ctx, "service.reservation.Reserve: insufficient stock for ticket_class_id=%d (available=%d, requested=%d)",
					id, tc.Total-tc.Reserved-tc.Sold, q)
				return ErrInsufficientStock
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
				return ErrInsufficientStock
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

// sortedInt64Keys returns the map keys in ascending order, so every operation
// that updates multiple ticket_class rows locks them in the same (ascending
// id) order -- preserving the deadlock-freedom invariant.
func sortedInt64Keys[V any](m map[int64]V) []int64 {
	keys := make([]int64, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	return keys
}

func (s implReservationService) Confirm(ctx context.Context, oCode string) error {
	err := s.repo.GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return s.confirmReservationTx(ctx, tx, oCode)
	})
	return err
}

// confirmDelta is how much of a ticket class's confirm comes from the hold
// this order still owns, versus how much has to be taken back out of free
// stock because the expiry worker already released it.
type confirmDelta struct {
	fromReserved int
	fromFree     int
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
		return ErrNotFound
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

	deltas := make(map[int64]*confirmDelta, len(rs))
	rIDs := make([]int64, 0, len(rs))
	for _, r := range rs {
		d := deltas[r.TicketClassID]
		if d == nil {
			d = &confirmDelta{}
			deltas[r.TicketClassID] = d
		}

		switch r.Status {
		case models.ReservationStatusActive:
			// Still holding its quantity in `reserved`, even if past
			// expires_at -- the worker just has not swept it yet. Confirming
			// is a pure reserved -> sold move and is always safe.
			d.fromReserved += r.Qty

		case models.ReservationStatusExpired:
			// The worker already handed the stock back. Payment succeeded
			// anyway, so try to take it again out of what is free.
			s.l.Warnf(ctx, "service.reservation.Confirm: reservation %d for order_code=%s was already expired; attempting re-acquire of qty=%d",
				r.ID, oCode, r.Qty)
			d.fromFree += r.Qty

		default:
			// CONFIRMED mixed with unconfirmed, or CANCELLED: an order half
			// applied or explicitly released. Never guess -- surface it.
			s.l.Warnf(ctx, "service.reservation.Confirm: conflict for reservation %d (status=%s)", r.ID, r.Status)
			return ErrStateConflict
		}
		rIDs = append(rIDs, r.ID)
	}

	for _, tcID := range sortedInt64Keys(deltas) {
		d := deltas[tcID]

		if d.fromReserved > 0 {
			result := tx.Model(&models.TicketClass{}).
				Where("id = ? AND reserved >= ?", tcID, d.fromReserved).
				Updates(map[string]any{
					"reserved": gorm.Expr("reserved - ?", d.fromReserved),
					"sold":     gorm.Expr("sold + ?", d.fromReserved),
				})
			if result.Error != nil {
				s.l.Errorf(ctx, "service.reservation.Confirm.UpdateTicketClass: ticket_class_id=%d: %v", tcID, result.Error)
				return result.Error
			}
			if result.RowsAffected == 0 {
				s.l.Errorf(ctx, "service.reservation.Confirm: insufficient reserved for ticket_class_id=%d (needed=%d)", tcID, d.fromReserved)
				return ErrInventoryDrift
			}
		}

		if d.fromFree > 0 {
			result := tx.Model(&models.TicketClass{}).
				Where("id = ? AND total - reserved - sold >= ?", tcID, d.fromFree).
				Update("sold", gorm.Expr("sold + ?", d.fromFree))
			if result.Error != nil {
				s.l.Errorf(ctx, "service.reservation.Confirm.ReacquireTicketClass: ticket_class_id=%d: %v", tcID, result.Error)
				return result.Error
			}
			if result.RowsAffected == 0 {
				s.l.Errorf(ctx, "service.reservation.Confirm: cannot re-acquire %d for ticket_class_id=%d on order_code=%s -- stock is gone, order needs a refund",
					d.fromFree, tcID, oCode)
				return ErrStateConflict
			}
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
	var rs []models.Reservation
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("order_code = ?", oCode).Find(&rs).Error; err != nil {
		s.l.Errorf(ctx, "service.reservation.Release.LockReservations: %v", err)
		return err
	}
	if len(rs) == 0 {
		s.l.Infof(ctx, "service.reservation.Release: no reservations for order_code=%s, no-op", oCode)
		return nil // idempotent: nothing to release
	}

	// Idempotency: all already terminal (cancelled/expired) → no-op.
	allReleased := true
	for _, r := range rs {
		if r.Status != models.ReservationStatusCancelled && r.Status != models.ReservationStatusExpired {
			allReleased = false
			break
		}
	}
	if allReleased {
		s.l.Infof(ctx, "service.reservation.Release: order_code=%s already released, no-op", oCode)
		return nil
	}

	tcUps := make(map[int64]int)
	rIDs := make([]int64, 0, len(rs))
	for _, r := range rs {
		if r.Status == models.ReservationStatusConfirmed {
			s.l.Warnf(ctx, "service.reservation.Release: conflict, reservation %d already confirmed", r.ID)
			return ErrStateConflict
		}
		if r.Status != models.ReservationStatusActive {
			continue // already cancelled/expired; leave as-is
		}
		tcUps[r.TicketClassID] += r.Qty
		rIDs = append(rIDs, r.ID)
	}

	for _, tcID := range sortedInt64Keys(tcUps) {
		qty := tcUps[tcID]
		result := tx.Model(&models.TicketClass{}).
			Where("id = ? AND reserved >= ?", tcID, qty).
			Update("reserved", gorm.Expr("reserved - ?", qty))
		if result.Error != nil {
			s.l.Errorf(ctx, "service.reservation.Release.DecrementReserved: ticket_class_id=%d: %v", tcID, result.Error)
			return result.Error
		}
		if result.RowsAffected == 0 {
			s.l.Errorf(ctx, "service.reservation.Release: insufficient reserved for ticket_class_id=%d (needed=%d)", tcID, qty)
			return ErrInventoryDrift
		}
	}

	if len(rIDs) > 0 {
		if err := tx.Model(&models.Reservation{}).
			Where("id IN ?", rIDs).
			Update("status", models.ReservationStatusCancelled).Error; err != nil {
			s.l.Errorf(ctx, "service.reservation.Release.UpdateReservations: %v", err)
			return err
		}
	}

	s.l.Infof(ctx, "service.reservation.Release: cancelled %d reservations for order_code=%s", len(rIDs), oCode)
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
			Where("status = ? AND expires_at <= ?", models.ReservationStatusActive, now).
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

		for _, tcID := range sortedInt64Keys(tsQtyMap) {
			totalQty := tsQtyMap[tcID]
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
