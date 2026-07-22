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

		// fk_ticket_class_reservations was created with contradictory OnDelete
		// tags on the two sides of the GORM relation (CASCADE on
		// TicketClass.Reservations, RESTRICT on Reservation.TicketClass).
		// AutoMigrate creates the constraint once, the first time either model
		// is migrated and it does not yet exist, and never revisits it after
		// that -- so whichever side was migrated first (TicketClass, in
		// main.go's AutoMigrate call) won, and every database ever bootstrapped
		// against these tags is stuck on CASCADE. That let DeleteTicketClass
		// silently destroy every reservation referencing it, including
		// CONFIRMED ones (paid orders). The struct tags are now both RESTRICT,
		// but changing them does not touch an already-migrated database, so
		// this statement repairs one in place.
		//
		// Guarded: dropping and re-adding a foreign key constraint takes
		// ACCESS EXCLUSIVE on both tables, which on a busy reservation table
		// would queue behind in-flight transactions and then block new ones
		// behind it. Only run that DDL when confdeltype is actually wrong
		// ('c' = CASCADE, 'a' = NO ACTION) -- the already-correct case
		// ('r' = RESTRICT) does a plain catalog SELECT and takes no lock.
		`DO $$
		   DECLARE
		     current_action "char";
		   BEGIN
		     SELECT confdeltype INTO current_action
		       FROM pg_constraint
		      WHERE conname = 'fk_ticket_class_reservations';

		     IF current_action IS NOT NULL AND current_action <> 'r' THEN
		       ALTER TABLE reservation DROP CONSTRAINT fk_ticket_class_reservations;
		       ALTER TABLE reservation ADD CONSTRAINT fk_ticket_class_reservations
		         FOREIGN KEY (ticket_class_id) REFERENCES ticket_class (id) ON DELETE RESTRICT;
		     END IF;
		   END $$`,
	}
}
