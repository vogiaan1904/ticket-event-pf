# TicketBottle V2 — root orchestration.
# Service code lives under services/<name>-svc; local infra/compose under development/.
# See CLAUDE.md for the full picture.

.PHONY: help proto proto-go proto-ts

help:
	@echo "TicketBottle V2"
	@echo "  make proto      Regenerate ALL gRPC stubs from the single source of truth (proto/)"
	@echo "  make proto-go   Regenerate Go stubs only (order, inventory, waitroom)"
	@echo "  make proto-ts   Regenerate TS stubs only (api-gateway, event, user, payment)"
	@echo ""
	@echo "  Local dev (compose) lives in development/ — cd development && make up-aws"

# ---- Proto: edit contracts in proto/, then regenerate every consumer ----
# There is ONE source of truth: the root proto/ directory. Do not reintroduce
# per-service .proto copies (protos/, src/protos/, protos-submodule/).
proto: proto-go proto-ts

proto-go:
	$(MAKE) -C services/order-svc protoc-all
	$(MAKE) -C services/inventory-svc protoc-all
	$(MAKE) -C services/waitroom-svc protoc-all

proto-ts:
	cd services/api-gateway && npm run update:proto
	cd services/event-svc && npm run update:proto
	cd services/user-svc && npm run update:proto
	cd services/payment-svc && npm run update:proto
