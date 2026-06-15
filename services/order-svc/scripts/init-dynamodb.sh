#!/bin/bash
# Order Service - DynamoDB Table Initialization
# This script is mounted into LocalStack and runs on startup

set -e

TABLE_NAME="ticketbottle-orders"

echo "[order-service] Checking DynamoDB table: $TABLE_NAME"

# Check if table already exists
if awslocal dynamodb describe-table --table-name "$TABLE_NAME" > /dev/null 2>&1; then
    echo "[order-service] Table $TABLE_NAME already exists, skipping creation"
    exit 0
fi

echo "[order-service] Creating DynamoDB table: $TABLE_NAME"

awslocal dynamodb create-table \
    --table-name "$TABLE_NAME" \
    --attribute-definitions \
        AttributeName=PK,AttributeType=S \
        AttributeName=SK,AttributeType=S \
        AttributeName=GSI1PK,AttributeType=S \
        AttributeName=GSI1SK,AttributeType=S \
        AttributeName=GSI2PK,AttributeType=S \
        AttributeName=GSI2SK,AttributeType=S \
    --key-schema \
        AttributeName=PK,KeyType=HASH \
        AttributeName=SK,KeyType=RANGE \
    --global-secondary-indexes \
        '[
            {
                "IndexName": "GSI1",
                "KeySchema": [
                    {"AttributeName": "GSI1PK", "KeyType": "HASH"},
                    {"AttributeName": "GSI1SK", "KeyType": "RANGE"}
                ],
                "Projection": {"ProjectionType": "ALL"},
                "ProvisionedThroughput": {"ReadCapacityUnits": 5, "WriteCapacityUnits": 5}
            },
            {
                "IndexName": "GSI2",
                "KeySchema": [
                    {"AttributeName": "GSI2PK", "KeyType": "HASH"},
                    {"AttributeName": "GSI2SK", "KeyType": "RANGE"}
                ],
                "Projection": {"ProjectionType": "ALL"},
                "ProvisionedThroughput": {"ReadCapacityUnits": 5, "WriteCapacityUnits": 5}
            }
        ]' \
    --provisioned-throughput ReadCapacityUnits=5,WriteCapacityUnits=5

# Wait for table to be active
echo "[order-service] Waiting for table to be active..."
awslocal dynamodb wait table-exists --table-name "$TABLE_NAME"

echo "[order-service] Table $TABLE_NAME created and active"
