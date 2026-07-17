# Claims inflation and nil claims design

Date: 2026-07-16
Status: approved pending review

## Summary

Two engine features that add realism to every generated line of business, motor personal first: stochastic claims inflation applied by occurrence year, and nil claims (claims closed without payment). Both are driven by new per-LoB parameters, both keep the existing invariants, and the motor preset is recalibrated so the Schedule P realism gate still passes. These are the first two items from the mission's "further features of real claims data" backlog; recoveries and reopened claims are deferred to a separate spec.

## Decisions made during design

- Inflation kind: occurrence-year severity inflation. Each claim's ground-up loss is scaled by an inflation index at its occurrence year; the whole claim (initial estimate, ultimate, payments, case path) then lives at that price level. Payment-date (calendar-year) inflation is explicitly out of scope - it would break the ultimate-first invariant and interacts with case adequacy, so it deserves its own design later.
- Inflation is stochastic, not a fixed factor: one simulated annual factor per calendar year, drawn from its own labelled sub-stream of the master seed. The user steers only the average level (one knob); the year-on-year volatility is a per-LoB YAML parameter not shown in the UI form.
- Inflation applies to the whole ground-up loss - own damage and third party alike (not third party only). Own damage already inherits sum insured drift (asset-value trend, by underwriting year); claims inflation (claims-cost trend, by occurrence year) is a distinct, real second trend. The compounding requires the motor preset to be recalibrated.
- Nil claims: one parameter, `nil_probability`. Each reported claim is nil with that probability; a nil claim gets its initial case estimate, interim pure revisions, then a single release to zero at close, and never any payments. No dedicated nil close-lag multiplier (nils are small-estimate claims, which already close faster via the existing size logic).
- `nil_probability: 0` switches nil claims off, and this must be clear to the user: the YAML comment and the UI tooltip both state it, and a test proves the off state from the output (with the parameter at 0, no generated claim closes without payment).

## Parameters and validation

New YAML keys under `claims`, mirrored in the exported config DTOs and the `lob` domain object:

```yaml
claims:
  inflation:
    mean: 1.04        # average annual claims inflation factor (user-facing knob)
    volatility: 0.015 # sigma of the mean-1 lognormal noise on each year's factor (not in the UI form)
  nil_probability: 0.08  # probability a reported claim closes without payment; 0 switches nil claims off
```

Validation in `lob`:

- `inflation.mean` must be positive (1.0 = flat prices, stated explicitly in the comment).
- `inflation.volatility` must not be negative.
- `nil_probability` must be in `[0, 1)`, with the YAML comment and UI tooltip both stating that 0 disables nil claims.

Old YAML files lacking the new keys fail validation on the zero inflation mean. This is deliberate: strict decoding (`KnownFields(true)`) is the repo's config philosophy, and a missing inflation mean should be an explicit error, not a silent behavior change. The embedded motor preset is updated in the same change, so the CLI default keeps working.

## The inflation path

A new simulation input, seeded like the existing stages: `src.Split("inflation")` off the master source, so the path is reproducible per seed and independent of the book, claims, and runoff sub-streams (changing a parameter in another stage never reshuffles the inflation draws).

Mechanics, for each calendar year `y` of the run window:

- Draw an annual factor `f_y = mean * meanOneLogNormal(volatility)` - the same mean-1 lognormal noise pattern the book simulator uses for year-on-year size.
- The cumulative index starts at 1.0 in the start year and compounds: `index[startYear] = 1.0`, `index[y] = index[y-1] * f_y`.

Each claim's ground-up loss (own damage and third party alike) is multiplied by `index[occurrenceYear]` before the excess is subtracted. Everything downstream - initial estimate, ultimate, payments, case path - inherits the inflation automatically, and every existing invariant holds untouched.

The index is a small value object in the claim domain package (it parameterizes claim severities). `GenerateDataset` builds it from `src.Split("inflation")`, the run window, and the LoB inflation params, and passes it into the claim simulator as a fourth input.

## Nil claims mechanics

The nil decision is drawn in the claim-events stage, inside each policy's existing per-policy sub-stream: after a claim is confirmed reportable, draw `nil = src.Bernoulli(nil_probability)`. A probability of 0 makes no draw at all, so it is a true no-op, and moving the knob between two positive probabilities is stable; but enabling nils from 0 inserts one draw per reported claim and so does shift subsequent draws within a multi-claim policy. The `Claim` struct gains a `Nil bool` field, carried to the runoff stage but not written to claims.csv - the CSV schema is unchanged. Nil-ness is observable in the data the way it is in a real extract: a closed claim whose transactions contain no payments.

Runoff for a nil claim:

- First row is the initial case estimate on the report date, as today (the "outstanding = running sum of ESTIMATE rows" convention holds).
- Interim pure revisions still occur at the existing `revisions_per_year` intensity, as noise around the still-outstanding reserve rather than converging toward a true cost - the insurer does not know it is a nil until it closes.
- At the close date, a single ESTIMATE row releases the full outstanding to zero. No payments, ever. No interim payments are drawn and no final settlement payment is made.

Invariants after this change: outstanding case is exactly zero at close (unchanged), and total paid equals the ultimate, where a nil claim's ultimate is zero. The incurred triangle gains a mild, realistic feature - case raised and later released with no payment - so incurred development can drift down late, which is what reference-company triangles show. Non-nil claims are completely untouched by this path.

## Calibration

Both features shift the triangles: inflation compounds on top of sum insured drift (raising severities and the loss ratio through the decade), and nils add case that releases without payment (softening late incurred development). The motor preset is recalibrated so `TestDefaultPresetIsRealistic` still passes against the Schedule P bands - likely nudging `own_damage_median_fraction` and/or `premium_rate_factor` downward to offset the added inflation trend. Target default values: `inflation.mean` around 1.03-1.05, `inflation.volatility` around 0.01-0.02, `nil_probability` around 0.08. The spec sets the targets; the calibration determines the exact numbers, and the realism gate is the acceptance test.

## UI changes

Two new visible fields in the Claims group of the parameters form:

- "Claims inflation" - tooltip: average annual claims inflation factor, applied by occurrence year.
- "Nil claim probability" - tooltip: probability a claim closes without payment; 0 switches nil claims off.

The inflation volatility rides through on preset defaults with no form field, as agreed. The preset endpoint and generate round-trip pick the new fields up automatically through the DTOs.

To make the features observable in the UI, the summary tab gains one column: Nil claims per year (count of claims closed without payment), next to the Claims column. This is a small server-side addition to `Summarize`. The severity histogram already ignores zero-paid claims by construction, which is now correct behavior rather than a corner case.

## Testing

- Inflation path unit tests: deterministic per seed and label; index starts at 1.0 and compounds; the realised average factor tracks the configured mean; independence from the other sub-streams.
- Nil runoff unit tests: a nil claim emits no payments, the outstanding case is exactly zero at close, and its ultimate (total paid) is zero.
- Off-switch test: with `nil_probability: 0`, no generated claim closes without payment (every closed claim has at least one payment) - the switch is proven from the output, not just documented.
- The end-to-end invariant sweep is extended to cover nil claims (paid == ultimate still holds with zero-paid claims present).
- The realism gate (`TestDefaultPresetIsRealistic`) stays the acceptance test for the recalibrated preset.
- Existing determinism tests continue to pass (same seed + config = byte-identical output).
