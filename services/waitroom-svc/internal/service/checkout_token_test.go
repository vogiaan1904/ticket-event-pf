package service

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/vogiaan1904/ticketbottle-waitroom/config"
	"github.com/vogiaan1904/ticketbottle-waitroom/internal/models"
	repo "github.com/vogiaan1904/ticketbottle-waitroom/internal/repository/redis"
	"github.com/vogiaan1904/ticketbottle-waitroom/pkg/logger"
	"github.com/vogiaan1904/ticketbottle-waitroom/pkg/redis"
)

func newTokenRig(t *testing.T, expiry time.Duration) SessionService {
	t.Helper()
	addr := os.Getenv("WAITROOM_TEST_REDIS_ADDR")
	if addr == "" {
		if os.Getenv("CI") != "" {
			t.Fatalf("CI requires WAITROOM_TEST_REDIS_ADDR to point at a test redis")
		}
		t.Skip("WAITROOM_TEST_REDIS_ADDR not set; skipping Redis integration test")
	}
	cli := redis.NewClient(config.RedisConfig{Addr: addr, PoolSize: 5, MinIdleConns: 1})
	l := logger.InitializeZapLogger(logger.ZapConfig{Level: "error", Mode: "development", Encoding: "console"})
	return NewSessionService(repo.NewRedisSessionRepository(cli, l), config.JWTConfig{Secret: "test", Expiry: expiry}, l)
}

// admitWithToken stores an admitted session holding a fresh checkout token.
func admitWithToken(t *testing.T, ss SessionService, uID, eID string) (*models.Session, string) {
	t.Helper()
	ctx := context.Background()
	s, err := ss.CreateSession(ctx, uID, eID, "agent", "10.0.0.1", time.Now())
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if err := ss.UpdateSessionStatus(ctx, s.ID, models.SessionStatusAdmitted); err != nil {
		t.Fatalf("UpdateSessionStatus: %v", err)
	}
	tok, err := ss.GenerateCheckoutToken(ctx, s)
	if err != nil {
		t.Fatalf("GenerateCheckoutToken: %v", err)
	}
	if err := ss.UpdateCheckoutToken(ctx, s.ID, tok, time.Now().Add(5*time.Minute)); err != nil {
		t.Fatalf("UpdateCheckoutToken: %v", err)
	}
	return s, tok
}

func TestValidateCheckoutTokenAdmitsItsHolder(t *testing.T) {
	ss := newTokenRig(t, 5*time.Minute)
	s, tok := admitWithToken(t, ss, "u-1", "e-1")

	got, err := ss.ValidateCheckoutToken(context.Background(), tok, "u-1", "e-1")
	if err != nil {
		t.Fatalf("ValidateCheckoutToken: %v", err)
	}
	if got != s.ID {
		t.Fatalf("session id = %q, want %q", got, s.ID)
	}
}

func TestValidateCheckoutTokenRefuses(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name  string
		after func(ss SessionService, s *models.Session)
		user  string
		event string
	}{
		{name: "another user", user: "u-2", event: "e-1"},
		{name: "another event", user: "u-1", event: "e-2"},
		{name: "revoked after a completed checkout", user: "u-1", event: "e-1",
			after: func(ss SessionService, s *models.Session) {
				_ = ss.InvalidateCheckoutToken(ctx, s.ID, "completed")
			}},
		{name: "a completed session", user: "u-1", event: "e-1",
			after: func(ss SessionService, s *models.Session) {
				_ = ss.UpdateSessionStatus(ctx, s.ID, models.SessionStatusCompleted)
			}},
		{name: "a failed session", user: "u-1", event: "e-1",
			after: func(ss SessionService, s *models.Session) {
				_ = ss.UpdateSessionStatus(ctx, s.ID, models.SessionStatusFailed)
			}},
		{name: "an expired session", user: "u-1", event: "e-1",
			after: func(ss SessionService, s *models.Session) {
				_ = ss.UpdateSessionStatus(ctx, s.ID, models.SessionStatusExpired)
			}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ss := newTokenRig(t, 5*time.Minute)
			s, tok := admitWithToken(t, ss, "u-1", "e-1")
			if tc.after != nil {
				tc.after(ss, s)
			}
			if _, err := ss.ValidateCheckoutToken(ctx, tok, tc.user, tc.event); !errors.Is(err, ErrCheckoutTokenRefused) {
				t.Fatalf("err = %v, want ErrCheckoutTokenRefused", err)
			}
		})
	}
}

func TestValidateCheckoutTokenRefusesAnExpiredOrForeignToken(t *testing.T) {
	ctx := context.Background()

	expired := newTokenRig(t, -time.Minute)
	_, tok := admitWithToken(t, expired, "u-1", "e-1")
	if _, err := expired.ValidateCheckoutToken(ctx, tok, "u-1", "e-1"); !errors.Is(err, ErrCheckoutTokenRefused) {
		t.Fatalf("expired: err = %v, want ErrCheckoutTokenRefused", err)
	}

	ss := newTokenRig(t, 5*time.Minute)
	for _, tok := range []string{"", "not-a-jwt"} {
		if _, err := ss.ValidateCheckoutToken(ctx, tok, "u-1", "e-1"); !errors.Is(err, ErrCheckoutTokenRefused) {
			t.Fatalf("token %q: err = %v, want ErrCheckoutTokenRefused", tok, err)
		}
	}
}

// An ended checkout leaves its session revoked, whichever way it ended.
func TestAnEndedCheckoutLeavesTheSessionRevoked(t *testing.T) {
	ctx := context.Background()
	ends := map[string]func(WaitroomService, *models.Session) error{
		"completed": func(w WaitroomService, s *models.Session) error {
			return w.HandleCheckoutCompleted(ctx, CheckoutCompletedInput{SessionID: s.ID, UserID: s.UserID, EventID: s.EventID, Timestamp: time.Now()})
		},
		"failed": func(w WaitroomService, s *models.Session) error {
			return w.HandleCheckoutFailed(ctx, CheckoutFailedInput{SessionID: s.ID, UserID: s.UserID, EventID: s.EventID, Timestamp: time.Now()})
		},
		"expired": func(w WaitroomService, s *models.Session) error {
			return w.HandleCheckoutExpired(ctx, CheckoutExpiredInput{SessionID: s.ID, UserID: s.UserID, EventID: s.EventID, Timestamp: time.Now()})
		},
	}
	for name, end := range ends {
		t.Run(name, func(t *testing.T) {
			ss := newTokenRig(t, 5*time.Minute)
			l := logger.InitializeZapLogger(logger.ZapConfig{Level: "error", Mode: "development", Encoding: "console"})
			cli := redis.NewClient(config.RedisConfig{Addr: os.Getenv("WAITROOM_TEST_REDIS_ADDR"), PoolSize: 5, MinIdleConns: 1})
			w := &waitroomService{ssSvc: ss, qSvc: NewQueueService(repo.NewRedisQueueRepository(cli, l), l), l: l}
			s, _ := admitWithToken(t, ss, "u-1", "e-"+name)

			if err := end(w, s); err != nil {
				t.Fatalf("end checkout: %v", err)
			}
			got, err := ss.GetSession(ctx, s.ID)
			if err != nil {
				t.Fatalf("GetSession: %v", err)
			}
			if !got.TokenInvalidated || got.TokenInvalidationReason != name {
				t.Fatalf("stored session: invalidated=%v reason=%q, want true %q", got.TokenInvalidated, got.TokenInvalidationReason, name)
			}
		})
	}
}
