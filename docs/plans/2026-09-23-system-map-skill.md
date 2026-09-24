# System map: one place an agent learns where each part of the system is written down

**Status: COMPLETE 2026-09-23.** Recorded as [0012](../decisions/0012-agents-learn-the-system-from-a-map-of-documents.md).
The map check runs in CI; two evaluation rounds are under *Evaluation*, and what the
work found but did not fix is under *Found, not fixed*.

**Goal:** An agent — or the architect briefing one — can find the document that owns
any design question about the platform or one of its services, know how far to trust
it, and verify it in the code; and the map cannot go stale without CI failing.

**Architecture:** A project skill, `.claude/skills/system-map`, that holds facts about
*documents*, never facts about the *system*. `SKILL.md` routes by kind of question and
by service; `references/<service>.md` is one reading guide per service;
`scripts/check_map.py` fails when a cited path is gone or a design-bearing document is
not cited. A `SessionStart` hook on `compact` points a compacted session back at it.

**Tech stack:** Markdown, Python 3 (stdlib only), GitHub Actions, Claude Code hooks.

**Decision:** [0012](../decisions/0012-agents-learn-the-system-from-a-map-of-documents.md).

---

## Design

### The problem

The platform's design is written down, but across 40 tracked documents of mixed
freshness. An agent starts with the root `CLAUDE.md` and a service's `CLAUDE.md` once
it touches that service's files; everything else it must find. It loses the thread
after a compaction, and a subagent starts cold. The architect cannot say what to read
either, because no document says where each question is answered.

Discovery is half of it. The other half is drift, found while surveying:

| Where | Says | Code says |
|---|---|---|
| `README.md:85,96`, `docs/ARCHITECTURE.md:24,57` | `Reserve` runs under `SELECT … FOR UPDATE` | a guarded `UPDATE`, no `FOR UPDATE` (`35e864f`) |
| `.claude/skills/trace-purchase-flow/SKILL.md:21` | the invariant is `FOR UPDATE` inside a tx | same |
| `docs/ARCHITECTURE.md` §5 | CQRS in the Event service | `services/event-svc/CLAUDE.md`: no CQRS |
| `docs/ARCHITECTURE.md:24` | hold ~15 min | `PaymentTimeout + ReservationHoldGrace` |
| `services/waitroom-svc/docs/SYSTEM_FLOW_README.md` | Redis real-time updates to clients | removed with the push stream, `872858b` |
| root `CLAUDE.md` | the README port table is stale | it matches |

Two contradictions are not drift but open calls, and are left for the architect:

- `services/api-gateway/src/CLAUDE.md` (818 lines, from `init`) loads whenever an agent
  touches gateway code and prescribes `dtos/req` + `dtos/resp`, which the root
  *Canonical TS layout* forbids for new structure.
- The root `CLAUDE.md` calls `event-svc` the converged reference layout, and in the
  same list forbids the `controllers/grpc/dtos` + `dtos/` split that `event-svc` has;
  the `add-service` skill says not to copy it.

### Options

1. **One routing skill that holds facts about documents only (Chosen)** →
   [0012](../decisions/0012-agents-learn-the-system-from-a-map-of-documents.md).
   One description in the skill list; per-service detail loads only when needed; the
   facts stay in their one home. Cost: a map to keep current — bounded by the check.
2. **A skill per service.** Precise triggering, but seven descriptions always in
   context, overlap with each service's `CLAUDE.md`, and no home for a question that
   crosses services.
3. **A consolidated architecture reference inside the skill.** Fastest to read once,
   and a fourth copy of every fact — the failure `35e864f` already demonstrated.

### Rules the skill must keep

- It states where a thing is written and how far to trust it — never what the system does.
  A mechanism belongs to the document that owns it.
- Every path it cites is repo-relative, so the check can verify it.
- Trust is by kind of document: code, then the binding root `CLAUDE.md`, then a
  service's `CLAUDE.md` and the skills, then decisions (why), designs (target), service
  deep dives, the narrative README/ARCHITECTURE, and plans (a record of a day).
- `SKILL.md` stays under 200 lines; a service guide under 80.

### Not built

- **Description optimisation** (`skill-creator`'s `run_loop`, ~300 `claude -p` calls).
  Deferred until the skill has been used; the evals below test routing, not triggering.
- **A subagent definition that preloads the skill.** The skill tells whoever briefs a
  subagent to name it; a dedicated agent type is worth it only if that proves not enough.
- **A write-time hook for the check.** CI catches it at push; a third `PostToolUse`
  hook would run on every edit.

## Review focus

1. A cited path with a line or anchor suffix (`file.go:123`, `doc.md#section`) is
   checked on its file part, not reported missing. → Task 1, `test_suffixes_are_stripped`.
2. A placeholder (`services/<svc>/CLAUDE.md`, `{a,b}`, `*`) is skipped, not reported.
   → Task 1, `test_placeholders_are_not_paths`.
3. A new design document is red in CI until the map cites it.
   → Task 1, `test_uncited_doc_is_reported`; Task 4 mutation run.
4. Untracked or ignored documents (`docs/labs/`, `tools/`) are never required, or a
   local run goes red while CI is green. → Task 1, `test_untracked_doc_is_not_required`.
5. A code file a guide cites is renamed → red. → Task 1, `test_missing_path_is_reported`;
   Task 4 mutation run.

---

## Task 1: The check, test first

**Files:** create `.claude/skills/system-map/scripts/check_map.py`,
`.claude/skills/system-map/scripts/check_map_test.py`.

**Interface:** `problems(root: str, skill_dir: str) -> list[str]`; CLI
`python3 .claude/skills/system-map/scripts/check_map.py [--root DIR]`, exit 1 on any
problem. A *required* document is a git-tracked `.md` file outside `node_modules/`,
`vendor/`, `docs/plans/`, `docs/decisions/NNNN-*`, `.claude/hooks/`, other skills'
`references/`, and the map itself.

- [x] Write `check_map_test.py` (unittest, temp git repo per test): clean map passes;
      missing path reported; uncited doc reported; untracked doc not required;
      `:123` and `#anchor` stripped; placeholders skipped; excluded kinds not required.
- [x] Run it — red: module not found.
- [x] Write `check_map.py`.
- [x] Run it — green.

## Task 2: The map

**Files:** create `.claude/skills/system-map/SKILL.md` and
`references/{api-gateway,user,event,order,payment,inventory,waitroom}.md`.

- [x] `SKILL.md`: what is already in context; trust by kind of document; route by
      question; route by service; the procedure (owner → read → verify in code →
      report drift); how to brief a subagent; keeping the map true.
- [x] Each guide: read-in-this-order with what each document owns and its trust; by
      question; code entry points; edges (contracts, topics, store, chart); decisions
      and plans; documents not to trust for current behaviour.
- [x] `python3 .claude/skills/system-map/scripts/check_map.py` — green on the repo.

## Task 3: Correct the drift the map would route into

**Files:** `README.md`, `docs/ARCHITECTURE.md`, `.claude/skills/trace-purchase-flow/SKILL.md`,
`services/waitroom-svc/docs/SYSTEM_FLOW_README.md`, root `CLAUDE.md`.

- [x] Verify each row of the drift table against the code before editing it.
- [x] Replace each wrong claim with the correct one or a pointer to its owner — no new
      copies of numbers that live elsewhere.
- [x] `grep -rn "FOR UPDATE" README.md docs/ARCHITECTURE.md .claude/skills` shows only
      correct uses.

## Task 4: Wiring

**Files:** root `CLAUDE.md`, `.claude/settings.json`, create `.claude/hooks/after-compact.md`,
`.github/workflows/system-map.yml`.

- [x] Register row: *Where does an agent start to learn a part of the system?* → the
      skill. One pointer line under *Documentation layout*.
- [x] `SessionStart` hook, matcher `compact`, prints `after-compact.md`.
- [x] Workflow on push to `main`/`dev` and on PRs, no path filter (a renamed code
      file breaks a guide as surely as a new doc): run the tests, then the check.
- [x] Mutation: remove one citation → red naming it; cite a missing file → red; restore → green.

## Task 5: Evaluate with `skill-creator`

- [x] `evals/evals.json`: three prompts where the right answer depends on reading the
      owning document — the reserve mechanism, a paid order in `REFUND_REQUIRED`, and
      raising the waitroom's concurrency cap.
- [x] Run each with the skill and without it; grade against assertions; aggregate a
      benchmark; write the static review page.
- [x] Revise the skill on what the transcripts show; record the result in 0012's Outcome.

## Task 6: Record

- [x] Draft 0012 `proposed` with the delegation quoted; regenerate the index.
- [x] On landing: accept 0012 with its Outcome; mark this plan complete.

---

## Evaluation

Each prompt ran once with the skill and once without, on the same working tree;
answers were graded against the assertions in `.claude/skills/system-map/evals/evals.json`.

| Round | Prompts | Model | Pass rate with / without | Mean tokens with / without | Mean time with / without |
|---|---|---|---|---|---|
| 1 | reserve path, `REFUND_REQUIRED`, a new gateway route — narrow, named | Opus | 100% / 100% | 133k / 133k | 270s / 254s |
| 2 | onboarding reading list, a subagent brief — broad | Sonnet | 100% / 92% | 89k / 103k | 117s / 195s |

- On a narrow question a capable model finds the owner without the map: the root
  `CLAUDE.md` register plus grep is enough. The map changed nothing measurable.
- On a broad question the map cut time by about 40% and tokens by about 14%. With
  one run per arm this is a direction, not a measurement.
- The with-skill runs found three errors in the map itself, all fixed in `b093948`:
  a webhook the map described as idempotent is not, a stale document labelled
  trustworthy, and an error-mapping pointer one file off. Every arm, with or
  without the map, also found drift in documents the survey had missed.

## Found, not fixed

Verified against the code; each needs a decision or work beyond this plan.

- **`OrdersNeedingRefund` can never fire.** Its expression reads
  `tb_order_workflow_duration_seconds_count{workflow="ConfirmOrder",outcome="refund_required"}`;
  the only observation is `services/order-svc/internal/order/service/order.go:228`,
  which hard-codes `workflow="CreateOrder"`. Its runbook section greps
  `deploy/order-service` where the refund runs in `order-consumer`, and reads
  `tb_inventory_reserve_total`, which counts `Reserve` only.
- **The cluster's `payment-webhook` has no `PENDING` guard** — a repeated
  `/complete/<code>` writes a second outbox row and a duplicate `payment.completed`.
  (First written here as producing `REFUND_REQUIRED`, which it does not; see
  `docs/plans/2026-09-24-architect-calls-from-the-system-map.md`.)
- **`services/inventory-svc/docs/MODELS.md`** is stale beyond repair by edit: column
  types, repository layer and examples. Deleting it is the likely call.
- **`services/payment-svc/lambdas/README.md`** still describes the retired
  `outbox-processor` and a Prisma layer.
- **`services/order-svc/docs/SYSTEM.md`** says a confirm that fails after retries
  becomes `REFUND_REQUIRED`; infrastructure failures leave the order `PENDING`.
  `TIMEOUT` is handled but never written.
- **The gateway neither rate-limits nor sets security headers**, and the TS layout
  reference contradicts itself — both now rows in the register's *Open* table.
