package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

type Config struct {
	Env      string
	Server   ServerConfig
	Log      LogConfig
	Postgres PostgresConfig
}

type ServerConfig struct {
	GRpcPort     int
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
	IdleTimeout  time.Duration
}

type PostgresConfig struct {
	URL             string
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
	ConnMaxIdleTime time.Duration
}

type LogConfig struct {
	Level    string
	Mode     string
	Encoding string
}

func Load() (*Config, error) {
	_ = godotenv.Load()

	cfg := &Config{
		Env: getEnv("ENV", "development"),
		Server: ServerConfig{
			GRpcPort:     getEnvAsInt("SERVER_GRPC_PORT", 50057),
			ReadTimeout:  getEnvAsDuration("SERVER_READ_TIMEOUT", 30*time.Second),
			WriteTimeout: getEnvAsDuration("SERVER_WRITE_TIMEOUT", 30*time.Second),
			IdleTimeout:  getEnvAsDuration("SERVER_IDLE_TIMEOUT", 60*time.Second),
		},
		Log: LogConfig{
			Level:    getEnv("LOG_LEVEL", "info"),
			Mode:     getEnv("LOG_MODE", "development"),
			Encoding: getEnv("LOG_ENCODING", "console"),
		},
		Postgres: PostgresConfig{
			URL:             getEnv("POSTGRES_URL", "postgresql://root:root@localhost:5435/ticketbottle?sslmode=disable"),
			MaxOpenConns:    getEnvAsInt("POSTGRES_MAX_OPEN_CONNS", 25),
			MaxIdleConns:    getEnvAsInt("POSTGRES_MAX_IDLE_CONNS", 10),
			ConnMaxLifetime: getEnvAsDuration("POSTGRES_CONN_MAX_LIFETIME", 5*time.Minute),
			ConnMaxIdleTime: getEnvAsDuration("POSTGRES_CONN_MAX_IDLE_TIME", 10*time.Minute),
		},
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("config validation failed: %w", err)
	}

	return cfg, nil
}

func (c *Config) Validate() error {
	if c.Server.GRpcPort <= 0 || c.Server.GRpcPort > 65535 {
		return fmt.Errorf("invalid server port: %d", c.Server.GRpcPort)
	}

	return nil
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvAsInt(key string, defaultValue int) int {
	valueStr := getEnv(key, "")
	if valueStr == "" {
		return defaultValue
	}

	value, err := strconv.Atoi(valueStr)
	if err != nil {
		return defaultValue
	}

	return value
}

func getEnvAsDuration(key string, defaultValue time.Duration) time.Duration {
	valueStr := getEnv(key, "")
	if valueStr == "" {
		return defaultValue
	}

	value, err := time.ParseDuration(valueStr)
	if err != nil {
		return defaultValue
	}

	return value
}

func getEnvAsSlice(key string, defaultValue []string) []string {
	valueStr := getEnv(key, "")
	if valueStr == "" {
		return defaultValue
	}

	// Split by comma
	var result []string
	for _, v := range strings.Split(valueStr, ",") {
		if trimmed := strings.TrimSpace(v); trimmed != "" {
			result = append(result, trimmed)
		}
	}

	if len(result) == 0 {
		return defaultValue
	}

	return result
}

func getEnvAsBool(key string, defaultValue bool) bool {
	valueStr := getEnv(key, "")
	if valueStr == "" {
		return defaultValue
	}

	value, err := strconv.ParseBool(valueStr)
	if err != nil {
		return defaultValue
	}

	return value
}
