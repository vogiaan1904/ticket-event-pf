# The end of a sell-out

**Status: COMPLETE 2026-10-01.** A chair that takes no ticket holds a place for at
most 5 minutes; verified on k3s. Decision:
→ [0027](../decisions/0027-an-admitted-buyer-has-five-minutes-to-start-a-checkout.md), accepted.

**Goal:** A chair that takes no ticket holds a place for at most 5 minutes, so the end
of a sell-out never stalls for the token's 15.

**Spec:** `docs/design/admission-sizing.md`, *The end of a sell-out*. Where this plan
and the spec disagree, the spec wins.

## Global constraints

- Only the waitroom's token lifetime changes. order-svc reads `JWT_EXPIRY` into a field
  nothing uses; it stays.
- A ConfigMap change rolls the app that reads it (0020); the render check proves the
  value reaches `waitroom-config`.
- The checkout-expiry gate's premise changes: A's chair now frees at its own window,
  before its order expires. The gate follows.

## Review focus

1. **A checkout started in time still completes.** The token is checked only at
   create; gate 2 and the sold-out gate pass.
2. **An expired order still ends everywhere.** `EXPIRED` on the wire, `TIMEOUT` stored,
   the session expired, the hold released.
3. **The window is the chart's, not a code default.** The service's own default stays
   15 minutes for local runs; the chart sets 5.

---

### Task 1: the window is five minutes, from values

- `assert-render.sh`: `waitroom-config` carries `JWT_EXPIRY: "5m"` by default, and
  `--set waitroom.checkoutWindow=7m` reaches it. Red first: it reads `15m`.
- `values.yaml`: `waitroom.checkoutWindow: 5m`; `config.yaml`: the waitroom's
  `JWT_EXPIRY` reads it. Regenerate the golden.

### Task 2: the gates

- `deploy/scripts/gate-sell-out-tail.sh`, `make -C deploy k3s-gate-sell-out-tail`: two
  tickets, three buyers, nobody orders. Two are admitted and the third waits; the two
  chairs expire about 300s after admission and the third is admitted then, not 15
  minutes later.
- `gate-checkout-expiry.sh`: waits for A's order to read `EXPIRED` (510–660s), and
  checks A's chair is already gone.

### Task 3: documents

The design's section and step 5; the waitroom guide's token and deploy notes; the
bound in `docs/design/eks-stateful-tier.md`; the README's admission paragraph, which
still names the 100 that 0025 deleted; the admission diagram's labels; the register.

### Task 4: verify on k3s, then record

Deploy; gate 2, sold-out, room, orders, sell-out tail and checkout-expiry. Stop the
box. 0027 accepted with an Outcome; this plan COMPLETE with Results.

## Results

Tasks 1–3 landed as `66d232a`, `d7b35eb` and `895d7b1`. The render check went red at
`15m` first. The final review was a self-review, with no fresh reviewer: only the
token's own expiry and the chair's TTL read `JWT_EXPIRY`, and both still match. It
found nothing Critical or Important.

Where the run departs from the tasks as written:
- Task 3 also corrected two documents that still described the room of 100 that 0025
  deleted: the README's admission paragraph and the admission diagram, re-exported.
- A run of the checkout-expiry gate started on revision 60 was stopped: its script was
  edited while it ran, and bash reads a script as it goes. The run below is a fresh one.

Task 4, on k3s on 2026-10-01, revision 61 (`sha-895d7b1`, `JWT_EXPIRY: 5m`):

| Check | Result |
|---|---|
| gate 2, sold-out, room, orders | passed |
| `make -C deploy k3s-gate-sell-out-tail` | passed: chairs ending in 291s; the third buyer admitted 292s after the room filled |
| `make -C deploy k3s-gate-checkout-expiry` | passed: A's order expired at 555s, its chair already gone; B untouched |
