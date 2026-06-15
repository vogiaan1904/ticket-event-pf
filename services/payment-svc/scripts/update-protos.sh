#!/usr/bin/env bash
set -euo pipefail

# Root proto/ is the single source of truth. This service keeps a synced copy in
# src/protos/ because the NestJS gRPC transport loads .proto at runtime (protoPath)
# and nest-cli copies src/protos/** into dist as build assets. So: sync the copy
# from root, then regenerate the TypeScript stubs (generated from root proto/).

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SERVICE_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
cd "${SERVICE_ROOT}"

echo "[protos] Syncing contracts from root proto/ -> src/protos/ ..."
mkdir -p src/protos
cp -f ../../proto/*.proto src/protos/

echo "[protos] Regenerating TypeScript stubs ..."
npm run proto:all

echo "[protos] Done."
