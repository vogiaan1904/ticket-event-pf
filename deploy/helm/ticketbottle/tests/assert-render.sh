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

for overlay in k3s; do
  helm template tb "$CHART" -f "$CHART/values-$overlay.yaml" -f "$SECRETS" > "$actual"
  golden="$HERE/golden/values-$overlay.yaml"
  [ -f "$golden" ] || fail "no golden file for $overlay — run render-golden.sh"
  diff -u "$golden" "$actual" \
    || fail "$overlay drifted. If the change is intended, re-run render-golden.sh in the same commit."
  echo "OK  $overlay renders as expected"
done
# Credentials belong in Secrets: a ConfigMap is readable by anything holding
# `get configmaps`, and `kubectl describe` prints it in full.
for overlay in k3s; do
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
off=$(helm template tb "$CHART" -f "$CHART/values-k3s.yaml" -f "$SECRETS" --set postgres.enabled=false)
grep -q "name: postgres$" <<<"$off" && fail "postgres.enabled=false still renders a postgres object"
grep -q "name: order-service" <<<"$off" || fail "postgres.enabled=false wrongly removed an application workload"
echo "OK  postgres.enabled=false removes only the datastore"

ext=$(helm template tb "$CHART" -f "$CHART/values-k3s.yaml" -f "$SECRETS" \
      --set postgres.enabled=false --set postgres.hosts.payment=pay.example.com \
      --set postgres.hosts.shared=shared.example.com)
grep -q "pg_isready -h postgres " <<<"$ext" && fail "a migration Job still waits on the in-cluster host"
echo "OK  migration Jobs wait on their configured host"

# A DSN now lives in a Secret, so every container that reads a database-backed
# service's config must mount its Secret too -- envFrom is per-container, and a
# Job or sidecar that pulls only the ConfigMap starts with no DATABASE_URL.
for overlay in k3s; do
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
for overlay in k3s; do
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

# Apps a Service routes to sleep before SIGTERM, so kube-proxy drops them before
# they close their port; the gateway's grace period covers its drain deadline.
# Distinct values, so a template that hard-codes 5, 65 or 75 cannot pass.
helm template tb "$CHART" -f "$CHART/values-k3s.yaml" -f "$SECRETS" \
  --set shutdown.preStopSleepSeconds=7 --set shutdown.gatewayDrainSeconds=11 > "$actual"
deployment() { awk -v n="$1" 'BEGIN { RS = "\n---\n" } /kind: Deployment/ && $0 ~ ("\n  name: " n "\n")' "$actual"; }
for d in app-gateway user-service event-service order-service payment-service \
         waitroom-service inventory-service payment-webhook; do
  grep -q "sleep: { seconds: 7 }" <<<"$(deployment "$d")" || fail "$d does not sleep before SIGTERM"
done
for d in order-consumer outbox-relay; do
  grep -q "preStop" <<<"$(deployment "$d")" && fail "$d sleeps, though no Service routes requests to it"
done
grep -q "terminationGracePeriodSeconds: 23$" <<<"$(deployment app-gateway)" \
  || fail "app-gateway's grace period is not sleep + drain + 5"
grep -q "terminationGracePeriodSeconds" <<<"$(deployment event-service)" \
  && fail "event-service has its own grace period, though only the gateway drains long requests"
grep -q 'SHUTDOWN_DRAIN_SECONDS: "11"' "$actual" || fail "gateway-config does not carry the drain deadline"
echo "OK  apps with a Service sleep before SIGTERM; the gateway's grace covers its drain"

# A gateway stop must outlast the longest checkout, or every rollout cuts some.
helm template tb "$CHART" -f "$CHART/values-k3s.yaml" -f "$SECRETS" > "$actual"
create=$(sed -n 's/^  ORDER_CREATE_TIMEOUT: "\([0-9]*\)\([sm]\)"$/\1 \2/p' "$actual")
drain=$(sed -n 's/^  SHUTDOWN_DRAIN_SECONDS: "\([0-9]*\)"$/\1/p' "$actual")
[ -n "$create" ] && [ -n "$drain" ] || fail "cannot read ORDER_CREATE_TIMEOUT (Ns or Nm) or SHUTDOWN_DRAIN_SECONDS"
read -r n unit <<<"$create"; [ "$unit" = m ] && n=$((n * 60))
[ "$drain" -gt "$n" ] || fail "the gateway drains for ${drain}s, but a checkout may run ${n}s"
echo "OK  the gateway's drain outlasts a checkout (${drain}s > ${n}s)"

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

# Door speed is a per-target value, so it must reach the waitroom's ConfigMap.
dr=$(helm template tb "$CHART" -f "$CHART/values-k3s.yaml" -f "$SECRETS" --set waitroom.releaseRate=3)
grep -q 'QUEUE_DEFAULT_RELEASE_RATE: "3"' <<<"$dr" || fail "waitroom.releaseRate does not reach waitroom-config"
echo "OK  the waitroom's door speed comes from values"

# The window to start a checkout bounds every chair that takes no ticket (0027).
wm() { awk '/^metadata: { name: waitroom-config,/{f=1} f && /JWT_EXPIRY:/{print $2; exit}' <<<"$1"; }
[ "$(wm "$dr")" = '"5m"' ] || fail "waitroom-config's JWT_EXPIRY is $(wm "$dr"), want \"5m\""
cw=$(helm template tb "$CHART" -f "$CHART/values-k3s.yaml" -f "$SECRETS" --set waitroom.checkoutWindow=7m)
[ "$(wm "$cw")" = '"7m"' ] || fail "waitroom.checkoutWindow does not reach waitroom-config"
echo "OK  the waitroom's checkout window comes from values"

# A ConfigMap change must roll the app that reads it, and only that app: each app
# carries a digest of its own ConfigMap. Change the waitroom's and compare.
digest() { awk -v d="$2" '/^---/{k=0;f=0} /^kind: Deployment$/{k=1} k && $0=="  name: "d{f=1} f && /checksum\/config:/{print $2; exit}' <<<"$1"; }
r7=$(helm template tb "$CHART" -f "$CHART/values-k3s.yaml" -f "$SECRETS" --set waitroom.releaseRate=7)
r8=$(helm template tb "$CHART" -f "$CHART/values-k3s.yaml" -f "$SECRETS" --set waitroom.releaseRate=8)
[ -n "$(digest "$r7" waitroom-service)" ] || fail "waitroom-service carries no checksum/config"
[ "$(digest "$r7" waitroom-service)" != "$(digest "$r8" waitroom-service)" ] || fail "a waitroom config change does not roll the waitroom"
[ "$(digest "$r7" order-service)" = "$(digest "$r8" order-service)" ] || fail "a waitroom config change rolls order-service too"
echo "OK  a config change rolls the app that reads it, and only that app"

# A migration reaches the database before the code that reads it rolls (0022). On a
# first install it cannot: Postgres itself comes from this chart.
helm template tb "$CHART" -f "$CHART/values-k3s.yaml" -f "$SECRETS" > "$actual"
for svc in user event payment; do
  hook=$(awk -v n="$svc-migrate" 'BEGIN { RS = "\n---\n" } /kind: Job/ && $0 ~ ("\n  name: " n "\n")' "$actual" \
         | sed -n 's/^ *"helm.sh\/hook": *//p')
  case ",$hook," in *,pre-upgrade,*) ;; *) fail "$svc-migrate runs as '$hook': an upgrade rolls new code before its schema";; esac
  case ",$hook," in *,post-install,*) ;; *) fail "$svc-migrate runs as '$hook': a first install never migrates";; esac
done
echo "OK  migrations run before an upgrade's rollout, and after a first install"

echo "all render assertions passed"
