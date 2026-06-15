

protoc-all:
	$(MAKE) protoc PROTO=protos-submodule/inventory.proto OUT_DIR=pkg/grpc/inventory
	$(MAKE) protoc-event

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

run:
	go run cmd/api/main.go