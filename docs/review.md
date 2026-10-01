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
against reference loss ratios developed to age 10. MR-5 is narrowed to the
step that remains at the size threshold. MR-6 has shipped: the book writes a
warm-up underwriting year, so AY1998 has a full book in force.

IDs are **MR** (model review). Severity uses the `docs/todo.md` scale: **high**
undermines the mission, **medium** worth addressing soon, **low** fix when
touching the area.

## 1. MR-5 (low) - the own-damage size stretch is a cliff at the threshold

- Where: `internal/domain/claim/claim.go`, `closeLagRegime`.
- The hidden calendar trend this finding first described is fixed: the
  threshold now compares the cost in start-year dollars, deflated by the claims
  inflation index. Own-damage paid in development year 1 is flat at 0.852-0.868
  of ultimate across AY1999-2007 (it fell from 0.849 to 0.835 before). The
  share above the deflated threshold still falls slightly, 2.2% to 1.6%,
  because sum insured inflates at 3% against 4% for claims, so more large
  claims hit the sum-insured cap. That is a model effect, not an artefact.
- What remains is the step: a claim just above the threshold has 3x the mean
  lag of one just below it.
- Action: consider a smooth size relationship such as
  `mean x (size/threshold)^beta` above the threshold. It replaces
  `size_multiplier`, so it breaks existing YAMLs; deferred on 2026-10-01 in
  favour of keeping the schema.

## 2. MR-7 (low) - salvage is not tied to total losses

- Where: `internal/domain/transaction/recovery.go`, `simulateClaim`.
- 98% of salvage rows (3,723 of 3,798) land on partially damaged vehicles.
  Salvage comes from selling a written-off vehicle.
- Action: make salvage eligibility depend on the claim reaching the sum-insured
  cap (`Claim.Ultimate == Claim.CoverLimit` since MR-1), and size it off the
  sum insured.

## 3. MR-8 (low) - third-party severity is a bare Pareto

- Where: `internal/domain/claim/claim.go`, `drawGroundUpLoss`.
- No third-party claim is below about $3,050 after excess, the mode sits at the
  Pareto minimum, there is no policy limit, and the excess is applied to
  liability claims, which a US auto liability book would not do.
- Action: a lognormal body with a Pareto tail, an optional liability limit, and
  a switch for applying the excess to third-party claims.

## 4. MR-9 (low) - there is almost no pure IBNR, and third-party close lag ignores size

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

## 5. MR-12 (low) - one setting drives two kinds of variation

- Where: `internal/domain/policy/book.go`, `simulatePolicy`.
- `spread` sets both the sum-insured lognormal sigma and the risk-factor
  standard deviation, so a YAML author cannot set them independently.
- Action: split it into two parameters.

## 6. MR-13 (low) - the loss-ratio drift band is far tighter than the reference

- Where: `internal/domain/triangle/compare.go`, `driftTolerance`.
- The gate fails a run whose second-half accident-year loss ratio is outside
  [1/1.10, 1.10] of its first-half one. Across the 96 reference companies,
  developed to age 10, that ratio runs from 0.54 (P5) to 1.47 (P95), median
  0.91, and only 31% of them sit inside the band.
- The generated drift comes mostly from the simulated inflation path, which
  pricing knows only by its mean. Over 120 seeds of the preset at a 40k book
  it has mean 1.007 and standard deviation 0.050, and 9 of 120 seeds fall
  outside the band (3 of 120 before MR-6 gave AY1998 its full weight). The
  gate passes because it runs three fixed seeds.
- The band is a guard against systematic drift, such as pricing and claims
  inflation trending apart, not a realism band. As a guard it also caps
  realistic randomness: it is what holds the preset's `adequacy_volatility`
  at 0.03.
- Action: decide what the check is for. To keep it as a drift guard, score
  the expected drift over several seeds, or remove the inflation path's
  noise before scoring. To make it a realism check, use the reference P5-P95
  band like the other metrics.
