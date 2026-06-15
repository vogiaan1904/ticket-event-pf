package jwt

import (
	"context"
	"fmt"

	"github.com/golang-jwt/jwt"
	"github.com/vogiaan1904/ticketbottle-order/internal/models"
	"github.com/vogiaan1904/ticketbottle-order/pkg/logger"
)

type Manager interface {
	Verify(ctx context.Context, token string) (Payload, error)
}

type Payload struct {
	jwt.StandardClaims
	models.CheckoutTokenClaim
}

type implManager struct {
	secretKey string
	logger    logger.Logger
}

func NewManager(secretKey string, logger logger.Logger) Manager {
	return &implManager{
		secretKey: secretKey,
		logger:    logger,
	}
}

// Verify verifies the token and returns the payload
func (m implManager) Verify(ctx context.Context, token string) (Payload, error) {
	if token == "" {
		m.logger.Errorf(ctx, "pkg.jwt.Verify: token is empty")
		return Payload{}, ErrInvalidToken
	}

	keyFunc := func(token *jwt.Token) (any, error) {
		_, ok := token.Method.(*jwt.SigningMethodHMAC)
		if !ok {
			m.logger.Errorf(ctx, "pkg.jwt.Verify: invalid signing method: %v", token.Method)
			return nil, fmt.Errorf("invalid signing method: %v", token.Method)
		}
		return []byte(m.secretKey), nil
	}

	jwtToken, err := jwt.ParseWithClaims(token, &Payload{}, keyFunc)
	if err != nil {
		m.logger.Errorf(ctx, "pkg.jwt.Verify: failed to parse token: %v", err)
		return Payload{}, fmt.Errorf("failed to parse token: %w", err)
	}

	payload, ok := jwtToken.Claims.(*Payload)
	if !ok {
		m.logger.Errorf(ctx, "pkg.jwt.Verify: failed to parse claims to Payload")
		return Payload{}, fmt.Errorf("failed to parse claims")
	}

	return *payload, nil
}
