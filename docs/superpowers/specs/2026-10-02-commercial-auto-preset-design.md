> **OUT OF CONTEXT - do not read (2026-10-02):** historical design record, kept for provenance only. Agents must not load this file into context or treat it as a source of truth; it records decisions as of its own date and may not match current behaviour. For how the system works today see `README.md`, `AGENTS.md`, and `docs/architecture.md`.

# Commercial auto preset - design

Date: 2026-10-02

## Goal

Add a second shipped line of business, commercial auto (`motor-commercial`),
calibrated against its own Schedule P reference, Part 1C commercial auto/truck
liability. This is the roadmap's "second line of business" item: per-line
reference data, a fleet book, liability sections that take no excess (MR-8's
switch), and slower settlement.

## Decisions

- The line-of-business YAML and the `lob` domain object hold simulation
  parameters only. Which Schedule P line a class is scored against, and which
  of its sections are scored, is evaluation, and lives in the preset registry.
  The existing `scored: true` section flag moves out with it.
- A fleet is a level above today's policies. Fleets are simulated first; each
  vehicle on a fleet is then a policy simulated by today's mechanism, given the
  fleet's values.
- The per-section excess switch ships, and only commercial auto uses it.
  Personal motor's liability keeps the excess; MR-8 stays open for it.
- The commercial auto pool uses a $1m-a-year size floor (42 companies).

## 1. Fleet book

YAML, under `book`:

```yaml
fleet:
  size: {median: 2, sigma: 1.1}  # lognormal vehicles per fleet, rounded, at least 1
  sum_insured_sigma: 0.5         # between fleets: lognormal sigma of the fleet's median vehicle value
  risk_spread: 0.4               # between fleets: sd of the mean-one gamma fleet risk factor
```

- `fleet.size.median` 0, or no `fleet` block, switches fleets off, and the
  other fleet fields are then not required. Switched on, the median must be at
  least 1 and the sigmas and the risk spread must not be negative.
- Fleets off is today's code path draw for draw: each policy is a fleet of one,
  its `fleet_id` equals its `policy_id`, and it draws from `policy-<id>`
  exactly as now. Personal motor's simulation does not move.
- Fleets on: each fleet draws from its own `fleet-<id>` stream, in a fixed
  order: cover start, excess (from `excess_choices`), vehicle count
  (`max(1, round(LogNormal(ln median, sigma)))`), median vehicle value
  (`sum_insured_median`, drifted to the underwriting year, times a median-one
  lognormal of `sum_insured_sigma`), and the fleet risk factor (mean-one gamma
  with standard deviation `risk_spread`; 1 with no draw at 0). Every vehicle
  shares the fleet's cover dates and excess: a fleet is one contract with one
  inception date and one deductible.
- Each vehicle is a policy with its own `policy-<id>` stream, ids running on
  across fleets. It draws its sum insured as `LogNormal(ln fleet median,
  spread)` and its risk factor as the fleet risk factor times today's mean-one
  gamma of `spread`, and is priced as today. `spread` therefore becomes the
  within-fleet heterogeneity when fleets are on. MR-12 (splitting `spread`)
  stays open.
- Claims, pricing, the ledger and the aggregates are unchanged: claims are
  drawn per policy (vehicle) as now, and the fleet risk factor correlates the
  claim frequency of a fleet's vehicles.
- Book size counts fleets: `initial_book_size` and `growth_factor` count fleets
  written, and the vehicles follow from them. `policy.ProjectedSize` projects
  policy rows (vehicles), using the lognormal mean of the fleet size as the
  expected vehicles per fleet, so the UI's run cap still bounds the rows that
  cost run time.
- `policy.Policy` gains `FleetID`. `policies.csv` gains a `fleet_id` column
  after `policy_id`. `exposure.csv` is unchanged in meaning: its policy-years
  are vehicle-years.
- Form fields: `book.fleet.size.median`, `book.fleet.size.sigma`,
  `book.fleet.sum_insured_sigma`, `book.fleet.risk_spread`.

## 2. Per-section excess switch

- `no_excess: true` on a claims section and on its pricing section, as `limit`
  sits on both. The zero value keeps today's behaviour: the excess applies.
- With it on, the claim stage takes no excess off the ground-up loss, so every
  loss is reported, and a sum-insured section's cover limit is the full sum
  insured. Pricing prices the section at excess 0.
- Only commercial auto's two liability sections set it. A boolean switch, like
  `recoveries`, so no form field.

## 3. Realism profiles outside the YAML

- Domain and YAML: remove `SectionParams.Scored`, `ClaimParams.ScoredSections`
  and the config `scored` field. `motor-personal.yaml` drops its two
  `scored: true` lines; its simulation is unchanged.
- `refdata` embeds `ppauto_pos98-07.csv` and `comauto_pos_98-07.csv`, with a
  file name per Schedule P line.
- `application`:
  - `ReferenceLine{ID, Label, Criteria}` for `private_passenger_auto`
    (`PersonalMotorCriteria`, unchanged) and `commercial_auto`
    (`CommercialAutoCriteria`: net premium CV under 0.399, net-to-direct CV
    under 0.125, Meyers' limits for the line, and at least $1m a year; no
    reinsurer passes, so nothing is excluded; 42 companies).
  - `RealismProfile{Line string, Sections []string}`, with a method that
    resolves the section names to indices against a line of business and
    errors on a name it lacks. No sections scores the whole book.
- `config.PresetInfo` gains `Realism application.RealismProfile`. Both presets
  score `third_party_property` and `third_party_injury`, motor against
  `private_passenger_auto` and commercial against `commercial_auto`.
- Web: `NewServer` takes one selected pool per line. The run request carries
  `preset`. A run with no preset, an unknown one, a profile whose line has no
  pool or whose sections the parameters lack is not scored. The realism JSON
  carries `scored`, the line's label, the pool's company count and its size
  floor, and `app.js` builds the scope text from them, or says the run is not
  scored. The download path ignores `preset`.
- CLI: `generate --preset ID` (default `motor-personal`), which cannot be
  combined with `--config`. `ui` loads every line's pool.

## 4. The preset

`internal/infrastructure/config/motor-commercial.yaml`, registered as
`motor-commercial` / "Motor commercial":

- Book: a fleet block (median about 2 vehicles, heavy tail), a per-vehicle
  median value of about $35k, commercial physical damage deductibles of
  $250-$5,000.
- `own_damage`: physical damage, sum-insured lognormal, recoveries, short-tail
  settlement. Not scored.
- `third_party_property`: lognormal, larger than personal motor's,
  `no_excess`.
- `third_party_injury`: Pareto with a heavier tail, `no_excess`, a $1m limit
  as in a combined single limit, and slower settlement than personal motor.
- Runoff: cases opened deficient (`case_adequacy_mean` above 1), since Part
  1C's case incurred develops upward (median factor about 1.26 from age 1 to
  2, against 1.12 for personal auto).
- A target loss ratio of about 0.60, against the pool's median of 0.55 and
  P5-P95 band of 0.33-0.68.
- Calibration aims at Part 1C's paid pattern (a median of 32% of age-10 paid by
  the end of year 1) and is checked on seeds 1-30.

## 5. Tests

- `TestDefaultPresetIsRealistic` becomes table-driven over every registered
  preset with a realism profile, scoring each against its line's pool on seeds
  1, 42 and 7. The drift guard and the loss-ratio-near-target test cover both
  presets.
- `TestCommercialAutoPool` pins the 42 companies.
- A new golden hash over the commercial preset's annual triangles.
- Motor: `wantHash` changes for the `fleet_id` column only;
  `wantAggregateHash` and `wantAnnualHash` must not change.
- Domain tests: fleet draws and their switch-off, vehicles sharing cover and
  excess, the fleet risk factor, `ProjectedSize` with fleets, `no_excess` in
  the claim stage and in pricing.
- Config: `TestToDomainMapsEveryField` covers the new fields; the realism
  profiles resolve against their presets.
- Server: `/api/lobs` lists both presets; a commercial run scores against the
  commercial pool; the not-scored path.
- CLI: `--preset`, and `--preset` with `--config` rejected.

## 6. Docs

README (CLI, fleet book, `no_excess`, the realism section for each line, the
model diagrams), `docs/architecture.md`, `data/reference/README.md`,
`AGENTS.md` (gate test, embedded files, where the realism profile lives, the
adding-a-parameter and adding-a-line checklists), `docs/mission.md`, the
roadmap (the second-line-of-business section goes), MR-8 (the switch ships;
open: switching it on for personal motor's liability with a recalibration,
and the body shape), and the README screenshots.

## Out of scope

MR-12 (splitting `spread`); the todo's exposure milestone (the UI stays
loopback-only); vehicles joining or leaving a fleet mid-term.
