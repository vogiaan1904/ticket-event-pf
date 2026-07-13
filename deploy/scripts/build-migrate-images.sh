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
  docker image rm "$tag" >/dev/null 2>&1 || true
}

# Prisma migration images (builder stage: has the prisma CLI + migrations).
# Built separately from the runtime images so infra-up can run schema
# migrations standalone, before the app tier's images exist.
build ticketbottle/user-migrate:local    services/user-svc     services/user-svc/Dockerfile    builder
build ticketbottle/event-migrate:local   services/event-svc    services/event-svc/Dockerfile   builder
build ticketbottle/payment-migrate:local services/payment-svc  services/payment-svc/Dockerfile builder

docker builder prune -f >/dev/null 2>&1 || true

echo "MIGRATE IMAGES BUILT AND LOADED"
