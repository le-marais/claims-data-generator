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

## 1. MR-14 (medium) - one third-party section stands in for all of Schedule P Part 1B

- Where: `internal/infrastructure/config/motor-personal.yaml` (`third_party`
  section), `internal/domain/lob/lob.go` (`ClaimParams.validate`, at most one
  scored section), `internal/application/realism.go` (`SectionComparison`).
- The reference is Schedule P Part 1B, private passenger auto
  liability/medical: bodily injury liability, property damage liability,
  personal injury protection, medical payments and uninsured motorist, on an
  accident-year basis, net of reinsurance, with defence and cost containment
  in paid and incurred, paid net of salvage and subrogation, and incurred
  including bulk and IBNR. Physical damage is Part 1J, which has no 10-year
  history.
- By claim count Part 1B is mostly third-party property damage: small claims
  settled in weeks. Injury claims are fewer, larger and slow. The preset has one
  `third_party` section, described as injury claims (Pareto from $4,000, mean
  close 340 days), whose lags were tuned until the blend's annual triangles
  fit the bands. Company 10007 pays about 49% of accident year 1998 within
  12 months, which a pure injury book would not. The triangles pass, but at
  claim level the section is one claim type that is too frequent and fast for
  injury and too large and slow for property damage, which undercuts the
  mission's transaction-level realism.
- The gate can score only one section, so the two claim types cannot be split
  and still scored together against Part 1B.
- `docs/roadmap.md` says "Schedule P carries liability lines only" and calls
  commercial auto "the closest short-tail fit". It is the CAS extract that
  holds only liability lines. Schedule P itself has homeowners (Part 1A) and
  commercial multiple peril (Part 1E) on ten years, and special property (1I)
  and auto physical damage (1J) on short histories. Commercial auto liability
  is not short-tail.
- Out of scope: first-party injury cover (personal injury protection, medical
  payments, uninsured motorist), which a comprehensive personal motor product
  barely carries, and defence costs and reinsurance, which the calibration
  absorbs implicitly. The README should say so.
- Action: allow several scored sections and score their union against the
  reference; add a `lognormal` severity kind in dollars; split `third_party`
  into `third_party_property` and `third_party_injury` and recalibrate; state
  what Part 1B includes and excludes in the README and the preset; correct the
  roadmap wording. MR-8's limit and excess switch stay separate.

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
- The limit and the excess switch are per-section settings, so they belong on
  `SectionParams` with the second line of business. The body shape shows in
  the claim-size histogram but barely moves the triangles, so it can wait.
- Action: with the second line of business, an optional per-section limit and
  a per-section switch for applying the excess. Later, a lognormal body with a
  Pareto tail as a third severity kind.
