> **OUT OF CONTEXT - do not read (2026-10-03):** implementation plan, deleted once its work ships. Agents must not load this file into context or treat it as a source of truth. For how the system works today see `README.md`, `AGENTS.md`, and `docs/architecture.md`.

# Payment delay implementation plan

**Goal:** add `runoff.payment_delay_days`, per `docs/superpowers/specs/2026-10-03-payment-delay-design.md`.

**Global constraints:** byte-identical output for a seed and config; D 0 leaves output unchanged; YAML holds simulation parameters only; sentence case, no em dashes.

### Task 1: parameter and validation

- `lob.RunoffParams.PaymentDelayDays`: not negative, a whole number.
- `lob_test.go`: field-naming cases for -1 and 2.5.

### Task 2: claim stage

- `claim.ClaimSimulator.WithPaymentDelay(days int)` and `ReopenSimulator.WithPaymentDelay(days int)`: a paying first episode's close is report + D + the drawn lag; a reopen's close lag is D + the drawn lag.
- Tests: paying episodes last at least D; a nil first episode's close equals the no-delay close; reopen episodes last at least D; D 0 changes nothing.

### Task 3: runoff

- `transaction.RunoffSimulator`: interim days in [max(1, D), duration - max(1, D)]; spacing hold-over; bill events (kind after payment) that top up the case D days before each payment; revisions in a payment's window clamped to [payment, outstanding].
- Tests: the delay invariant over the ledger, spacing, total paid, D 0 identical.
- `internal/application/generate.go`: wire the delay into the claim and reopen simulators; an application test that a preset run keeps the rule.

### Task 4: config, form, presets, docs

- Config field, form field, `payment_delay_days: 7` in both presets with comments; preset close-lag comments.
- Golden hashes, realism gate, re-measure; README, architecture, MR-20.
- Checks; delete this plan; push; update PR #34.
