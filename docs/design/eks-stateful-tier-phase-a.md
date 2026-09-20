# EKS Stateful Tier — Phase (a): Chart Toggles Implementation Plan

**Goal:** Make the Helm chart able to point each service at an external Postgres host without forking a manifest, and get every database credential out of the ConfigMaps — while kind and k3s keep rendering exactly what they render today.

**Architecture:** Follows the existing `dynamodb.enabled` precedent. A `postgres.enabled` flag gates the in-cluster StatefulSet, and a `postgres.hosts` map gives each service its own host. All hosts default to `postgres`, so every current target is unaffected. Credentials move from the ConfigMaps into per-service Secrets, which `envFrom` already supports.

**Tech Stack:** Helm 3, `helm template` golden-file assertions, bash.

**Design:** `docs/design/eks-stateful-tier.md`

## Status — 2026-09-20

| Task | State | Landed in |
|---|---|---|
| 0 Golden-render harness | **done** | `b72b982` |
| 1 Per-service Postgres host | **done** | `090ba78` |
| 2 Gate the Postgres StatefulSet | **done** | `090ba78` |
| 3 Move the DSNs into Secrets | **done** | `40e3834`, `53f0a09` |
| 4 Migrations wait on the right host | **done** | `090ba78` |
| 5 The EKS overlay opts out | **out of scope** | see spec |

Verify rather than trust this table:

```bash
deploy/helm/ticketbottle/tests/assert-render.sh   # all eight assertions -> passes
```

The credential assertion is no longer opt-in: Task 3 turned it on, so a DSN
returning to a ConfigMap now fails the suite. The Postgres password is now
`required` from the secrets file like every other credential, and the goldens
render from a committed fixture rather than from real secrets.

**Phase (a) is complete.** Revision 27 on the k3s box ran every migration Job
and passed the purchase-flow gate with all four DSNs served from Secrets.

**Commit messages deliberately never name a phase or task** (root `CLAUDE.md`), so
git history cannot answer "where are we". This table and the checkboxes below are
the record; keep them current in the same commit as the work.

## Global Constraints

- **Chart invariant #1:** targets are values overlays, never forked manifests. Do not add a second mechanism for "external datastore" — follow `dynamodb.enabled`.
- **`values-local.yaml` and `values-k3s.yaml` must render byte-identical to today** through Task 2. Task 3 changes them deliberately and re-baselines the golden files in the same commit.
- **No database password may appear in any ConfigMap** once Task 3 lands. This is asserted, not intended.
- **No secret values in committed files.** `deploy/secrets.values.yaml` is gitignored and supplies them. The golden renders are committed, so they are rendered from `tests/fixture-secrets.yaml`; an assertion fails if a value from the real file reaches a golden.
- **Comment budget** (root `CLAUDE.md`): 3 lines inline, 5 on a symbol, 8 for a file header. No paragraphs.
- **Commit messages describe the platform.** No mention of plans, phases, or task numbers.
- Phase (a) creates **no AWS resource.** Only Task 3's runtime check needs a cluster: four services start reading their DSN from a different object, which renders perfectly and can still break. kind is retired here, so that check runs on the k3s box.

## File Structure

| File | Responsibility | Task |
|---|---|---|
| `deploy/helm/ticketbottle/tests/render-golden.sh` | **Create.** Regenerates the golden renders. Run deliberately when a change is meant to alter output. | 0 |
| `deploy/helm/ticketbottle/tests/assert-render.sh` | **Create.** The test: re-renders each overlay and diffs against its golden file, plus the no-password-in-ConfigMap assertion. | 0 |
| `deploy/helm/ticketbottle/tests/fixture-secrets.yaml` | **Create.** Fixed, non-secret inputs for the goldens. The harness renders from this, never from `deploy/secrets.values.yaml`. | 3 |
| `deploy/helm/ticketbottle/tests/golden/values-local.yaml` | **Create.** Committed baseline render. | 0 |
| `deploy/helm/ticketbottle/tests/golden/values-k3s.yaml` | **Create.** Committed baseline render. | 0 |
| `deploy/helm/ticketbottle/values.yaml` | **Modify.** `postgres.enabled`, `postgres.hosts`. | 1, 2 |
| `deploy/helm/ticketbottle/templates/apps/config.yaml` | **Modify.** Host comes from values; DSN and password leave the ConfigMap. | 1, 3 |
| `deploy/helm/ticketbottle/templates/infra/postgres.yaml` | **Modify.** Wrapped in the `enabled` gate. | 2 |
| `deploy/helm/ticketbottle/templates/apps/secrets.yaml` | **Modify.** Adds `user-secrets` and `inventory-secrets`; every service's DSN moves here. | 3 |
| `deploy/helm/ticketbottle/templates/apps/user.yaml` | **Modify.** Wire `.secret` — this workload has none today. | 3 |
| `deploy/helm/ticketbottle/templates/apps/inventory.yaml` | **Modify.** Wire `.secret` — this workload has none today. | 3 |
| `deploy/helm/ticketbottle/templates/infra/temporal.yaml` | **Modify.** `POSTGRES_PWD` comes from a Secret, defined in this file so the infra tier does not depend on the app tier. | 3 |
| `deploy/helm/ticketbottle/templates/apps/migrations.yaml` | **Modify.** `wait-postgres` host comes from values (4); the Jobs mount their service's Secret (3). | 3, 4 |
| `deploy/helm/ticketbottle/templates/apps/outbox-relay.yaml` | **Modify.** Mounts `payment-secrets`; the relay opens its own connection. | 3 |
| `deploy/helm/ticketbottle/templates/apps/payment-events.yaml` | **Modify.** Mounts `payment-secrets`; the webhook opens its own connection. | 3 |
| `deploy/helm/ticketbottle/values-eks.yaml` | **Deferred with phases (b)-(f).** | ~~5~~ |

`values-local.yaml` and `values-k3s.yaml` are **not** modified. That is the point.

---

### Task 0: Golden-render harness

There is no way to assert "renders identically" today, and every later task depends on that assertion. This task builds it and captures the current output as the baseline.

**Files:**
- Create: `deploy/helm/ticketbottle/tests/render-golden.sh`
- Create: `deploy/helm/ticketbottle/tests/assert-render.sh`
- Create: `deploy/helm/ticketbottle/tests/golden/values-local.yaml`
- Create: `deploy/helm/ticketbottle/tests/golden/values-k3s.yaml`

**Interfaces:**
- Consumes: nothing.
- Produces: `deploy/helm/ticketbottle/tests/assert-render.sh`, exit 0 when every overlay matches its golden file and no ConfigMap carries a password. Every later task runs it.

- [x] **Step 1: Write the test harness**

Create `deploy/helm/ticketbottle/tests/assert-render.sh`:

```bash
#!/usr/bin/env bash
# Chart render assertions. Run from anywhere; paths resolve to the chart.
# Fails when an overlay's output drifts from its committed golden file, or
# when a database credential appears in a ConfigMap.
set -euo pipefail
HERE="$(cd "$(dirname "$0")" && pwd)"
CHART="$HERE/.."
SECRETS="$CHART/../../secrets.values.yaml"
fail() { echo "FAIL: $1"; exit 1; }

[ -f "$SECRETS" ] || fail "deploy/secrets.values.yaml missing — run 'make -C deploy secrets-init'"

for overlay in local k3s; do
  out=$(helm template tb "$CHART" -f "$CHART/values-$overlay.yaml" -f "$SECRETS")
  golden="$HERE/golden/values-$overlay.yaml"
  [ -f "$golden" ] || fail "no golden file for $overlay — run render-golden.sh"
  diff -u "$golden" <(printf '%s\n' "$out") \
    || fail "$overlay render drifted from its golden file. If the change is intended, re-run render-golden.sh in the same commit."
  echo "OK  $overlay renders as expected"
done

# Credentials belong in Secrets. A ConfigMap is readable by anything with
# `get configmaps` and is dumped in full by `kubectl describe`.
for overlay in local k3s; do
  cms=$(helm template tb "$CHART" -f "$CHART/values-$overlay.yaml" -f "$SECRETS" \
        | awk '/^kind: ConfigMap$/{f=1} /^---$/{f=0} f')
  echo "$cms" | grep -qiE '(DATABASE_PASSWORD|postgresql://[^:]+:[^@]+@)' \
    && fail "$overlay: a database credential is in a ConfigMap"
  echo "OK  $overlay ConfigMaps carry no credential"
done

echo "all render assertions passed"
```

Create `deploy/helm/ticketbottle/tests/render-golden.sh`:

```bash
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
```

- [x] **Step 2: Run the test to verify it fails**

```bash
chmod +x deploy/helm/ticketbottle/tests/*.sh
deploy/helm/ticketbottle/tests/assert-render.sh
```

Expected: FAIL with `no golden file for local — run render-golden.sh`.

- [x] **Step 3: Capture the baseline**

```bash
deploy/helm/ticketbottle/tests/render-golden.sh
```

- [x] **Step 4: Run the test to verify it passes on renders, and fails on credentials**

```bash
deploy/helm/ticketbottle/tests/assert-render.sh
```

Expected: `OK  local renders as expected`, `OK  k3s renders as expected`, then **FAIL: `local: a database credential is in a ConfigMap`**. That failure is correct and expected — it is the defect Task 3 fixes. Record it; do not fix it here.

- [x] **Step 5: Make the credential assertion opt-in until Task 3**

So the harness is usable as a pass/fail gate in Tasks 1 and 2, guard the credential block:

```bash
# Enabled by Task 3, which moves the DSNs into Secrets. Until then the
# assertion documents a known defect rather than gating the build.
if [ "${ASSERT_NO_CONFIGMAP_CREDS:-0}" = "1" ]; then
```

Wrap the credential `for` loop in that `if`, closing with `fi` before the final echo.

- [x] **Step 6: Verify the gate passes and the assertion still works when asked**

```bash
deploy/helm/ticketbottle/tests/assert-render.sh                            # expect: all render assertions passed
ASSERT_NO_CONFIGMAP_CREDS=1 deploy/helm/ticketbottle/tests/assert-render.sh # expect: FAIL on the credential
```

- [x] **Step 7: Commit**

```bash
git add deploy/helm/ticketbottle/tests
git commit -m "test(helm): assert each overlay's render against a committed baseline

A chart change that alters kind or k3s output is currently invisible until
something breaks on a cluster. The golden renders make drift a diff, and the
credential assertion is wired but off, because the DSNs are still in ConfigMaps."
```

---

### Task 1: Per-service Postgres host

**Files:**
- Modify: `deploy/helm/ticketbottle/values.yaml` (the `postgres:` block, line 32)
- Modify: `deploy/helm/ticketbottle/templates/apps/config.yaml` (every `@postgres:5432` and `DATABASE_HOST`)

**Interfaces:**
- Consumes: `tests/assert-render.sh` from Task 0.
- Produces: `.Values.postgres.hosts.payment`, `.Values.postgres.hosts.inventory`, `.Values.postgres.hosts.shared` — three strings, each a hostname. Tasks 4 and 5 read them.

- [x] **Step 1: Run the test to confirm a clean starting point**

```bash
deploy/helm/ticketbottle/tests/assert-render.sh
```

Expected: `all render assertions passed`.

- [x] **Step 2: Add the hosts map to values.yaml**

In `deploy/helm/ticketbottle/values.yaml`, inside the `postgres:` block, after `password: root`:

```yaml
  # Which host each service's database lives on. All three are the in-cluster
  # Service by default; a target with an external database overrides them.
  hosts:
    payment: postgres
    inventory: postgres
    shared: postgres      # user, event, and Temporal's two databases
```

- [x] **Step 3: Use the hosts in config.yaml**

In `deploy/helm/ticketbottle/templates/apps/config.yaml`, replace each hardcoded host. `user` and `event` take `.shared`, `inventory` takes `.inventory`, `payment` takes `.payment`.

For `user-config`:

```yaml
  DATABASE_URL: postgresql://{{ .Values.postgres.user }}:{{ .Values.postgres.password }}@{{ .Values.postgres.hosts.shared }}:5432/ticketbottle_user
  DATABASE_HOST: {{ .Values.postgres.hosts.shared }}
```

For `event-config`:

```yaml
  DATABASE_URL: postgresql://{{ .Values.postgres.user }}:{{ .Values.postgres.password }}@{{ .Values.postgres.hosts.shared }}:5432/ticketbottle_event
  DATABASE_HOST: {{ .Values.postgres.hosts.shared }}
```

For `inventory-config`:

```yaml
  POSTGRES_URL: postgresql://{{ .Values.postgres.user }}:{{ .Values.postgres.password }}@{{ .Values.postgres.hosts.inventory }}:5432/ticketbottle_inventory?sslmode=disable
```

For `payment-config`:

```yaml
  DATABASE_URL: postgresql://{{ .Values.postgres.user }}:{{ .Values.postgres.password }}@{{ .Values.postgres.hosts.payment }}:5432/ticketbottle_payment
  DATABASE_HOST: {{ .Values.postgres.hosts.payment }}
```

Search for any remaining `@postgres:5432` or `DATABASE_HOST: postgres` in that file and convert it the same way:

```bash
grep -n "@postgres:5432\|DATABASE_HOST: postgres" deploy/helm/ticketbottle/templates/apps/config.yaml
```

Expected after the edit: no output.

- [x] **Step 4: Run the test to verify the render is unchanged**

```bash
deploy/helm/ticketbottle/tests/assert-render.sh
```

Expected: `all render assertions passed`. Because every host defaults to `postgres`, the rendered text is identical. A diff here means a host was mapped to the wrong role.

- [x] **Step 5: Commit**

```bash
git add deploy/helm/ticketbottle/values.yaml deploy/helm/ticketbottle/templates/apps/config.yaml
git commit -m "feat(helm): give each service its own Postgres host value

Every DSN pointed at the single in-cluster Service by name, so a target with
an external database had nowhere to say so. The three roles -- payment,
inventory, and the shared instance behind user, event and Temporal -- are the
failure domains worth separating; all three still resolve to the in-cluster
Service, so every current target renders unchanged."
```

---

### Task 2: Gate the Postgres StatefulSet

**Files:**
- Modify: `deploy/helm/ticketbottle/values.yaml` (the `postgres:` block)
- Modify: `deploy/helm/ticketbottle/templates/infra/postgres.yaml` (whole file)

**Interfaces:**
- Consumes: `.Values.postgres.hosts` from Task 1.
- Produces: `.Values.postgres.enabled`, a boolean. Task 5 sets it false.

- [x] **Step 1: Write the failing test**

Append to `deploy/helm/ticketbottle/tests/assert-render.sh`, before the final `echo`:

```bash
# A target with an external database must render no StatefulSet and no
# postgres Service, and must still render every application workload.
off=$(helm template tb "$CHART" -f "$CHART/values-local.yaml" -f "$SECRETS" \
      --set postgres.enabled=false)
echo "$off" | grep -q "name: postgres$" \
  && fail "postgres.enabled=false still renders a postgres object"
echo "$off" | grep -q "name: order-service" \
  || fail "postgres.enabled=false wrongly removed an application workload"
echo "OK  postgres.enabled=false removes only the datastore"
```

- [x] **Step 2: Run the test to verify it fails**

```bash
deploy/helm/ticketbottle/tests/assert-render.sh
```

Expected: FAIL with `postgres.enabled=false still renders a postgres object` — the flag does not exist yet, so the template ignores it.

- [x] **Step 3: Add the flag and gate the template**

In `values.yaml`, as the first key of the `postgres:` block:

```yaml
postgres:
  # false where the database is external; the StatefulSet, its Service and the
  # init ConfigMap stop rendering and `hosts` below points at the real one.
  enabled: true
```

In `templates/infra/postgres.yaml`, add as the very first line:

```
{{- if .Values.postgres.enabled }}
```

and as the very last line:

```
{{- end }}
```

- [x] **Step 4: Run the test to verify it passes**

```bash
deploy/helm/ticketbottle/tests/assert-render.sh
```

Expected: all four `OK` lines, ending `all render assertions passed`. The golden files are unchanged because `enabled` defaults to `true`.

- [x] **Step 5: Commit**

```bash
git add deploy/helm/ticketbottle/values.yaml deploy/helm/ticketbottle/templates/infra/postgres.yaml deploy/helm/ticketbottle/tests/assert-render.sh
git commit -m "feat(helm): let a target opt out of the in-cluster Postgres

Mirrors dynamodb.enabled, which already lets EKS use the real service instead
of the in-cluster stand-in. Defaults true, so kind and k3s are unaffected."
```

---

### Task 3: Move the DSNs into Secrets

The password is in a ConfigMap, which anything with `get configmaps` can read and `kubectl describe` prints in full. Prisma and the Go services read a full DSN string, so the whole URL moves, not just the password field.

`user-service` and `inventory-service` have **no Secret today** — `templates/apps/secrets.yaml` defines only `event-secrets`, `order-secrets`, `waitroom-secrets`, `payment-secrets` and `gateway-secrets`. Both need one created and wired.

`templates/infra/temporal.yaml:26` also carries the password, as a **literal env value in the Deployment spec** rather than through a ConfigMap. `kubectl get deploy temporal -o yaml` prints it, so it is the same leak by a different route and is fixed in the same task.

**Files:**
- Modify: `deploy/helm/ticketbottle/templates/apps/secrets.yaml`
- Modify: `deploy/helm/ticketbottle/templates/apps/config.yaml`
- Modify: `deploy/helm/ticketbottle/templates/apps/user.yaml`
- Modify: `deploy/helm/ticketbottle/templates/apps/inventory.yaml`
- Modify: `deploy/helm/ticketbottle/templates/infra/temporal.yaml` (the Secret lives here, not in `apps/secrets.yaml`)
- Modify: `deploy/helm/ticketbottle/templates/apps/migrations.yaml` — all three Jobs run `prisma migrate deploy`, which needs `DATABASE_URL`
- Modify: `deploy/helm/ticketbottle/templates/apps/outbox-relay.yaml` — `outbox-relay/src/runtime.ts:58` opens its own `pg.Client`
- Modify: `deploy/helm/ticketbottle/templates/apps/payment-events.yaml` — `lambdas/common/db/kysely.ts:10` throws without it
- Modify: `deploy/helm/ticketbottle/tests/golden/values-local.yaml` (regenerated)
- Modify: `deploy/helm/ticketbottle/tests/golden/values-k3s.yaml` (regenerated)

**Interfaces:**
- Consumes: `.Values.postgres.hosts` from Task 1.
- Produces: Secrets `user-secrets` and `inventory-secrets`; `DATABASE_URL` / `POSTGRES_URL` keys served from Secrets for all four database-backed services.

- [x] **Step 1: Turn the credential assertion on**

In `tests/assert-render.sh`, change the guard added in Task 0 Step 5 to run unconditionally — delete the `if [ "${ASSERT_NO_CONFIGMAP_CREDS:-0}" = "1" ]; then` line and its matching `fi`.

- [x] **Step 2: Run the test to verify it fails**

```bash
deploy/helm/ticketbottle/tests/assert-render.sh
```

Expected: FAIL with `local: a database credential is in a ConfigMap`.

- [x] **Step 3: Move the DSNs**

In `templates/apps/config.yaml`, delete these keys from `user-config`, `event-config` and `payment-config`:

```
DATABASE_URL, DATABASE_USERNAME, DATABASE_PASSWORD
```

and from `inventory-config` delete `POSTGRES_URL`. Leave `DATABASE_HOST`, `DATABASE_PORT` and `DATABASE_NAME` in place — a hostname is configuration, not a credential.

In `templates/apps/secrets.yaml`, add two new Secrets and extend the existing ones. Add to `event-secrets`' `stringData`:

```yaml
  DATABASE_URL: postgresql://{{ .Values.postgres.user }}:{{ .Values.postgres.password }}@{{ .Values.postgres.hosts.shared }}:5432/ticketbottle_event
  DATABASE_USERNAME: {{ .Values.postgres.user }}
  DATABASE_PASSWORD: {{ .Values.postgres.password }}
```

Add to `payment-secrets`' `stringData`:

```yaml
  DATABASE_URL: postgresql://{{ .Values.postgres.user }}:{{ .Values.postgres.password }}@{{ .Values.postgres.hosts.payment }}:5432/ticketbottle_payment
  DATABASE_USERNAME: {{ .Values.postgres.user }}
  DATABASE_PASSWORD: {{ .Values.postgres.password }}
```

Append two new Secrets at the end of the file. The file's last line is the `{{- end }}` closing the `{{- if .Values.apps.enabled }}` guard opened at the top, so the new blocks go immediately above it — the snippet below reproduces that closing line as its final line:

```yaml
---
apiVersion: v1
kind: Secret
metadata: { name: user-secrets, namespace: {{ include "tb.namespace" . }} }
type: Opaque
stringData:
  DATABASE_URL: postgresql://{{ .Values.postgres.user }}:{{ .Values.postgres.password }}@{{ .Values.postgres.hosts.shared }}:5432/ticketbottle_user
  DATABASE_USERNAME: {{ .Values.postgres.user }}
  DATABASE_PASSWORD: {{ .Values.postgres.password }}
---
apiVersion: v1
kind: Secret
metadata: { name: inventory-secrets, namespace: {{ include "tb.namespace" . }} }
type: Opaque
stringData:
  POSTGRES_URL: postgresql://{{ .Values.postgres.user }}:{{ .Values.postgres.password }}@{{ .Values.postgres.hosts.inventory }}:5432/ticketbottle_inventory?sslmode=disable
{{- end }}
```

- [x] **Step 4: Wire the two workloads that had no Secret**

`_appservice.tpl:42` already adds `secretRef` when the caller passes `.secret`. Each workload file is a single `tb.appService` line; add the key exactly as `event.yaml` already does.

Replace the whole of `templates/apps/user.yaml` with:

```
{{ include "tb.appService" (dict "ctx" . "name" "user-service" "image" "ticketbottle/user" "port" 50052 "config" "user-config" "secret" "user-secrets" "svcName" "user-service" "probe" "tcp" "metricsPort" 2112) }}
```

Replace the whole of `templates/apps/inventory.yaml` with:

```
{{ include "tb.appService" (dict "ctx" . "name" "inventory-service" "image" "ticketbottle/inventory" "port" 50057 "config" "inventory-config" "secret" "inventory-secrets" "svcName" "inventory-service" "probe" "tcp" "metricsPort" 2112) }}
```

Confirm both now reference a Secret:

```bash
helm template tb deploy/helm/ticketbottle -f deploy/helm/ticketbottle/values-local.yaml -f deploy/secrets.values.yaml \
  | grep -c "secretRef: { name: user-secrets }"        # expect 1
helm template tb deploy/helm/ticketbottle -f deploy/helm/ticketbottle/values-local.yaml -f deploy/secrets.values.yaml \
  | grep -c "secretRef: { name: inventory-secrets }"   # expect 1
```

- [x] **Step 4b: Take the password out of the Temporal Deployment**

Add a `temporal-secrets` Secret at the top of `templates/infra/temporal.yaml`. It does **not** go in
`apps/secrets.yaml`: that whole file is inside `{{- if .Values.apps.enabled }}`, and `make infra-up`
sets `apps.enabled=false`, so the Deployment would reference a Secret that never renders and the pod
would sit in `CreateContainerConfigError`. Infra must not depend on the app tier.

```yaml
---
apiVersion: v1
kind: Secret
metadata: { name: temporal-secrets, namespace: {{ include "tb.namespace" . }} }
type: Opaque
stringData:
  POSTGRES_PWD: {{ .Values.postgres.password }}
```

In `templates/infra/temporal.yaml`, replace line 26 with a reference:

```yaml
            - name: POSTGRES_PWD
              valueFrom:
                secretKeyRef: { name: temporal-secrets, key: POSTGRES_PWD }
```

Leave line 25 (`POSTGRES_USER`) as a literal — a username is not a credential.

Note the coupling: `temporal-secrets` renders only when `apps.enabled` is true, but the Temporal
Deployment is in the infra tier. `make infra-up` sets `apps.enabled=false`, so confirm Temporal still
renders and starts under infra-only:

```bash
helm template tb deploy/helm/ticketbottle -f deploy/helm/ticketbottle/values-local.yaml \
  -f deploy/secrets.values.yaml --set apps.enabled=false | grep -c "name: temporal-secrets"
```

If that returns 0, move the Secret out of the `apps.enabled` guard rather than making the infra tier
depend on the app tier.

- [x] **Step 5: Re-baseline the golden files**

The render is *meant* to change here, so regenerate in the same commit that causes it:

```bash
deploy/helm/ticketbottle/tests/render-golden.sh
```

- [x] **Step 6: Run the test to verify it passes**

```bash
deploy/helm/ticketbottle/tests/assert-render.sh
```

Expected: `all render assertions passed`, now including both `ConfigMaps carry no credential` lines.

- [x] **Step 7: Verify on a real cluster**

The migrations Job and four services now read their DSN from a different object. This is the one task in the plan that can break at runtime while rendering perfectly.

kind is retired on this machine for disk pressure, so the k3s box is the venue.
It is the only step in this phase that is not free.

```bash
make -C deploy start-ec2-k3s     # the public IP changes on every start
make -C deploy k3s-allow-ip      # the SSH allowlist is pinned to one /32
make -C deploy k3s-kubeconfig
make -C deploy tunnel-ec2-k3s    # blocks; its own terminal
make -C deploy k3s-deploy
make -C deploy k3s-gate2
make -C deploy stop-ec2-k3s
```

A box that has been stopped for a day holds an expired ECR token, so the pods
come back `ErrImagePull` before any of this. `ecr-refresh.timer` fixes it on its
own schedule; `sudo systemctl start ecr-refresh.service` does it now.

Expected: the gate passes. If a service crash-loops on a missing `DATABASE_URL`, its workload is missing the `.secret` wiring from Step 4.

- [x] **Step 8: Commit**

```bash
git add deploy/helm/ticketbottle/templates deploy/helm/ticketbottle/tests
git commit -m "fix(helm): serve database credentials from Secrets, not ConfigMaps

A ConfigMap is readable by anything with 'get configmaps' in the namespace and
kubectl describe prints it in full, so the Postgres password was the one
credential the chart still published. Prisma and the Go services read a whole
DSN, so the URL moves rather than just the password field.

user-service and inventory-service had no Secret at all; both get one."
```

---

### Task 4: Migrations wait on the right host

**Files:**
- Modify: `deploy/helm/ticketbottle/templates/apps/migrations.yaml:29`

**Interfaces:**
- Consumes: `.Values.postgres.hosts` from Task 1.
- Produces: nothing new.

- [x] **Step 1: Write the failing test**

Append to `tests/assert-render.sh`, before the final `echo`:

```bash
# The migration Jobs must wait on the host their own service uses, not on a
# hardcoded in-cluster name that does not exist on an external target.
ext=$(helm template tb "$CHART" -f "$CHART/values-local.yaml" -f "$SECRETS" \
      --set postgres.enabled=false \
      --set postgres.hosts.payment=pay.example.com \
      --set postgres.hosts.shared=shared.example.com)
echo "$ext" | grep -q "pg_isready -h postgres " \
  && fail "a migration Job still waits on the hardcoded in-cluster host"
echo "OK  migration Jobs wait on their configured host"
```

- [x] **Step 2: Run the test to verify it fails**

```bash
deploy/helm/ticketbottle/tests/assert-render.sh
```

Expected: FAIL with `a migration Job still waits on the hardcoded in-cluster host`.

- [x] **Step 3: Parameterise the host**

`migrations.yaml` opens with `{{- range $svc := list "user" "event" "payment" }}`. Only those three have Prisma migration Jobs — `inventory` is Go with GORM AutoMigrate and has none — so `payment` maps to its own host and the other two to `shared`.

Immediately after the `range` line, derive `$host`:

```
{{- $host := $.Values.postgres.hosts.shared }}
{{- if eq $svc "payment" }}{{- $host = $.Values.postgres.hosts.payment }}{{- end }}
```

Then replace line 29's command:

```yaml
          command: ["sh","-c","until pg_isready -h {{ $host }} -U {{ $.Values.postgres.user }}; do echo waiting for postgres; sleep 2; done"]
```

- [x] **Step 4: Run the test to verify it passes**

```bash
deploy/helm/ticketbottle/tests/assert-render.sh
```

Expected: `all render assertions passed`, including the new line. The golden files are unchanged — every host still defaults to `postgres`.

- [x] **Step 5: Commit**

```bash
git add deploy/helm/ticketbottle/templates/apps/migrations.yaml deploy/helm/ticketbottle/tests/assert-render.sh
git commit -m "fix(helm): migration Jobs wait on their own service's database host

The readiness loop was pinned to the in-cluster Service name, so on a target
whose database is external every migration Job would have waited forever on a
host that does not exist there."
```

---

### Task 5: The EKS overlay opts out — OUT OF SCOPE

**Deferred 2026-09-19 with phases (b)–(f).** Committing `payment.rds.invalid` and
its two siblings would leave the EKS overlay pointing at hostnames that resolve
nowhere, for instances nobody is building. Phase (a) ends at Task 4.

The original task follows, unchanged, for whenever phase (b) resumes.

This renders the EKS target against external hosts. The hostnames are placeholders until phase (b) creates the instances and Terraform emits the real endpoints — that is expected, and the overlay is not deployed in this phase.

**Files:**
- Modify: `deploy/helm/ticketbottle/values-eks.yaml`

**Interfaces:**
- Consumes: `.Values.postgres.enabled` and `.Values.postgres.hosts`.
- Produces: the EKS overlay that phase (b) fills in with real endpoints.

- [ ] **Step 1: Write the failing test**

Append to `tests/assert-render.sh`, before the final `echo`:

```bash
# EKS renders no in-cluster database, and no workload may fall back to the
# in-cluster Service name for its DSN.
eks=$(helm template tb "$CHART" -f "$CHART/values-eks.yaml" -f "$SECRETS")
echo "$eks" | grep -q "name: postgres$" \
  && fail "values-eks still renders an in-cluster postgres"
echo "$eks" | grep -q "@postgres:5432" \
  && fail "values-eks has a DSN pointing at the in-cluster Service"
echo "OK  values-eks uses an external database"
```

- [ ] **Step 2: Run the test to verify it fails**

```bash
deploy/helm/ticketbottle/tests/assert-render.sh
```

Expected: FAIL with `values-eks still renders an in-cluster postgres`.

- [ ] **Step 3: Opt the overlay out**

Append to `deploy/helm/ticketbottle/values-eks.yaml`:

```yaml
# Three RDS instances, split by failure domain. Endpoints come from
# `terraform -chdir=terraform/envs/eks output` once phase (b) creates them;
# these placeholders render but do not resolve.
postgres:
  enabled: false
  hosts:
    payment: payment.rds.invalid
    inventory: inventory.rds.invalid
    shared: shared.rds.invalid
```

- [ ] **Step 4: Run the test to verify it passes**

```bash
deploy/helm/ticketbottle/tests/assert-render.sh
```

Expected: `all render assertions passed`, including `values-eks uses an external database`.

- [ ] **Step 5: Confirm the other overlays are still untouched**

```bash
git diff --stat deploy/helm/ticketbottle/values-local.yaml deploy/helm/ticketbottle/values-k3s.yaml
```

Expected: no output. Neither file is modified anywhere in this plan.

- [ ] **Step 6: Commit**

```bash
git add deploy/helm/ticketbottle/values-eks.yaml deploy/helm/ticketbottle/tests/assert-render.sh
git commit -m "feat(helm): point the EKS target at external databases

Three hosts split by failure domain: the money path, the contended inventory
counter, and a shared instance for user, event and Temporal. The endpoints are
placeholders until the instances exist; the target is not deployed until then."
```

---

## Done when

- [x] `deploy/helm/ticketbottle/tests/assert-render.sh` exits 0.
- [x] `git diff` against the phase start shows **no change** to `values-local.yaml` or `values-k3s.yaml`.
- [x] No ConfigMap on any overlay contains `DATABASE_PASSWORD` or a `postgresql://user:pass@` URL.
- [x] The purchase-flow gate passes on the k3s box.

`helm template -f values-eks.yaml` renders no Postgres StatefulSet only once
Task 5 lands, which is deferred.

## Not in this plan

Phases (b) through (f) of the spec — Terraform RDS and private subnets, Secrets Manager and the CSI driver, migrations against three live endpoints, Redpanda RF=3, and the Gate 4b guard. Each gets its own plan. Phase (a) stands alone: it costs nothing, touches no AWS resource, and leaves every existing target rendering as it does today.
