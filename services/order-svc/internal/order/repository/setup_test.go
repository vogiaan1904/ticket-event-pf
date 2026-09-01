package repository

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	pkgDynamo "github.com/vogiaan1904/ticketbottle-order/pkg/dynamodb"
	"github.com/vogiaan1904/ticketbottle-order/pkg/logger"
)

// These run against a real table on DynamoDB local:
//
//	docker compose -f services/order-svc/docker-compose.dev.yml up -d
const (
	defaultTestDynamoEndpoint = "http://localhost:8000"
	testTableName             = "ticketbottle-orders"
)

func newTestRepo(t *testing.T) *implRepository {
	t.Helper()

	endpoint := os.Getenv("TEST_DYNAMO_ENDPOINT")
	if endpoint == "" {
		endpoint = defaultTestDynamoEndpoint
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client, err := pkgDynamo.NewClient(ctx, pkgDynamo.Config{
		TableName: testTableName,
		Region:    "us-east-1",
		Endpoint:  endpoint,
	})
	if err != nil {
		t.Fatalf("build dynamodb client: %v", err)
	}
	db := client.DB()

	if err := ensureOrdersTable(ctx, db); err != nil {
		// A missing table means DynamoDB local isn't reachable -- these tests
		// are the only thing standing between a retried create and a paid
		// order getting silently overwritten. Locally, skipping keeps
		// `go test ./...` usable; in CI a missing datastore must never let
		// the suite report success.
		if os.Getenv("CI") != "" {
			t.Fatalf("CI requires a reachable test dynamodb (%s): %v", endpoint, err)
		}
		t.Skipf("skipping: cannot reach test dynamodb (%s): %v", endpoint, err)
	}

	if err := clearTestTable(ctx, db); err != nil {
		t.Fatalf("clear test table: %v", err)
	}
	t.Cleanup(func() {
		_ = clearTestTable(context.Background(), db)
	})

	return &implRepository{
		l:         logger.InitializeTestZapLogger(),
		db:        db,
		tableName: testTableName,
		clock:     time.Now,
	}
}

// ensureOrdersTable creates the single-table schema the Helm chart's
// dynamodb-init job creates in every other environment, so a test run needs
// nothing beyond a running dynamodb-local. A DescribeTable error other than
// "table does not exist" means the endpoint itself is unreachable and is
// returned as-is for the caller to treat as a skip/fail signal.
func ensureOrdersTable(ctx context.Context, db *dynamodb.Client) error {
	_, err := db.DescribeTable(ctx, &dynamodb.DescribeTableInput{
		TableName: aws.String(testTableName),
	})
	if err == nil {
		return nil
	}

	var notFound *types.ResourceNotFoundException
	if !errors.As(err, &notFound) {
		return err
	}

	throughput := &types.ProvisionedThroughput{
		ReadCapacityUnits:  aws.Int64(5),
		WriteCapacityUnits: aws.Int64(5),
	}

	_, err = db.CreateTable(ctx, &dynamodb.CreateTableInput{
		TableName: aws.String(testTableName),
		AttributeDefinitions: []types.AttributeDefinition{
			{AttributeName: aws.String("PK"), AttributeType: types.ScalarAttributeTypeS},
			{AttributeName: aws.String("SK"), AttributeType: types.ScalarAttributeTypeS},
			{AttributeName: aws.String("GSI1PK"), AttributeType: types.ScalarAttributeTypeS},
			{AttributeName: aws.String("GSI1SK"), AttributeType: types.ScalarAttributeTypeS},
			{AttributeName: aws.String("GSI2PK"), AttributeType: types.ScalarAttributeTypeS},
			{AttributeName: aws.String("GSI2SK"), AttributeType: types.ScalarAttributeTypeS},
		},
		KeySchema: []types.KeySchemaElement{
			{AttributeName: aws.String("PK"), KeyType: types.KeyTypeHash},
			{AttributeName: aws.String("SK"), KeyType: types.KeyTypeRange},
		},
		GlobalSecondaryIndexes: []types.GlobalSecondaryIndex{
			{
				IndexName: aws.String(pkgDynamo.GSI1Name),
				KeySchema: []types.KeySchemaElement{
					{AttributeName: aws.String("GSI1PK"), KeyType: types.KeyTypeHash},
					{AttributeName: aws.String("GSI1SK"), KeyType: types.KeyTypeRange},
				},
				Projection:            &types.Projection{ProjectionType: types.ProjectionTypeAll},
				ProvisionedThroughput: throughput,
			},
			{
				IndexName: aws.String(pkgDynamo.GSI2Name),
				KeySchema: []types.KeySchemaElement{
					{AttributeName: aws.String("GSI2PK"), KeyType: types.KeyTypeHash},
					{AttributeName: aws.String("GSI2SK"), KeyType: types.KeyTypeRange},
				},
				Projection:            &types.Projection{ProjectionType: types.ProjectionTypeAll},
				ProvisionedThroughput: throughput,
			},
		},
		ProvisionedThroughput: throughput,
	})
	if err != nil {
		var inUse *types.ResourceInUseException
		if errors.As(err, &inUse) {
			return nil
		}
		return err
	}

	waiter := dynamodb.NewTableExistsWaiter(db)
	return waiter.Wait(ctx, &dynamodb.DescribeTableInput{TableName: aws.String(testTableName)}, 30*time.Second)
}

// clearTestTable wipes every item so one test's data can never leak into the
// next. DynamoDB has no TRUNCATE, so this scans and deletes item by item;
// the volumes in these tests are small enough that this stays cheap.
func clearTestTable(ctx context.Context, db *dynamodb.Client) error {
	result, err := db.Scan(ctx, &dynamodb.ScanInput{
		TableName:            aws.String(testTableName),
		ProjectionExpression: aws.String("PK, SK"),
	})
	if err != nil {
		return err
	}

	for _, item := range result.Items {
		if _, err := db.DeleteItem(ctx, &dynamodb.DeleteItemInput{
			TableName: aws.String(testTableName),
			Key:       item,
		}); err != nil {
			return err
		}
	}

	return nil
}
