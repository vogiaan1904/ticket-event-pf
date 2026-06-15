package dynamodb

import (
	"fmt"
	"time"
)

// Key prefixes for single-table design
const (
	OrderPrefix = "ORDER#"
	ItemPrefix  = "ITEM#"
	UserPrefix  = "USER#"
	EventPrefix = "EVENT#"
)

// GSI names
const (
	GSI1Name = "GSI1"
	GSI2Name = "GSI2"
)

// BuildOrderPK builds the partition key for an order
func BuildOrderPK(code string) string {
	return OrderPrefix + code
}

// BuildOrderSK builds the sort key for an order
func BuildOrderSK(code string) string {
	return OrderPrefix + code
}

// BuildItemSK builds the sort key for an order item
func BuildItemSK(itemID string) string {
	return ItemPrefix + itemID
}

// BuildUserGSI1PK builds the GSI1 partition key for user queries
func BuildUserGSI1PK(userID string) string {
	return UserPrefix + userID
}

// BuildOrderGSI1SK builds the GSI1 sort key for order (for sorting by createdAt)
func BuildOrderGSI1SK(createdAt time.Time, code string) string {
	return fmt.Sprintf("%s%s#%s", OrderPrefix, createdAt.Format(time.RFC3339Nano), code)
}

// BuildEventGSI2PK builds the GSI2 partition key for event queries
func BuildEventGSI2PK(eventID string) string {
	return EventPrefix + eventID
}

// BuildOrderGSI2SK builds the GSI2 sort key for order (for sorting by createdAt)
func BuildOrderGSI2SK(createdAt time.Time, code string) string {
	return fmt.Sprintf("%s%s#%s", OrderPrefix, createdAt.Format(time.RFC3339Nano), code)
}
