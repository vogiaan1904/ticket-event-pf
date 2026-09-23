# Decision records: written when a call is made, reviewed from one table

**Status: NOT STARTED.**

**Goal:** Every architectural decision gets a record in `docs/decisions/` when it is
made, is confirmed with evidence when the work lands, and can be reviewed as a
question / chose / instead of / cost table — without a ritual the architect has to
perform.

**Derived from:** the format and rules in `docs/decisions/README.md` as revised by
this plan, and the lesson of records 0001 and 0002: a blocking ritual was
abandoned within a day, so every mechanism here either runs itself or advises.

**Decided on 2026-09-23** (the architect's answers):

| Question | Answer |
|---|---|
| Who reads the records first? | Both — the tracked record is the source, a review view is derived |
| When is a record written? | At decision time (`proposed`), confirmed when the work lands (`accepted` + Outcome) |
| How are records grouped? | One record per decision, grouped by arc in a generated index |
| Scope | Trigger line, skill, advisory plan hook, CI index check, and a backfill of 0003–0011 |

## Files

| File | Responsibility |
|---|---|
| `docs/decisions/README.md` | The format, the lifecycle, the one rule, and the generated index |
| `docs/decisions/index.py` | Parses records, validates them, writes or checks the index block |
| `docs/decisions/index_test.py` | The index's contract, stdlib `unittest` |
| `docs/decisions/0003…0011-*.md` | Backfilled records; 0001 and 0002 gain an arc and a review row |
| `.github/workflows/decision-records.yml` | Runs the tests and `index.py --check` when `docs/decisions/**` changes |
| `.claude/skills/decision-records/SKILL.md` | How to draft, accept, supersede, link, and review |
| `.claude/hooks/working-stance.md` | One line: the trigger, injected on every message |
| `.claude/hooks/check-decision-records.py` + `.claude/settings.json` | Advisory PostToolUse check on plan edits |
| `docs/README.md`, root `CLAUDE.md` | A record now changes once more (Outcome); a register row points here |

## Task 1: The index, test first

**Contract** (`index_test.py`), each case against records written to a temp dir:

- a valid record appears in the block under its arc, linked by number, with its status
- records of one arc share a table; arcs are ordered by their first record's number
- a superseded record stays listed, its status naming the successor
- a record missing its review row, arc, date, or a known status fails validation
  and names the file
- a title whose number disagrees with the filename fails validation
- `--check` passes on a fresh block and fails on a stale one

- [ ] Write `index_test.py`; run `python3 -m unittest docs/decisions/index_test.py` — fails, no `index.py`
- [ ] Write `index.py`; the same command passes
- [ ] Break one validation rule in `index.py`; its test must fail; restore

## Task 2: The design text

- [ ] `docs/decisions/README.md`: header fields (`Arc`, `Where it lives`), the review
  row, `proposed | accepted | superseded`, the Outcome section, the index markers,
  and the one rule restated — a Decision section quotes the decider, records a
  delegation as a delegation, or says it was reconstructed and from what
- [ ] `docs/README.md`: a record changes after acceptance only to gain an Outcome or
  a superseded-by pointer
- [ ] Root `CLAUDE.md`: a Decided row — where a decision's why is recorded and reviewed

## Task 3: The records

- [ ] 0001, 0002: add `Arc: working-agreement` and a review row; nothing else changes
- [ ] 0003–0005 (`waitroom-stampede`): Decision says recorded retroactively, from which plan and commits
- [ ] 0006, 0009 (D1, D2): Decision quotes the delegation verbatim
- [ ] 0007, 0008: Decision says the approach came with the work order and was not put as a separate choice
- [ ] 0010 (`deploy-targets`): Decision quotes the architect's instruction
- [ ] 0011 (`working-agreement`): this mechanism; Decision quotes the four answers above
- [ ] `python3 docs/decisions/index.py` writes the block; `--check` passes

## Task 4: CI

- [ ] `decision-records.yml`, path-filtered to `docs/decisions/**` and itself
- [ ] Verified by the push: the workflow runs and passes

## Task 5: The trigger and the procedure

- [ ] The stance line, under the existing trade-off rule
- [ ] `SKILL.md`: links to the README for format rather than restating it

## Task 6: The safety net

- [ ] `check-decision-records.py`, registered for `Write|Edit`, silent outside `docs/plans/`
- [ ] Flags a `(Chosen)` option with no `docs/decisions/NNNN` link
- [ ] Flags a `COMPLETE` plan that links a record still `proposed`
- [ ] Verified with three payloads: an unlinked choice (flags), a linked one (silent),
  a non-plan path (silent)

## Not in scope

- Catching a decision made in chat with no plan. Only the stance line covers that;
  add more only if the records show it missing things.
- Backfilling arcs older than today's.
