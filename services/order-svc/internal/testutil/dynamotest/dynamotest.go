// Package dynamotest gives every package's integration tests the same real
// DynamoDB local, so a fix to the datastore contract (a new conditional
// write, a new index) can be exercised from the repository layer and from
// anything that calls it -- currently the Temporal activities -- without
// each package re-deriving how to reach and seed the table.
package dynamotest

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
)

// These run against a real table on DynamoDB local:
//
//	docker compose -f services/order-svc/docker-compose.dev.yml up -d
const (
	DefaultEndpoint = "http://localhost:8000"
	TableName       = "ticketbottle-orders"
)

// NewClient returns a DynamoDB client wired to TEST_DYNAMO_ENDPOINT (falling
// back to DefaultEndpoint) with TableName already created and empty. A
// missing datastore skips the test locally -- these tests are the only thing
// standing between a retried write and a paid order getting silently
// overwritten, but a developer without dynamodb-local running still needs
// `go test ./...` to work -- and hard-fails when CI is set, so a missing
// datastore can never let the suite report success in CI.
func NewClient(t *testing.T) *dynamodb.Client {
	t.Helper()

	endpoint := os.Getenv("TEST_DYNAMO_ENDPOINT")
	if endpoint == "" {
		endpoint = DefaultEndpoint
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client, err := pkgDynamo.NewClient(ctx, pkgDynamo.Config{
		TableName: TableName,
		Region:    "us-east-1",
		Endpoint:  endpoint,
	})
	if err != nil {
		t.Fatalf("build dynamodb client: %v", err)
	}
	db := client.DB()

	if err := ensureOrdersTable(ctx, db); err != nil {
		if os.Getenv("CI") != "" {
			t.Fatalf("CI requires a reachable test dynamodb (%s): %v", endpoint, err)
		}
		t.Skipf("skipping: cannot reach test dynamodb (%s): %v", endpoint, err)
	}

	if err := clearTable(ctx, db); err != nil {
		t.Fatalf("clear test table: %v", err)
	}
	t.Cleanup(func() {
		_ = clearTable(context.Background(), db)
	})

	return db
}

// ensureOrdersTable creates the single-table schema the Helm chart's
// dynamodb-init job creates in every other environment, so a test run needs
// nothing beyond a running dynamodb-local. A DescribeTable error other than
// "table does not exist" means the endpoint itself is unreachable and is
// returned as-is for the caller to treat as a skip/fail signal.
func ensureOrdersTable(ctx context.Context, db *dynamodb.Client) error {
	_, err := db.DescribeTable(ctx, &dynamodb.DescribeTableInput{
		TableName: aws.String(TableName),
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
		TableName: aws.String(TableName),
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
	return waiter.Wait(ctx, &dynamodb.DescribeTableInput{TableName: aws.String(TableName)}, 30*time.Second)
}

// clearTable wipes every item so one test's data can never leak into the
// next. DynamoDB has no TRUNCATE, so this scans and deletes item by item;
// the volumes in these tests are small enough that this stays cheap.
func clearTable(ctx context.Context, db *dynamodb.Client) error {
	result, err := db.Scan(ctx, &dynamodb.ScanInput{
		TableName:            aws.String(TableName),
		ProjectionExpression: aws.String("PK, SK"),
	})
	if err != nil {
		return err
	}

	for _, item := range result.Items {
		if _, err := db.DeleteItem(ctx, &dynamodb.DeleteItemInput{
			TableName: aws.String(TableName),
			Key:       item,
		}); err != nil {
			return err
		}
	}

	return nil
}
