# Model review

Open findings from the model review of the claims simulation logic
(2026-09-30). This file lists unresolved findings only. When a finding is
fixed, delete it and renumber the positions so the list stays dense; the
finding ID is stable, and its full text stays recoverable from git history.
Name the closed IDs in the shipping commit message.

The review read the whole simulation path (policy book, claim events,
reopening, case runoff, recoveries, pricing, triangles, realism gate) and ran
controlled experiments: the shipped preset over 1998-2007 with a 40k initial
book, seed 1 unless stated, toggling one feature at a time to isolate each
effect. It also analysed the 96 embedded Schedule P companies directly.

IDs are **MR** (model review). Severity uses the `docs/todo.md` scale: **high**
undermines the mission, **medium** worth addressing soon, **low** fix when
touching the area.

## 1. MR-1 (high) - the severity model sizes the initial case estimate, not the true loss

- Where: `internal/domain/claim/claim.go` (`simulateClaim` stores the capped,
  net-of-excess loss as `InitialEstimate`); `internal/domain/transaction/runoff.go`
  (`drawUltimate` sets ultimate = initial x lognormal(case adequacy));
  `internal/domain/claim/reopen.go` (reopen estimate from the initial estimate).
- The sum-insured cap binds on the estimate, not on what is paid. 1.84% of
  own-damage claims pay more than sum insured minus excess, including 42% of
  paid total losses; 727 claims in a 10-year run pay more than the sum insured
  itself. A reopen adds a further 45% of the initial estimate on top.
- `case_adequacy_mean` changes the loss cost rather than reserve adequacy:
  1.25 lifts the gross loss ratio from 0.706 to 0.883 (+25%), while incurred
  12-24 barely moves (0.928 to 0.969) because SL-7 removes the bias at the
  first revision.
- The configured severity parameters describe case estimates at report, not
  paid severity, which carries an extra lognormal noise of sigma 0.35.
- Action: draw the true ultimate from the severity model (after excess, capped
  for own damage) and derive the opening case estimate from it. Cap the reopen
  payment at the cover left. Design alongside SL-7 in `docs/todo.md`.

## 2. MR-2 (high) - the realism reference is liability-only, but the preset is 80% own damage

- Where: `data/reference/schedule p/ppauto_pos98-07`,
  `internal/application/realism.go`, the `close_lag` block of
  `internal/infrastructure/config/motor-personal.yaml`.
- The CAS `ppauto` set is Schedule P's private passenger auto liability/medical
  line: it holds no physical damage. The preset's claims are 79.6% own damage.
- To fit liability-speed paid development, own damage is slowed to a mean of
  148 days from report to close (P90 307, P99 1,014), with a 6x mean (about
  720 days) above $20k. Even so, the generated book pays faster than 88-93% of
  reference companies at the first three age-to-age factors (paid 24-36 factor
  1.076 against a reference median of 1.18).
- "Realistic" therefore means "develops like a US auto liability book", with
  a product mix that book does not contain.
- Action: score the third-party (liability) component alone against `ppauto`,
  on its share of premium, and free own-damage settlement from the liability
  calibration.

## 3. MR-3 (medium) - pricing leaves out nil claims and lags inflation by half a year

- Where: `internal/domain/lob/expectedloss.go` (`ExpectedPolicyLoss` has no nil
  term); `internal/domain/policy/book.go` (premium trended by
  `InflationMean^y` at the underwriting year); `internal/domain/claim/claim.go`
  (losses inflated by occurrence year).
- Controlled runs (own damage only, 4 seeds pooled): with nil claims and
  inflation off the loss ratio is 0.7199 against a 0.72 target, so the formula
  is otherwise exact. Nil claims at 0.08 give 0.662 (0.72 x 0.92). Inflation at
  1.04 gives 0.733, because about half of each policy's cover falls in the next
  occurrence year.
- For the preset the expected gross loss ratio is about 0.68, not 0.72, so
  "priced perfectly, lands on target" is false in `lob.go`,
  `motor-personal.yaml` and `README.md`. The `pricing` block has no nil
  setting, so a YAML author cannot correct it.
- Action: add an assumed nil probability to `PricingParams`, and trend
  inflation over the cover term, `Mean^y x (1 - f + f x Mean)` where `f` is the
  share of cover in the next year.

## 4. MR-4 (medium) - generated incurred is compared against reference incurred that includes IBNR

- Where: `internal/infrastructure/schedulep/reader.go` (reads only
  `PaidTriangle` and `IncurredTriangle`); `internal/domain/triangle/compare.go`
  (`lossRatio`).
- The reference incurred behaves like Schedule P total incurred (paid, case,
  bulk and IBNR): the median 12-24 factor is 0.974 and incurred converges on
  paid by age 10. Generated incurred is case plus paid, with no IBNR.
- Generated incurred also lands below 1, for an unrelated reason: nil-claim
  case releases and post-close recoveries. The 12-24 factor is 0.928 with the
  preset, 0.954 with nil claims off, and 1.019 with recoveries also off. The
  gate would penalise realistic reporting delays, whose IBNR emergence pushes
  case-incurred factors above a band centred below 1.
- The "ultimate loss ratio" metric (formerly SL-2) scores a fully developed
  generated diagonal against the reference's mixed-maturity latest diagonal.
  The reference bias runs high, not low: loss ratios developed to age 10 are
  0.981 x the latest-diagonal value at the median (P5 0.91, P95 1.01).
- The reference files already carry the actual later development
  (`FuturePaid`, `FutureIncurred`), which the reader ignores, so no chain-ladder
  completion is needed. The developed band would be [0.256, 0.799] against the
  current [0.265, 0.843].
- Action: read the full squares and score the loss ratio on developed values;
  either add IBNR to the generated incurred for the comparison or state in the
  realism wording that the incurred check compares different quantities.

## 5. MR-5 (medium) - a fixed-dollar size threshold creates a hidden trend in settlement speed

- Where: `internal/domain/claim/claim.go`, `closeLagRegime`.
- The own-damage close-lag stretch applies above a nominal $20k, while claims
  inflate at 4% a year. The share of own-damage claims above it grows from
  2.4% (AY1998) to 5.8% (AY2007).
- Own-damage paid in development year 1 falls from 0.656 to 0.607 of ultimate
  across AY1999-2007; with the stretch off it is flat at 0.70-0.73. Chain
  ladder assumes a stable pattern, so this is an unintended calendar trend. The
  step at the threshold is also a cliff: a claim just above it has a 6x mean.
- Action: compare the base-year value (deflated by the inflation index) with
  the threshold, and consider a smooth size relationship such as
  `mean x (size/threshold)^beta`.

## 6. MR-6 (medium) - the first accident year behaves like a start-up book

- Where: `internal/domain/policy/book.go` (no policies in force at the window
  start); the comment on `ExposureByMonth` in
  `internal/domain/triangle/exposure.go`; `docs/detailed-architecture.md`
  section 10.3.
- AY1998 exposure ramps from 77 exposure units in January to about 1,700 a
  month, and its claims are back-loaded (571 in January-June against 1,667 in
  July-December). AY1998 pays 0.42 of ultimate in development year 1, against
  about 0.53 for later years. On a valuation-date cut, the late-age factors
  come from exactly this row.
- The docs say the accident basis also thins at the end of the window. It does
  not: December 2007 carries a normal month (2,295 units).
- Action: simulate a warm-up underwriting year before the window and keep only
  its in-window occurrences; correct the docs.

## 7. MR-7 (low) - salvage is not tied to total losses

- Where: `internal/domain/transaction/recovery.go`, `simulateClaim`.
- 98% of salvage rows (3,723 of 3,798) land on partially damaged vehicles.
  Salvage comes from selling a written-off vehicle.
- Action: make salvage eligibility depend on the claim reaching the sum-insured
  cap, and size it off the sum insured.

## 8. MR-8 (low) - third-party severity is a bare Pareto

- Where: `internal/domain/claim/claim.go`, `drawGroundUpLoss`.
- No third-party claim is below about $3,050 after excess, the mode sits at the
  Pareto minimum, there is no policy limit, and the excess is applied to
  liability claims, which a US auto liability book would not do.
- Action: a lognormal body with a Pareto tail, an optional liability limit, and
  a switch for applying the excess to third-party claims.

## 9. MR-9 (low) - there is almost no pure IBNR, and third-party close lag ignores size

- Where: `internal/domain/claim/claim.go` (one report lag for both claim types;
  `closeLagRegime` applies the size stretch to own damage only).
- 87% of claims are reported in the occurrence month and 99.5% by development
  month 2. That matches the brief for own damage, but third-party claims report
  later in practice, and reported-count methods have nothing to estimate.
- The mission says larger claims take longer to close; for third-party claims,
  which drive late development, size and duration are independent.
- Action: a third-party report lag, and a size link in the third-party close
  lag.

## 10. MR-10 (low) - inflation steps up about 4% every 1 January

- Where: `internal/domain/claim/inflation.go`, `InflationIndex.For`.
- The index is constant within a calendar year and jumps at each year end,
  which shows as a sawtooth in the monthly-origin triangles.
- Action: interpolate the index by occurrence date. This pairs with the
  cover-term pricing in MR-3.

## 11. MR-11 (low) - off-by-one in the exposed fraction

- Where: `internal/domain/claim/claim.go`, `exposedFraction`.
- It divides in-window days by 364 (`DaysBetween(CoverStart, CoverEnd)`)
  rather than the 365-day term, giving +0.27% frequency on truncated
  last-year policies. A policy ending exactly on the window end gets a fraction
  of 1 instead of 364/365.
- Action: divide by the number of cover days.

## 12. MR-12 (low) - one setting drives two kinds of variation

- Where: `internal/domain/policy/book.go`, `simulatePolicy`.
- `spread` sets both the sum-insured lognormal sigma and the risk-factor
  standard deviation, so a YAML author cannot set them independently.
- Action: split it into two parameters.
