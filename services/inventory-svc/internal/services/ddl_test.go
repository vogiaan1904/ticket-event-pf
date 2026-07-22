package service

import (
	"testing"

	"github.com/vogiaan/ticketbottle-inventory/internal/models"
	"gorm.io/gorm"
)

// The corrective DDL in models.PostMigrateStatements is the highest-risk
// statement in this package and, before this test, had zero coverage: every
// other test's newTestDB creates a fresh database where AutoMigrate itself
// lays down fk_ticket_class_reservations as RESTRICT (from the now-corrected
// struct tags), so the repair branch's guarded IF never actually executes on
// that path. A typo in the constraint name looked up there would leave every
// real, already-migrated database stuck on CASCADE and this suite would
// still be green. This test forces a database into that pre-fix state
// directly, so the repair branch actually runs.
func TestPostMigrateStatements_RepairsCascadeToRestrict(t *testing.T) {
	repo := newTestDB(t)
	db := repo.GetDB()

	// Simulate a pre-fix database: force the FK back onto the wrong action,
	// the way an already-migrated database left over from the old
	// contradictory GORM tags would be.
	if err := db.Exec(`ALTER TABLE reservation DROP CONSTRAINT fk_ticket_class_reservations`).Error; err != nil {
		t.Fatalf("force-drop constraint: %v", err)
	}
	if err := db.Exec(`ALTER TABLE reservation ADD CONSTRAINT fk_ticket_class_reservations
		FOREIGN KEY (ticket_class_id) REFERENCES ticket_class (id) ON DELETE CASCADE`).Error; err != nil {
		t.Fatalf("force-add CASCADE constraint: %v", err)
	}
	assertConfDelType(t, db, "c")

	for _, stmt := range models.PostMigrateStatements() {
		if err := db.Exec(stmt).Error; err != nil {
			t.Fatalf("PostMigrateStatements: %v", err)
		}
	}
	assertConfDelType(t, db, "r")

	// Idempotency: rerunning against an already-correct constraint must be a
	// no-op (plain catalog SELECT, no lock) and leave the action RESTRICT.
	for _, stmt := range models.PostMigrateStatements() {
		if err := db.Exec(stmt).Error; err != nil {
			t.Fatalf("PostMigrateStatements (second run): %v", err)
		}
	}
	assertConfDelType(t, db, "r")
}

func assertConfDelType(t *testing.T, db *gorm.DB, want string) {
	t.Helper()
	var got string
	err := db.Raw(`SELECT confdeltype FROM pg_constraint
		WHERE conname = 'fk_ticket_class_reservations' AND conrelid = 'reservation'::regclass`).
		Scan(&got).Error
	if err != nil {
		t.Fatalf("query confdeltype: %v", err)
	}
	if got != want {
		t.Fatalf("confdeltype = %q, want %q", got, want)
	}
}
