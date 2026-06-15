#!/bin/bash

echo "Cleaning up LocalStack"

# Colors
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m'

cd "$(dirname "$0")/.."

# Delete Lambda functions
echo -e "${YELLOW}Deleting Lambda functions${NC}"
awslocal lambda delete-function --function-name payment-webhook-handler 2>/dev/null && echo -e "${GREEN}Deleted payment-webhook-handler${NC}"
awslocal lambda delete-function --function-name outbox-processor 2>/dev/null && echo -e "${GREEN}Deleted outbox-processor${NC}"
awslocal lambda delete-function --function-name outbox-cleanup 2>/dev/null && echo -e "${GREEN}Deleted outbox-cleanup${NC}"

# Delete API Gateway
echo -e "${YELLOW}Deleting API Gateway${NC}"
API_ID=$(awslocal apigateway get-rest-apis --query 'items[0].id' --output text 2>/dev/null)
if [ "$API_ID" != "None" ] && [ -n "$API_ID" ]; then
  awslocal apigateway delete-rest-api --rest-api-id "$API_ID" 2>/dev/null
  echo -e "${GREEN}Deleted API Gateway${NC}"
fi

# Delete EventBridge rules
echo -e "${YELLOW}Deleting EventBridge schedules${NC}"
awslocal events remove-targets --rule outbox-processor-schedule --ids 1 2>/dev/null
awslocal events delete-rule --name outbox-processor-schedule 2>/dev/null && echo -e "${GREEN}Deleted outbox-processor schedule${NC}"
awslocal events remove-targets --rule outbox-cleanup-schedule --ids 1 2>/dev/null
awslocal events delete-rule --name outbox-cleanup-schedule 2>/dev/null && echo -e "${GREEN}Deleted outbox-cleanup schedule${NC}"

# Delete IAM role
echo -e "${YELLOW}Deleting IAM role${NC}"
awslocal iam delete-role --role-name ticketbottle-lambda-role 2>/dev/null && echo -e "${GREEN}Deleted IAM role${NC}"

# Clean up test response files
rm -f response.json
rm -f env.*.json

# Stop LocalStack container (optional)
echo -e "${YELLOW}To stop LocalStack:${NC}"
echo -e "  cd ../../development && docker-compose -f docker-compose.dev.yml down localstack"

echo -e "\n${GREEN}LocalStack resources cleaned up${NC}"
