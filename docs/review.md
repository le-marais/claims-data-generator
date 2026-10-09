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

## 1. MR-21 (medium) - the case estimate jumps up and down around the true cost

- Where: `internal/domain/transaction/runoff.go`, `runEpisode`;
  `runoff.revision_sigma`.
- Each revision sets the case to the remaining cost times the adequacy bias
  times fresh mean-one lognormal noise. Consecutive revisions are independent
  draws around the true cost, not a path that moves when information
  arrives, so the case swings both ways. A real case reserve is sticky: it
  steps when new information arrives, mostly in one direction. Revision days
  are drawn independently too, so two revisions can fall on one day.
- In the personal preset (seed 1, 1998-2000, 20,000 policies), the median
  revision moves the case 23% on own damage, 21% on property damage and 14%
  on injury. 47%, 43% and 31% of revisions move it more than 25%, and
  consecutive revisions reverse direction 54%, 53% and 65% of the time. In
  the 150-policy run on the same seed, injury claim 39 ($23,503) moved its
  case from $26,485 to $36,100 on 2000-10-22, to $33,280 on 11-04 and to
  $26,723 on 11-08; claim 35 raised its case twice on 2000-08-22.
- The jumps show in `transactions.csv` and add noise to incurred
  development, which case-based reserving methods read.
- Action: model the case as a path that moves part of the way from its
  current level toward the aim at each revision, with at most one revision
  a day; re-check the incurred factors against the realism gate.

## 2. MR-22 (low) - case estimates are set to the cent

- Where: `internal/domain/transaction/estimate.go`, `CaseEstimator.Apply`;
  `internal/domain/transaction/runoff.go`, revision and bill targets.
- Every case estimate a handler sets - the opening case, each revision, each
  bill above the case - is a computed amount to the cent, such as $599.12,
  $10,267.66 or $2,895.72. Handlers set reserves in round figures, and many
  insurers open every claim of a type at a standard reserve. Payments to the
  cent are realistic; case estimates to the cent are not.
- Of the 8,682 case openings (first reports and reopens) in the personal
  preset (seed 1, 1998-2000, 20,000 policies), none is a whole $100 and 13
  are a whole $10.
- Action: round each case estimate a handler sets to a step that grows with
  its size, for example $50 under $1,000, $100 under $10,000 and $1,000
  above, and consider a standard opening reserve per section. A case set to
  a bill, payments and the release at close stay exact.

## 3. MR-20 (low) - revision timing is not tied to claim events

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

## 4. MR-12 (low) - one setting drives two kinds of variation

- Where: `internal/domain/policy/book.go`, `drawVehicle`.
- `spread` sets both the sum-insured lognormal sigma and the risk-factor
  standard deviation, so a YAML author cannot set them independently. On a
  fleet book it is the spread of both within a fleet; the fleet block sets
  the two apart between fleets (`sum_insured_sigma`, `risk_spread`).
- Do it with the first class that needs the two set apart. It changes the
  schema, so it belongs with that class's other schema changes.
- Action: split it into two parameters.
