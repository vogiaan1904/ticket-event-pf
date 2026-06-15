.PHONY: help init docker-up docker-down run test lint clean

help: ## Display this help screen
	@grep -E '^[a-zA-Z_-]+:.*?## .*$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-30s\033[0m %s\n", $1, $2}'

init: ## Initialize project
	@echo "Installing dependencies..."
	go mod download
	go mod tidy
	@echo "Creating .env file..."
	@if [ ! -f .env ]; then cp .env.example .env; fi
	@echo "Making scripts executable..."
	chmod +x scripts/*.sh
	@echo "Done!"

docker-up: ## Start Docker containers
	docker-compose up -d
	@echo "Waiting for services to be ready..."
	@sleep 10
	@echo "Creating Kafka topics..."
	@bash scripts/create-kafka-topics.sh
	@echo "Services are ready!"
	@echo "Redis: localhost:6379"
	@echo "Redis Commander: http://localhost:8081"
	@echo "Kafka: localhost:9092"
	@echo "Kafka UI: http://localhost:8082"

docker-down: ## Stop Docker containers
	docker-compose down

docker-logs: ## Show Docker logs
	docker-compose logs -f

kafka-topics-create: ## Create Kafka topics
	@bash scripts/create-kafka-topics.sh

kafka-topics-list: ## List Kafka topics
	docker exec waitroom-kafka kafka-topics --list --bootstrap-server localhost:9092

kafka-console-producer: ## Start Kafka console producer (usage: make kafka-console-producer TOPIC=QUEUE_READY)
	docker exec -it waitroom-kafka kafka-console-producer --bootstrap-server localhost:9092 --topic $(TOPIC)

kafka-console-consumer: ## Start Kafka console consumer (usage: make kafka-console-consumer TOPIC=QUEUE_READY)
	docker exec -it waitroom-kafka kafka-console-consumer --bootstrap-server localhost:9092 --topic $(TOPIC) --from-beginning

run: ## Run the application
	go run cmd/api/main.go

build: ## Build the application
	go build -o bin/waitroom-service cmd/api/main.go

test: ## Run tests
	go test -v -race -coverprofile=coverage.out ./...

test-coverage: test ## Run tests with coverage report
	go tool cover -html=coverage.out

lint: ## Run linter
	@which golangci-lint > /dev/null || (echo "Installing golangci-lint..." && go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest)
	golangci-lint run ./...

clean: ## Clean build artifacts
	rm -rf bin/
	rm -f coverage.out

redis-cli: ## Connect to Redis CLI
	docker exec -it waitroom-redis redis-cli

redis-flush: ## Flush all Redis data (dangerous!)
	docker exec -it waitroom-redis redis-cli FLUSHALL

dev: docker-up run ## Start development environment

protoc-all:
	$(MAKE) protoc PROTO=protos-submodule/waitroom.proto OUT_DIR=protogen/waitroom
	$(MAKE) protoc PROTO=protos-submodule/event.proto OUT_DIR=protogen/event

protoc:
	protoc --go_out=$(OUT_DIR) --go_opt=paths=source_relative \
	--go-grpc_out=$(OUT_DIR) --go-grpc_opt=paths=source_relative \
	-I=protos-submodule $(PROTO)

update-proto:
	@echo "Updating git submodule..."
	git submodule update --remote --recursive protos-submodule

	@echo "Regenerating proto code..."
	make protoc-all

	@echo "Proto code regenerated."