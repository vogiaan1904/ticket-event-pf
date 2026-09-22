# Go service CI Implementation Plan

**Status: open 2026-09-22.**

**Goal:** Run the three Go services' existing test suites on every push. 34 test
files covering the saga orchestrator, the oversell guard and the queue are in
the tree today and no workflow executes any of them.

**Architecture:** One workflow, `go-tests.yml`, mirroring `ts-tests.yml`: unit
tests only, every collaborator mocked, no database or broker on the runner. A
matrix over the three service directories rather than three copied jobs — the
steps are identical, and `build-push-ecr.yml` is the repo's precedent for a
matrix over services.

**Tech Stack:** GitHub Actions, `actions/setup-go@v5`, Go 1.25.

**Spec:** none. Derived from a read of `services/{order,inventory,waitroom}-svc`
and `.github/workflows/` on 2026-09-22, and from the measurement in
`docs/design/eks-stateful-tier.md`'s backlog reconciliation.

## What is wrong today

**1. No workflow runs Go tests.** `.github/workflows/` holds `build-push-ecr.yml`,
`chart-assertions.yml` and `ts-tests.yml`. None invokes `go test`. A repo-wide
grep for `go-version`, `go test` and `golang` across the workflows returns
nothing.

The suites are not stubs:

| Service | Test files | `go test ./...` | `go test -race ./...` |
|---|---|---|---|
| `order-svc` | 14 | green, ~11s | green, ~17s |
| `inventory-svc` | 14 | green, ~7s | green, ~17s |
| `waitroom-svc` | 6 | green, ~8s | green, ~13s |

All three were run locally on 2026-09-22 before this plan was written. Nothing
here is expected to go red on the first run; if it does, the cause is the runner
environment, not the tests.

**2. `pull_request.paths` omits the workflow file in both existing workflows.**
`ts-tests.yml` and `chart-assertions.yml` list the workflow itself under
`push.paths` but not under `pull_request.paths`, so a pull request that changes
only the workflow does not run it. The new file must not repeat that, and the
two existing ones are a one-line fix each — left inconsistent they are drift.

## Decisions

| Decision | Call | Why |
|---|---|---|
| Job shape | **Matrix over three services** | The steps are identical; `ts-tests.yml` copies its job four times only because Prisma services need an extra step |
| `fail-fast` | **`false`** | One service failing must not hide the other two |
| Toolchain pin | **`go-version-file: <svc>/go.mod`** | The services pin 1.25.0, 1.25.0 and 1.25.1; go.mod is already that pin, so nothing has to be kept in sync by hand |
| Race detector | **On** | These three own the saga, the oversell guard and the queue. Their bugs are concurrency-shaped, and `-race` costs about ten seconds |
| Dependency cache | **`cache-dependency-path` per service** | `inventory` and `waitroom` vendor their modules and download nothing; `order` does not vendor and needs the cache |

### On `paths` and the matrix

`paths` gates the **workflow**, not the job. `ts-tests.yml` already runs all four
of its jobs when any one TS service changes. A matrix therefore costs no
precision that the existing arrangement has — it only stops three near-identical
job bodies from having to be edited in lockstep.

## File structure

| File | Change | Task |
|---|---|---|
| `.github/workflows/go-tests.yml` | create | 1 |
| `.github/workflows/ts-tests.yml` | add the workflow file to `pull_request.paths` | 2 |
| `.github/workflows/chart-assertions.yml` | same | 2 |

---

### Task 1: The three Go services run their tests on every push

**Files:**
- Create: `.github/workflows/go-tests.yml`

**Step 1 — write the workflow.** One `test` job, `name: ${{ matrix.svc }}` so
the check names read `order-svc` / `inventory-svc` / `waitroom-svc` and match
the shape `ts-tests.yml` produces. `working-directory` comes from the matrix;
`matrix` is available in `jobs.<id>.defaults.run`.

**Step 2 — verify before pushing.** The claim this task makes is "these suites
pass on a clean runner", and the evidence for it is the run, not the file. Push
to `dev` and read the conclusion from `gh run view --json conclusion`, not from
`gh run watch --exit-status` — that returns a spurious non-zero when stdout is
redirected.

**Verification:**
- `gh run view <id> --json conclusion` reports `success`
- three checks appear, named for the three services
- the run takes under two minutes

**Expected:** green on the first run. Both `order-svc` module download and the
two vendored services resolve without further configuration.

---

### Task 2: A workflow change is tested by the pull request that makes it

**Files:**
- Modify: `.github/workflows/ts-tests.yml`
- Modify: `.github/workflows/chart-assertions.yml`

Add each workflow's own path to its `pull_request.paths` list, matching what
`push.paths` already carries and what `go-tests.yml` does by construction.

**Verification:**
- every workflow under `.github/workflows/` that has a `pull_request.paths` list
  names itself in it

**Expected:** no behaviour change on `dev` or `main`; the effect is only visible
on a pull request that touches a workflow and nothing else.

---

## Found, not fixed

- **`order-svc` does not vendor its modules**; `inventory-svc` and
  `waitroom-svc` do. Not a defect, but it means `order-svc` is the one Go
  service whose CI needs the network. Worth converging one way or the other.

## Outcome

_To be filled on completion._
