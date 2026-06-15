# TicketBottle Development Environment

This directory contains Docker Compose configurations for local development.

## Two Modes, Two Codebases

The Order service has two versions with **different code**:

| Mode | Database | Code Branch | Image Tag |
|------|----------|-------------|-----------|
| Legacy | MongoDB | `legacy/mongodb` | `ticketbottle-order-*:legacy` |
| AWS | DynamoDB | `main` | `ticketbottle-order-*:aws` |

**Important:** You must build the correct images before running!

## Quick Start

### Step 1: Build Images

**For Legacy Mode (MongoDB):**
```bash
# Checkout legacy branch
cd ../services/order-svc
git checkout legacy/mongodb

# Build legacy images
cd ../development
make build-legacy
```

**For AWS Mode (DynamoDB):**
```bash
# Checkout main branch (with DynamoDB code)
cd ../services/order-svc
git checkout main

# Build AWS images
cd ../development
make build-aws
```

### Step 2: Run Environment

**Legacy Mode:**
```bash
make up-legacy
```

**AWS Mode:**
```bash
make up-aws
```

## Command Reference

### Build Commands

| Command | Description |
|---------|-------------|
| `make build-legacy` | Build Order images from `legacy/mongodb` branch |
| `make build-aws` | Build Order images from current code (DynamoDB) |
| `make list-images` | Show all Order service images |

### Run Commands

| Command | Description |
|---------|-------------|
| `make up-legacy` | Start with MongoDB (requires legacy images) |
| `make up-aws` | Start with DynamoDB (requires AWS images) |
| `make up-infra` | Start infrastructure only (Kafka, Redis, etc.) |
| `make up-localstack` | Start LocalStack only |

### Common Commands

| Command | Description |
|---------|-------------|
| `make down` | Stop all services |
| `make logs` | View all logs |
| `make logs-order` | View Order service logs |
| `make logs-localstack` | View LocalStack logs |
| `make status` | Check container and image status |
| `make clean` | Remove all containers and volumes |

## File Structure

```
development/
├── docker-compose.dev.yml      # Base config (legacy mode)
├── docker-compose.aws.yml      # AWS override (DynamoDB mode)
├── Makefile                    # Build and run commands
├── README.md                   # This file
├── envs/
│   ├── .env.order              # Order service (MongoDB)
│   ├── .env.order.aws          # Order service (DynamoDB)
│   ├── .env.localstack         # LocalStack configuration
│   └── ...                     # Other service configs
└── scripts/
    └── init-aws.sh             # LocalStack init (creates DynamoDB table)
```

## How It Works

### Legacy Mode (`make up-legacy`)

```
docker-compose.dev.yml
        │
        ├── MongoDB container
        ├── ticketbottle-order-api:legacy ──► connects to MongoDB
        └── ticketbottle-order-consumer:legacy
```

### AWS Mode (`make up-aws`)

```
docker-compose.dev.yml + docker-compose.aws.yml
        │
        ├── LocalStack (DynamoDB)
        ├── ticketbottle-order-api:aws ──► connects to DynamoDB
        └── ticketbottle-order-consumer:aws
        
        (MongoDB is excluded)
```

## Image Details

### Legacy Images (MongoDB)
- `ticketbottle-order-api:legacy`
- `ticketbottle-order-consumer:legacy`

Built from `legacy/mongodb` branch with:
- `go.mongodb.org/mongo-driver`
- `bson` tags on models
- MongoDB repository implementation

### AWS Images (DynamoDB)
- `ticketbottle-order-api:aws`
- `ticketbottle-order-consumer:aws`

Built from `main` branch with:
- `github.com/aws/aws-sdk-go-v2`
- `dynamodbav` tags on models
- DynamoDB repository implementation

## Service Endpoints

### Infrastructure
| Service | Port | Description |
|---------|------|-------------|
| Kafka | 9092 | Message broker |
| Kafka UI | 8090 | Kafka management UI |
| Redis (Waitroom) | 6379 | Queue management |
| Redis (Auth) | 6380 | Authentication cache |
| Temporal | 7233 | Workflow orchestration |
| Temporal UI | 8080 | Temporal management UI |
| LocalStack | 4566 | AWS services (AWS mode only) |

### Databases
| Service | Port | Mode |
|---------|------|------|
| PostgreSQL (Event) | 5434 | Both |
| PostgreSQL (Inventory) | 5435 | Both |
| PostgreSQL (Payment) | 5433 | Both |
| PostgreSQL (User) | 5436 | Both |
| MongoDB (Order) | 27017 | Legacy only |
| DynamoDB (Order) | 4566 | AWS only |

### Application Services
| Service | Port | Protocol |
|---------|------|----------|
| API Gateway | 3000 | HTTP |
| User Service | 50052 | gRPC |
| Event Service | 50053 | gRPC |
| Order Service | 50054 | gRPC |
| Payment Service | 50055 | gRPC |
| Waitroom Service | 50056 | gRPC |
| Inventory Service | 50057 | gRPC |

## Troubleshooting

### "Legacy images not found"
```bash
# Build legacy images first
cd ../services/order-svc
git checkout legacy/mongodb
cd ../development
make build-legacy
```

### "AWS images not found"
```bash
# Build AWS images first
cd ../services/order-svc
git checkout main
cd ../development
make build-aws
```

### Order service can't connect to DynamoDB
1. Check LocalStack is healthy: `make status`
2. Check endpoint in `.env.order.aws`: should be `http://localstack:4566`
3. Verify table exists: `make test-dynamodb`

### Switching between modes
```bash
# Stop everything first
make down

# Then start the desired mode
make up-legacy  # or make up-aws
```

### Check which images are being used
```bash
make list-images
# or
docker images | grep ticketbottle-order
```
