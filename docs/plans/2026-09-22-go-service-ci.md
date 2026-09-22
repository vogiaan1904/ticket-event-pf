# Go service CI Implementation Plan

**Status: open 2026-09-22.**

**Goal:** Run the three Go services' test suites on every push. 34 test files
covering the saga orchestrator, the oversell guard and the queue are in the tree
today and no workflow executes any of them.

**Architecture:** One workflow, `go-tests.yml`. Three jobs, one per service,
each declaring the datastore its suite actually needs as a service container.

**Tech Stack:** GitHub Actions, `actions/setup-go@v5`, Go 1.25.

**Spec:** none. Derived from a read of `services/{order,inventory,waitroom}-svc`
and `.github/workflows/` on 2026-09-22, and from the backlog reconciliation in
`docs/design/eks-stateful-tier.md`.

## What is wrong today

**1. No workflow runs Go tests.** `.github/workflows/` holds `build-push-ecr.yml`,
`chart-assertions.yml` and `ts-tests.yml`. None invokes `go test`.

**2. These are integration suites, and two of the three harnesses know it.**
`order-svc` and `inventory-svc` both gate on a reachable datastore and branch on
`CI`:

```go
if os.Getenv("CI") != "" {
    t.Fatalf("CI requires a reachable test dynamodb (%s): %v", endpoint, err)
}
t.Skipf("skipping: cannot reach test dynamodb (%s): %v", endpoint, err)
```

Skip locally, fail in CI — so the suite can never report success having asserted
nothing. A workflow that runs `go test` on a bare runner therefore goes red, and
correctly so.

| Service | Test files | Needs | Read from |
|---|---|---|---|
| `order-svc` | 14 | DynamoDB Local on `:8000` | `TEST_DYNAMO_ENDPOINT` |
| `inventory-svc` | 14 | Postgres on `:5435`, db `ticketbottle_inventory_test`, `root`/`root` | `TEST_POSTGRES_URL` |
| `waitroom-svc` | 6 | Redis | `WAITROOM_TEST_REDIS_ADDR` |

**3. `waitroom-svc`'s gate is not `CI`-aware.** Its nine Redis tests call a bare
`t.Skip` when `WAITROOM_TEST_REDIS_ADDR` is unset. It is the one service whose
suite passes on a bare runner — by asserting nothing. Adding it to CI without
fixing the gate buys a green check that means nothing.

**4. `pull_request.paths` omits the workflow file in both existing workflows.**
`ts-tests.yml` and `chart-assertions.yml` list themselves under `push.paths` but
not under `pull_request.paths`, so a pull request changing only a workflow does
not run it.

## Decisions

| Decision | Call | Why |
|---|---|---|
| Job shape | **Three explicit jobs** | Each needs a different service container; a matrix cannot vary `services` by matrix value |
| Toolchain pin | **`go-version-file: <svc>/go.mod`** | The services pin 1.25.0, 1.25.0 and 1.25.1; go.mod is already that pin |
| Race detector | **On** | These three own the saga, the oversell guard and the queue. Their bugs are concurrency-shaped, and `-race` costs about ten seconds |
| Postgres port | **Published on 5435** | The port `docker-compose.dev.yml` uses and the harness defaults to, so `TEST_POSTGRES_URL` stays unset in CI |
| DynamoDB readiness | **A TCP wait step, not a healthcheck** | `amazon/dynamodb-local` carries no shell tools for `--health-cmd`, and the harness retries `DescribeTable` only three times |
| `waitroom` gate | **Made `CI`-aware in this plan** | Otherwise its green check asserts nothing, which is the failure this whole change exists to prevent |

### A reversal, recorded

The first version of this workflow was a **matrix over the three service
directories**, on the reasoning that the job bodies were identical. They are
not: each service needs a different datastore, and `services:` cannot be varied
by matrix value. The matrix was chosen against a premise — "unit tests only,
they mock every collaborator" — that was carried over from `ts-tests.yml` and
never checked against these three services. It was wrong.

The first run proved it: `order-svc` and `inventory-svc` failed on 24 tests, all
with `connection refused`.

### Why the local run did not catch it

`go test ./...` was run against all three services before the workflow was
written, and all three were green with zero skips. That evidence was real and
unrepresentative: this machine had the dev datastores up — Docker publishing
Postgres on 5435 and DynamoDB Local on 8000 — so the suites connected. The local
pass proved the tests work *with* their datastores. It said nothing about
whether a runner needs them.

**The rule:** a suite that skips or connects based on the environment is not
measured by running it in yours. Check what it does on a bare host, or read its
harness, before claiming what CI will do.

## File structure

| File | Change | Task |
|---|---|---|
| `.github/workflows/go-tests.yml` | create | 1 |
| `services/waitroom-svc/internal/repository/redis/queue_repository_test.go` | `CI`-aware gate | 1 |
| `.github/workflows/ts-tests.yml` | add the workflow file to `pull_request.paths` | 2 |
| `.github/workflows/chart-assertions.yml` | same | 2 |

---

### Task 1: The three Go services run their tests, against real datastores, on every push

**Files:**
- Create: `.github/workflows/go-tests.yml`
- Modify: `services/waitroom-svc/internal/repository/redis/queue_repository_test.go`

**Step 1 — the workflow.** Three jobs named for their services, each with one
service container: `amazon/dynamodb-local:2.5.2` on 8000, `postgres:15-alpine`
published on 5435 with `POSTGRES_DB=ticketbottle_inventory_test`, and
`redis:7-alpine` on 6379 with `WAITROOM_TEST_REDIS_ADDR` set in the job env.

**Step 2 — close the waitroom gate.** Mirror the other two harnesses: fail when
`CI` is set and the address is not, skip otherwise.

**Verification:**
- `CI=true go test ./internal/repository/redis/` fails with the new message
- `CI=true WAITROOM_TEST_REDIS_ADDR=... go test ./internal/repository/redis/`
  passes against a throwaway `redis:7-alpine`
- the run reports `success` for all three jobs via
  `gh run view <id> --json conclusion` — not `gh run watch --exit-status`, which
  returns a spurious non-zero when stdout is redirected

Both gate checks were run before pushing: red without Redis, green with it.

---

### Task 2: A workflow change is tested by the pull request that makes it

**Files:**
- Modify: `.github/workflows/ts-tests.yml`
- Modify: `.github/workflows/chart-assertions.yml`

Add each workflow's own path to its `pull_request.paths`, matching `push.paths`
and what `go-tests.yml` does by construction.

**Verification:**
- every workflow with a `pull_request.paths` list names itself in it
  (`build-push-ecr.yml` has no such list, so there is nothing to assert)

**Expected:** no behaviour change on `dev` or `main`.

---

## Found, not fixed

- **`order-svc` does not vendor its modules**; `inventory-svc` and
  `waitroom-svc` do. It is the one Go service whose CI needs the network. Worth
  converging one way or the other.
- **CI now runs the integration suites but not against the real datastores.**
  DynamoDB Local is not DynamoDB and `postgres:15-alpine` is not RDS. The suites
  assert logic, not provider behaviour; the purchase-flow gates remain the only
  thing that exercises the real ones.

## Outcome

_To be filled on completion._
