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
exposed fraction counts cover days. So has MR-4: the loss ratio is scored
against reference loss ratios developed to age 10.

IDs are **MR** (model review). Severity uses the `docs/todo.md` scale: **high**
undermines the mission, **medium** worth addressing soon, **low** fix when
touching the area.

## 1. MR-5 (medium) - a fixed-dollar size threshold creates a hidden trend in settlement speed

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

## 2. MR-6 (medium) - the first accident year behaves like a start-up book

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

## 3. MR-7 (low) - salvage is not tied to total losses

- Where: `internal/domain/transaction/recovery.go`, `simulateClaim`.
- 98% of salvage rows (3,723 of 3,798) land on partially damaged vehicles.
  Salvage comes from selling a written-off vehicle.
- Action: make salvage eligibility depend on the claim reaching the sum-insured
  cap (`Claim.Ultimate == Claim.CoverLimit` since MR-1), and size it off the
  sum insured.

## 4. MR-8 (low) - third-party severity is a bare Pareto

- Where: `internal/domain/claim/claim.go`, `drawGroundUpLoss`.
- No third-party claim is below about $3,050 after excess, the mode sits at the
  Pareto minimum, there is no policy limit, and the excess is applied to
  liability claims, which a US auto liability book would not do.
- Action: a lognormal body with a Pareto tail, an optional liability limit, and
  a switch for applying the excess to third-party claims.

## 5. MR-9 (low) - there is almost no pure IBNR, and third-party close lag ignores size

- Where: `internal/domain/claim/claim.go` (one report lag for both claim types;
  `closeLagRegime` applies the size stretch to own damage only).
- 87% of claims are reported in the occurrence month and 99.5% by development
  month 2. That matches the brief for own damage, but third-party claims report
  later in practice, and reported-count methods have nothing to estimate.
- The mission says larger claims take longer to close; for third-party claims,
  which drive late development, size and duration are independent.
- The realism gate's incurred check compares generated case incurred with
  Schedule P total incurred, which includes IBNR (carried over from MR-4).
  Today it passes because both sit below 1 at early ages for different
  reasons: reference IBNR released over time, generated nil claims releasing
  their case. A real third-party report lag pushes case-incurred factors above
  1 as late claims are reported, against bands centred below 1.
- Action: a third-party report lag, and a size link in the third-party close
  lag. With the report lag, add the unreported claims' cost (pure IBNR) to the
  generated incurred the gate scores, so the incurred check stays like for
  like.

## 6. MR-12 (low) - one setting drives two kinds of variation

- Where: `internal/domain/policy/book.go`, `simulatePolicy`.
- `spread` sets both the sum-insured lognormal sigma and the risk-factor
  standard deviation, so a YAML author cannot set them independently.
- Action: split it into two parameters.
