# Claims data generator - MVP design

Date: 2026-07-15
Status: approved pending review

## Summary

A local Go CLI app, `claimsgen`, that generates fully synthetic insurance claims data (policies, claims, transactions) as CSV files for use as dummy input to reserving processes. MVP covers personal motor insurance, parameterized per line of business so other short tail classes can be added later. CLI first; a web front end is a later phase. Realism is assessed against the Schedule P private passenger auto reference data (`data/reference/ppauto_pos98-07/`) via a library function wired into the test suite, exposable as an app feature later.

## Decisions made during design

- Interface: CLI for the MVP, web UI later.
- Output format: CSV.
- Scale: realistic book - around 10k-100k policies per year over ~10 years, matching the Schedule P 1998-2007 span.
- Realism evaluation: a library function used as a unit test in the MVP, possible app feature later.
- Tech stack: Go. Single static binary, distributions from `gonum/stat/distuv`, easy embedded web server later.
- Runoff design: ultimate-first (simulate true ultimate, then payments, then case path) rather than deriving payments from a freely simulated case path, because calibration against paid and incurred triangles needs direct control over totals.

## Architecture

Domain-driven, layered Go module. Dependencies point inward only - the domain depends on nothing outside itself.

```
cmd/claimsgen/                   Interface layer: CLI entry point (stdlib flag)
internal/application/            Use cases: GenerateDataset, EvaluateRealism -
                                 orchestrate domain services, no business logic
internal/domain/
    policy/                      Policy entity, PolicyBook aggregate,
                                 BookSimulator domain service (step 1)
    claim/                       Claim entity with lifecycle (occurred -> reported -> closed),
                                 ClaimSimulator domain service (step 2)
    transaction/                 Transaction entity, CaseEstimatePath value object,
                                 RunoffSimulator domain service (steps 3-4)
    lob/                         LineOfBusiness value object: the full parameter set
                                 (frequency, severity, lags, excess choices)
    triangle/                    DevelopmentTriangle value object, aggregation from
                                 transactions, realism comparison service
    shared/                      Money, date helpers, RandomSource interface
internal/infrastructure/
    config/                      YAML adapter mapping files to LineOfBusiness presets
    csv/                         repository-style writers for the three datasets
    scheduleP/                   reader for the reference JSON triangles
    random/                      gonum-backed distribution implementations of RandomSource
```

The ubiquitous language comes from reserving: book, exposure, occurrence, report lag, case estimate, runoff, development triangle. Distributions sit behind a domain-owned `RandomSource` interface so the domain expresses what is sampled while gonum stays in infrastructure. Domain logic is testable with fixed random sources.

### CLI usage

```
claimsgen generate --config motor-personal.yaml --seed 42 --out ./output/
```

A `motor-personal.yaml` preset is embedded in the binary, so the zero-config happy path is `claimsgen generate`.

### Reproducibility

One master seed derives independent sub-streams per stage and per policy. The same seed and config always produce byte-identical CSVs, and adding a stage later does not reshuffle existing streams.

## Output datasets

Three CSVs with a policy -> claims -> transactions key chain:

- **policies.csv**: `policy_id, cover_start, cover_end, sum_insured, excess, risk_factor, premium`
- **claims.csv**: `claim_id, policy_id, occurrence_date, report_date, close_date, initial_estimate`
- **transactions.csv**: `transaction_id, claim_id, date, type (PAYMENT | ESTIMATE), amount`

Transaction convention: `ESTIMATE` rows carry the signed movement in the outstanding case estimate; `PAYMENT` rows carry the amount paid (positive). A payment event writes a `PAYMENT` row plus a matching negative `ESTIMATE` row (money out reduces outstanding). A pure revision writes only an `ESTIMATE` row. Outstanding case = initial estimate plus cumulative `ESTIMATE` movements after report, and reaches exactly zero at the close date.

There is no valuation date - all claims develop fully and run to closure, supporting out-of-sample analysis.

## Simulation model

### Step 1: policy book

For each calendar year in the simulated period:

- Book size is a stochastic recursion. Year 1 uses the configured initial size; each later year's count = previous year's count x growth factor x a lognormal noise term with mean 1 (volatility is a parameter). The trend compounds off the actual prior-year book, and some years genuinely shrink.
- Per policy:
  - Cover start uniform over the year, 12-month term (exposure spills into the next year).
  - Sum insured ~ lognormal. Mean drifts per calendar year (vehicle value inflation); sigma is driven by the book's spread parameter - the single knob for homogeneous vs volatile books.
  - Risk factor ~ gamma with mean 1, variance tied to the spread parameter. Multiplies claim frequency.
  - Excess sampled from the configured discrete set (0, 100, 300, 500, 1000 for motor) with configurable weights.
  - Premium = sum insured x rate factor x risk factor loading, so the book's implied loss ratio can be checked against a target.

### Step 2: claim events

Per policy, claim count ~ Poisson with mean = base frequency x risk factor x exposed fraction of the term. Per claim:

- Occurrence date uniform over the exposed period.
- Report lag ~ lognormal, parameterized so motor gives mostly 0-2 days with a soft upper end near 30 days. Lognormal is chosen over exponential because the mass near zero plus a long thin tail matches "most within a day or two, outliers allowed".
- Ground-up loss ~ mixture: with probability ~85% an own-damage claim from a lognormal scaled by the policy's sum insured; with probability ~15% a third-party liability claim from a Pareto - heavy tailed, not capped by sum insured. Mixture weight and both distributions are LoB parameters.
- Initial estimate = ground-up loss minus excess. If the loss falls below the excess the claim is not reportable and is discarded; the frequency parameter is calibrated as "reported claims" so this introduces no bias.
- Close lag ~ gamma (shape > 1 avoids unrealistic mass at near-zero delays; chosen over pure exponential for this reason). Mean is scaled by a size multiplier: initial estimates above a threshold get a longer mean delay (step function), and the multiplier also loads on risk factor. Small claims close in days to weeks; large liability claims take months to years.

### Steps 3-4: case estimate runoff and payments

Ultimate-first design, per claim:

1. **True ultimate.** Ultimate cost = initial estimate x lognormal error. The mean sets systematic case adequacy (over/under-reserving); the sigma sets how wrong individual initial estimates are. Both are LoB parameters.
2. **Payment schedule.** Number of interim payments scales with claim duration (Poisson on duration; small own-damage claims typically 0-1 interim payments, long liability claims several). A final settlement payment always lands on the close date. Ultimate is split across payments by a Dirichlet draw, with a concentration parameter controlling front- vs back-loading and a configurable share reserved for the settlement.
3. **Case estimate path.** Between report and close, pure revision events arrive via a Poisson process. At each revision the assessor re-estimates remaining cost: outstanding = (ultimate - paid to date) x lognormal noise, with noise variance shrinking as the claim ages - estimates jump up or down early, settle near truth late. Payments also reduce outstanding one-for-one. At close, outstanding snaps to exactly zero.
4. **Transactions emitted** as per the output convention above.

Invariants, enforced by construction and asserted in tests:

- Outstanding case is never negative.
- Cumulative paid never exceeds ultimate.
- Outstanding is exactly zero at close.
- Total paid equals ultimate exactly.

## Configuration

One YAML file per line of business, mapping directly onto the `LineOfBusiness` domain object with names from the ubiquitous language (`base_frequency`, `report_lag`, `severity.own_damage`, `severity.third_party`, `close_lag`, `case_adequacy`, ...). The `motor-personal.yaml` preset ships embedded in the binary as the default. Run-level settings (seed, number of years, initial book size, output directory) are CLI flags. Config is validated on load - unknown keys, out-of-range values, and missing sections fail fast with the offending field named.

## Realism evaluation

`domain/triangle` aggregates generated claims and transactions into 10x10 paid and incurred development triangles by occurrence year, mirroring the Schedule P shape, plus earned premium by year. Comparison metrics against the ~145 reference companies: age-to-age development factors and ultimate loss ratios, scored as inside the band observed across reference companies per development age. In the MVP this runs as a unit test (generate with the default preset, assert the triangles land within Schedule P bands); the same function can back an in-app report later.

## Testing

- Unit tests per domain service using fixed/stubbed `RandomSource` implementations.
- Invariant tests on generated output: occurrence <= report <= close, initial estimate above excess, outstanding never negative, outstanding exactly zero at close, total paid equals ultimate, every transaction linked to a claim and every claim to a policy.
- Determinism test: same seed and config produce byte-identical CSVs.
- Realism test as above.

## Error handling

Generation is pure given config + seed, so the error surface is config validation and file I/O - both reported with context and nonzero exit codes. Internal invariant violations panic immediately (they are bugs, not user errors).

## Out of scope for the MVP

- Web front end (later phase).
- Other lines of business (the parameterization supports them; only motor personal ships).
- Nil claims, reopened claims, recoveries, claims inflation across calendar years (listed in the mission as beyond MVP).
- Open claims at a valuation date - all claims run to closure by design.
