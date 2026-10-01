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
Figures are as measured then, except where a finding says otherwise. MR-1 to
MR-4, MR-6, MR-10, MR-11 and MR-13 have since shipped; their text is in git
history.

On 2026-10-01 the remaining findings were re-ranked against `docs/mission.md`
(synthetic data a reserving team can use without manual fixes, one engine for
several short-tail classes, realism checked against Schedule P). Positions
follow that ranking; the sequence across this file and `docs/todo.md` lives in
`docs/roadmap.md`. MR-5, the step left at the own-damage size threshold, was
dropped as an accepted simplification and is listed in the README instead.

IDs are **MR** (model review). Severity uses the `docs/todo.md` scale: **high**
undermines the mission, **medium** worth addressing soon, **low** fix when
touching the area.

## 1. MR-9 (medium) - there is almost no pure IBNR, and third-party close lag ignores size

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
- Raised to medium on 2026-10-01: it is the largest gap between the data and
  what a reserving demo needs, and the mission names both report lags and
  larger claims taking longer to close.
- Action: a third-party report lag, and a size link in the third-party close
  lag. With the report lag, add the unreported claims' cost (pure IBNR) to the
  generated incurred the gate scores, so the incurred check stays like for
  like.

## 2. MR-7 (low) - salvage is not tied to total losses

- Where: `internal/domain/transaction/recovery.go`, `simulateClaim`.
- 98% of salvage rows (3,723 of 3,798) land on partially damaged vehicles.
  Salvage comes from selling a written-off vehicle.
- Cheap, and it protects trust in the transaction-level detail, the mission's
  first differentiator.
- Action: make salvage eligibility depend on the claim reaching the sum-insured
  cap (`Claim.Ultimate == Claim.CoverLimit` since MR-1), and size it off the
  sum insured.

## 3. MR-12 (low) - one setting drives two kinds of variation

- Where: `internal/domain/policy/book.go`, `simulatePolicy`.
- `spread` sets both the sum-insured lognormal sigma and the risk-factor
  standard deviation, so a YAML author cannot set them independently.
- Do it with the second line of business: a commercial property class needs
  the two set apart, and it changes the schema, so it belongs with that
  class's other schema changes.
- Action: split it into two parameters.

## 4. MR-8 (low) - third-party severity is a bare Pareto

- Where: `internal/domain/claim/claim.go`, `drawGroundUpLoss`.
- No third-party claim is below about $3,050 after excess, the mode sits at the
  Pareto minimum, there is no policy limit, and the excess is applied to
  liability claims, which a US auto liability book would not do.
- The limit and the excess switch are per-class decisions, so they belong with
  the second line of business. The body shape shows in the claim-size
  histogram but barely moves the triangles, so it can wait.
- Action: with the second line of business, an optional liability limit and a
  switch for applying the excess to third-party claims. Later, a lognormal
  body with a Pareto tail.
