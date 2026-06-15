# AWS Lambda Deployment Strategy - TicketBottle Payment Service

## 🎯 Vision Overview

**What We're Building:**
You have 3 Lambda functions that handle payment webhooks and outbox event processing:
- `payment-webhook-handler`: Receives payment callbacks from ZaloPay/PayOS
- `outbox-processor`: Reads unpublished events from database and sends to Kafka (runs every minute)
- `outbox-cleanup`: Cleans up old events from database (runs daily)

**Why This Architecture:**
- **Cost-Effective**: Only pay when webhooks are called or scheduled jobs run
- **Scalable**: Lambda auto-scales with webhook traffic
- **Separation**: Main gRPC service stays on EKS, auxiliary tasks move to Lambda
- **AWS-Native**: Uses API Gateway, EventBridge, VPC networking

**Deployment Approach:**
We'll use **AWS SAM (Serverless Application Model)** - it's like a simplified CloudFormation specifically for serverless apps. SAM handles:
- Lambda functions
- Lambda layers (shared dependencies)
- API Gateway (webhook endpoints)
- EventBridge schedules (cron jobs)
- VPC networking (database/Kafka access)
- IAM permissions

---

## 📋 Prerequisites - What You Need to Prepare in AWS

### 1. AWS Account Setup

**Install AWS CLI:**
```bash
# macOS
brew install awscli

# Verify installation
aws --version  # Should show version 2.x
```

**Configure AWS Credentials:**
```bash
# This will prompt for:
# - AWS Access Key ID
# - AWS Secret Access Key
# - Default region (use us-east-1)
# - Default output format (use json)
aws configure

# Verify configuration
aws sts get-caller-identity
```

**Install SAM CLI:**
```bash
# macOS
brew tap aws/tap
brew install aws-sam-cli

# Verify installation
sam --version  # Should show version 1.100+
```

### 2. AWS Infrastructure Prerequisites

Before deploying Lambda functions, you need these AWS resources created:

#### A. VPC and Networking
Your Lambdas need VPC access to reach Aurora database and Kafka cluster.

**What you need:**
- VPC ID (e.g., `vpc-0abc123def456`)
- Private Subnet IDs in at least 2 AZs (e.g., `subnet-0abc123,subnet-0def456`)
- Security Group allowing Lambda to access:
  - Aurora (port 5432)
  - Kafka (port 9092)

**How to find/create:**
```bash
# List VPCs
aws ec2 describe-vpcs --query 'Vpcs[*].[VpcId,Tags[?Key==`Name`].Value|[0]]' --output table

# List private subnets in your VPC
aws ec2 describe-subnets \
  --filters "Name=vpc-id,Values=vpc-YOUR_VPC_ID" \
  --query 'Subnets[*].[SubnetId,AvailabilityZone,CidrBlock]' \
  --output table

# Create security group for Lambda functions
aws ec2 create-security-group \
  --group-name ticketbottle-lambda-sg \
  --description "Security group for TicketBottle Lambda functions" \
  --vpc-id vpc-YOUR_VPC_ID

# Get the security group ID from output, then add rules
aws ec2 authorize-security-group-egress \
  --group-id sg-YOUR_SG_ID \
  --protocol tcp \
  --port 5432 \
  --cidr 10.0.0.0/16  # Your VPC CIDR
```

#### B. Database Connection String
Get your Aurora PostgreSQL connection string:

```bash
# Store in AWS Systems Manager Parameter Store (recommended)
aws ssm put-parameter \
  --name "/ticketbottle/payment/DATABASE_URL" \
  --value "postgresql://username:password@your-aurora-endpoint:5432/payment_db?schema=public" \
  --type "SecureString" \
  --description "Payment service database URL"
```

#### C. Kafka Configuration
You need Kafka broker addresses. If using MSK:

```bash
# List MSK clusters
aws kafka list-clusters --query 'ClusterInfoList[*].[ClusterName,ClusterArn]' --output table

# Get broker endpoints
aws kafka get-bootstrap-brokers --cluster-arn arn:aws:kafka:us-east-1:YOUR_ACCOUNT:cluster/YOUR_CLUSTER
```

Store Kafka brokers:
```bash
aws ssm put-parameter \
  --name "/ticketbottle/payment/KAFKA_BROKERS" \
  --value "broker1.kafka.us-east-1.amazonaws.com:9092,broker2.kafka.us-east-1.amazonaws.com:9092" \
  --type "String"
```

#### D. Payment Provider Credentials
Store your ZaloPay and PayOS credentials:

```bash
# ZaloPay credentials
aws ssm put-parameter --name "/ticketbottle/payment/ZALOPAY_APP_ID" --value "YOUR_APP_ID" --type "SecureString"
aws ssm put-parameter --name "/ticketbottle/payment/ZALOPAY_KEY1" --value "YOUR_KEY1" --type "SecureString"
aws ssm put-parameter --name "/ticketbottle/payment/ZALOPAY_KEY2" --value "YOUR_KEY2" --type "SecureString"

# PayOS credentials
aws ssm put-parameter --name "/ticketbottle/payment/PAYOS_CLIENT_ID" --value "YOUR_CLIENT_ID" --type "SecureString"
aws ssm put-parameter --name "/ticketbottle/payment/PAYOS_API_KEY" --value "YOUR_API_KEY" --type "SecureString"
aws ssm put-parameter --name "/ticketbottle/payment/PAYOS_CHECKSUM_KEY" --value "YOUR_CHECKSUM" --type "SecureString"
```

#### E. S3 Bucket for Deployment Artifacts
SAM needs an S3 bucket to upload your Lambda code:

```bash
# Create deployment bucket
aws s3 mb s3://ticketbottle-lambda-deployments-YOUR_ACCOUNT_ID

# Enable versioning (best practice)
aws s3api put-bucket-versioning \
  --bucket ticketbottle-lambda-deployments-YOUR_ACCOUNT_ID \
  --versioning-configuration Status=Enabled
```

---

## 🔧 Development Workflow - How Developers Work with TypeScript Lambdas

### Phase 1: Local Development

#### 1. Project Setup
```bash
cd ticketbottle-payment/lambdas

# Install dependencies
npm install

# Generate Prisma client
npm run generate
```

#### 2. Local Testing with SAM
SAM CLI lets you test Lambda functions locally before deploying.

**Test webhook handler locally:**
```bash
# Build the Lambda functions
sam build --template-file template.yaml

# Start local API Gateway
sam local start-api --env-vars env.json

# In another terminal, test webhook
curl -X POST http://127.0.0.1:3000/webhook/zalopay \
  -H "Content-Type: application/json" \
  -d @test-events/zalopay-callback.json
```

**Create `env.json` for local testing:**
```json
{
  "PaymentWebhookHandlerFunction": {
    "NODE_ENV": "development",
    "LOG_LEVEL": "debug",
    "DATABASE_URL": "postgresql://localhost:5432/payment_db",
    "KAFKA_BROKERS": "localhost:9092",
    "KAFKA_SSL": "false",
    "ZALOPAY_APP_ID": "test_app_id",
    "ZALOPAY_KEY1": "test_key1",
    "ZALOPAY_KEY2": "test_key2"
  },
  "OutboxProcessorFunction": {
    "NODE_ENV": "development",
    "DATABASE_URL": "postgresql://localhost:5432/payment_db",
    "KAFKA_BROKERS": "localhost:9092"
  },
  "OutboxCleanupFunction": {
    "NODE_ENV": "development",
    "DATABASE_URL": "postgresql://localhost:5432/payment_db"
  }
}
```

**Invoke a function directly:**
```bash
# Test outbox processor
sam local invoke OutboxProcessorFunction --event test-events/empty.json --env-vars env.json

# Test with Docker (includes all layers)
sam local invoke OutboxProcessorFunction \
  --event test-events/empty.json \
  --env-vars env.json \
  --docker-network host
```

#### 3. Unit Testing
```bash
# Run Jest tests
npm test

# Run with coverage
npm run test:coverage

# Watch mode during development
npm run test:watch
```

#### 4. Build Process
```bash
# Compile TypeScript
npm run build

# Build Lambda layers (dependencies + common code)
npm run build:layers

# This creates:
# - build/dependencies-layer.zip (node_modules + Prisma client)
# - build/common-layer.zip (shared code)
# - build/payment-webhook-handler.zip
# - build/outbox-processor.zip
# - build/outbox-cleanup.zip
```

**What happens in `build:layers`:**
1. Compiles TypeScript to JavaScript
2. Installs production dependencies only
3. Generates Prisma client
4. Creates Lambda-compatible directory structure
5. Zips everything for deployment

---

## 🚀 Deployment Workflow - Step by Step

### Phase 2: Deploy to Development Environment

#### Step 1: Update SAM Configuration
Edit `samconfig.toml` with your AWS infrastructure details:

```toml
[dev.deploy.parameters]
stack_name = "ticketbottle-payment-lambdas-dev"
s3_bucket = "ticketbottle-lambda-deployments-YOUR_ACCOUNT_ID"
s3_prefix = "dev"
region = "us-east-1"
capabilities = "CAPABILITY_IAM"
parameter_overrides = [
  "Environment=dev",
  "VpcId=vpc-YOUR_VPC_ID",
  "PrivateSubnetIds=subnet-0abc123,subnet-0def456",
  "DatabaseUrl=/ticketbottle/payment/DATABASE_URL",  # SSM parameter name
  "KafkaBrokers=/ticketbottle/payment/KAFKA_BROKERS",
  "ZaloPayAppId=/ticketbottle/payment/ZALOPAY_APP_ID",
  "ZaloPayKey1=/ticketbottle/payment/ZALOPAY_KEY1",
  "ZaloPayKey2=/ticketbottle/payment/ZALOPAY_KEY2",
  "PayOSClientId=/ticketbottle/payment/PAYOS_CLIENT_ID",
  "PayOSApiKey=/ticketbottle/payment/PAYOS_API_KEY",
  "PayOSChecksumKey=/ticketbottle/payment/PAYOS_CHECKSUM_KEY"
]
```

#### Step 2: Build and Deploy
```bash
cd ticketbottle-payment/lambdas

# Build Lambda functions and layers
sam build --template-file template.yaml

# Validate template
sam validate

# Deploy to dev environment
sam deploy --config-env dev

# First time deployment (guided):
sam deploy --guided --config-env dev
```

**What `sam deploy` does:**
1. Packages Lambda code and uploads to S3
2. Creates CloudFormation stack
3. Provisions Lambda functions, layers, API Gateway, EventBridge schedules
4. Sets up IAM roles and permissions
5. Configures VPC networking

#### Step 3: Verify Deployment
```bash
# Check stack status
aws cloudformation describe-stacks \
  --stack-name ticketbottle-payment-lambdas-dev \
  --query 'Stacks[0].StackStatus'

# Get webhook URLs
aws cloudformation describe-stacks \
  --stack-name ticketbottle-payment-lambdas-dev \
  --query 'Stacks[0].Outputs'

# Test webhook endpoint
curl -X POST https://YOUR_API_ID.execute-api.us-east-1.amazonaws.com/dev/webhook/zalopay \
  -H "Content-Type: application/json" \
  -d '{"test": "data"}'

# Check Lambda logs
sam logs --stack-name ticketbottle-payment-lambdas-dev --name PaymentWebhookHandlerFunction --tail
```

---

## 🔄 Common Development Practices

### Practice 1: Environment-Specific Deployments

**Development Environment:**
```bash
sam deploy --config-env dev
```
- Lower memory/timeout settings
- Debug logging enabled
- Outbox retention: 7 days

**Staging Environment:**
```bash
sam deploy --config-env staging
```
- Production-like configuration
- Info logging
- Used for integration testing

**Production Environment:**
```bash
sam deploy --config-env prod
```
- Optimized memory/timeout
- Error logging only
- Outbox retention: 30 days
- Reserved concurrency for webhook handler

### Practice 2: Hot Reload During Development

Use `sam sync` for rapid iteration:

```bash
# Watch for changes and auto-deploy
sam sync --stack-name ticketbottle-payment-lambdas-dev --watch

# This is faster than full deployment because it:
# - Skips CloudFormation if infrastructure unchanged
# - Directly updates Lambda code
# - Shows logs in real-time
```

### Practice 3: Viewing Logs

```bash
# Tail logs from all functions
sam logs --stack-name ticketbottle-payment-lambdas-dev --tail

# Filter by function
sam logs --stack-name ticketbottle-payment-lambdas-dev --name PaymentWebhookHandlerFunction --tail

# View logs from specific time
sam logs --stack-name ticketbottle-payment-lambdas-dev --start-time '10min ago'

# Or use CloudWatch CLI
aws logs tail /aws/lambda/ticketbottle-payment-webhook-handler-dev --follow
```

### Practice 4: Testing in AWS

**Invoke Lambda directly (bypass API Gateway):**
```bash
# Create test event
cat > test-event.json <<EOF
{
  "body": "{\"order_code\": \"test123\"}",
  "headers": {
    "Content-Type": "application/json"
  },
  "httpMethod": "POST",
  "path": "/webhook/zalopay"
}
EOF

# Invoke
aws lambda invoke \
  --function-name ticketbottle-payment-webhook-handler-dev \
  --payload file://test-event.json \
  --log-type Tail \
  response.json

# View response
cat response.json
```

**Test EventBridge schedules:**
```bash
# Manually trigger outbox processor
aws lambda invoke \
  --function-name ticketbottle-payment-outbox-processor-dev \
  --invocation-type Event \
  response.json
```

### Practice 5: Updating Only Code (No Infrastructure Changes)

When you only change handler code (not SAM template):

```bash
# Build
npm run build

# Update function code directly
aws lambda update-function-code \
  --function-name ticketbottle-payment-webhook-handler-dev \
  --zip-file fileb://build/payment-webhook-handler.zip

# Or use SAM sync (recommended)
sam sync --stack-name ticketbottle-payment-lambdas-dev --code
```

### Practice 6: Rollback

If deployment fails or introduces bugs:

```bash
# List previous versions
aws lambda list-versions-by-function \
  --function-name ticketbottle-payment-webhook-handler-dev

# Create alias pointing to previous version
aws lambda update-alias \
  --function-name ticketbottle-payment-webhook-handler-dev \
  --name live \
  --function-version 5  # Previous working version

# Or rollback entire CloudFormation stack
aws cloudformation describe-stacks \
  --stack-name ticketbottle-payment-lambdas-dev \
  --query 'Stacks[0].Parameters'

# Then redeploy previous SAM template from git
git checkout <previous-commit>
sam deploy --config-env dev
```

---

## 🔐 Security Best Practices

### 1. Never Hardcode Secrets
❌ **Don't do this:**
```typescript
const ZALOPAY_KEY = "abc123def456";  // NEVER!
```

✅ **Do this:**
```typescript
const ZALOPAY_KEY = process.env.ZALOPAY_KEY1;  // From SSM Parameter Store
```

### 2. Use Parameter Store References
In SAM template:
```yaml
Environment:
  Variables:
    ZALOPAY_KEY1: '{{resolve:ssm:/ticketbottle/payment/ZALOPAY_KEY1:1}}'
```

### 3. Restrict IAM Permissions
Lambda execution roles should follow least privilege:
```yaml
Policies:
  - Statement:
    - Effect: Allow
      Action:
        - ec2:CreateNetworkInterface  # VPC access only
        - ec2:DescribeNetworkInterfaces
        - ec2:DeleteNetworkInterface
      Resource: '*'
  - Statement:
    - Effect: Allow
      Action:
        - ssm:GetParameter  # Specific parameters only
      Resource: 'arn:aws:ssm:us-east-1:*:parameter/ticketbottle/payment/*'
```

---

## 📊 Monitoring and Debugging

### CloudWatch Dashboards
After deployment, create a dashboard:

```bash
# Use AWS Console -> CloudWatch -> Dashboards
# Or create via CLI (example)
aws cloudwatch put-dashboard \
  --dashboard-name TicketBottle-Payment-Lambdas \
  --dashboard-body file://dashboard.json
```

**Key metrics to monitor:**
- Lambda invocations
- Error count/rate
- Duration (p50, p95, p99)
- Concurrent executions
- Throttles

### CloudWatch Alarms
Set up alarms for production:

```bash
# Webhook handler error rate > 5%
aws cloudwatch put-metric-alarm \
  --alarm-name "payment-webhook-errors" \
  --alarm-description "Payment webhook error rate exceeded" \
  --metric-name Errors \
  --namespace AWS/Lambda \
  --statistic Sum \
  --period 300 \
  --evaluation-periods 1 \
  --threshold 5 \
  --comparison-operator GreaterThanThreshold \
  --dimensions Name=FunctionName,Value=ticketbottle-payment-webhook-handler-prod
```

### X-Ray Tracing
Enable X-Ray in SAM template for distributed tracing:
```yaml
Globals:
  Function:
    Tracing: Active
```

Then view traces in AWS Console -> X-Ray -> Service Map

---

## 🔄 CI/CD Pipeline (GitHub Actions Example)

### Typical Workflow

**`.github/workflows/deploy-lambdas.yml`:**
```yaml
name: Deploy Lambda Functions

on:
  push:
    branches:
      - main
    paths:
      - 'ticketbottle-payment/lambdas/**'
  pull_request:
    branches:
      - main
    paths:
      - 'ticketbottle-payment/lambdas/**'

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3

      - name: Setup Node.js
        uses: actions/setup-node@v3
        with:
          node-version: '20'

      - name: Install dependencies
        run: |
          cd ticketbottle-payment/lambdas
          npm ci

      - name: Run tests
        run: |
          cd ticketbottle-payment/lambdas
          npm test

      - name: Build
        run: |
          cd ticketbottle-payment/lambdas
          npm run build

  deploy-dev:
    needs: test
    if: github.ref == 'refs/heads/main'
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3

      - name: Setup AWS SAM
        uses: aws-actions/setup-sam@v2

      - name: Configure AWS credentials
        uses: aws-actions/configure-aws-credentials@v2
        with:
          aws-access-key-id: ${{ secrets.AWS_ACCESS_KEY_ID }}
          aws-secret-access-key: ${{ secrets.AWS_SECRET_ACCESS_KEY }}
          aws-region: us-east-1

      - name: SAM Build
        run: |
          cd ticketbottle-payment/lambdas
          sam build

      - name: SAM Deploy to Dev
        run: |
          cd ticketbottle-payment/lambdas
          sam deploy --config-env dev --no-confirm-changeset --no-fail-on-empty-changeset

  deploy-prod:
    needs: deploy-dev
    if: github.ref == 'refs/heads/main'
    runs-on: ubuntu-latest
    environment: production  # Requires manual approval
    steps:
      # Same as deploy-dev but with --config-env prod
      - name: SAM Deploy to Production
        run: |
          cd ticketbottle-payment/lambdas
          sam deploy --config-env prod --no-confirm-changeset --no-fail-on-empty-changeset
```

### Manual Deployment Checklist

For important production deployments, follow this checklist:

1. ✅ All tests passing
2. ✅ Code reviewed and approved
3. ✅ Tested in dev environment
4. ✅ Tested in staging environment
5. ✅ Database migrations applied (if any)
6. ✅ Secrets updated in Parameter Store (if changed)
7. ✅ CloudWatch alarms configured
8. ✅ Rollback plan documented
9. ✅ Deploy during low-traffic hours
10. ✅ Monitor logs for 30 minutes post-deployment

---

## 🎯 Quick Reference Commands

### Daily Development Commands
```bash
# Install dependencies
npm install

# Generate Prisma client
npm run generate

# Run tests
npm test

# Build locally
npm run build

# Test locally with SAM
sam build && sam local start-api

# Deploy to dev
sam build && sam deploy --config-env dev

# View logs
sam logs --stack-name ticketbottle-payment-lambdas-dev --tail
```

### Deployment Commands
```bash
# First time setup
sam deploy --guided --config-env dev

# Regular deployment
sam build && sam deploy --config-env dev

# Fast iteration (hot reload)
sam sync --stack-name ticketbottle-payment-lambdas-dev --watch

# Deploy only code changes
sam sync --stack-name ticketbottle-payment-lambdas-dev --code

# Deploy to production
sam build && sam deploy --config-env prod
```

### Debugging Commands
```bash
# Check stack status
aws cloudformation describe-stacks --stack-name ticketbottle-payment-lambdas-dev

# Get API endpoint
aws cloudformation describe-stacks \
  --stack-name ticketbottle-payment-lambdas-dev \
  --query 'Stacks[0].Outputs[?OutputKey==`WebhookApiUrl`].OutputValue' \
  --output text

# Invoke function directly
aws lambda invoke \
  --function-name ticketbottle-payment-webhook-handler-dev \
  --payload '{"test":"data"}' \
  response.json

# View logs
aws logs tail /aws/lambda/ticketbottle-payment-webhook-handler-dev --follow

# Delete stack (cleanup)
aws cloudformation delete-stack --stack-name ticketbottle-payment-lambdas-dev
```

---

## 🚨 Common Issues and Solutions

### Issue 1: VPC Cold Start Timeout
**Problem:** First invocation takes 10+ seconds
**Solution:** Use VPC ENI reuse (already configured in template)

### Issue 2: Database Connection Pool Exhausted
**Problem:** "Too many connections" error
**Solution:** Use RDS Proxy or reduce Lambda concurrency

### Issue 3: Kafka Connection Failures
**Problem:** Lambda can't reach Kafka brokers
**Solution:**
- Check security group allows port 9092
- Verify subnets have route to Kafka
- Use VPC endpoints if using MSK

### Issue 4: Prisma Client Not Found
**Problem:** `Cannot find module '@prisma/client'`
**Solution:**
```bash
cd ticketbottle-payment/lambdas
npm run generate
npm run build:layers
sam build && sam deploy
```

---

## 📝 Next Steps After Deployment

1. **Configure Payment Providers:**
   - Update ZaloPay dashboard with webhook URL
   - Update PayOS dashboard with webhook URL

2. **Test Webhook Flow:**
   - Create test payment from your app
   - Verify webhook received in CloudWatch Logs
   - Check outbox table has event
   - Verify event published to Kafka (within 1 minute)

3. **Set Up Monitoring:**
   - Create CloudWatch dashboard
   - Configure alarms for errors
   - Set up SNS notifications

4. **Document Runbooks:**
   - How to handle failed webhooks
   - How to manually replay events
   - How to rollback deployment

5. **Optimize Costs:**
   - Review Lambda memory settings after 1 week
   - Consider provisioned concurrency if cold starts are an issue
   - Set up Cost Explorer alerts

---

## 💡 Learning Resources

- [AWS SAM Documentation](https://docs.aws.amazon.com/serverless-application-model/)
- [AWS Lambda Best Practices](https://docs.aws.amazon.com/lambda/latest/dg/best-practices.html)
- [TypeScript Lambda Tutorial](https://docs.aws.amazon.com/lambda/latest/dg/lambda-typescript.html)
- [VPC Lambda Networking](https://docs.aws.amazon.com/lambda/latest/dg/configuration-vpc.html)
