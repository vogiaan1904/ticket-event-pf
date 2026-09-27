# Decision protocol — design

**Status:** adopted 2026-09-22. Decision record:
[`docs/decisions/0001`](../decisions/0001-decisions-are-surfaced-then-owned.md).
**Applies to:** every session on this repository.

## The problem

A recommendation that arrives before the option space closes the question before
it is open. The reasoning happens somewhere else and lands pre-digested; what
gets approved is a conclusion, and a conclusion you approved is one you can
describe but not defend.

Producing correct code is not the goal of this repository. Producing an engineer
who can defend it is.

## The two tiers

Nothing is decided silently. Not everything costs a round-trip.

| | Trigger | What happens |
|---|---|---|
| **Surfaced** | Any trade-off met mid-work | One line, inline: the choice, the alternative, what the alternative costs. Work continues. Override at any time. |
| **Gated** | Consequences outlive the session | Full stop. Forces → options → the architect's call → teach-back → implementation. |

**Gated covers:** a schema, a wire contract, deploy topology, the shape of CI,
the error taxonomy, what is allowed to land on `main`, and any change to this
protocol.

**Surfaced covers the rest:** which flag, which helper, where a check lives.

Worked example, from the session that produced this document:

- `-race` on the Go suites → **surfaced**. "Using `-race`; bare `go test` is ten
  seconds faster and misses concurrency bugs."
- three CI jobs instead of a matrix → **gated**. It is the shape of CI, and it
  outlives the session that chose it.

## The gated sequence

1. **Forces.** What is true that makes this a question. No options yet.
2. **Options.** Two or three that were genuinely on the table, each with its
   cost. Including at least one the agent expects to lose.
3. **No recommendation** — unless asked for, in which case it is given plainly
   and marked as the agent's.
4. **The call.** The architect's.
5. **Teach-back.** One line, in their words, on why. If it does not come out,
   the decision is not finished and implementation does not start.
6. **Record.** A file in `docs/decisions/`, with the teach-back as the
   Decision section, verbatim.

## Why this is not a skill

Skills load when their description matches the task at hand. "I am about to make
a decision" is not a task type — it is something that happens inside every task.
A skill that must fire on all of them fires reliably on none.

The rule therefore lives where it is always loaded, and the artifact lives in
`docs/decisions/`.

## Failure modes this is watched for

- **Options theatre.** Two options presented where only one was ever viable, so
  the choice is formal and the reasoning still belongs to the agent. The guard
  is step 2: an option the agent expects to lose, stated at its strongest.
- **Teach-back as a quiz.** The point is to find out whether the reasoning
  transferred, not whether it can be recited. A wrong teach-back means the
  explanation failed, and the next move is a better explanation.
- **Tier drift.** Gated decisions quietly handled as surfaced ones because the
  session is moving. The tell is a decision that turns out to need a record and
  has none.
