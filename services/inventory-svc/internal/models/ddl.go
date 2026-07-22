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

		// The oversell backstop. NOT VALID means the constraint enforces every
		// new write immediately but does not scan pre-existing rows -- a boot
		// that fails on historical drift would take the service down rather
		// than surface the drift. Validate deliberately, out of band:
		//   ALTER TABLE ticket_class VALIDATE CONSTRAINT chk_ticket_class_capacity;
		`DO $$ BEGIN
		   ALTER TABLE ticket_class ADD CONSTRAINT chk_ticket_class_capacity
		     CHECK (reserved >= 0 AND sold >= 0 AND reserved + sold <= total) NOT VALID;
		 EXCEPTION WHEN duplicate_object THEN NULL; END $$`,

		`DO $$ BEGIN
		   ALTER TABLE ticket_class ADD CONSTRAINT chk_ticket_class_total_nonneg
		     CHECK (total >= 0) NOT VALID;
		 EXCEPTION WHEN duplicate_object THEN NULL; END $$`,

		`DO $$ BEGIN
		   ALTER TABLE reservation ADD CONSTRAINT chk_reservation_qty_positive
		     CHECK (qty > 0) NOT VALID;
		 EXCEPTION WHEN duplicate_object THEN NULL; END $$`,
	}
}
