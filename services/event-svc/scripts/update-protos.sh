#!/usr/bin/env bash

set -euo pipefail

# Resolve repo root
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
cd "${REPO_ROOT}"

echo "[protos] Ensuring submodule is initialized..."
if [ ! -d "protos/.git" ]; then
  git submodule update --init --recursive protos
fi

echo "[protos] Updating submodule to latest remote..."
git submodule update --remote --recursive protos

echo "[protos] Copying .proto files to src/protos directory..."
mkdir -p src/protos
cp -f protos/*.proto src/protos/


echo "[protos] Generating TypeScript files from .proto definitions..."
npm run proto:all

echo "[protos] Done."
