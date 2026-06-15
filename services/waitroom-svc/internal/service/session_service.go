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
	CreateSession(ctx context.Context, uID, eID string, userAgent, ipAddr string) (*models.Session, error)
	UpdateSession(ctx context.Context, ss *models.Session) error
	UpdateSessionStatus(ctx context.Context, sessionID string, status models.SessionStatus) error
	GetSession(ctx context.Context, ssID string) (*models.Session, error)
	GenerateCheckoutToken(ctx context.Context, ss *models.Session) (string, error)
	ValidateSession(ctx context.Context, ssID string) error
	UpdateCheckoutToken(ctx context.Context, sessionID string, token string, expAt time.Time) error
	InvalidateCheckoutToken(ctx context.Context, sessionID string, reason string) error
	ValidateCheckoutToken(ctx context.Context, token string) error
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

func (s *sessionService) CreateSession(ctx context.Context, uID, eID string, userAgent, ipAddr string) (*models.Session, error) {
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

func (s *sessionService) ValidateSession(ctx context.Context, ssID string) error {
	ss, err := s.repo.Get(ctx, ssID)
	if err != nil {
		return err
	}

	if ss.IsExpired() {
		return ErrSessionExpired
	}

	if !ss.IsActive() {
		return ErrInvalidSessionStatus
	}

	return nil
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

func (s *sessionService) ValidateCheckoutToken(ctx context.Context, token string) error {
	if token == "" {
		return ErrTokenEmpty
	}

	claims := jwt.MapClaims{}
	parsedToken, err := jwt.ParseWithClaims(token, claims, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, ErrTokenUnexpectedSignature
		}
		return []byte(s.conf.Secret), nil
	})

	if err != nil {
		s.l.Warnf(ctx, "Invalid JWT token: %v", err)
		return fmt.Errorf("%w: %v", ErrTokenInvalid, err)
	}

	if !parsedToken.Valid {
		return ErrTokenNotValid
	}

	isBlacklisted, err := s.repo.IsTokenInvalidated(ctx, token)
	if err != nil {
		s.l.Errorf(ctx, "Failed to check token blacklist: %v", err)
		return fmt.Errorf("failed to validate token: %w", err)
	}

	if isBlacklisted {
		s.l.Warnf(ctx, "Token is blacklisted")
		return ErrTokenInvalidated
	}

	sessionID, ok := claims["session_id"].(string)
	if !ok {
		return ErrTokenInvalidClaims
	}

	ss, err := s.repo.Get(ctx, sessionID)
	if err != nil {
		if err == redis.Nil {
			return ErrSessionNotFound
		}
		return fmt.Errorf("failed to get session: %w", err)
	}

	if ss.Status != models.SessionStatusAdmitted {
		s.l.Warnf(ctx, "Session status not admitted - session_id: %s, status: %s", sessionID, ss.Status)
		return ErrSessionNotAdmitted
	}

	if ss.TokenInvalidated {
		s.l.Warnf(ctx, "Token marked as invalidated in session - session_id: %s, reason: %s",
			sessionID, ss.TokenInvalidationReason)
		return ErrTokenInvalidated
	}

	if ss.CheckoutExpiresAt != nil && time.Now().After(*ss.CheckoutExpiresAt) {
		s.l.Warnf(ctx, "Checkout token expired - session_id: %s, expired_at: %v",
			sessionID, ss.CheckoutExpiresAt)
		return ErrTokenExpired
	}

	s.l.Debugf(ctx, "Checkout token validated successfully - session_id: %s, user_id: %s, event_id: %s",
		sessionID, ss.UserID, ss.EventID)

	return nil
}
