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

## 2. MR-8 (low) - injury severity is a bare Pareto, and liability takes the excess

- Where: `internal/domain/claim/claim.go`, `drawGroundUpLoss` and
  `simulateClaim`.
- No third-party injury claim is below the Pareto minimum less the excess:
  with scale 6000 and excesses of $0-$1,000, about $5,000-$6,000 after excess
  in start-year dollars, and the mode sits at that floor. The `lognormal` kind
  gives property damage a lognormal body, but injury is still a bare Pareto.
- The excess is applied to liability claims, which a US auto liability book
  would not do. On $1,000-excess policies it discards about 26% of property
  damage ground-up losses (median 1800, sigma 0.9), so the reported property
  damage frequency and severity depend on the insured's own-damage excess.
- The excess switch is a per-section setting, so it belongs on
  `SectionParams`, beside `limit`. The body shape shows in the claim-size
  histogram but barely moves the triangles, so it can wait.
- Action: with the second line of business, a per-section switch for applying
  the excess. Later, a lognormal body with a Pareto tail as another severity
  kind.
