package activities

import (
	"testing"

	repo "github.com/vogiaan1904/ticketbottle-order/internal/order/repository"
	"github.com/vogiaan1904/ticketbottle-order/internal/testutil/dynamotest"
	"github.com/vogiaan1904/ticketbottle-order/pkg/logger"
)

// These run against a real table on DynamoDB local:
//
//	docker compose -f services/order-svc/docker-compose.dev.yml up -d
func newTestOrderActivities(t *testing.T) *OrderActivities {
	t.Helper()

	db := dynamotest.NewClient(t)
	return NewOrderActivities(repo.New(logger.InitializeTestZapLogger(), db, dynamotest.TableName))
}
