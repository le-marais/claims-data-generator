# Review

Open review findings, recorded as reviews happen. This file lists unresolved
findings only. When a finding is fixed, delete it and renumber the positions so
the list stays dense; the finding ID is stable, and its full text stays
recoverable from git history. Name the closed IDs in the shipping commit
message.

Positions are in priority order against `docs/mission.md`. Figures in a finding
are as measured when it was recorded, unless it says otherwise.

IDs are **MR** (model review). Severity uses the `docs/todo.md` scale: **high**
undermines the mission, **medium** worth addressing soon, **low** fix when
touching the area.

## 1. MR-12 (low) - one setting drives two kinds of variation

- Where: `internal/domain/policy/book.go`, `simulatePolicy`.
- `spread` sets both the sum-insured lognormal sigma and the risk-factor
  standard deviation, so a YAML author cannot set them independently.
- Do it with the second line of business: a commercial property class needs
  the two set apart, and it changes the schema, so it belongs with that
  class's other schema changes.
- Action: split it into two parameters.

## 2. MR-8 (low) - third-party severity is a bare Pareto

- Where: `internal/domain/claim/claim.go`, `drawGroundUpLoss`.
- No third-party claim is below about $3,050 after excess, the mode sits at the
  Pareto minimum, there is no policy limit, and the excess is applied to
  liability claims, which a US auto liability book would not do.
- The limit and the excess switch are per-section settings, so they belong on
  `SectionParams` with the second line of business. The body shape shows in
  the claim-size histogram but barely moves the triangles, so it can wait.
- Action: with the second line of business, an optional per-section limit and
  a per-section switch for applying the excess. Later, a lognormal body with a
  Pareto tail as a third severity kind.
