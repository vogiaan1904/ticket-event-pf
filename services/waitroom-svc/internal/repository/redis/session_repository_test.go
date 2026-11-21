package repository

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/vogiaan1904/ticketbottle-waitroom/internal/models"
	"github.com/vogiaan1904/ticketbottle-waitroom/pkg/logger"
)

func newTestSessionRepo(t *testing.T) SessionRepository {
	t.Helper()
	_, cli := newTestRepo(t)
	l := logger.InitializeZapLogger(logger.ZapConfig{Level: "error", Mode: "development", Encoding: "console"})
	return NewRedisSessionRepository(cli, l)
}

// fullSession sets every field to a value that is not its zero.
func fullSession() *models.Session {
	at := func(m int) *time.Time { t := time.Date(2027, 1, 1, 10, m, 0, 0, time.UTC); return &t }
	return &models.Session{
		ID: uuid.NewString(), UserID: "u-1", EventID: "e-1", Position: 7,
		QueuedAt: *at(1), Status: models.SessionStatusAdmitted, AdmittedAt: at(2), CompletedAt: at(3),
		ExpiresAt: *at(59), CheckoutToken: "tok", CheckoutExpiresAt: at(7),
		TokenInvalidated: true, TokenInvalidatedAt: at(4), TokenInvalidationReason: "completed",
		ConnectionID: "conn", UserAgent: "agent", IPAddress: "10.0.0.1", LastHeartbeatAt: *at(5),
		AttemptCount: 2, QueueScore: 1798797600.25, CreatedAt: *at(0), UpdatedAt: *at(6),
	}
}

// A field added to Session and left out of the fixture would be read back as a zero
// value it was never given, so nothing would notice it going unstored.
func TestFullSessionSetsEveryField(t *testing.T) {
	v := reflect.ValueOf(*fullSession())
	for i := range v.NumField() {
		if v.Field(i).IsZero() {
			t.Errorf("fullSession leaves %s zero", v.Type().Field(i).Name)
		}
	}
}

func TestASessionReadsBackEveryField(t *testing.T) {
	repo := newTestSessionRepo(t)
	want := fullSession()

	if err := repo.Create(context.Background(), want); err != nil {
		t.Fatalf("create: %v", err)
	}
	got, err := repo.Get(context.Background(), want.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("read back\n%+v\nwant\n%+v", got, want)
	}
}

// A status write reads the session and writes it whole: nothing else may change.
func TestAStatusUpdateKeepsEveryOtherField(t *testing.T) {
	repo := newTestSessionRepo(t)
	want := fullSession()
	if err := repo.Create(context.Background(), want); err != nil {
		t.Fatalf("create: %v", err)
	}

	if err := repo.UpdateStatus(context.Background(), want.ID, models.SessionStatusCompleted); err != nil {
		t.Fatalf("update status: %v", err)
	}
	got, err := repo.Get(context.Background(), want.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	want.Status, want.UpdatedAt = models.SessionStatusCompleted, got.UpdatedAt
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("read back\n%+v\nwant\n%+v", got, want)
	}
}
