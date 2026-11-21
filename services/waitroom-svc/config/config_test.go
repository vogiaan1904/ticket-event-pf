package config

import (
	"strings"
	"testing"
)

// A known signing key mints a checkout for any user, so there is no default.
func TestValidateRefusesAnEmptyJWTSecret(t *testing.T) {
	for _, env := range []string{"development", "production"} {
		c := &Config{Env: env, Server: ServerConfig{GRpcPort: 50056}, Redis: RedisConfig{Addr: "localhost:6379"}}
		if err := c.Validate(); err == nil || !strings.Contains(err.Error(), "JWT_SECRET") {
			t.Fatalf("env %s: err = %v, want a JWT_SECRET refusal", env, err)
		}
	}
}
