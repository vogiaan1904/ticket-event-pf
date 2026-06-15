# TicketBottle Development Environment
# =====================================
#
# Two modes:
# - docker-compose.dev.yml : MongoDB (original)
# - docker-compose.aws.yml : DynamoDB via LocalStack

.PHONY: help up down up-aws down-aws logs clean

help:
	@echo "TicketBottle Development Commands"
	@echo "=================================="
	@echo ""
	@echo "MONGODB MODE (Original):"
	@echo "  make up             - Start with MongoDB"
	@echo "  make down           - Stop MongoDB mode"
	@echo ""
	@echo "AWS MODE (DynamoDB):"
	@echo "  make up-aws         - Start with DynamoDB (LocalStack)"
	@echo "  make down-aws       - Stop AWS mode"
	@echo ""
	@echo "COMMON:"
	@echo "  make logs           - View logs (use COMPOSE=aws for AWS mode)"
	@echo "  make status         - Check container status"
	@echo "  make clean          - Remove all containers and volumes"
	@echo ""

# =====================================
# MONGODB MODE
# =====================================

up:
	@echo "Starting TicketBottle (MongoDB Mode)..."
	docker-compose -f docker-compose.dev.yml up -d

down:
	docker-compose -f docker-compose.dev.yml down

# =====================================
# AWS MODE (DynamoDB via LocalStack)
# =====================================

up-aws:
	@echo "Starting TicketBottle (AWS Mode - DynamoDB)..."
	docker-compose -f docker-compose.aws.yml up -d
	@echo ""
	@echo "Waiting for LocalStack to initialize..."
	@sleep 10
	@docker exec ticketbottle-localstack awslocal dynamodb list-tables 2>/dev/null || \
		echo "LocalStack still initializing, check 'make logs-localstack'"

down-aws:
	docker-compose -f docker-compose.aws.yml down

# =====================================
# LOGS
# =====================================

logs:
	docker-compose -f docker-compose.dev.yml logs -f

logs-aws:
	docker-compose -f docker-compose.aws.yml logs -f

logs-order:
	docker logs -f ticketbottle-order-service

logs-consumer:
	docker logs -f ticketbottle-order-consumer

logs-localstack:
	docker logs -f ticketbottle-localstack

# =====================================
# STATUS & CLEAN
# =====================================

status:
	@echo "Container Status:"
	@docker ps --format "table {{.Names}}\t{{.Status}}\t{{.Ports}}" | grep ticketbottle || echo "No containers running"

clean:
	docker-compose -f docker-compose.dev.yml down -v --remove-orphans
	docker-compose -f docker-compose.aws.yml down -v --remove-orphans
	docker system prune -f

# =====================================
# TESTING
# =====================================

test-dynamodb:
	@docker exec ticketbottle-localstack awslocal dynamodb describe-table --table-name ticketbottle-orders

shell-localstack:
	docker exec -it ticketbottle-localstack bash
