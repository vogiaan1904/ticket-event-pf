# TicketBottle Payment Lambda Functions

AWS Lambda functions for the TicketBottle payment service, handling webhook callbacks, outbox event processing, and cleanup operations.

## Architecture

This Lambda deployment follows a hybrid architecture:

- **Core gRPC Service**: Remains on EKS for synchronous payment operations
- **Lambda Functions**: Handle asynchronous, event-driven tasks
  - `payment-webhook-handler`: Processes payment provider callbacks
  - `outbox-processor`: Publishes outbox events to Kafka
  - `outbox-cleanup`: Manages outbox table maintenance

## Project Structure

```
lambdas/
├── common/                      # Shared code layer
│   ├── config/                  # Environment configuration
│   ├── database/                # Prisma client singleton
│   ├── kafka/                   # Kafka producer wrapper
│   ├── logger/                  # Winston logger
│   ├── types/                   # Type definitions
│   ├── constants/               # Kafka topics, etc.
│   └── utils/                   # Error handling, helpers
├── payment-webhook-handler/     # Webhook Lambda
│   ├── gateways/                # Payment gateway implementations
│   │   ├── zalopay/
│   │   └── payos/
│   ├── handlers/                # Business logic
│   └── index.ts                 # Lambda entry point
├── outbox-processor/            # Outbox processor Lambda
│   ├── handlers/
│   └── index.ts
├── outbox-cleanup/              # Outbox cleanup Lambda
│   ├── handlers/
│   └── index.ts
├── __tests__/                   # Unit tests
├── scripts/                     # Build scripts
├── cloudformation.yaml          # CloudFormation template
├── template.yaml                # SAM template
└── package.json                 # Dependencies
```

## Prerequisites

- Node.js >= 20.0.0
- AWS CLI configured
- SAM CLI (optional, for local testing)
- Docker (for SAM local)

## Installation

```bash
cd ticketbottle-payment/lambdas
npm install
npm run generate  # Generate Prisma client
```

## Development

### Build

```bash
# Compile TypeScript
npm run build

# Build Lambda layers and functions
npm run build:layers
```

### Testing

```bash
# Run all tests
npm test

# Watch mode
npm run test:watch

# Coverage report
npm run test:coverage
```

### Local Testing with SAM

```bash
# Start API Gateway locally
sam local start-api --template template.yaml

# Invoke function locally
sam local invoke PaymentWebhookHandlerFunction --event test-events/zalopay-webhook.json

# Generate sample events
sam local generate-event apigateway http-api-proxy > test-events/api-event.json
```

## Deployment

### Option 1: Using SAM CLI (Recommended)

```bash
# Build
sam build

# Deploy to dev
sam deploy --config-env dev --parameter-overrides \
  VpcId=vpc-xxx \
  PrivateSubnetIds=subnet-xxx,subnet-yyy \
  DatabaseUrl=postgresql://... \
  KafkaBrokers=broker1:9092 \
  ZaloPayAppId=xxx \
  ZaloPayKey1=xxx \
  ZaloPayKey2=xxx \
  PayOSClientId=xxx \
  PayOSApiKey=xxx \
  PayOSChecksumKey=xxx

# Deploy to production
sam deploy --config-env prod --guided
```

### Option 2: Using CloudFormation

```bash
# Build layers and functions
npm run build:layers

# Upload artifacts to S3
aws s3 cp build/dependencies-layer.zip s3://your-bucket/dev/layers/
aws s3 cp build/common-layer.zip s3://your-bucket/dev/layers/
aws s3 cp build/payment-webhook-handler.zip s3://your-bucket/dev/functions/
aws s3 cp build/outbox-processor.zip s3://your-bucket/dev/functions/
aws s3 cp build/outbox-cleanup.zip s3://your-bucket/dev/functions/

# Deploy stack
aws cloudformation deploy \
  --template-file cloudformation.yaml \
  --stack-name ticketbottle-payment-lambdas-dev \
  --parameter-overrides \
    Environment=dev \
    VpcId=vpc-xxx \
    PrivateSubnetIds=subnet-xxx,subnet-yyy \
    DatabaseUrl=postgresql://... \
    # ... other parameters
  --capabilities CAPABILITY_IAM
```

## Environment Variables

### Required for All Functions

- `NODE_ENV`: Environment (dev, staging, prod)
- `LOG_LEVEL`: Logging level (debug, info, warn, error)
- `DATABASE_URL`: PostgreSQL connection string
- `KAFKA_BROKERS`: Kafka broker addresses
- `KAFKA_SSL`: Enable Kafka SSL (true/false)

### Payment Webhook Handler Only

- `ZALOPAY_APP_ID`: ZaloPay application ID
- `ZALOPAY_KEY1`: ZaloPay MAC generation key
- `ZALOPAY_KEY2`: ZaloPay MAC verification key
- `PAYOS_CLIENT_ID`: PayOS client ID
- `PAYOS_API_KEY`: PayOS API key
- `PAYOS_CHECKSUM_KEY`: PayOS checksum key

### Outbox Processor Only

- `OUTBOX_BATCH_SIZE`: Number of events to process per invocation (default: 50)
- `OUTBOX_MAX_RETRIES`: Maximum retry attempts (default: 3)

### Outbox Cleanup Only

- `OUTBOX_RETENTION_DAYS`: Days to retain published events (default: 7 for dev, 30 for prod)

## API Endpoints

After deployment, you'll receive webhook URLs:

```
Payment Webhook Handler:
  ZaloPay: https://{api-id}.execute-api.{region}.amazonaws.com/{env}/webhook/zalopay
  PayOS:   https://{api-id}.execute-api.{region}.amazonaws.com/{env}/webhook/payos
```

Configure these URLs in your payment provider dashboards.

## Lambda Configurations

### Payment Webhook Handler
- **Trigger**: API Gateway HTTP POST
- **Memory**: 512 MB
- **Timeout**: 30 seconds
- **Concurrency**: Unlimited (handles webhook bursts)

### Outbox Processor
- **Trigger**: EventBridge (every 1 minute)
- **Memory**: 512 MB
- **Timeout**: 5 minutes
- **Concurrency**: 1 (prevents duplicate processing)

### Outbox Cleanup
- **Trigger**: EventBridge (daily)
- **Memory**: 256 MB
- **Timeout**: 5 minutes
- **Concurrency**: 1

## Monitoring

### CloudWatch Logs

Logs are sent to CloudWatch Log Groups:
- `/aws/lambda/ticketbottle-payment-webhook-handler-{env}`
- `/aws/lambda/ticketbottle-payment-outbox-processor-{env}`
- `/aws/lambda/ticketbottle-payment-outbox-cleanup-{env}`

### Metrics to Monitor

- **Webhook Handler**: Invocation errors, duration, throttles
- **Outbox Processor**: Processing rate, failure rate, batch size
- **Outbox Cleanup**: Deleted event count, failed event count

### Alarms (Recommended)

1. Webhook Handler error rate > 5%
2. Outbox Processor consecutive failures > 3
3. Outbox Cleanup failed events > 10
4. Lambda cold start duration > 3s

## Troubleshooting

### Webhook Handler Issues

**Problem**: 401/403 from payment provider
- Verify MAC/signature keys are correct
- Check request body format matches provider specs

**Problem**: Timeout
- Check database connection
- Verify VPC security groups allow RDS access

### Outbox Processor Issues

**Problem**: Events not publishing to Kafka
- Verify Kafka brokers are reachable from VPC
- Check Kafka authentication credentials
- Review Kafka topic permissions

**Problem**: High retry count
- Check Kafka cluster health
- Verify network connectivity
- Review CloudWatch logs for specific errors

### Database Connection Issues

**Problem**: Too many connections
- Prisma client is singleton - should reuse connections
- Check Lambda concurrency settings
- Consider using RDS Proxy

## Performance Optimization

### Lambda Layers
- Dependencies layer (~50 MB): Contains node_modules
- Common layer (~5 MB): Shared business logic
- Both layers are cached and reused across invocations

### Cold Start Mitigation
- Provisioned concurrency for webhook handler (if needed)
- Singleton patterns for Prisma and Kafka clients
- Minimal imports in Lambda entry points

### Cost Optimization
- Outbox processor runs every minute but processes in batches
- Cleanup runs daily during off-peak hours
- VPC ENI reuse reduces cold start time

## Security

### Network
- All Lambdas run in private subnets
- Security groups restrict egress to RDS, Kafka, HTTPS
- No public internet access except through NAT Gateway

### Secrets
- Payment provider keys stored in Parameter Store/Secrets Manager
- Database credentials encrypted at rest
- IAM roles follow least privilege principle

### API Security
- API Gateway rate limiting
- Request validation
- CloudWatch logging enabled

## Contributing

1. Create feature branch from `aws-lambda`
2. Write unit tests for new functionality
3. Run `npm test` and `npm run build:layers`
4. Create pull request with detailed description

## License

MIT - Copyright (c) 2025 TicketBottle
