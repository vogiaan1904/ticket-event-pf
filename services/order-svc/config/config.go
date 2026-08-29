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
	Env          string
	Server       ServerConfig
	Redis        RedisConfig
	DynamoDB     DynamoDBConfig
	JWT          JWTConfig
	Log          LogConfig
	Kafka        KafkaConfig
	Microservice MicroserviceConfig
	Temporal     TemporalConfig
}

type ServerConfig struct {
	GRpcPort     int
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
	IdleTimeout  time.Duration

	PaymentTimeoutSeconds int32

	// How long Create waits for the CreateOrder saga before giving up. Bounds a
	// gRPC handler; on expiry the workflow is cancelled so it cannot go on
	// reserving inventory for a caller that has left.
	CreateOrderTimeout time.Duration
}

type RedisConfig struct {
	Addr         string
	Password     string
	DB           int
	MaxRetries   int
	PoolSize     int
	MinIdleConns int
}

type DynamoDBConfig struct {
	TableName string
	Region    string
	Endpoint  string // For LocalStack or local development
}

type KafkaConfig struct {
	Brokers              []string
	ProducerRetryMax     int
	ProducerRequiredAcks int
	Enabled              bool
	ConsumerGroupID      string
}

type MicroserviceConfig struct {
	Event     string
	Inventory string
	Payment   string
}

type JWTConfig struct {
	Secret string
	Expiry time.Duration
}

type LogConfig struct {
	Level    string
	Mode     string
	Encoding string
}

type TemporalConfig struct {
	HostPort  string
	Namespace string
	TaskQueue string
}

func Load() (*Config, error) {
	// Load .env file if exists
	_ = godotenv.Load()

	cfg := &Config{
		Env: getEnv("ENV", "development"),
		Server: ServerConfig{
			GRpcPort:     getEnvAsInt("SERVER_GRPC_PORT", 50054),
			ReadTimeout:  getEnvAsDuration("SERVER_READ_TIMEOUT", 30*time.Second),
			WriteTimeout: getEnvAsDuration("SERVER_WRITE_TIMEOUT", 30*time.Second),
			IdleTimeout:  getEnvAsDuration("SERVER_IDLE_TIMEOUT", 60*time.Second),

			PaymentTimeoutSeconds: int32(getEnvAsInt("PAYMENT_TIMEOUT_SECONDS", 600)),
			CreateOrderTimeout:    getEnvAsDuration("ORDER_CREATE_TIMEOUT", 30*time.Second),
		},
		DynamoDB: DynamoDBConfig{
			TableName: getEnv("DYNAMODB_TABLE_NAME", "ticketbottle-orders"),
			Region:    getEnv("AWS_REGION", "us-east-1"),
			Endpoint:  getEnv("DYNAMODB_ENDPOINT", ""), // Empty for AWS, set for LocalStack
		},
		JWT: JWTConfig{
			Secret: getEnv("JWT_SECRET", "your-super-secret-key-change-in-production"),
			Expiry: getEnvAsDuration("JWT_EXPIRY", 15*time.Minute),
		},
		Log: LogConfig{
			Level:    getEnv("LOG_LEVEL", "info"),
			Mode:     getEnv("LOG_MODE", "development"),
			Encoding: getEnv("LOG_ENCODING", "console"),
		},
		Kafka: KafkaConfig{
			Brokers:              getEnvAsSlice("KAFKA_BROKERS", []string{"localhost:9092"}),
			ProducerRetryMax:     getEnvAsInt("KAFKA_PRODUCER_RETRY_MAX", 3),
			ProducerRequiredAcks: getEnvAsInt("KAFKA_PRODUCER_REQUIRED_ACKS", 1),
			Enabled:              getEnvAsBool("KAFKA_ENABLED", true),
			ConsumerGroupID:      getEnv("KAFKA_CONSUMER_GROUP_ID", "order-service"),
		},
		Microservice: MicroserviceConfig{
			Event:     getEnv("EVENT_SERVICE_ADDR", "localhost:50053"),
			Inventory: getEnv("INVENTORY_SERVICE_ADDR", "localhost:50057"),
			Payment:   getEnv("PAYMENT_SERVICE_ADDR", "localhost:50055"),
		},
		Temporal: TemporalConfig{
			HostPort:  getEnv("TEMPORAL_HOST_PORT", "localhost:7233"),
			Namespace: getEnv("TEMPORAL_NAMESPACE", "default"),
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

	if c.JWT.Secret == "" || c.JWT.Secret == "your-super-secret-key-change-in-production" {
		if c.Env == "production" {
			return fmt.Errorf("JWT secret must be set in production")
		}
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
