# Recoveries (salvage and subrogation) design

Date: 2026-07-17
Status: approved pending review

## Summary

Recoveries add money coming back on a claim: salvage (selling the insured vehicle's wreck) and subrogation (recovering the payout from an at-fault third party). They appear as two new transaction types, `SALVAGE` and `SUBROGATION`, carrying positive money-in amounts. Recoveries are pure cash events - the case estimate stays gross and every existing runoff invariant survives untouched. They attach only to own-damage, non-nil claims, arrive after the close date, and the realism gate moves to net-of-recoveries paid triangles to match Schedule P. This is the third item from the mission's "further features of real claims data" backlog; reopened claims remain deferred to their own spec.

## Decisions made during design

- Schema: two distinct transaction types, `SALVAGE` and `SUBROGATION`, with positive amounts (money in). Most like a real claims extract, keeps gross paid trivially derivable as the sum of `PAYMENT` rows, and lets the two behave differently. Negative payments were rejected as ambiguous; a single `RECOVERY` type was rejected as losing a distinction a real extract shows.
- Gross versus net: recoveries are cash-only. The case estimate keeps tracking gross cost exactly as today (sums to zero at close, gross paid = ultimate); recovery rows never touch the case. Net paid = payments minus recoveries is derivable by consumers. A net case estimate and a separate recovery reserve were both rejected for the first iteration - they rewrite the runoff invariants for little visible gain.
- Timing: recoveries land after the close date. Salvage shortly after close (weeks), subrogation much later (months), which is how it works in reality. Since recoveries are cash-only and the case is already zero, nothing breaks; the invariant becomes "no case activity after close" instead of "close date is the last transaction".
- Eligibility: own-damage claims only. The claim entity gains a carried (not exported) own-damage flag set from the severity mixture draw. Salvage sells the insured vehicle's wreck; subrogation recovers the own-damage payout from an at-fault third party. Third-party liability payouts do not generate recoveries for the insurer. Nil claims have no payments, so they draw no recoveries.
- Amounts: each recovery is a random share of the claim's gross paid amount, drawn around a configurable mean share and bounded below 1. Self-scaling with severity and inflation, one user-facing knob per type. Modeling salvage from sum insured was rejected - it needs a total-loss concept the engine does not have.
- UI: the triangle tab gets a gross / net-of-recoveries toggle - gross-versus-net reserving is the workflow this feature enables. The realism tab and gate always use net, matching Schedule P.

## Parameters and validation

New YAML block under `claims`, mirrored in the exported config DTOs and the `lob` domain object:

```yaml
claims:
  recoveries:
    salvage:
      probability: 0.10   # chance an own-damage claim yields salvage; 0 switches salvage off
      mean_share: 0.15    # average salvage recovery as a share of the claim's gross paid
    subrogation:
      probability: 0.20   # chance an own-damage claim is subrogated; 0 switches subrogation off
      mean_share: 0.80    # average subrogation recovery as a share of the claim's gross paid
```

Each type also carries a share concentration (spread of the share draw) and a lag parameter (mean days after close) that ride on preset defaults and stay out of the UI form, like inflation volatility.

Validation in `lob`:

- `probability` must be in `[0, 1)` for each type, with the YAML comment and UI tooltip both stating that 0 disables that recovery type.
- `mean_share` must be in `(0, 1)`.
- Concentration must be positive; lag means must not be negative.

Consistent with the repo's strict-decoding config philosophy, old YAML files lacking the new block fail validation, and the embedded motor preset is updated in the same change so the CLI default keeps working.

## Claim entity

`Claim` gains an `OwnDamage bool` field set by the claim simulator when the severity mixture picks the own-damage component. It is carried to the runoff stage but never written to claims.csv, exactly like `Nil`. The severity draw itself is unchanged - the simulator just records which component fired.

## Recovery mechanics

Recovery draws live in their own labelled sub-stream per claim (`src.Split("recovery-claim-<id>")` off the master source), so enabling recoveries never reshuffles the book, claim, inflation, or runoff draws - existing streams stay byte-identical apart from the new rows.

Per eligible claim (own-damage and not nil), each recovery type independently:

- Bernoulli draw on its probability. A probability of 0 makes no draw at all, so it is a true no-op per type.
- If it fires, amount = gross paid x a share drawn from a Beta distribution with mean `mean_share` and the configured concentration (a new `Beta` draw on `RandomSource`, backed by gonum), so the share is naturally bounded in (0, 1). When both types fire on one claim, the two shares are drawn against gross paid independently but their sum is capped strictly below 1 (subrogation, drawn second, is reduced if needed).
- Date = close date + a lognormal lag in days (the report-lag pattern): salvage short (mean weeks), subrogation long (mean months).
- One transaction row per type at most, so a claim has zero, one, or two recovery rows.

Amounts below one cent after rounding emit no row.

## Invariants

New, enforced by construction and asserted in tests:

- Recovery rows appear only on own-damage, non-nil claims.
- Recovery amounts are strictly positive.
- Total recovered per claim is strictly less than gross paid.
- No `ESTIMATE` activity after close (replaces "close date carries the last transaction"; recovery rows are the only post-close transactions).

Unchanged: outstanding case is never negative, outstanding is exactly zero at close, total gross paid equals ultimate.

## Triangles and realism

The triangle domain gains net paid = payments minus recoveries, aggregated by occurrence year and development age alongside the existing gross paid. Because recoveries land after close, cumulative net paid can develop downward at late ages - a realistic feature Schedule P net triangles show.

The realism gate compares net paid triangles against the Schedule P reference data, which is net of salvage and subrogation. Incurred stays gross case plus net paid, noted as a mild approximation (Schedule P case reserves anticipate recoveries; ours do not).

The motor preset is recalibrated so `TestDefaultPresetIsRealistic` passes with recoveries on. Recoveries pull the net loss ratio down, so severity or the premium rate factor gets nudged up to compensate. The spec sets the parameter shape; the calibration determines the exact numbers, and the realism gate is the acceptance test.

## UI changes

- Four new visible fields in a "Recoveries" group of the parameters form: salvage probability, salvage mean share, subrogation probability, subrogation mean share. Tooltips state the share-of-gross-paid meaning and that probability 0 switches the type off. Concentration and lag parameters ride through on preset defaults with no form field.
- The triangle tab gains a gross / net-of-recoveries toggle. The realism tab always uses net.
- The summary tab gains a "Recovered" column per occurrence year (total salvage plus subrogation received), next to the existing columns.

## CSV contract

transactions.csv is unchanged in shape; the `type` column gains the two new values `SALVAGE` and `SUBROGATION` with positive amounts. The README documents the convention: gross paid is the sum of `PAYMENT` rows, net paid subtracts `SALVAGE` and `SUBROGATION` rows, and recovery rows may be dated after the claim's close date. claims.csv and policies.csv are untouched.

## Testing

- Unit tests: recoveries never on third-party or nil claims; per-claim recovered total strictly below gross paid; recovery dates strictly after close; salvage lag shorter than subrogation lag in expectation; sub-stream independence (recovery draws do not shift other stages).
- Off-switch tests: with a type's probability at 0, no rows of that type appear in the output - proven from the output per type, not just documented.
- Determinism: same seed and config produce byte-identical CSVs, including recovery rows.
- The end-to-end invariant sweep is extended with the new invariants (no post-close case activity, recovery bounds, eligibility).
- `TestDefaultPresetIsRealistic` stays the acceptance test for the recalibrated preset against net paid triangles.
