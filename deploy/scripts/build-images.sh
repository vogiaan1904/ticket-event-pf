#!/usr/bin/env bash
set -euo pipefail
CLUSTER=ticketbottle
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"   # repo root
cd "$ROOT"

build() { # <tag> <context> <dockerfile> [target]
  local tag=$1 ctx=$2 df=$3 target=${4:-}
  echo "==> build $tag"
  if [ -n "$target" ]; then
    docker build -t "$tag" -f "$df" --target "$target" "$ctx"
  else
    docker build -t "$tag" -f "$df" "$ctx"
  fi
  kind load docker-image "$tag" --name "$CLUSTER"
  # Disk-frugal: once the image lives in the kind node, the host copy is redundant.
  # Dropping it (and the build cache below) keeps kind's shared Docker VM disk from
  # filling up on this polyglot build. Rebuild is by code change anyway.
  docker image rm "$tag" >/dev/null 2>&1 || true
}

# Runtime images (Prisma *-migrate images are built by build-migrate-images.sh,
# during infra-up, so schema migrations don't depend on the app tier being built)
build ticketbottle/user:local            services/user-svc           services/user-svc/Dockerfile
build ticketbottle/event:local           services/event-svc          services/event-svc/Dockerfile
build ticketbottle/payment:local         services/payment-svc        services/payment-svc/Dockerfile
build ticketbottle/inventory:local       services/inventory-svc      services/inventory-svc/Dockerfile
build ticketbottle/waitroom:local        services/waitroom-svc       services/waitroom-svc/Dockerfile
build ticketbottle/order-api:local       services/order-svc          services/order-svc/cmd/api/Dockerfile
build ticketbottle/order-consumer:local  services/order-svc          services/order-svc/cmd/consumer/Dockerfile
build ticketbottle/gateway:local         services/api-gateway        services/api-gateway/Dockerfile

# Payment-events adapter (Phase 0B-2): outbox->Kafka publisher + simulated webhook.
build ticketbottle/payment-events:local  deploy/adapters/payment-events  deploy/adapters/payment-events/Dockerfile

# Reclaim the build cache this run generated (npm ci / go build layers).
docker builder prune -f >/dev/null 2>&1 || true

echo "RUNTIME IMAGES BUILT AND LOADED"
