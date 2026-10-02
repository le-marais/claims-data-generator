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

## 1. MR-19 (medium) - the final payment is always exactly 40% of the claim

- Where: `internal/domain/transaction/runoff.go`, `drawInterimPayments` and
  `runEpisode`.
- When an episode draws any interim payments, they share exactly
  `1 - settlement_share` of its cost and the final settlement pays the rest,
  so the final payment is always `settlement_share` of the cost. An episode
  with no interim payment pays 100% at close. The final payment's share takes
  only those two values.
- In the personal motor preset (seed 1, 1998-2000, 20,000 policies in the
  first year), 2,036 of the 2,044 single-episode paying claims with two or
  more payments have a final payment within $1.50 of 40% of their cost; 5,226
  of the 8,182 claims pay once. Claims 3, 13, 18 and 48 of a 150-policy run on
  the same seed split exactly 60/40.
- The triangles barely notice, but anyone reading `transactions.csv` sees a
  generated pattern, which undermines the transaction-level realism the
  mission names as a differentiator.
- Action: draw each episode's settlement share, for example a Beta with mean
  `settlement_share`, so the final payment varies by claim; refresh the golden
  hashes and re-check the realism gate.

## 2. MR-20 (low) - payment and revision timing is not tied to claim events

- Where: `internal/domain/transaction/runoff.go`, `drawInterimPayments` and
  `drawRevisions`; `lob.RunoffParams`.
- Interim payments and case revisions fall on uniformly random days of an
  episode, at Poisson rates per year of its open duration. One `runoff` block
  serves every section, so a small repair and a slow injury claim share
  `payments_per_year` and `revisions_per_year`, and nothing ties a revision to
  an event such as a repair estimate arriving.
- In the 150-policy seed-1 run of MR-19: claim 13, a repair of about $500, paid
  in two instalments over 88 days; claim 28, $5,532 of property damage, had
  four case revisions in three and a half months; claim 33 left its case
  untouched for four and a half months and revised it only on the close date.
  Each is possible, but a real file shows them less often.
- Action: when a class needs it, move the payment and revision rates onto
  `SectionParams`, and consider a revision soon after report, when the first
  estimate arrives.

## 3. MR-12 (low) - one setting drives two kinds of variation

- Where: `internal/domain/policy/book.go`, `drawVehicle`.
- `spread` sets both the sum-insured lognormal sigma and the risk-factor
  standard deviation, so a YAML author cannot set them independently. On a
  fleet book it is the spread of both within a fleet; the fleet block sets
  the two apart between fleets (`sum_insured_sigma`, `risk_spread`).
- Do it with the first class that needs the two set apart. It changes the
  schema, so it belongs with that class's other schema changes.
- Action: split it into two parameters.
