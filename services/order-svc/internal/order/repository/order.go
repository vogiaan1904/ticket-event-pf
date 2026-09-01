package repository

import (
	"context"
	"errors"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/expression"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/vogiaan1904/ticketbottle-order/internal/models"
	"github.com/vogiaan1904/ticketbottle-order/internal/order"
	pkgDynamo "github.com/vogiaan1904/ticketbottle-order/pkg/dynamodb"
	"github.com/vogiaan1904/ticketbottle-order/pkg/paginator"
)

var ErrOrderNotFound = errors.New("order not found")

// isConditionalCheckFailed reports whether a DynamoDB write was refused by its
// own ConditionExpression -- the shared shape every conditional write in this
// package (order creation, purchase-slot claims) checks for.
func isConditionalCheckFailed(err error) bool {
	var cond *types.ConditionalCheckFailedException
	return errors.As(err, &cond)
}

func (r *implRepository) Create(ctx context.Context, opt CreateOrderOption) (models.Order, error) {
	o := r.buildOrderModel(opt)

	item, err := attributevalue.MarshalMap(o)
	if err != nil {
		r.l.Errorf(ctx, "order.repository.Create.MarshalMap: %v", err)
		return models.Order{}, err
	}

	// attribute_not_exists on the partition key makes the create a claim: the
	// first writer of a code wins and any replay is refused. Without it a
	// retried PutItem overwrites whatever is there, including a paid order.
	_, err = r.db.PutItem(ctx, &dynamodb.PutItemInput{
		TableName:           aws.String(r.tableName),
		Item:                item,
		ConditionExpression: aws.String("attribute_not_exists(PK)"),
	})
	if err != nil {
		if isConditionalCheckFailed(err) {
			r.l.Warnf(ctx, "order.repository.Create: code %s is already taken", opt.Code)
			return models.Order{}, order.ErrOrderAlreadyExists
		}
		r.l.Errorf(ctx, "order.repository.Create.PutItem: %v", err)
		return models.Order{}, err
	}

	return o, nil
}

func (r *implRepository) GetByCode(ctx context.Context, code string) (models.Order, error) {
	pk := pkgDynamo.BuildOrderPK(code)
	sk := pkgDynamo.BuildOrderSK(code)

	result, err := r.db.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(r.tableName),
		Key: map[string]types.AttributeValue{
			"PK": &types.AttributeValueMemberS{Value: pk},
			"SK": &types.AttributeValueMemberS{Value: sk},
		},
	})
	if err != nil {
		r.l.Errorf(ctx, "order.repository.GetByCode.GetItem: %v", err)
		return models.Order{}, err
	}

	if result.Item == nil {
		return models.Order{}, ErrOrderNotFound
	}

	var o models.Order
	if err := attributevalue.UnmarshalMap(result.Item, &o); err != nil {
		r.l.Errorf(ctx, "order.repository.GetByCode.UnmarshalMap: %v", err)
		return models.Order{}, err
	}

	// Check soft delete
	if o.DeletedAt != nil {
		return models.Order{}, ErrOrderNotFound
	}

	return o, nil
}

func (r *implRepository) GetOne(ctx context.Context, opt GetOneOrderOption) (models.Order, error) {
	// If code is provided, use direct lookup
	if opt.Code != "" {
		return r.GetByCode(ctx, opt.Code)
	}

	// Otherwise, use query with filters
	orders, err := r.List(ctx, ListOrderOption(opt))
	if err != nil {
		return models.Order{}, err
	}

	if len(orders) == 0 {
		return models.Order{}, ErrOrderNotFound
	}

	return orders[0], nil
}

func (r *implRepository) GetMany(ctx context.Context, opt GetManyOrderOption) ([]models.Order, paginator.Paginator, error) {
	opt.Pag.Adjust()

	var indexName *string
	var keyCondition expression.KeyConditionBuilder
	var filterCondition expression.ConditionBuilder
	hasFilter := false

	// Determine which index to use based on filters
	if opt.UserID != "" {
		indexName = aws.String(pkgDynamo.GSI1Name)
		keyCondition = expression.Key("GSI1PK").Equal(expression.Value(pkgDynamo.BuildUserGSI1PK(opt.UserID)))
	} else if opt.EventID != "" {
		indexName = aws.String(pkgDynamo.GSI2Name)
		keyCondition = expression.Key("GSI2PK").Equal(expression.Value(pkgDynamo.BuildEventGSI2PK(opt.EventID)))
	} else {
		// Scan is required when no specific index can be used
		return r.scanOrders(ctx, opt)
	}

	// Build filter expressions
	filterCondition = expression.Name("entity_type").Equal(expression.Value(string(pkgDynamo.EntityTypeOrder)))
	hasFilter = true

	// Add soft delete filter
	filterCondition = filterCondition.And(
		expression.AttributeNotExists(expression.Name("deleted_at")),
	)

	// Add status filter if provided
	if opt.Status != nil {
		filterCondition = filterCondition.And(
			expression.Name("status").Equal(expression.Value(string(*opt.Status))),
		)
	}

	// Add session ID filter if provided
	if opt.SessionID != "" {
		filterCondition = filterCondition.And(
			expression.Name("session_id").Equal(expression.Value(opt.SessionID)),
		)
	}

	// Build expression
	var expr expression.Expression
	var err error
	if hasFilter {
		expr, err = expression.NewBuilder().
			WithKeyCondition(keyCondition).
			WithFilter(filterCondition).
			Build()
	} else {
		expr, err = expression.NewBuilder().
			WithKeyCondition(keyCondition).
			Build()
	}
	if err != nil {
		r.l.Errorf(ctx, "order.repository.GetMany.BuildExpression: %v", err)
		return nil, paginator.Paginator{}, err
	}

	// Decode cursor if provided
	var exclusiveStartKey map[string]types.AttributeValue
	if opt.Pag.Cursor != "" {
		exclusiveStartKey, err = pkgDynamo.DecodeCursor(opt.Pag.Cursor)
		if err != nil {
			r.l.Errorf(ctx, "order.repository.GetMany.DecodeCursor: %v", err)
			return nil, paginator.Paginator{}, err
		}
	}

	// Execute query
	queryInput := &dynamodb.QueryInput{
		TableName:                 aws.String(r.tableName),
		IndexName:                 indexName,
		KeyConditionExpression:    expr.KeyCondition(),
		FilterExpression:          expr.Filter(),
		ExpressionAttributeNames:  expr.Names(),
		ExpressionAttributeValues: expr.Values(),
		Limit:                     aws.Int32(int32(opt.Pag.Limit)),
		ExclusiveStartKey:         exclusiveStartKey,
		ScanIndexForward:          aws.Bool(false), // Descending order (newest first)
	}

	result, err := r.db.Query(ctx, queryInput)
	if err != nil {
		r.l.Errorf(ctx, "order.repository.GetMany.Query: %v", err)
		return nil, paginator.Paginator{}, err
	}

	var orders []models.Order
	if err := attributevalue.UnmarshalListOfMaps(result.Items, &orders); err != nil {
		r.l.Errorf(ctx, "order.repository.GetMany.UnmarshalListOfMaps: %v", err)
		return nil, paginator.Paginator{}, err
	}

	// Encode next cursor
	nextCursor, err := pkgDynamo.EncodeCursor(result.LastEvaluatedKey)
	if err != nil {
		r.l.Errorf(ctx, "order.repository.GetMany.EncodeCursor: %v", err)
		return nil, paginator.Paginator{}, err
	}

	pag := paginator.Paginator{
		Count:      int64(len(orders)),
		PageSize:   opt.Pag.Limit,
		NextCursor: nextCursor,
		HasMore:    result.LastEvaluatedKey != nil,
	}

	return orders, pag, nil
}

func (r *implRepository) scanOrders(ctx context.Context, opt GetManyOrderOption) ([]models.Order, paginator.Paginator, error) {
	// Build filter expression
	filterCondition := expression.Name("entity_type").Equal(expression.Value(string(pkgDynamo.EntityTypeOrder)))
	filterCondition = filterCondition.And(
		expression.AttributeNotExists(expression.Name("deleted_at")),
	)

	if opt.Status != nil {
		filterCondition = filterCondition.And(
			expression.Name("status").Equal(expression.Value(string(*opt.Status))),
		)
	}

	if opt.SessionID != "" {
		filterCondition = filterCondition.And(
			expression.Name("session_id").Equal(expression.Value(opt.SessionID)),
		)
	}

	if opt.Code != "" {
		filterCondition = filterCondition.And(
			expression.Name("code").Equal(expression.Value(opt.Code)),
		)
	}

	expr, err := expression.NewBuilder().WithFilter(filterCondition).Build()
	if err != nil {
		r.l.Errorf(ctx, "order.repository.scanOrders.BuildExpression: %v", err)
		return nil, paginator.Paginator{}, err
	}

	// Decode cursor
	var exclusiveStartKey map[string]types.AttributeValue
	if opt.Pag.Cursor != "" {
		exclusiveStartKey, err = pkgDynamo.DecodeCursor(opt.Pag.Cursor)
		if err != nil {
			r.l.Errorf(ctx, "order.repository.scanOrders.DecodeCursor: %v", err)
			return nil, paginator.Paginator{}, err
		}
	}

	result, err := r.db.Scan(ctx, &dynamodb.ScanInput{
		TableName:                 aws.String(r.tableName),
		FilterExpression:          expr.Filter(),
		ExpressionAttributeNames:  expr.Names(),
		ExpressionAttributeValues: expr.Values(),
		Limit:                     aws.Int32(int32(opt.Pag.Limit)),
		ExclusiveStartKey:         exclusiveStartKey,
	})
	if err != nil {
		r.l.Errorf(ctx, "order.repository.scanOrders.Scan: %v", err)
		return nil, paginator.Paginator{}, err
	}

	var orders []models.Order
	if err := attributevalue.UnmarshalListOfMaps(result.Items, &orders); err != nil {
		r.l.Errorf(ctx, "order.repository.scanOrders.UnmarshalListOfMaps: %v", err)
		return nil, paginator.Paginator{}, err
	}

	nextCursor, err := pkgDynamo.EncodeCursor(result.LastEvaluatedKey)
	if err != nil {
		r.l.Errorf(ctx, "order.repository.scanOrders.EncodeCursor: %v", err)
		return nil, paginator.Paginator{}, err
	}

	pag := paginator.Paginator{
		Count:      int64(len(orders)),
		PageSize:   opt.Pag.Limit,
		NextCursor: nextCursor,
		HasMore:    result.LastEvaluatedKey != nil,
	}

	return orders, pag, nil
}

func (r *implRepository) List(ctx context.Context, opt ListOrderOption) ([]models.Order, error) {
	// Use GetMany with a large limit for listing
	result, _, err := r.GetMany(ctx, GetManyOrderOption{
		FilterOrder: opt.FilterOrder,
		Pag: paginator.PaginatorQuery{
			Limit: 1000, // Large limit for list operations
		},
	})
	return result, err
}

func (r *implRepository) Update(ctx context.Context, code string, opt UpdateOrderOption) (models.Order, error) {
	pk := pkgDynamo.BuildOrderPK(code)
	sk := pkgDynamo.BuildOrderSK(code)
	now := r.clock()

	// Build update expression
	updateBuilder := expression.Set(
		expression.Name("status"),
		expression.Value(string(opt.Status)),
	).Set(
		expression.Name("updated_at"),
		expression.Value(now),
	)

	if opt.PaidAt != nil {
		updateBuilder = updateBuilder.Set(
			expression.Name("paid_at"),
			expression.Value(*opt.PaidAt),
		)
	}

	expr, err := expression.NewBuilder().WithUpdate(updateBuilder).Build()
	if err != nil {
		r.l.Errorf(ctx, "order.repository.Update.BuildExpression: %v", err)
		return models.Order{}, err
	}

	result, err := r.db.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName: aws.String(r.tableName),
		Key: map[string]types.AttributeValue{
			"PK": &types.AttributeValueMemberS{Value: pk},
			"SK": &types.AttributeValueMemberS{Value: sk},
		},
		UpdateExpression:          expr.Update(),
		ExpressionAttributeNames:  expr.Names(),
		ExpressionAttributeValues: expr.Values(),
		ReturnValues:              types.ReturnValueAllNew,
	})
	if err != nil {
		r.l.Errorf(ctx, "order.repository.Update.UpdateItem: %v", err)
		return models.Order{}, err
	}

	var o models.Order
	if err := attributevalue.UnmarshalMap(result.Attributes, &o); err != nil {
		r.l.Errorf(ctx, "order.repository.Update.UnmarshalMap: %v", err)
		return models.Order{}, err
	}

	return o, nil
}

func (r *implRepository) Delete(ctx context.Context, code string) error {
	pk := pkgDynamo.BuildOrderPK(code)
	sk := pkgDynamo.BuildOrderSK(code)
	now := r.clock()

	// Soft delete by setting deleted_at
	updateBuilder := expression.Set(
		expression.Name("deleted_at"),
		expression.Value(now),
	).Set(
		expression.Name("updated_at"),
		expression.Value(now),
	)

	expr, err := expression.NewBuilder().WithUpdate(updateBuilder).Build()
	if err != nil {
		r.l.Errorf(ctx, "order.repository.Delete.BuildExpression: %v", err)
		return err
	}

	_, err = r.db.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName: aws.String(r.tableName),
		Key: map[string]types.AttributeValue{
			"PK": &types.AttributeValueMemberS{Value: pk},
			"SK": &types.AttributeValueMemberS{Value: sk},
		},
		UpdateExpression:          expr.Update(),
		ExpressionAttributeNames:  expr.Names(),
		ExpressionAttributeValues: expr.Values(),
	})
	if err != nil {
		r.l.Errorf(ctx, "order.repository.Delete.UpdateItem: %v", err)
		return err
	}

	return nil
}
