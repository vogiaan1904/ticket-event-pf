package repository

import (
	"testing"
	"time"

	"github.com/vogiaan1904/ticketbottle-order/internal/testutil/dynamotest"
	"github.com/vogiaan1904/ticketbottle-order/pkg/logger"
)

// These run against a real table on DynamoDB local:
//
//	docker compose -f services/order-svc/docker-compose.dev.yml up -d
func newTestRepo(t *testing.T) *implRepository {
	t.Helper()

	return &implRepository{
		l:         logger.InitializeTestZapLogger(),
		db:        dynamotest.NewClient(t),
		tableName: dynamotest.TableName,
		clock:     time.Now,
	}
}
