protoc-all:
	$(MAKE) protoc PROTO=../../proto/inventory.proto OUT_DIR=pkg/grpc/inventory
	$(MAKE) protoc PROTO=../../proto/event.proto OUT_DIR=pkg/grpc/event
	$(MAKE) protoc PROTO=../../proto/payment.proto OUT_DIR=pkg/grpc/payment
	$(MAKE) protoc PROTO=../../proto/order.proto OUT_DIR=pkg/grpc/order

protoc:
	protoc --go_out=$(OUT_DIR) --go_opt=paths=source_relative \
	--go-grpc_out=$(OUT_DIR) --go-grpc_opt=paths=source_relative \
	-I=../../proto $(PROTO)

update-proto:

	@echo "Regenerating proto code..."
	make protoc-all

	@echo "Proto code regenerated."

run-api: ## Run the application
	go run cmd/api/main.go

run-consumer: ## Run the consumer
	go run cmd/consumer/main.go