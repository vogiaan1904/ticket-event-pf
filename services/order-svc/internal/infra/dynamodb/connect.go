package dynamodb

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/vogiaan1904/ticketbottle-order/config"
	pkgDynamo "github.com/vogiaan1904/ticketbottle-order/pkg/dynamodb"
)

const (
	connectTimeout = 10 * time.Second
)

// Connect creates a new DynamoDB client
func Connect(cfg config.DynamoDBConfig) (*pkgDynamo.Client, error) {
	ctx, cancelFunc := context.WithTimeout(context.Background(), connectTimeout)
	defer cancelFunc()

	client, err := pkgDynamo.NewClient(ctx, pkgDynamo.Config{
		TableName: cfg.TableName,
		Region:    cfg.Region,
		Endpoint:  cfg.Endpoint,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to connect to DynamoDB: %w", err)
	}

	log.Println("Connected to DynamoDB!")

	return client, nil
}

// Disconnect closes the DynamoDB client
func Disconnect(client *pkgDynamo.Client) {
	if client == nil {
		return
	}

	if err := client.Close(); err != nil {
		log.Printf("Error closing DynamoDB client: %v", err)
	}

	log.Println("Connection to DynamoDB closed.")
}
