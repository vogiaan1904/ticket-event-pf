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

// HardDelete permanently deletes a record
func (r *Repository) HardDelete(ctx context.Context, model interface{}) error {
	return r.WithContext(ctx).Unscoped().Delete(model).Error
}

// FindAll retrieves all records
func (r *Repository) FindAll(ctx context.Context, models interface{}) error {
	return r.WithContext(ctx).Find(models).Error
}

// FindWhere retrieves records matching conditions
func (r *Repository) FindWhere(ctx context.Context, models interface{}, query interface{}, args ...interface{}) error {
	return r.WithContext(ctx).Where(query, args...).Find(models).Error
}

// Count counts records
func (r *Repository) Count(ctx context.Context, model interface{}, count *int64) error {
	return r.WithContext(ctx).Model(model).Count(count).Error
}

// Exists checks if a record exists
func (r *Repository) Exists(ctx context.Context, model interface{}, conditions ...interface{}) (bool, error) {
	var count int64
	err := r.WithContext(ctx).Model(model).Where(conditions[0], conditions[1:]...).Count(&count).Error
	return count > 0, err
}
