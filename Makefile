# TicketBottle V2 — root orchestration.
# Service code lives under services/<name>-svc; cluster operations under deploy/.
# See CLAUDE.md for the full picture.

.PHONY: help proto proto-go proto-ts

help:
	@echo "TicketBottle V2"
	@echo "  make proto      Regenerate ALL gRPC stubs from the single source of truth (proto/)"
	@echo "  make proto-go   Regenerate Go stubs only (order, inventory, waitroom)"
	@echo "  make proto-ts   Regenerate TS stubs only (api-gateway, event, user, payment)"
	@echo ""
	@echo "  Full stack runs on k3s — make -C deploy start-ec2-k3s k3s-deploy k3s-gate2"

# ---- Proto: edit contracts in proto/, then regenerate every consumer ----
# There is ONE source of truth: the root proto/ directory. Do not reintroduce
# protos/ or protos-submodule/; each TS service's src/protos/ is synced from here.
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
