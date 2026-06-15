

protoc-all:
	$(MAKE) protoc PROTO=../../proto/inventory.proto OUT_DIR=pkg/grpc/inventory

protoc:
	protoc --go_out=$(OUT_DIR) --go_opt=paths=source_relative \
	--go-grpc_out=$(OUT_DIR) --go-grpc_opt=paths=source_relative \
	-I=../../proto $(PROTO)

update-proto:

	@echo "Regenerating proto code..."
	make protoc-all

	@echo "Proto code regenerated."

run:
	go run cmd/api/main.go