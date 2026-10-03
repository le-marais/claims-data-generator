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

## 1. MR-20 (low) - revision timing is not tied to claim events

- Where: `internal/domain/transaction/runoff.go`, `drawRevisions` and
  `drawInterimPayments`; `lob.RunoffParams`.
- Case revisions fall on uniformly random days of an episode at a Poisson
  rate per year of its open duration, and interim payments do too, within
  the payment delay. One `runoff` block serves every section, so a small
  repair and a slow injury claim share `payments_per_year` and
  `revisions_per_year`. Apart from the bill before a payment, nothing ties a
  revision to an event such as a repair estimate arriving.
- In a 150-policy seed-1 run of the personal motor preset: claim 28, $5,532
  of property damage, had four case revisions in three and a half months;
  claim 33 left its case untouched for four and a half months. Each is
  possible, but a real file shows them less often.
- Action: when a class needs it, move the payment and revision rates onto
  `SectionParams`, and consider a revision soon after report, when the first
  estimate arrives.

## 2. MR-12 (low) - one setting drives two kinds of variation

- Where: `internal/domain/policy/book.go`, `drawVehicle`.
- `spread` sets both the sum-insured lognormal sigma and the risk-factor
  standard deviation, so a YAML author cannot set them independently. On a
  fleet book it is the spread of both within a fleet; the fleet block sets
  the two apart between fleets (`sum_insured_sigma`, `risk_spread`).
- Do it with the first class that needs the two set apart. It changes the
  schema, so it belongs with that class's other schema changes.
- Action: split it into two parameters.
