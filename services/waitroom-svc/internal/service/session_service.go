package service

import (
	"context"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"github.com/vogiaan1904/ticketbottle-waitroom/config"
	"github.com/vogiaan1904/ticketbottle-waitroom/internal/models"
	repo "github.com/vogiaan1904/ticketbottle-waitroom/internal/repository/redis"
	"github.com/vogiaan1904/ticketbottle-waitroom/pkg/logger"
	"github.com/vogiaan1904/ticketbottle-waitroom/pkg/redis"
)

type SessionService interface {
	CreateSession(ctx context.Context, uID, eID string, userAgent, ipAddr string, saleStartAt time.Time) (*models.Session, error)
	UpdateSession(ctx context.Context, ss *models.Session) error
	UpdateSessionStatus(ctx context.Context, sessionID string, status models.SessionStatus) error
	GetSession(ctx context.Context, ssID string) (*models.Session, error)
	GenerateCheckoutToken(ctx context.Context, ss *models.Session) (string, error)
	ActiveSession(ctx context.Context, ssID, userID string) (*models.Session, error)
	UpdateCheckoutToken(ctx context.Context, sessionID string, token string, expAt time.Time) error
	InvalidateCheckoutToken(ctx context.Context, sessionID string, reason string) error
	ValidateCheckoutToken(ctx context.Context, token, userID, eventID string) (string, error)
}

type sessionService struct {
	repo repo.SessionRepository
	conf config.JWTConfig
	l    logger.Logger
}

func NewSessionService(
	repo repo.SessionRepository,
	conf config.JWTConfig,
	l logger.Logger,
) SessionService {
	return &sessionService{
		repo: repo,
		conf: conf,
		l:    l,
	}
}

func (s *sessionService) CreateSession(ctx context.Context, uID, eID string, userAgent, ipAddr string, saleStartAt time.Time) (*models.Session, error) {
	now := time.Now()
	ssID := uuid.New().String()

	ss := &models.Session{
		ID:              ssID,
		UserID:          uID,
		EventID:         eID,
		Position:        0,
		QueuedAt:        now,
		Status:          models.SessionStatusQueued,
		ExpiresAt:       now.Add(2 * time.Hour),
		UserAgent:       userAgent,
		IPAddress:       ipAddr,
		LastHeartbeatAt: now,
		AttemptCount:    1,
		CreatedAt:       now,
		UpdatedAt:       now,
		// Drawn here so it is written once, with the session it belongs to.
		QueueScore: models.DrawQueueScore(now, saleStartAt),
	}

	if err := s.repo.Create(ctx, ss); err != nil {
		s.l.Errorf(ctx, "Failed to save session to Redis: %v", err)
		return nil, err
	}

	return ss, nil
}

func (s *sessionService) UpdateSession(ctx context.Context, ss *models.Session) error {
	return s.repo.Update(ctx, ss.ID, ss)
}

func (s *sessionService) UpdateSessionStatus(ctx context.Context, sessionID string, status models.SessionStatus) error {
	if err := s.repo.UpdateStatus(ctx, sessionID, status); err != nil {
		if err == redis.Nil {
			s.l.Warnf(ctx, "sessionService.UpdateSessionStatus: %v", ErrSessionNotFound)
			return ErrSessionNotFound
		}
		s.l.Errorf(ctx, "sessionService.UpdateSessionStatus: %v", err)
		return err
	}
	return nil
}

func (s *sessionService) GetSession(ctx context.Context, ssID string) (*models.Session, error) {
	ss, err := s.repo.Get(ctx, ssID)
	if err != nil {
		if err == redis.Nil {
			s.l.Warnf(ctx, "sessionService.GetSession: %v", ErrSessionNotFound)
			return nil, ErrSessionNotFound
		}
		s.l.Errorf(ctx, "sessionService.GetSession: %v", err)
		return nil, err
	}

	return ss, nil
}

func (s *sessionService) GenerateCheckoutToken(ctx context.Context, ss *models.Session) (string, error) {
	expAt := time.Now().Add(s.conf.Expiry)

	claims := jwt.MapClaims{
		"session_id": ss.ID,
		"user_id":    ss.UserID,
		"event_id":   ss.EventID,
		"exp":        expAt.Unix(),
		"iat":        time.Now().Unix(),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenStr, err := token.SignedString([]byte(s.conf.Secret))
	if err != nil {
		return "", fmt.Errorf("failed to sign token: %w", err)
	}

	return tokenStr, nil
}

// ActiveSession returns the caller's session only if it is usable.
// It returns the value rather than validating in isolation: every caller needs
// the session too, and a separate Validate call read it a second time.
func (s *sessionService) ActiveSession(ctx context.Context, ssID, userID string) (*models.Session, error) {
	// Via GetSession, not repo.Get: the repo answers a missing key with redis.Nil,
	// which the delivery layer has no sentinel for and renders 500, not 404.
	ss, err := s.GetSession(ctx, ssID)
	if err != nil {
		return nil, err
	}

	// Before any state check: a stranger must not learn the session exists.
	if ss.UserID != userID {
		return nil, ErrSessionNotFound
	}

	if ss.IsExpired() {
		return nil, ErrSessionExpired
	}

	// Ended by its line closing: the buyer needs "sold out", not "invalid status".
	if ss.Status == models.SessionStatusSoldOut {
		return nil, ErrSoldOut
	}

	if !ss.IsActive() {
		return nil, ErrInvalidSessionStatus
	}

	return ss, nil
}

func (s *sessionService) UpdateCheckoutToken(ctx context.Context, sessionID string, token string, expAt time.Time) error {
	if err := s.repo.UpdateCheckoutToken(ctx, sessionID, token, expAt); err != nil {
		return fmt.Errorf("failed to update checkout token: %w", err)
	}
	return nil
}

func (s *sessionService) InvalidateCheckoutToken(ctx context.Context, sessionID string, reason string) error {
	if err := s.repo.InvalidateToken(ctx, sessionID, reason); err != nil {
		s.l.Errorf(ctx, "Failed to invalidate token - session_id: %s, error: %v", sessionID, err)
		return fmt.Errorf("failed to invalidate checkout token: %w", err)
	}

	return nil
}

// ValidateCheckoutToken returns the session a checkout token admits.
// Every refusal wraps ErrCheckoutTokenRefused; any other error is Redis failing.
func (s *sessionService) ValidateCheckoutToken(ctx context.Context, token, userID, eventID string) (string, error) {
	refuse := func(cause error) (string, error) {
		return "", fmt.Errorf("%w: %w", ErrCheckoutTokenRefused, cause)
	}
	if token == "" {
		return refuse(ErrTokenEmpty)
	}

	claims := jwt.MapClaims{}
	_, err := jwt.ParseWithClaims(token, claims, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, ErrTokenUnexpectedSignature
		}
		return []byte(s.conf.Secret), nil
	})
	if err != nil {
		return refuse(fmt.Errorf("%w: %v", ErrTokenInvalid, err))
	}

	sessionID, _ := claims["session_id"].(string)
	if sessionID == "" || claims["user_id"] != userID || claims["event_id"] != eventID {
		return refuse(ErrTokenInvalidClaims)
	}

	blacklisted, err := s.repo.IsTokenInvalidated(ctx, token)
	if err != nil {
		return "", fmt.Errorf("failed to check token blacklist: %w", err)
	}
	if blacklisted {
		return refuse(ErrTokenInvalidated)
	}

	ss, err := s.repo.Get(ctx, sessionID)
	if err == redis.Nil {
		return refuse(ErrSessionNotFound)
	}
	if err != nil {
		return "", fmt.Errorf("failed to get session: %w", err)
	}

	// Invariant: only an admitted session's token buys; every ended status refuses.
	switch {
	case ss.Status != models.SessionStatusAdmitted:
		return refuse(ErrSessionNotAdmitted)
	case ss.TokenInvalidated:
		return refuse(ErrTokenInvalidated)
	case ss.CheckoutExpiresAt != nil && time.Now().After(*ss.CheckoutExpiresAt):
		return refuse(ErrTokenExpired)
	}
	return sessionID, nil
}
