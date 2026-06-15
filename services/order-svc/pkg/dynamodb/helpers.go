package dynamodb

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

// EntityType represents the type of entity in the single-table design
type EntityType string

const (
	EntityTypeOrder     EntityType = "ORDER"
	EntityTypeOrderItem EntityType = "ORDER_ITEM"
)

// SoftDeleteFilter returns a filter expression for soft-deleted items
func SoftDeleteFilter() string {
	return "attribute_not_exists(deleted_at) OR deleted_at = :null"
}

// SoftDeleteFilterValues returns the expression attribute values for soft delete filter
func SoftDeleteFilterValues() map[string]types.AttributeValue {
	nullVal, _ := attributevalue.Marshal(nil)
	return map[string]types.AttributeValue{
		":null": nullVal,
	}
}

// EncodeCursor encodes the LastEvaluatedKey to a base64 string
func EncodeCursor(lastKey map[string]types.AttributeValue) (string, error) {
	if lastKey == nil {
		return "", nil
	}

	// Convert to a simpler map for JSON encoding
	simpleMap := make(map[string]interface{})
	for k, v := range lastKey {
		var val interface{}
		if err := attributevalue.Unmarshal(v, &val); err != nil {
			return "", err
		}
		simpleMap[k] = val
	}

	jsonBytes, err := json.Marshal(simpleMap)
	if err != nil {
		return "", err
	}

	return base64.StdEncoding.EncodeToString(jsonBytes), nil
}

// DecodeCursor decodes a base64 cursor string to LastEvaluatedKey
func DecodeCursor(cursor string) (map[string]types.AttributeValue, error) {
	if cursor == "" {
		return nil, nil
	}

	jsonBytes, err := base64.StdEncoding.DecodeString(cursor)
	if err != nil {
		return nil, err
	}

	var simpleMap map[string]interface{}
	if err := json.Unmarshal(jsonBytes, &simpleMap); err != nil {
		return nil, err
	}

	result := make(map[string]types.AttributeValue)
	for k, v := range simpleMap {
		av, err := attributevalue.Marshal(v)
		if err != nil {
			return nil, err
		}
		result[k] = av
	}

	return result, nil
}

// GenerateItemID generates a unique ID for order items
func GenerateItemID() string {
	return fmt.Sprintf("%d", time.Now().UnixNano())
}
