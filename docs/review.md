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
Figures are as measured then. Since then MR-1 and MR-2 have shipped: the
realism gate now scores the third-party liability section alone, and the
preset's own-damage close lag is shorter (mean 40 days, 3x above the size
threshold). Findings whose numbers predate that say so. MR-3, MR-10 and MR-11
have shipped too: pricing allows for nil claims and trends to the cover
midpoint, the inflation index moves smoothly by occurrence date, and the
exposed fraction counts cover days.

IDs are **MR** (model review). Severity uses the `docs/todo.md` scale: **high**
undermines the mission, **medium** worth addressing soon, **low** fix when
touching the area.

## 1. MR-4 (medium) - generated incurred is compared against reference incurred that includes IBNR

- Where: `internal/infrastructure/schedulep/reader.go` (reads only
  `PaidTriangle` and `IncurredTriangle`); `internal/domain/triangle/compare.go`
  (`lossRatio`).
- The reference incurred behaves like Schedule P total incurred (paid, case,
  bulk and IBNR): the median 12-24 factor is 0.974 and incurred converges on
  paid by age 10. Generated incurred is case plus paid, with no IBNR.
- Generated incurred also lands below 1, for an unrelated reason: nil-claim
  case releases and post-close recoveries. On the whole book the 12-24 factor
  was 0.928 with the preset, 0.954 with nil claims off, and 1.019 with
  recoveries also off. The liability section the gate now scores has no
  recoveries, so nil releases alone put its 12-24 factor at 0.982. The
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

## 2. MR-5 (medium) - a fixed-dollar size threshold creates a hidden trend in settlement speed

- Where: `internal/domain/claim/claim.go`, `closeLagRegime`.
- Measured before MR-2 recalibrated the stretch from 6x to 3x; the mechanism
  is unchanged, only its size.
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

## 3. MR-6 (medium) - the first accident year behaves like a start-up book

- Where: `internal/domain/policy/book.go` (no policies in force at the window
  start); the comment on `ExposureByMonth` in
  `internal/domain/triangle/exposure.go`; `docs/detailed-architecture.md`
  section 10.3.
- AY1998 exposure ramps from 77 exposure units in January to about 1,700 a
  month, and its claims are back-loaded (571 in January-June against 1,667 in
  July-December). AY1998 paid 0.42 of ultimate in development year 1, against
  about 0.53 for later years, before MR-2 shortened own-damage settlement;
  the ramp-up itself is unchanged. On a valuation-date cut, the late-age factors
  come from exactly this row.
- The docs say the accident basis also thins at the end of the window. It does
  not: December 2007 carries a normal month (2,295 units).
- Action: simulate a warm-up underwriting year before the window and keep only
  its in-window occurrences; correct the docs.

## 4. MR-7 (low) - salvage is not tied to total losses

- Where: `internal/domain/transaction/recovery.go`, `simulateClaim`.
- 98% of salvage rows (3,723 of 3,798) land on partially damaged vehicles.
  Salvage comes from selling a written-off vehicle.
- Action: make salvage eligibility depend on the claim reaching the sum-insured
  cap (`Claim.Ultimate == Claim.CoverLimit` since MR-1), and size it off the
  sum insured.

## 5. MR-8 (low) - third-party severity is a bare Pareto

- Where: `internal/domain/claim/claim.go`, `drawGroundUpLoss`.
- No third-party claim is below about $3,050 after excess, the mode sits at the
  Pareto minimum, there is no policy limit, and the excess is applied to
  liability claims, which a US auto liability book would not do.
- Action: a lognormal body with a Pareto tail, an optional liability limit, and
  a switch for applying the excess to third-party claims.

## 6. MR-9 (low) - there is almost no pure IBNR, and third-party close lag ignores size

- Where: `internal/domain/claim/claim.go` (one report lag for both claim types;
  `closeLagRegime` applies the size stretch to own damage only).
- 87% of claims are reported in the occurrence month and 99.5% by development
  month 2. That matches the brief for own damage, but third-party claims report
  later in practice, and reported-count methods have nothing to estimate.
- The mission says larger claims take longer to close; for third-party claims,
  which drive late development, size and duration are independent.
- Action: a third-party report lag, and a size link in the third-party close
  lag.

## 7. MR-12 (low) - one setting drives two kinds of variation

- Where: `internal/domain/policy/book.go`, `simulatePolicy`.
- `spread` sets both the sum-insured lognormal sigma and the risk-factor
  standard deviation, so a YAML author cannot set them independently.
- Action: split it into two parameters.
