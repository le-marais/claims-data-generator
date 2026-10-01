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
MR-4, MR-6, MR-9, MR-10, MR-11 and MR-13 have since shipped; their text is
in git history.

On 2026-10-01 the remaining findings were re-ranked against `docs/mission.md`
(synthetic data a reserving team can use without manual fixes, one engine for
several short-tail classes, realism checked against Schedule P). Positions
follow that ranking; the sequence across this file and `docs/todo.md` lives in
`docs/roadmap.md`. MR-5, the step left at the own-damage size threshold, was
dropped as an accepted simplification and is listed in the README instead.

IDs are **MR** (model review). Severity uses the `docs/todo.md` scale: **high**
undermines the mission, **medium** worth addressing soon, **low** fix when
touching the area.

## 1. MR-7 (low) - salvage is not tied to total losses

- Where: `internal/domain/transaction/recovery.go`, `simulateClaim`.
- 98% of salvage rows (3,723 of 3,798) land on partially damaged vehicles.
  Salvage comes from selling a written-off vehicle.
- Cheap, and it protects trust in the transaction-level detail, the mission's
  first differentiator.
- Action: make salvage eligibility depend on the claim reaching the sum-insured
  cap (`Claim.Ultimate == Claim.CoverLimit` since MR-1), and size it off the
  sum insured.

## 2. MR-12 (low) - one setting drives two kinds of variation

- Where: `internal/domain/policy/book.go`, `simulatePolicy`.
- `spread` sets both the sum-insured lognormal sigma and the risk-factor
  standard deviation, so a YAML author cannot set them independently.
- Do it with the second line of business: a commercial property class needs
  the two set apart, and it changes the schema, so it belongs with that
  class's other schema changes.
- Action: split it into two parameters.

## 3. MR-8 (low) - third-party severity is a bare Pareto

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
