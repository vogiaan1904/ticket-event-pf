package gorm

import (
	"context"
	"fmt"
	"log"
	"time"

	cfg "github.com/vogiaan/ticketbottle-inventory/config"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// DB wraps the GORM database connection
type DB struct {
	*gorm.DB
	config *cfg.PostgresConfig
}

// New creates a new database connection
func New(config *cfg.PostgresConfig) (*DB, error) {
	gormLogger := logger.New(
		log.New(log.Writer(), "\r\n", log.LstdFlags),
		logger.Config{
			SlowThreshold:             time.Second,
			LogLevel:                  logger.Info,
			IgnoreRecordNotFoundError: true,
			Colorful:                  true,
		},
	)

	db, err := gorm.Open(postgres.Open(config.URL), &gorm.Config{
		Logger:                 gormLogger,
		SkipDefaultTransaction: true,
		PrepareStmt:            true,
		NowFunc: func() time.Time {
			return time.Now().UTC()
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	// Get underlying sql.DB to configure connection pool
	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("failed to get database instance: %w", err)
	}

	// Set connection pool settings
	sqlDB.SetMaxOpenConns(config.MaxOpenConns)
	sqlDB.SetMaxIdleConns(config.MaxIdleConns)
	sqlDB.SetConnMaxLifetime(config.ConnMaxLifetime)
	sqlDB.SetConnMaxIdleTime(config.ConnMaxIdleTime)

	if err := db.Exec("SET TIME ZONE 'UTC'").Error; err != nil {
		return nil, fmt.Errorf("failed to set timezone to UTC: %w", err)
	}

	time.Local = time.UTC

	return &DB{
		DB:     db,
		config: config,
	}, nil
}

func Connect(config *cfg.PostgresConfig, maxRetries int, retryInterval time.Duration) (*DB, error) {
	var db *DB
	var err error

	for i := 0; i < maxRetries; i++ {
		db, err = New(config)
		if err == nil {
			// Test the connection
			sqlDB, _ := db.DB.DB()
			if err := sqlDB.Ping(); err == nil {
				log.Printf("Successfully connected to database on attempt %d", i+1)
				return db, nil
			}
		}

		log.Printf("Failed to connect to database (attempt %d/%d): %v", i+1, maxRetries, err)
		if i < maxRetries-1 {
			time.Sleep(retryInterval)
		}
	}

	return nil, fmt.Errorf("failed to connect to database after %d attempts: %w", maxRetries, err)
}

func (db *DB) Close() error {
	sqlDB, err := db.DB.DB()
	if err != nil {
		return fmt.Errorf("failed to get database instance: %w", err)
	}

	if err := sqlDB.Close(); err != nil {
		return fmt.Errorf("failed to close database connection: %w", err)
	}

	log.Println("Database connection closed")
	return nil
}

func (db *DB) Ping(ctx context.Context) error {
	sqlDB, err := db.DB.DB()
	if err != nil {
		return fmt.Errorf("failed to get database instance: %w", err)
	}

	if err := sqlDB.PingContext(ctx); err != nil {
		return fmt.Errorf("failed to ping database: %w", err)
	}

	return nil
}

func (db *DB) Stats() map[string]interface{} {
	sqlDB, err := db.DB.DB()
	if err != nil {
		return map[string]interface{}{"error": err.Error()}
	}

	stats := sqlDB.Stats()
	return map[string]interface{}{
		"max_open_connections": stats.MaxOpenConnections,
		"open_connections":     stats.OpenConnections,
		"in_use":               stats.InUse,
		"idle":                 stats.Idle,
		"wait_count":           stats.WaitCount,
		"wait_duration":        stats.WaitDuration.String(),
		"max_idle_closed":      stats.MaxIdleClosed,
		"max_lifetime_closed":  stats.MaxLifetimeClosed,
	}
}

func (db *DB) AutoMigrate(models ...interface{}) error {
	if err := db.DB.AutoMigrate(models...); err != nil {
		return fmt.Errorf("failed to auto migrate: %w", err)
	}
	log.Println("Database migration completed successfully")
	return nil
}

func (db *DB) IsHealthy(ctx context.Context) bool {
	return db.Ping(ctx) == nil
}
