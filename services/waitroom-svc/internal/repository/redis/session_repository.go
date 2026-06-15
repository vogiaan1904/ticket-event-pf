package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/vogiaan1904/ticketbottle-waitroom/internal/models"
	"github.com/vogiaan1904/ticketbottle-waitroom/pkg/logger"
	"github.com/vogiaan1904/ticketbottle-waitroom/pkg/redis"
)

type SessionRepository interface {
	Create(ctx context.Context, ss *models.Session) error
	Get(ctx context.Context, ssID string) (*models.Session, error)
	Update(ctx context.Context, ssID string, ss *models.Session) error
	UpdateStatus(ctx context.Context, ssID string, status models.SessionStatus) error
	UpdatePosition(ctx context.Context, ssID string, pos int64) error
	UpdateCheckoutToken(ctx context.Context, ssID string, token string, expAt time.Time) error
	Delete(ctx context.Context, ssID string) error
	GetByUserAndEvent(ctx context.Context, uID, eID string) (*models.Session, error)
	Exists(ctx context.Context, ssID string) (bool, error)

	InvalidateToken(ctx context.Context, ssID string, reason string) error
	IsTokenInvalidated(ctx context.Context, token string) (bool, error)
}

type redisSessionRepository struct {
	cli *redis.Client
	l   logger.Logger
}

func NewRedisSessionRepository(cli *redis.Client, l logger.Logger) SessionRepository {
	return &redisSessionRepository{
		cli: cli,
		l:   l,
	}
}

func (r *redisSessionRepository) Create(ctx context.Context, ss *models.Session) error {
	key := r.sessionKey(ss.ID)

	data, err := json.Marshal(ss)
	if err != nil {
		return fmt.Errorf("failed to marshal session: %w", err)
	}

	// Use pipeline for atomic operation
	pipe := r.cli.GetClient().Pipeline()
	pipe.Set(ctx, key, data, 2*time.Hour)

	ueKey := r.userEventKey(ss.UserID, ss.EventID)
	pipe.Set(ctx, ueKey, ss.ID, 2*time.Hour)

	if _, err := pipe.Exec(ctx); err != nil {
		r.l.Errorf(ctx, "redisSessionRepository.Create: %v", err)
		return err
	}

	return nil
}

func (r *redisSessionRepository) Get(ctx context.Context, ssID string) (*models.Session, error) {
	key := r.sessionKey(ssID)

	data, err := r.cli.Get(ctx, key)
	if err != nil {
		r.l.Errorf(ctx, "redisSessionRepository.Get: %v", err)
		return nil, err
	}

	var ss models.Session
	if err := json.Unmarshal(data, &ss); err != nil {
		r.l.Errorf(ctx, "redisSessionRepository.Get: %v", err)
		return nil, err
	}

	return &ss, nil
}

func (r *redisSessionRepository) Update(ctx context.Context, ssID string, ss *models.Session) error {
	key := r.sessionKey(ssID)

	ss.UpdatedAt = time.Now()

	data, err := json.Marshal(ss)
	if err != nil {
		r.l.Errorf(ctx, "redisSessionRepository.Update: %v", err)
		return err
	}

	ttl, err := r.cli.TTL(ctx, key)
	if err != nil {
		r.l.Errorf(ctx, "redisSessionRepository.Update: %v", err)
		return err
	}

	if ttl <= 0 {
		ttl = 2 * time.Hour
	}

	if err := r.cli.Set(ctx, key, data, ttl); err != nil {
		r.l.Errorf(ctx, "redisSessionRepository.Update: %v", err)
		return err
	}

	return nil
}

func (r *redisSessionRepository) UpdateStatus(ctx context.Context, ssID string, status models.SessionStatus) error {
	ss, err := r.Get(ctx, ssID)
	if err != nil {
		return err
	}

	ss.Status = status
	ss.UpdatedAt = time.Now()

	return r.Update(ctx, ssID, ss)
}

func (r *redisSessionRepository) UpdatePosition(ctx context.Context, ssID string, pos int64) error {
	ss, err := r.Get(ctx, ssID)
	if err != nil {
		r.l.Errorf(ctx, "redisSessionRepository.UpdatePosition.Get: %v", err)
		return err
	}

	ss.Position = pos
	ss.UpdatedAt = time.Now()

	return r.Update(ctx, ssID, ss)
}

func (r *redisSessionRepository) UpdateCheckoutToken(ctx context.Context, ssID string, token string, expAt time.Time) error {
	ss, err := r.Get(ctx, ssID)
	if err != nil {
		return err
	}

	now := time.Now()
	ss.CheckoutToken = token
	ss.CheckoutExpiresAt = &expAt
	ss.AdmittedAt = &now
	ss.Status = models.SessionStatusAdmitted
	ss.UpdatedAt = now

	return r.Update(ctx, ssID, ss)
}

func (r *redisSessionRepository) Delete(ctx context.Context, ssID string) error {
	ss, err := r.Get(ctx, ssID)
	if err != nil {
		if err == redis.Nil {
			return nil // Already deleted
		}

		r.l.Errorf(ctx, "redisSessionRepository.Delete.Get: %v", err)
		return err
	}

	pipe := r.cli.GetClient().Pipeline()

	// Delete session
	pipe.Del(ctx, r.sessionKey(ssID))

	// Delete user->session index
	pipe.Del(ctx, r.userEventKey(ss.UserID, ss.EventID))

	if _, err := pipe.Exec(ctx); err != nil {
		r.l.Errorf(ctx, "redisSessionRepository.Delete: %v", err)
		return err
	}

	return nil
}

func (r *redisSessionRepository) GetByUserAndEvent(ctx context.Context, uID, eID string) (*models.Session, error) {
	ueKey := r.userEventKey(uID, eID)

	ssID, err := r.cli.GetString(ctx, ueKey)
	if err != nil {
		r.l.Errorf(ctx, "redisSessionRepository.GetByUserAndEvent: %v", err)
		return nil, err
	}

	return r.Get(ctx, ssID)
}

func (r *redisSessionRepository) Exists(ctx context.Context, ssID string) (bool, error) {
	key := r.sessionKey(ssID)
	exists, err := r.cli.Exists(ctx, key)
	if err != nil {
		r.l.Errorf(ctx, "redisSessionRepository.Exists: %v", err)
		return false, err
	}

	return exists > 0, nil
}

func (r *redisSessionRepository) sessionKey(ssID string) string {
	return fmt.Sprintf("waitroom:session:%s", ssID)
}

func (r *redisSessionRepository) userEventKey(uID, eID string) string {
	return fmt.Sprintf("waitroom:user_session:%s:%s", uID, eID)
}

func (r *redisSessionRepository) tokenBlacklistKey(tokenHash string) string {
	return fmt.Sprintf("waitroom:token:blacklist:%s", tokenHash)
}

func (r *redisSessionRepository) hashToken(token string) string {
	hash := sha256.Sum256([]byte(token))
	return hex.EncodeToString(hash[:])
}

func (r *redisSessionRepository) InvalidateToken(ctx context.Context, ssID string, reason string) error {
	ss, err := r.Get(ctx, ssID)
	if err != nil {
		r.l.Errorf(ctx, "redisSessionRepository.InvalidateToken.Get: %v", err)
		return err
	}

	if ss.CheckoutToken == "" {
		r.l.Debugf(ctx, "No token to invalidate", "session_id", ssID)
		return nil
	}

	tokenHash := r.hashToken(ss.CheckoutToken)
	blacklistKey := r.tokenBlacklistKey(tokenHash)

	ttl := 15 * time.Minute
	if ss.CheckoutExpiresAt != nil {
		remaining := time.Until(*ss.CheckoutExpiresAt)
		if remaining > 0 {
			ttl = remaining
		}
	}

	pipe := r.cli.GetClient().Pipeline()

	invalidationData := map[string]interface{}{
		"session_id":     ssID,
		"reason":         reason,
		"invalidated_at": time.Now().Unix(),
	}
	data, _ := json.Marshal(invalidationData)
	pipe.Set(ctx, blacklistKey, data, ttl)

	now := time.Now()
	ss.TokenInvalidated = true
	ss.TokenInvalidatedAt = &now
	ss.TokenInvalidationReason = reason
	ss.UpdatedAt = now

	sessionData, err := json.Marshal(ss)
	if err != nil {
		r.l.Errorf(ctx, "redisSessionRepository.InvalidateToken.Marshal: %v", err)
		return err
	}

	sessionTTL, _ := r.cli.TTL(ctx, r.sessionKey(ssID))
	if sessionTTL <= 0 {
		sessionTTL = 2 * time.Hour
	}

	pipe.Set(ctx, r.sessionKey(ssID), sessionData, sessionTTL)

	if _, err := pipe.Exec(ctx); err != nil {
		r.l.Errorf(ctx, "redisSessionRepository.InvalidateToken.Exec: %v", err)
		return err
	}

	return nil
}

func (r *redisSessionRepository) IsTokenInvalidated(ctx context.Context, token string) (bool, error) {
	if token == "" {
		return false, fmt.Errorf("token cannot be empty")
	}

	tokenHash := r.hashToken(token)
	blacklistKey := r.tokenBlacklistKey(tokenHash)

	exists, err := r.cli.Exists(ctx, blacklistKey)
	if err != nil {
		r.l.Errorf(ctx, "redisSessionRepository.IsTokenInvalidated: %v", err)
		return false, err
	}

	return exists > 0, nil
}
