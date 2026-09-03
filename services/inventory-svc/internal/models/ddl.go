package models

// PostMigrateStatements returns the DDL applied right after AutoMigrate, in order.
// Invariant: every statement is idempotent -- boot and the test harness rerun
// them unconditionally. Source of truth for schema AutoMigrate cannot express
// (partial indexes, CHECK constraints). See docs/POST_MIGRATE_DDL.md.
func PostMigrateStatements() []string {
	return []string{
		`CREATE INDEX IF NOT EXISTS idx_reservation_active_expiry
		   ON reservation (status, expires_at) WHERE status = 'ACTIVE'`,

		// The oversell backstop, NOT VALID: enforced on new writes, history unscanned
		// so old drift cannot fail a boot. Validate out of band once drift is clean:
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

		// Repairs a database left on ON DELETE CASCADE by the old contradictory
		// GORM tags: AutoMigrate never revisits an existing FK, so correcting the
		// tags is not enough. Guarded -- the DROP/ADD takes ACCESS EXCLUSIVE.
		// See docs/POST_MIGRATE_DDL.md#the-fk_ticket_class_reservations-repair.
		`DO $$
		   DECLARE
		     current_action "char";
		   BEGIN
		     SELECT confdeltype INTO current_action
		       FROM pg_constraint
		      WHERE conname = 'fk_ticket_class_reservations'
		        AND conrelid = 'reservation'::regclass;

		     IF current_action IS NOT NULL AND current_action <> 'r' THEN
		       ALTER TABLE reservation DROP CONSTRAINT fk_ticket_class_reservations;
		       ALTER TABLE reservation ADD CONSTRAINT fk_ticket_class_reservations
		         FOREIGN KEY (ticket_class_id) REFERENCES ticket_class (id) ON DELETE RESTRICT NOT VALID;
		     END IF;
		   END $$`,
	}
}
