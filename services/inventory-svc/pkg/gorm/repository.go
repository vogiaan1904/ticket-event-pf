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

// Update updates a record
func (r *Repository) Update(ctx context.Context, model interface{}) error {
	return r.WithContext(ctx).Save(model).Error
}

// Delete soft deletes a record
func (r *Repository) Delete(ctx context.Context, model interface{}) error {
	return r.WithContext(ctx).Delete(model).Error
}
