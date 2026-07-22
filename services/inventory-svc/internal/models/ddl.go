package models

// PostMigrateStatements are the DDL statements applied immediately after
// AutoMigrate, in order. Every statement must be idempotent: both the service
// boot path and the test harness run them unconditionally on every start.
//
// This is the single source of truth for schema that AutoMigrate cannot
// express (partial indexes, CHECK constraints). Versioned migrations will
// eventually replace both this and AutoMigrate.
func PostMigrateStatements() []string {
	return []string{
		`CREATE INDEX IF NOT EXISTS idx_reservation_active_expiry
		   ON reservation (status, expires_at) WHERE status = 'ACTIVE'`,
	}
}
