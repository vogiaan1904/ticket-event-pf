# Platform documentation

Several kinds of document live here, and they have different rules because they
have different lifespans.

| | What it is | Naming | When it changes |
|---|---|---|---|
| `ARCHITECTURE.md`, `METRICS.md`, `RUNBOOK.md` | How the platform works now | Named | Whenever the platform does |
| `design/` | A target state and the reasoning for it | Named, no date | Until the target is reached or abandoned |
| `plans/` | A work order derived from a design | `YYYY-MM-DD-<name>.md` | Never, once executed — it records what was decided |
| `decisions/` | Why a choice was made, and by whom | `NNNN-<slug>.md` | Never — superseded records stay and gain a pointer |
| `diagrams/` | Diagram sources, hand-edited | Named | With the architecture |

**A decision record answers what neither of them does: why this and not the
alternative, in the words of whoever chose.** See `decisions/README.md`.

**A design is read repeatedly; a plan is read once and then becomes a record.**
That is why plans carry the date they were written and designs do not: a design
answers "how does this work", a plan answers "what did we do on that day".

A completed plan states so in its first lines. Its checkboxes are history, and a
reader must not have to scroll to learn that.

Plans are written for whoever does the work. They do not open with instructions
addressed to a tool.
