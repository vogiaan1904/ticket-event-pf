# Testing Lambda Functions with LocalStack

This guide shows how to test your Lambda functions locally using LocalStack integrated with the development environment.

## Prerequisites

### 1. Install awslocal

```bash
# Install awslocal (wrapper for AWS CLI that points to LocalStack)
brew install awslocal

# Or using pip
pip install awscli-local
```

### 2. Ensure Development Environment is Running

The LocalStack service is part of the main development docker-compose setup:

```bash
# From the project root
cd development
docker-compose -f docker-compose.dev.yml up -d
```

## LocalStack Pro Setup

**This project uses LocalStack Pro** which supports Lambda Layers (available via GitHub Student Developer Pack).

### 1. Get LocalStack Pro Auth Token

1. Sign up at [https://app.localstack.cloud/](https://app.localstack.cloud/) with your GitHub account
2. Access via GitHub Student Developer Pack (1,000 monthly CI credits + 110+ AWS services)
3. Navigate to "API Keys" or "Settings"
4. Copy your Auth Token

### 2. Configure Auth Token

Set the token as an environment variable:

```bash
# Add to your shell profile (~/.zshrc or ~/.bashrc)
export LOCALSTACK_AUTH_TOKEN="your-token-here"

# Or create .env file in development/ directory
echo "LOCALSTACK_AUTH_TOKEN=your-token-here" > development/.env

# Restart LocalStack to apply Pro license
cd development
docker-compose -f docker-compose.dev.yml down localstack
docker-compose -f docker-compose.dev.yml up -d localstack

# Verify Pro is activated
curl http://localhost:4566/_localstack/info | jq '{edition, is_license_activated}'
# Should show: {"edition": "pro", "is_license_activated": true}
```

## Build Lambda Functions for LocalStack

We use a **layer-based approach** (same as production AWS):

```bash
cd ticketbottle-payment/lambdas

# Build dependencies layer, common layer, and functions
npm run build:layers
```

This creates:
- `build/dependencies-layer.zip` (~76MB) - node_modules with Prisma
- `build/common-layer.zip` (~70KB) - shared common code
- `build/payment-webhook-handler.zip` (~40KB) - function code only
- `build/outbox-processor.zip` (~20KB) - function code only
- `build/outbox-cleanup.zip` (~20KB) - function code only

**Key Feature:** TypeScript path aliases (`@/common/*`) are automatically transformed to absolute paths (`/opt/nodejs/common/*`) during build.

## Deploy to LocalStack

### 1. Deploy All Functions

Use the deployment script that handles everything:

```bash
cd ticketbottle-payment/lambdas

# Deploy all functions to LocalStack
./scripts/deploy-to-dev-compose.sh
```

This script will:
- Create IAM role
- Publish Lambda layers (dependencies + common code)
- Create all Lambda functions with layers attached
- Set up environment variables
- Configure API Gateway endpoints

### 2. Verify Deployment

```bash
# List deployed functions
awslocal lambda list-functions

# Get function details
awslocal lambda get-function --function-name payment-webhook-handler
```

## Testing Lambda Functions

### 1. Test Payment Webhook Handler

#### Direct Lambda Invocation

```bash
cd ticketbottle-payment/lambdas

# Invoke with API Gateway event structure
awslocal lambda invoke \
  --function-name payment-webhook-handler \
  --payload fileb://test-events/zalopay-api-gateway.json \
  response.json

# View response
cat response.json | jq .
```

#### Test Event Format

The test event must be in API Gateway proxy event format:

```json
{
  "httpMethod": "POST",
  "path": "/webhook/zalopay",
  "headers": {
    "Content-Type": "application/json"
  },
  "requestContext": {
    "identity": {
      "sourceIp": "127.0.0.1"
    }
  },
  "body": "{\"app_id\":\"test_app_id_123\",\"app_trans_id\":\"230101_TEST001\",...}"
}
```

### 2. Test via HTTP (API Gateway)

```bash
# Get the API Gateway URL from deployment output
# It will look like: http://localhost:4566/restapis/{api-id}/dev/_user_request_

# Test ZaloPay webhook
curl -X POST "http://localhost:4566/restapis/{api-id}/dev/_user_request_/webhook/zalopay" \
  -H "Content-Type: application/json" \
  -d @test-events/zalopay-callback.json
```

### 3. View Lambda Logs

```bash
# View logs for webhook handler
awslocal logs tail /aws/lambda/payment-webhook-handler --follow

# View logs for outbox processor
awslocal logs tail /aws/lambda/outbox-processor --follow

# View logs for outbox cleanup
awslocal logs tail /aws/lambda/outbox-cleanup --follow
```

Or use npm scripts:

```bash
npm run local:logs
npm run local:logs:processor
npm run local:logs:cleanup
```

## Development Workflow

### Quick Development Cycle

```bash
# 1. Make code changes to Lambda functions

# 2. Rebuild layers and functions
npm run build:layers

# 3. Update function code in LocalStack
awslocal lambda update-function-code \
  --function-name payment-webhook-handler \
  --zip-file fileb://build/payment-webhook-handler.zip

# 4. If common code changed, update the layer
awslocal lambda publish-layer-version \
  --layer-name ticketbottle-common \
  --zip-file fileb://build/common-layer.zip

# 5. Test immediately
awslocal lambda invoke \
  --function-name payment-webhook-handler \
  --payload fileb://test-events/zalopay-api-gateway.json \
  response.json

# 6. Check logs
awslocal logs tail /aws/lambda/payment-webhook-handler --since 1m
```

### Full Redeploy

If you need to redeploy everything from scratch:

```bash
# Build all functions and layers
npm run build:layers

# Deploy everything
./scripts/deploy-to-dev-compose.sh
```

## Understanding the Build Process

### Layer-Based Architecture

We use **Lambda Layers** to share code and dependencies across functions:

1. **Dependencies Layer** - Contains all npm packages including Prisma
2. **Common Layer** - Contains shared TypeScript code compiled to JavaScript
3. **Function Code** - Contains only function-specific code

### Build Script (`scripts/build-layers.js`)

The build process:

1. **Compile TypeScript** to JavaScript using `tsc`
2. **Transform path aliases** - Converts `@/common/*` to `/opt/nodejs/common/*` using `sed`
3. **Create layers** - Packages dependencies and common code separately
4. **Package functions** - Creates small function zips with only handler code

**Key transformation:**
```javascript
// Before (TypeScript source)
import { logger } from '@/common/logger';

// After (compiled JavaScript with sed transformation)
const logger_1 = require("/opt/nodejs/common/logger");
```

This allows Lambda to find modules in the layers at runtime (`/opt/nodejs/`).

### Why Layers?

- **Smaller function packages** (~40KB vs ~55MB)
- **Faster deployments** (only update changed layer)
- **Shared dependencies** across functions
- **Matches production AWS setup** exactly

## Configuration Files

### Environment Variables

Functions read environment variables from `env.*.json` files:

**env.localstack.json** (webhook handler):
```json
{
  "Variables": {
    "NODE_ENV": "development",
    "DATABASE_URL": "postgresql://root:root@postgres-payment:5432/ticketbottle_payment",
    "KAFKA_BROKERS": "kafka:29092",
    "ZALOPAY_APP_ID": "test_app_id_123",
    ...
  }
}
```

**Note:** Use Docker service names (`postgres-payment`, `kafka`) not `localhost` since Lambdas run in Docker.

### Network Configuration

LocalStack must use the correct Docker network to communicate with other services:

In `development/docker-compose.dev.yml`:
```yaml
localstack:
  environment:
    - LAMBDA_DOCKER_NETWORK=development_ticketbottle-network
```

The network name includes the project directory prefix (`development_`).

## Troubleshooting

### Issue 1: "Cannot find module" Errors

**Problem:** Lambda can't find imported modules from layers

**Causes & Solutions:**

1. **LocalStack Pro not activated:**
```bash
# Check Pro status
curl http://localhost:4566/_localstack/info | jq '.edition'
# Should show "pro", not "community"

# If not activated, set LOCALSTACK_AUTH_TOKEN and restart
export LOCALSTACK_AUTH_TOKEN="your-token"
cd development && docker-compose -f docker-compose.dev.yml restart localstack
```

2. **Path aliases not transformed:**
```bash
# Check compiled JavaScript has absolute paths
head -20 payment-webhook-handler/dist/index.js
# Should see: require("/opt/nodejs/common/logger")
# NOT: require("@/common/logger") or require("common/logger")

# Rebuild if incorrect
npm run build:layers
```

3. **Layers not attached to function:**
```bash
# Verify layers are attached
awslocal lambda get-function-configuration \
  --function-name payment-webhook-handler \
  | jq '.Layers'
# Should show both dependencies and common layers
```

### Issue 2: Prisma Connection Errors

**Problem:** `PrismaClientInitializationError`

**Solutions:**

1. **Check Prisma is in dependencies layer:**
```bash
unzip -l build/dependencies-layer.zip | grep -E "@prisma|\.prisma" | head -10
# Should show nodejs/node_modules/@prisma and nodejs/node_modules/.prisma
```

2. **Verify DATABASE_URL uses Docker service name:**
```bash
awslocal lambda get-function-configuration --function-name payment-webhook-handler | jq '.Environment.Variables.DATABASE_URL'
# Should be: postgresql://root:root@postgres-payment:5432/...
# NOT: postgresql://root:root@localhost:5433/...
```

3. **Ensure postgres-payment is running:**
```bash
docker ps | grep postgres-payment
```

### Issue 3: Network Connection Errors

**Problem:** Lambda can't connect to Kafka, PostgreSQL, etc.

**Solution:** Verify LocalStack is using the correct Docker network:

```bash
# Check LocalStack network config
docker inspect ticketbottle-localstack | jq '.[0].HostConfig.NetworkMode'
# Should show: development_ticketbottle-network

# Check Lambda container network (when running)
docker ps | grep lambda
docker inspect {lambda-container} | jq '.[0].NetworkSettings.Networks'
```

If wrong, update `docker-compose.dev.yml`:
```yaml
- LAMBDA_DOCKER_NETWORK=development_ticketbottle-network
```

### Issue 4: Function Updates Not Taking Effect

**Problem:** Code changes don't appear when invoking Lambda

**Solution:**

1. **Rebuild completely:**
```bash
rm -rf build/*
npm run build:layers
```

2. **Delete and recreate function:**
```bash
awslocal lambda delete-function --function-name payment-webhook-handler
./scripts/deploy-to-dev-compose.sh
```

3. **Wait for update to complete:**
```bash
awslocal lambda get-function-configuration \
  --function-name payment-webhook-handler \
  | jq '.LastUpdateStatus'
# Wait until it shows "Successful"
```

### Issue 5: LocalStack Container Issues

**Problem:** LocalStack not responding or containers failing

**Solution:**

```bash
# Check LocalStack logs
docker logs ticketbottle-localstack --tail 100

# Restart LocalStack
cd development
docker-compose -f docker-compose.dev.yml restart localstack

# Full restart if needed
docker-compose -f docker-compose.dev.yml down localstack
docker-compose -f docker-compose.dev.yml up -d localstack

# Wait for ready
sleep 10
awslocal lambda list-functions
```

## NPM Scripts

Quick commands available in `package.json`:

```bash
# Deploy all functions to LocalStack
npm run local:deploy

# View logs for each function
npm run local:logs
npm run local:logs:processor
npm run local:logs:cleanup
```

## LocalStack Pro vs Free Tier

| Aspect | LocalStack Pro (This Setup) | LocalStack Free Tier |
|--------|----------------------------|---------------------|
| Lambda Layers | ✅ Fully supported | ❌ Not mounted to `/opt` |
| Common Code | Lambda Layer at `/opt/nodejs/common/*` | Must bundle into each function |
| Dependencies | Lambda Layer at `/opt/nodejs/node_modules/*` | Must bundle into each function |
| Build Approach | Layer-based (same as AWS) | esbuild bundling workaround |
| Function Size | ~40KB + shared layers | ~55MB per function |
| Deployment | Matches AWS production exactly | Different from production |
| Path Transforms | `@/common/*` → `/opt/nodejs/common/*` | `@/common/*` → bundled at build time |

**Note:** With LocalStack Pro, the local setup matches AWS production exactly - making testing more reliable.

## Next Steps

1. **Set up LocalStack Pro:** Export `LOCALSTACK_AUTH_TOKEN` or create `.env` file
2. **Start development environment:** `cd development && docker-compose -f docker-compose.dev.yml up -d`
3. **Verify Pro activation:** `curl http://localhost:4566/_localstack/info | jq '.edition'`
4. **Build layers and functions:** `cd ticketbottle-payment/lambdas && npm run build:layers`
5. **Deploy to LocalStack:** `./scripts/deploy-to-dev-compose.sh`
6. **Test functions:** Use test events in `test-events/`
7. **View logs:** `npm run local:logs`

---

**Key Takeaway:** LocalStack Pro provides full Lambda Layers support, allowing local testing to match AWS production exactly. TypeScript path aliases are automatically transformed to absolute Lambda layer paths during build.
