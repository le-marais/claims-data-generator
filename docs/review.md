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

## 3. MR-18 (low) - the other Schedule P lines are thin, and the gate folds the tail

- Where: `data/reference/schedule p/`, `internal/application/realism.go`
  (`grid.AnnualTriangles`, which folds development past age 10).
- Under the rules `application.PersonalMotorCriteria` applies, with each
  line's own Meyers limits, the complete
  companies that pass and are not reinsurers number: commercial auto 54,
  workers compensation 30, other liability 55, products liability 19,
  medical malpractice 2. A $5m-a-year floor leaves 25, 17, 15, 4 and 0, and a
  $1m floor 42, 25, 31, 8 and 2. Only commercial auto keeps 40 or more, and
  only at the lower floor, so the size floor has to be set per line.
- Products liability and medical malpractice are too thin for P5-P95 bands;
  Meyers left them out for the same reason. Medical malpractice is also
  claims-made, a different coverage trigger. Other liability is a mixed bucket
  that cedes heavily (kept net-to-direct median 0.72), so its bands would be
  too wide to discriminate.
- The CAS files hold these six liability lines only. Homeowners (Part 1A),
  commercial multiple peril (1E), special property (1I) and auto physical
  damage (1J) are not in them, so the roadmap's commercial property class has
  no Schedule P reference here.
- The annual triangles fold generated development past age 10 into the last
  column. Private passenger auto is at ultimate by age 10 (median paid factor
  at 9-10 of 1.000 on the stable pool), but workers compensation (1.011) and
  products liability (1.012) are not, so on a long-tail line the last factors
  and the loss ratio would be scored against a reference short of ultimate.
- Action: choose the second line of business knowing that commercial auto is
  the only other line with a reference pool; set the size floor per line;
  score long-tail lines at age 10 without folding.
