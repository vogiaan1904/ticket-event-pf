#!/usr/bin/env bash
# Fails when an overlay's render drifts from its committed golden file.
set -euo pipefail
HERE="$(cd "$(dirname "$0")" && pwd)"
CHART="$HERE/.."
SECRETS="$CHART/../../secrets.values.yaml"
fail() { echo "FAIL: $1"; exit 1; }

[ -f "$SECRETS" ] || fail "deploy/secrets.values.yaml missing — run 'make -C deploy secrets-init'"

# Render to a file, as render-golden.sh does: $(...) would strip helm's
# trailing blank line and every diff would report it.
actual="$(mktemp)"
trap 'rm -f "$actual"' EXIT

for overlay in local k3s; do
  helm template tb "$CHART" -f "$CHART/values-$overlay.yaml" -f "$SECRETS" > "$actual"
  golden="$HERE/golden/values-$overlay.yaml"
  [ -f "$golden" ] || fail "no golden file for $overlay — run render-golden.sh"
  diff -u "$golden" "$actual" \
    || fail "$overlay drifted. If the change is intended, re-run render-golden.sh in the same commit."
  echo "OK  $overlay renders as expected"
done
# Credentials belong in Secrets: a ConfigMap is readable by anything holding
# `get configmaps`, and `kubectl describe` prints it in full.
for overlay in local k3s; do
  cms=$(helm template tb "$CHART" -f "$CHART/values-$overlay.yaml" -f "$SECRETS" \
        | awk '/^kind: ConfigMap$/{f=1} /^---$/{f=0} f')
  if printf '%s' "$cms" | grep -qiE '(DATABASE_PASSWORD|postgresql://[^:]+:[^@]+@)'; then
    fail "$overlay: a database credential is in a ConfigMap"
  fi
  echo "OK  $overlay ConfigMaps carry no credential"
done

# An external-database target renders no datastore and still renders every
# application workload, and each migration Job waits on its own service's host.
off=$(helm template tb "$CHART" -f "$CHART/values-local.yaml" -f "$SECRETS" --set postgres.enabled=false)
printf '%s' "$off" | grep -q "name: postgres$" && fail "postgres.enabled=false still renders a postgres object"
printf '%s' "$off" | grep -q "name: order-service" || fail "postgres.enabled=false wrongly removed an application workload"
echo "OK  postgres.enabled=false removes only the datastore"

ext=$(helm template tb "$CHART" -f "$CHART/values-local.yaml" -f "$SECRETS" \
      --set postgres.enabled=false --set postgres.hosts.payment=pay.example.com \
      --set postgres.hosts.shared=shared.example.com)
printf '%s' "$ext" | grep -q "pg_isready -h postgres " && fail "a migration Job still waits on the in-cluster host"
echo "OK  migration Jobs wait on their configured host"

# A DSN now lives in a Secret, so every container that reads a database-backed
# service's config must mount its Secret too -- envFrom is per-container, and a
# Job or sidecar that pulls only the ConfigMap starts with no DATABASE_URL.
for overlay in local k3s; do
  helm template tb "$CHART" -f "$CHART/values-$overlay.yaml" -f "$SECRETS" > "$actual"
  awk '
    /configMapRef: \{ name: (user|event|payment|inventory)-config \}/ {
      match($0, /name: [a-z]+-config/); svc = substr($0, RSTART + 6, RLENGTH - 13)
      getline nxt
      if (nxt !~ ("secretRef: \\{ name: " svc "-secrets \\}")) { print svc; bad = 1 }
    }
    END { exit bad ? 1 : 0 }
  ' "$actual" > "$actual.bad" \
    || fail "$overlay: a container reads $(sort -u "$actual.bad" | tr '\n' ' ')config without the matching Secret"
  echo "OK  $overlay pairs every database config with its Secret"
done

echo "all render assertions passed"
