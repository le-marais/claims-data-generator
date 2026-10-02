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

- Where: `internal/domain/policy/book.go`, `drawVehicle`.
- `spread` sets both the sum-insured lognormal sigma and the risk-factor
  standard deviation, so a YAML author cannot set them independently. On a
  fleet book it is the spread of both within a fleet; the fleet block sets
  the two apart between fleets (`sum_insured_sigma`, `risk_spread`).
- Do it with the first class that needs the two set apart. It changes the
  schema, so it belongs with that class's other schema changes.
- Action: split it into two parameters.

## 2. MR-8 (low) - injury severity is a bare Pareto, and personal motor's liability takes the excess

- Where: `internal/domain/claim/claim.go`, `drawGroundUpLoss`;
  `internal/infrastructure/config/motor-personal.yaml`.
- No third-party injury claim is below the Pareto minimum less the excess:
  in the personal preset, with scale 6000 and excesses of $0-$1,000, about
  $5,000-$6,000 after excess in start-year dollars, and the mode sits at that
  floor; in the commercial preset, which takes no excess on liability, its
  $9,000 scale. The `lognormal` kind gives property damage a lognormal body,
  but injury is still a bare Pareto.
- The personal preset applies the excess to its liability claims, which a US
  auto liability book would not do. On $1,000-excess policies it discards
  about 26% of property damage ground-up losses (median 1800, sigma 0.9), so
  the reported property damage frequency and severity depend on the insured's
  own-damage excess. The per-section `no_excess` switch exists, and the
  commercial preset's liability sections use it; switching it on for the
  personal preset raises its liability frequency and cost, so its third-party
  frequencies need re-setting and its realism gate re-checking.
- The body shape shows in the claim-size histogram but barely moves the
  triangles, so it can wait.
- Action: set `no_excess: true` on the personal preset's two liability
  sections and recalibrate them. Later, a lognormal body with a Pareto tail as
  another severity kind.
