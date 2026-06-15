package gorm

import (
	"context"
	"fmt"

	"gorm.io/gorm"
)

// TransactionFunc is a function that runs within a transaction
type TransactionFunc func(*gorm.DB) error

// WithTransaction executes a function within a database transaction
func (db *DB) WithTransaction(ctx context.Context, fn TransactionFunc) error {
	return db.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := fn(tx); err != nil {
			return fmt.Errorf("transaction failed: %w", err)
		}
		return nil
	})
}

// BeginTransaction starts a new transaction
func (db *DB) BeginTransaction(ctx context.Context) *gorm.DB {
	return db.DB.WithContext(ctx).Begin()
}

// CommitTransaction commits a transaction
func (db *DB) CommitTransaction(tx *gorm.DB) error {
	if err := tx.Commit().Error; err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}
	return nil
}

// RollbackTransaction rolls back a transaction
func (db *DB) RollbackTransaction(tx *gorm.DB) error {
	if err := tx.Rollback().Error; err != nil {
		return fmt.Errorf("failed to rollback transaction: %w", err)
	}
	return nil
}
