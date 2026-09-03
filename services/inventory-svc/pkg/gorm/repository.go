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
// Deprecated: Save(model) writes every column, so a stale in-memory row wipes
// a concurrent writer's counters -- this service's oversell P0. Use a targeted
// Updates(map) inside SELECT ... FOR UPDATE (internal/services/reservation.go).
func (r *Repository) Update(ctx context.Context, model interface{}) error {
	return r.WithContext(ctx).Save(model).Error
}

// Delete deletes a record.
//
// Deprecated: an unlocked, unguarded hard delete -- on a ticket_class this is
// what destroyed live and paid reservations. Use the guarded pattern in
// internal/services/ticketclass.go: lock the parent, refuse live children.
func (r *Repository) Delete(ctx context.Context, model interface{}) error {
	return r.WithContext(ctx).Delete(model).Error
}
