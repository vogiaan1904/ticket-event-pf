package service

import (
	"context"
	"fmt"
	"os"
	"sync/atomic"
	"testing"
	"time"

	cfg "github.com/vogiaan/ticketbottle-inventory/config"
	"github.com/vogiaan/ticketbottle-inventory/internal/models"
	pkgGorm "github.com/vogiaan/ticketbottle-inventory/pkg/gorm"
	pkgLog "github.com/vogiaan/ticketbottle-inventory/pkg/logger"
)

var seedCounter atomic.Int64

const defaultTestDSN = "postgresql://root:root@localhost:5435/ticketbottle_inventory_test?sslmode=disable"

func newTestDB(t *testing.T) *pkgGorm.Repository {
	t.Helper()
	dsn := os.Getenv("TEST_POSTGRES_URL")
	if dsn == "" {
		dsn = defaultTestDSN
	}
	db, err := pkgGorm.New(&cfg.PostgresConfig{
		URL:             dsn,
		MaxOpenConns:    25,
		MaxIdleConns:    10,
		ConnMaxLifetime: 5 * time.Minute,
		ConnMaxIdleTime: 10 * time.Minute,
	})
	if err != nil {
		t.Skipf("skipping: cannot reach test postgres (%s): %v", dsn, err)
	}
	if err := db.AutoMigrate(&models.TicketClass{}, &models.Reservation{}); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	t.Cleanup(func() {
		db.Exec("TRUNCATE reservation, ticket_class RESTART IDENTITY CASCADE")
	})
	return pkgGorm.NewRepository(db)
}

func newTestLogger() pkgLog.Logger {
	return pkgLog.InitializeZapLogger(pkgLog.ZapConfig{
		Level:    "error",
		Mode:     "development",
		Encoding: "console",
	})
}

func seedTicketClass(t *testing.T, repo *pkgGorm.Repository, total, reserved, sold int) models.TicketClass {
	t.Helper()
	n := seedCounter.Add(1)
	tc := models.TicketClass{
		EventID:    fmt.Sprintf("evt-%s-%d", t.Name(), n),
		Name:       fmt.Sprintf("GA-%s-%d", t.Name(), n),
		PriceCents: 1000,
		Currency:   "USD",
		Total:      total,
		Reserved:   reserved,
		Sold:       sold,
		Status:     models.TicketClassStatusActive,
	}
	if err := repo.Create(context.Background(), &tc); err != nil {
		t.Fatalf("seed ticket class: %v", err)
	}
	return tc
}

func TestHarness_Connects(t *testing.T) {
	repo := newTestDB(t)
	tc := seedTicketClass(t, repo, 100, 0, 0)
	if tc.ID == 0 {
		t.Fatal("expected a generated ticket class id")
	}
}
