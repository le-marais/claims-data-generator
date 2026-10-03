> **OUT OF CONTEXT - do not read (2026-10-03):** historical design record, kept for provenance only. Agents must not load this file into context or treat it as a source of truth; it records decisions as of its own date and may not match current behaviour. For how the system works today see `README.md`, `AGENTS.md`, and `docs/architecture.md`.

# A minimum payment delay - design

Date: 2026-10-03

## Goal

A claim-level read after the per-section settlement found payments made
too soon after the case was raised: an own-damage claim reported and paid the
next day (3.7% of own-damage claims close within two days of report), a
third party paid one day after report, and every bill above the case raised
and paid on the same day. Real payments take time to process after the
information that sets the case.

## Rule

`runoff.payment_delay_days` (D, a whole number of days, 0 off): every payment
is at least D days after the last ESTIMATE row that raised the case. The
raises are the opening case at report, a reopen re-raising the case, an
upward revision, and the top-up when a bill exceeds the case.

## Design

### Claim stage

A paying episode stays open at least D days: its close lag is D plus the
gamma draw, so short claims do not pile up at D. A nil first episode keeps its
drawn lag. Reopen episodes always pay, so they all take the delay.
`mean_days` becomes the mean of the drawn part. `ClaimSimulator` and
`ReopenSimulator` take the delay through `WithPaymentDelay(days)`, which the
application wires from `runoff.payment_delay_days`.

### Runoff

For a paying episode with D above 0:

1. Interim payment days are drawn in [D, duration - D]; an episode shorter
   than 2D has none.
2. Payments are at least D days apart, the first at least D days after the
   open. An interim payment too soon after the previous one is held over to
   the next, as a payment below `min_payment` is.
3. Each payment has a bill D days before it. At the bill the case is raised
   to the payment if it is short; that raise is D days before the payment.
4. A revision in the D days up to a payment (after its bill, up to and
   including the payment day) may lower the case, but not below the payment,
   and may not raise it.

On a day with several events, revisions come first, then payments, then
bills. A bill on the day of the previous payment follows it.

With D at 0 nothing changes, and the draws are the same, so output is
byte-identical to the code without the parameter.

### Presets

Both presets use 7 days, about five working days to process a payment.
Paying claims close 7 days later on average.

### Config, UI and docs

The mirrored config field and `ToDomain`; a "Payment delay days" field in
Runoff; the README runoff diagram, close-lag text and worked example;
`docs/architecture.md`; preset comments where they give `mean_days` as the
whole close lag. MR-20 keeps only what this does not cover.

## Testing

- Validation: negative and fractional `payment_delay_days`.
- Claim stage: with a delay, every paying episode lasts at least D days; a
  nil first episode keeps its lag; reopen episodes last at least D days.
- Runoff: every payment at least D days after the last raise; payments at
  least D apart; total paid still the ultimate; D 0 gives the same output as
  before.
- Application: a preset run satisfies the rule on every claim.
- Golden hashes refreshed; the realism gate and the loss-ratio test pass.
