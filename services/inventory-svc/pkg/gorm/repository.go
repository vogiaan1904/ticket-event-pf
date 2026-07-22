package gorm

import (
	"context"

	"gorm.io/gorm"
)

// Repository provides a base repository pattern implementation
type Repository struct {
	db *DB
}

// NewRepository creates a new repository instance
func NewRepository(db *DB) *Repository {
	return &Repository{db: db}
}

// GetDB returns the underlying GORM DB instance
func (r *Repository) GetDB() *gorm.DB {
	return r.db.DB
}

// WithContext returns a new DB instance with context
func (r *Repository) WithContext(ctx context.Context) *gorm.DB {
	return r.db.DB.WithContext(ctx)
}

// Create inserts a new record
func (r *Repository) Create(ctx context.Context, model interface{}) error {
	return r.WithContext(ctx).Create(model).Error
}

// FindByID finds a record by ID
func (r *Repository) FindByID(ctx context.Context, model interface{}, id interface{}) error {
	return r.WithContext(ctx).First(model, id).Error
}

// Update updates a record.
//
// Deprecated: this is Save(model) under the hood, which writes every column
// on the row -- including counters like ticket_class.reserved/sold. Calling
// it with a model loaded outside the current transaction clobbers whatever a
// concurrent, properly-locked writer changed in the meantime; this is what
// caused this service's oversell P0 (a concurrent reservation's `reserved`
// increment was silently erased). Callers must instead issue a
// column-targeted Updates(map) inside a `SELECT ... FOR UPDATE` transaction,
// or a guarded conditional UPDATE, as internal/services/reservation.go does.
func (r *Repository) Update(ctx context.Context, model interface{}) error {
	return r.WithContext(ctx).Save(model).Error
}

// Delete deletes a record.
//
// Deprecated: this is a hard delete (neither model has a DeletedAt field, so
// there is no soft-delete to speak of) that takes no lock on the row being
// deleted and performs no state check before deleting it. Calling this on a
// ticket_class is exactly the unguarded, unlocked delete that let
// DeleteTicketClass silently destroy live reservations, including paid
// (CONFIRMED) ones -- the bug the FK RESTRICT and the in-service guard now
// exist to prevent. Callers must instead follow the guarded pattern in
// internal/services/ticketclass.go's implTicketClassService.Delete: lock the
// parent row FOR UPDATE, check for non-terminal children, and only then
// delete.
func (r *Repository) Delete(ctx context.Context, model interface{}) error {
	return r.WithContext(ctx).Delete(model).Error
}
