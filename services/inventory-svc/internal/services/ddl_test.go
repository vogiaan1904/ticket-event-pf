package service

import (
	"testing"

	"github.com/vogiaan/ticketbottle-inventory/internal/models"
	"gorm.io/gorm"
)

// A fresh newTestDB already has the FK as RESTRICT, so the repair branch never
// runs there: a typo in its constraint name would leave every real database on
// CASCADE with this suite still green. Force the pre-fix state instead.
func TestPostMigrateStatements_RepairsCascadeToRestrict(t *testing.T) {
	repo := newTestDB(t)
	db := repo.GetDB()

	// Pre-fix state: force the FK back onto the wrong ON DELETE action.
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
