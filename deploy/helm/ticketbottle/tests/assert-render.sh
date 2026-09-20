#!/usr/bin/env bash
# Fails when an overlay's render drifts from its committed golden file.
set -euo pipefail
HERE="$(cd "$(dirname "$0")" && pwd)"
CHART="$HERE/.."
SECRETS="$HERE/fixture-secrets.yaml"
fail() { echo "FAIL: $1"; exit 1; }

[ -f "$SECRETS" ] || fail "tests/fixture-secrets.yaml missing"

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
  if grep -qiE '(DATABASE_PASSWORD|postgresql://[^:]+:[^@]+@)' <<<"$cms"; then
    fail "$overlay: a database credential is in a ConfigMap"
  fi
  echo "OK  $overlay ConfigMaps carry no credential"
done

# grep -q closes the pipe at the first match, which fails a `printf |` producer
# under pipefail. Match against a here-string, never a pipeline.
# An external-database target renders no datastore and still renders every
# application workload, and each migration Job waits on its own service's host.
off=$(helm template tb "$CHART" -f "$CHART/values-local.yaml" -f "$SECRETS" --set postgres.enabled=false)
grep -q "name: postgres$" <<<"$off" && fail "postgres.enabled=false still renders a postgres object"
grep -q "name: order-service" <<<"$off" || fail "postgres.enabled=false wrongly removed an application workload"
echo "OK  postgres.enabled=false removes only the datastore"

ext=$(helm template tb "$CHART" -f "$CHART/values-local.yaml" -f "$SECRETS" \
      --set postgres.enabled=false --set postgres.hosts.payment=pay.example.com \
      --set postgres.hosts.shared=shared.example.com)
grep -q "pg_isready -h postgres " <<<"$ext" && fail "a migration Job still waits on the in-cluster host"
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

# A Deployment that mounts a Secret needs a digest of it in the pod template.
# Without one, rotating a Secret changes no Deployment, helm rolls nothing, and
# the pods keep serving the old value until something else happens to restart them.
for overlay in local k3s; do
  helm template tb "$CHART" -f "$CHART/values-$overlay.yaml" -f "$SECRETS" > "$actual"
  awk 'BEGIN { RS = "\n---\n" }
    /kind: Deployment/ && /secretRef/ && !/checksum\/secret/ {
      match($0, /name: [a-z-]+/); print substr($0, RSTART + 6, RLENGTH - 6); bad = 1
    }
    END { exit bad ? 1 : 0 }
  ' "$actual" > "$actual.nodigest" \
    || fail "$overlay: $(tr '\n' ' ' < "$actual.nodigest")mounts a Secret with no checksum annotation"
  echo "OK  $overlay rolls its pods when a Secret changes"
done

# The goldens are committed, so a real secret reaching one is published. Skipped
# where the developer's file is absent, as in CI.
REAL="$CHART/../../secrets.values.yaml"
if [ -f "$REAL" ]; then
  while read -r val; do
    [ ${#val} -ge 12 ] || continue
    grep -qF -- "$val" "$HERE"/golden/*.yaml \
      && fail "a value from deploy/secrets.values.yaml is in a golden file — render them with fixture-secrets.yaml"
  done < <(sed -n 's/^[[:space:]]*[A-Za-z]*:[[:space:]]*"\(.*\)"[[:space:]]*$/\1/p' "$REAL")
  echo "OK  goldens carry no real secret"
fi

echo "all render assertions passed"
