package dynamodb

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
)

// Config holds DynamoDB configuration
type Config struct {
	TableName string
	Region    string
	Endpoint  string // For LocalStack or local development
}

// Client wraps the DynamoDB client
type Client struct {
	db        *dynamodb.Client
	tableName string
}

// NewClient creates a new DynamoDB client
func NewClient(ctx context.Context, cfg Config) (*Client, error) {
	var opts []func(*config.LoadOptions) error

	opts = append(opts, config.WithRegion(cfg.Region))

	// For LocalStack or local development
	if cfg.Endpoint != "" {
		customResolver := aws.EndpointResolverWithOptionsFunc(
			func(service, region string, options ...interface{}) (aws.Endpoint, error) {
				return aws.Endpoint{
					URL:           cfg.Endpoint,
					SigningRegion: cfg.Region,
				}, nil
			},
		)
		opts = append(opts, config.WithEndpointResolverWithOptions(customResolver))
		opts = append(opts, config.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider("test", "test", ""),
		))
	}

	awsCfg, err := config.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return nil, err
	}

	client := dynamodb.NewFromConfig(awsCfg)

	return &Client{
		db:        client,
		tableName: cfg.TableName,
	}, nil
}

// DB returns the underlying DynamoDB client
func (c *Client) DB() *dynamodb.Client {
	return c.db
}

// TableName returns the configured table name
func (c *Client) TableName() string {
	return c.tableName
}

// Close closes the client (no-op for DynamoDB, but kept for interface consistency)
func (c *Client) Close() error {
	return nil
}
