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

## 1. MR-15 (high) - the realism reference mixes premium bases and keeps unstable companies

- Where: `data/reference/schedule p/ppauto_pos98-07/*.json`,
  `data/reference/gr-code-list.md`, `tools/prune-dec2025.ps1`,
  `internal/infrastructure/schedulep/reader.go`,
  `internal/domain/triangle/compare.go` (`CompareToReference`).
- The reference files come from the CAS loss reserving database, accident
  years 1998-2007. In all 772 files across the six lines, `EarnedPremium` is
  the CAS `EarnedPremDIR` column, direct and assumed premium, while paid and
  incurred are the net-of-reinsurance `CumPaidLoss` and `IncurredLosses`. Every
  loss ratio and drift the gate scores is net losses over gross premium. The
  README's "Part 1B is net of reinsurance" holds for the losses only.
- In the 96 kept companies, net premium is a median 0.89 of direct (P5 0.52).
  On net premium the loss-ratio band moves from [0.256, 0.799] to
  [0.425, 0.900] and the drift band from [0.537, 1.472] to [0.655, 1.159]. The
  preset's loss ratio, 0.69-0.76 on the gate seeds, sits near the top of
  today's band because the band is biased low.
- The JSON keeps no net or ceded premium, bulk reserve, single-entity flag or
  company name, so the hand curation could not see reinsurance or volume
  changes. 36 of the 96 kept companies fail Meyers' selection test (the
  coefficient of variation of net premium under 0.45 and of the net-to-direct
  ratio under 0.125, CAS Monograph 1, table 11) or are reinsurers; all 60 that
  pass are kept. Among the 36: Home State County Mutual (29297) keeps 5% of its
  direct premium, and its 0.008 loss ratio on direct premium is the band
  minimum; Colorado Farm Bureau (13641) keeps $4k of its $14.0m direct
  premium in 2007;
  Antilles (10308) writes about $70k a year and its 12-month incurred is ten
  times its ultimate; Dorinco (33499), Sirius America (35408) and Toa-Re
  (42439) are reinsurers.
- 29 kept companies write under $5m of net premium a year, against $9-18m a
  year on the scored sections of the gate's 40k book. Their factors are mostly
  claim sampling noise, and they set the band edges: three of the five lowest
  paid factors at age 2-3 come from companies writing $0.6m-$1.6m a year.
- On the 45 complete companies that pass Meyers' test, are not reinsurers and
  write at least $5m a year, the preset fails the paid factor at age 3-4 on
  all three gate seeds and at age 2-3 on two. It pays 54% of its 120-month
  paid within 12 months, against a median of 44%.
- On that pool the drift band is [0.752, 1.079] around a median of 0.92: the
  2001-2004 hard market improved the later accident years of most companies,
  a calendar effect the generator does not model. The cycle-free preset, mean
  drift 1.02, fails it on 6 of 60 seeds at a 40k book.
- Action: replace the JSON with the unmodified CAS CSVs; read net and direct
  premium; select the companies in code, with Meyers' limits, positive
  premium in every year, at least $5m a year and no reinsurers, in place of
  the keep-list and its script; score the loss ratio and drift on net
  premium; score drift relative to the pool median; recalibrate the preset's
  injury settlement; run the gate at a 100k book. Plan:
  `docs/superpowers/plans/2026-10-02-mr-15-rebuild-reference.md`.

## 2. MR-16 (medium) - incurred is scored including bulk reserves

- Where: `internal/domain/triangle/compare.go` (`ReferenceSet`),
  `internal/application/realism.go` (`SectionComparison`).
- Schedule P Part 2 incurred includes bulk and IBNR reserves (Part 4). The
  gate scores it against the generated paid plus case plus pure IBNR held at
  its true value. A company's bulk reserve is a reserving judgement, held
  early and released later, which the generator does not model, so the README
  calls the incurred check a loose sanity bound.
- Meyers scores reported incurred, Part 2 less Part 4. The CAS files carry
  Part 4 as `BulkLoss`, so case incurred is available, and its like-for-like
  generated counterpart is paid plus case (`AnnualSet.Incurred`).
- On MR-15's 45-company pool, case-incurred development at age 1-2 runs from
  0.99 (P5) to 1.38 (P95), median 1.12. The preset's reported incurred
  develops 1.03-1.05 there, at P22-P27, and sits at P9-P27 over the first
  three factors: inside the bands, but light.
- Action: after MR-15, read `BulkLoss`, score the generated reported incurred
  against case-incurred bands, and drop the loose-bound caveat.

## 3. MR-17 (medium) - each development age is scored alone

- Where: `internal/domain/triangle/compare.go` (`CompareToReference`,
  `checkAges`).
- Every age-to-age factor is checked against its own band, so a pattern that
  sits at the same edge at every age passes. In the current pool the preset's
  paid factors at ages 2-3, 3-4 and 4-5 sit at P9, P10 and P20, and it pays
  54.5% of its 120-month paid within 12 months, about P88 of the reference.
- Action: also score cumulative development, for example paid to date at each
  age as a share of paid at age 10, against the same shares across the
  reference companies.

## 4. MR-12 (low) - one setting drives two kinds of variation

- Where: `internal/domain/policy/book.go`, `simulatePolicy`.
- `spread` sets both the sum-insured lognormal sigma and the risk-factor
  standard deviation, so a YAML author cannot set them independently.
- Do it with the second line of business: a commercial property class needs
  the two set apart, and it changes the schema, so it belongs with that
  class's other schema changes.
- Action: split it into two parameters.

## 5. MR-8 (low) - injury severity is a bare Pareto, and liability takes the excess

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

## 6. MR-18 (low) - the other Schedule P lines are thin, and the gate folds the tail

- Where: `data/reference/schedule p/`, `internal/application/realism.go`
  (`grid.AnnualTriangles`, which folds development past age 10).
- Under MR-15's rules, with each line's own Meyers limits, the complete
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
