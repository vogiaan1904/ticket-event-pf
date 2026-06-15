package repository

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/expression"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/vogiaan1904/ticketbottle-order/internal/models"
	pkgDynamo "github.com/vogiaan1904/ticketbottle-order/pkg/dynamodb"
)

func (r *implRepository) CreateManyItems(ctx context.Context, orderCode string, opts []CreateOrderItemOption) ([]models.OrderItem, error) {
	if len(opts) == 0 {
		return []models.OrderItem{}, nil
	}

	items := make([]models.OrderItem, 0, len(opts))
	writeRequests := make([]types.WriteRequest, 0, len(opts))

	for _, opt := range opts {
		item := r.buildOrderItemModel(orderCode, opt)
		items = append(items, item)

		av, err := attributevalue.MarshalMap(item)
		if err != nil {
			r.l.Errorf(ctx, "order.repository.CreateManyItems.MarshalMap: %v", err)
			return nil, err
		}

		writeRequests = append(writeRequests, types.WriteRequest{
			PutRequest: &types.PutRequest{
				Item: av,
			},
		})
	}

	// DynamoDB BatchWriteItem has a limit of 25 items per request
	const batchSize = 25
	for i := 0; i < len(writeRequests); i += batchSize {
		end := i + batchSize
		if end > len(writeRequests) {
			end = len(writeRequests)
		}

		batch := writeRequests[i:end]
		_, err := r.db.BatchWriteItem(ctx, &dynamodb.BatchWriteItemInput{
			RequestItems: map[string][]types.WriteRequest{
				r.tableName: batch,
			},
		})
		if err != nil {
			r.l.Errorf(ctx, "order.repository.CreateManyItems.BatchWriteItem: %v", err)
			return nil, err
		}
	}

	return items, nil
}

func (r *implRepository) ListItemByOrderCode(ctx context.Context, orderCode string) ([]models.OrderItem, error) {
	pk := pkgDynamo.BuildOrderPK(orderCode)

	// Build key condition: PK = ORDER#<code> AND SK begins_with ITEM#
	keyCondition := expression.Key("PK").Equal(expression.Value(pk)).
		And(expression.Key("SK").BeginsWith(pkgDynamo.ItemPrefix))

	// Filter for entity type and soft delete
	filterCondition := expression.Name("entity_type").Equal(expression.Value(string(pkgDynamo.EntityTypeOrderItem))).
		And(expression.AttributeNotExists(expression.Name("deleted_at")))

	expr, err := expression.NewBuilder().
		WithKeyCondition(keyCondition).
		WithFilter(filterCondition).
		Build()
	if err != nil {
		r.l.Errorf(ctx, "order.repository.ListItemByOrderCode.BuildExpression: %v", err)
		return nil, err
	}

	result, err := r.db.Query(ctx, &dynamodb.QueryInput{
		TableName:                 aws.String(r.tableName),
		KeyConditionExpression:    expr.KeyCondition(),
		FilterExpression:          expr.Filter(),
		ExpressionAttributeNames:  expr.Names(),
		ExpressionAttributeValues: expr.Values(),
	})
	if err != nil {
		r.l.Errorf(ctx, "order.repository.ListItemByOrderCode.Query: %v", err)
		return nil, err
	}

	var items []models.OrderItem
	if err := attributevalue.UnmarshalListOfMaps(result.Items, &items); err != nil {
		r.l.Errorf(ctx, "order.repository.ListItemByOrderCode.UnmarshalListOfMaps: %v", err)
		return nil, err
	}

	return items, nil
}

func (r *implRepository) DeleteItemByOrderCode(ctx context.Context, orderCode string) error {
	// First, get all items for this order
	items, err := r.ListItemByOrderCode(ctx, orderCode)
	if err != nil {
		return err
	}

	if len(items) == 0 {
		return nil
	}

	now := r.clock()

	// Soft delete each item
	for _, item := range items {
		updateBuilder := expression.Set(
			expression.Name("deleted_at"),
			expression.Value(now),
		).Set(
			expression.Name("updated_at"),
			expression.Value(now),
		)

		expr, err := expression.NewBuilder().WithUpdate(updateBuilder).Build()
		if err != nil {
			r.l.Errorf(ctx, "order.repository.DeleteItemByOrderCode.BuildExpression: %v", err)
			return err
		}

		_, err = r.db.UpdateItem(ctx, &dynamodb.UpdateItemInput{
			TableName: aws.String(r.tableName),
			Key: map[string]types.AttributeValue{
				"PK": &types.AttributeValueMemberS{Value: item.PK},
				"SK": &types.AttributeValueMemberS{Value: item.SK},
			},
			UpdateExpression:          expr.Update(),
			ExpressionAttributeNames:  expr.Names(),
			ExpressionAttributeValues: expr.Values(),
		})
		if err != nil {
			r.l.Errorf(ctx, "order.repository.DeleteItemByOrderCode.UpdateItem: %v", err)
			return err
		}
	}

	return nil
}
