# LocalStack Quick Start Guide

## 🚀 Quick Start (3 Steps)

### Step 1: Install Prerequisites

```bash
# Install LocalStack CLI
brew install localstack/tap/localstack-cli

# Install awslocal (AWS CLI wrapper for LocalStack)
brew install awslocal

# Verify Docker is running
docker ps
```

### Step 2: Setup LocalStack

```bash
cd ticketbottle-payment/lambdas

# Start LocalStack and deploy Lambda functions
npm run local:setup
```

**What this does:**
- Starts LocalStack, PostgreSQL, and Kafka in Docker
- Publishes your Lambda layers
- Creates your 3 Lambda functions
- Sets up API Gateway for webhook endpoints
- Creates EventBridge schedules
- Runs database migrations

### Step 3: Test Your Lambdas

```bash
# Test webhook handler
awslocal lambda invoke \
  --function-name payment-webhook-handler \
  --payload '{}' \
  response.json

cat response.json

# Or run all integration tests
npm run local:test
```

---

## 📋 Available NPM Commands

```bash
# Setup LocalStack environment
npm run local:setup

# Run integration tests
npm run local:test

# View logs (real-time)
npm run local:logs              # Webhook handler logs
npm run local:logs:processor    # Outbox processor logs
npm run local:logs:cleanup      # Outbox cleanup logs

# Teardown LocalStack
npm run local:teardown
```

---

## 🧪 Testing Examples

### 1. Test via Lambda Invoke

```bash
# Invoke webhook handler directly
awslocal lambda invoke \
  --function-name payment-webhook-handler \
  --payload '{"test": "data"}' \
  response.json

# View response
cat response.json | jq
```

### 2. Test via HTTP (API Gateway)

```bash
# Get API endpoint
API_ID=$(awslocal apigateway get-rest-apis --query 'items[0].id' --output text)
API_ENDPOINT="http://localhost:4566/restapis/$API_ID/dev/_user_request_"

# Send webhook request
curl -X POST "$API_ENDPOINT/webhook/zalopay" \
  -H "Content-Type: application/json" \
  -d '{
    "app_id": "test_app_id_123",
    "amount": 50000,
    "status": 1
  }'
```

### 3. Test Outbox Processor

```bash
# Invoke outbox processor
awslocal lambda invoke \
  --function-name outbox-processor \
  --payload '{}' \
  response.json

# Check database for processed events
docker exec -it ticketbottle-postgres-test psql -U postgres -d payment_test -c "SELECT * FROM \"Outbox\" LIMIT 10;"
```

### 4. Test Outbox Cleanup

```bash
# Invoke cleanup function
awslocal lambda invoke \
  --function-name outbox-cleanup \
  --payload '{}' \
  response.json

cat response.json
```

---

## 🔍 Monitoring & Debugging

### View CloudWatch Logs

```bash
# List all log groups
awslocal logs describe-log-groups

# Tail specific function logs
awslocal logs tail /aws/lambda/payment-webhook-handler --follow

# Filter by pattern
awslocal logs filter-log-events \
  --log-group-name /aws/lambda/payment-webhook-handler \
  --filter-pattern "ERROR"
```

### View Docker Container Logs

```bash
# LocalStack logs
docker logs ticketbottle-localstack -f

# PostgreSQL logs
docker logs ticketbottle-postgres-test -f

# Kafka logs
docker logs ticketbottle-kafka-test -f
```

### Check Lambda Function Status

```bash
# List all functions
awslocal lambda list-functions \
  --query 'Functions[*].[FunctionName,Runtime,Handler,Timeout]' \
  --output table

# Get specific function details
awslocal lambda get-function --function-name payment-webhook-handler | jq
```

---

## 🔄 Development Workflow

### Typical Development Cycle

```bash
# 1. Make code changes to your Lambda function
vim payment-webhook-handler/handlers/webhook.handler.ts

# 2. Rebuild
npm run build:layers

# 3. Update Lambda function code
awslocal lambda update-function-code \
  --function-name payment-webhook-handler \
  --zip-file fileb://build/payment-webhook-handler.zip

# 4. Test immediately
awslocal lambda invoke \
  --function-name payment-webhook-handler \
  --payload '{}' \
  response.json

# 5. View logs
npm run local:logs
```

### Update All Functions After Code Changes

```bash
# Rebuild all
npm run build:layers

# Update all functions
awslocal lambda update-function-code \
  --function-name payment-webhook-handler \
  --zip-file fileb://build/payment-webhook-handler.zip

awslocal lambda update-function-code \
  --function-name outbox-processor \
  --zip-file fileb://build/outbox-processor.zip

awslocal lambda update-function-code \
  --function-name outbox-cleanup \
  --zip-file fileb://build/outbox-cleanup.zip

# Run tests
npm run local:test
```

---

## 🗄️ Database Operations

### Connect to PostgreSQL

```bash
# Using psql in Docker
docker exec -it ticketbottle-postgres-test psql -U postgres -d payment_test

# Or from your machine (if you have psql installed)
psql postgresql://postgres:postgres@localhost:5433/payment_test
```

### Common Database Queries

```sql
-- View all payments
SELECT * FROM "Payment" ORDER BY "createdAt" DESC LIMIT 10;

-- View outbox events
SELECT * FROM "Outbox" ORDER BY "createdAt" DESC LIMIT 10;

-- Count unpublished events
SELECT COUNT(*) FROM "Outbox" WHERE published = false;

-- View failed payments
SELECT * FROM "Payment" WHERE status = 'FAILED';
```

### Reset Database

```bash
# Reset database (drops all data)
cd ticketbottle-payment
DATABASE_URL="postgresql://postgres:postgres@localhost:5433/payment_test" npx prisma migrate reset

# Re-run migrations
DATABASE_URL="postgresql://postgres:postgres@localhost:5433/payment_test" npx prisma migrate deploy
```

---

## 🐛 Troubleshooting

### Issue: LocalStack not starting

```bash
# Check Docker is running
docker ps

# View LocalStack logs
docker logs ticketbottle-localstack

# Restart LocalStack
docker-compose -f docker-compose.localstack.yml restart
```

### Issue: Lambda function fails with "Cannot find module"

```bash
# Rebuild layers with Prisma client
npm run build:layers

# Recreate Lambda functions
npm run local:teardown
npm run local:setup
```

### Issue: Database connection fails

```bash
# Check PostgreSQL is running
docker ps | grep postgres

# Test connection
docker exec ticketbottle-postgres-test pg_isready -U postgres

# Verify connection string in env.localstack.json
# Should be: postgresql://postgres:postgres@postgres:5432/payment_test
```

### Issue: Kafka connection fails

```bash
# Check Kafka is running
docker ps | grep kafka

# Test from within LocalStack network
docker exec ticketbottle-localstack curl kafka:29093

# Verify broker address: kafka:29093 (not localhost)
```

### Issue: API Gateway returns 404

```bash
# List APIs
awslocal apigateway get-rest-apis

# Get API details
API_ID=$(awslocal apigateway get-rest-apis --query 'items[0].id' --output text)
awslocal apigateway get-resources --rest-api-id $API_ID

# Redeploy if needed
awslocal apigateway create-deployment --rest-api-id $API_ID --stage-name dev
```

---

## 📝 Environment Configuration

### Local Environment Files

- `env.localstack.json` - Webhook handler environment variables
- `env.outbox-processor.json` - Outbox processor environment variables
- `env.outbox-cleanup.json` - Outbox cleanup environment variables

### Update Environment Variables

```bash
# Edit environment file
vim env.localstack.json

# Update Lambda function
awslocal lambda update-function-configuration \
  --function-name payment-webhook-handler \
  --environment file://env.localstack.json
```

---

## 🎯 Next Steps

1. **Add Test Data:**
   ```bash
   # Create test payments in database
   docker exec -it ticketbottle-postgres-test psql -U postgres -d payment_test
   ```

2. **Create Test Events:**
   ```bash
   # Add test webhook payloads in test-events/
   mkdir -p test-events
   cat > test-events/zalopay-success.json <<EOF
   {
     "app_id": "test_app_id_123",
     "amount": 50000,
     "status": 1
   }
   EOF
   ```

3. **Integration with Main Service:**
   - Test end-to-end flow: Create payment → Receive webhook → Process outbox → Publish to Kafka

4. **Load Testing:**
   ```bash
   # Use Apache Bench to load test webhook endpoint
   API_ID=$(awslocal apigateway get-rest-apis --query 'items[0].id' --output text)
   API_URL="http://localhost:4566/restapis/$API_ID/dev/_user_request_/webhook/zalopay"

   ab -n 100 -c 10 -p test-events/zalopay-success.json -T application/json $API_URL
   ```

---

## 📚 Additional Resources

- [LocalStack Documentation](https://docs.localstack.cloud/)
- [AWS Lambda Developer Guide](https://docs.aws.amazon.com/lambda/)
- [Full Testing Guide](./LOCALSTACK_TESTING.md)
- [AWS Deployment Plan](./lucky-imagining-island.md)

---

## 🎬 Complete Example Session

```bash
# Terminal 1: Setup and monitor logs
cd ticketbottle-payment/lambdas
npm run local:setup
npm run local:logs

# Terminal 2: Run tests
cd ticketbottle-payment/lambdas

# Test webhook
awslocal lambda invoke \
  --function-name payment-webhook-handler \
  --payload '{"test": "data"}' \
  response.json && cat response.json | jq

# Test outbox processor
awslocal lambda invoke \
  --function-name outbox-processor \
  --payload '{}' \
  response.json && cat response.json | jq

# Test cleanup
awslocal lambda invoke \
  --function-name outbox-cleanup \
  --payload '{}' \
  response.json && cat response.json | jq

# Run integration tests
npm run local:test

# When done
npm run local:teardown
```

---

**Happy Testing! 🎉**
