#!/usr/bin/env bash
# Rewrites the golden renders. Run ONLY when output is meant to change, and
# commit the result alongside the change that caused it.
set -euo pipefail
HERE="$(cd "$(dirname "$0")" && pwd)"
CHART="$HERE/.."
SECRETS="$CHART/../../secrets.values.yaml"
mkdir -p "$HERE/golden"
for overlay in local k3s; do
  helm template tb "$CHART" -f "$CHART/values-$overlay.yaml" -f "$SECRETS" \
    > "$HERE/golden/values-$overlay.yaml"
  echo "wrote golden/values-$overlay.yaml"
done