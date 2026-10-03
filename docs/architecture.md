# Architecture

An overview of the code as it is on `main`: what each package owns, how a run flows through them, and the invariants that hold it together. Function-level detail lives in the doc comments. How the simulation behaves, with the model diagrams, is in the README's "How the simulation works", and the annotated presets `internal/infrastructure/config/motor-personal.yaml` and `motor-commercial.yaml` explain every parameter.

## Layout and dependency direction

Dependencies point inward: `infrastructure` depends on `application`, which depends on `domain`. The domain imports the standard library only.

```
cmd/claimsgen/            CLI entry point: the generate and ui subcommands
internal/
  domain/                 the simulation model
    shared/               value objects: Date, Month, Money, mean-one lognormal noise, the RandomSource interface
    lob/                  the LineOfBusiness parameter tree, validation, expected-loss pricing
    policy/               the policy book and its premium
    claim/                claim events, the claims inflation path, reopening
    transaction/          opening case estimates, case runoff and payments, recoveries
    triangle/             the monthly grid, coarsening, exposure, realism scoring
  application/            use cases: GenerateDataset, Aggregate, summary, histograms, realism
  infrastructure/         adapters
    config/               YAML to LineOfBusiness mapping, embedded preset registry
    random/               the RandomSource implementation
    csv/                  writers for the five CSVs
    schedulep/            Schedule P reference-file reader
    web/                  HTTP server, JSON view models, form-field registry, embedded static UI
data/reference/           CAS Schedule P files, private passenger auto and commercial auto embedded via refdata
tools/                    dev-only: README screenshots
```

`shared.RandomSource` is the seam between the pure domain and `random.Source`: the domain states the draws it needs, infrastructure supplies them.

## A run, end to end

`application.GenerateDataset` is the composition root. It validates the request (`GenerateRequest{LOB, StartYear, Years, InitialBookSize}`), then runs seven stages in order, each on its own labelled sub-stream of one seeded source:

```
random.NewSource(seed)
  Split("book")          policy.BookSimulator.Simulate         -> []Policy
  Split("inflation")     claim.NewInflationIndex               -> InflationIndex
  Split("claims")        claim.ClaimSimulator.Simulate         -> []Claim
  Split("reopening")     claim.ReopenSimulator.Apply           -> appends reopen episodes
  Split("case-estimate") transaction.CaseEstimator.Apply       -> sets each episode's opening case
  Split("runoff")        transaction.RunoffSimulator.Simulate  -> []Transaction
  Split("recovery")      transaction.RecoverySimulator.Apply   -> []Transaction with recoveries
                                    |
                                    v
                application.Dataset{Policies, Claims, Transactions}
```

The claim and reopen stages fix every claim's true cost before any case estimate or payment is drawn. The later stages decide how that cost is reserved and when it is paid, never its amount. The context is checked between stages only, so cancellation never reaches into the domain or changes the output.

Three read-only passes consume the `Dataset`:

- `application.Aggregate` builds the monthly grid and monthly exposure on the chosen origin basis, plus the accident-basis annual triangles and earned premium, into `Aggregates`.
- `application.Summarize` and `application.ComputeDistributions` build the UI's per-year table and its severity and lag histograms.
- `application.EvaluateRealism` builds `SectionComparison`, the annual triangles and premium of the scored sections taken together, and scores them with `triangle.CompareToReference` against a reference pool.

The CLI writes the three dataset CSVs and, from `Aggregates`, `triangles.csv` and `exposure.csv`, into a directory. The web server returns the analytics as JSON and serves the same five files as a zip download, which regenerates the run from its seed and parameters.

## Domain packages

### `lob` - the parameter tree

`LineOfBusiness{Name, Book, Pricing, Claims, Runoff}` is the whole parameter set; nothing else configures the engine. It holds simulation parameters only: how a line is scored for realism is its preset's `application.RealismProfile`. `Validate` names an offending field by its YAML path, screens NaN and infinity before the range checks, and skips the fields of a switched-off feature, so a YAML author never has to invent parameters for something they turned off.

`BookParams.Fleet` switches on a fleet book; its zero value writes every policy as its own fleet.

A line of business is a list of sections of cover, `ClaimParams.Sections`. Each `SectionParams` carries its own base frequency, severity (`sum_insured_lognormal`, capped at the sum insured, or the uncapped `lognormal` in start-year dollars, `pareto` and `lognormal_pareto`, a lognormal body under a Pareto tail joined continuously), an optional per-claim `Limit` in nominal dollars that caps a claim's cost and becomes its `CoverLimit`, whether it takes the policy's excess (`NoExcess` switches it off), report lag, close lag, how its claims are paid (`Settlement`: a lump-sum probability and a Beta-distributed settlement share) and recovery eligibility. Inflation, nil claims, reopening and the recovery parameters are shared across sections.

`PricingParams` is the insurer's assumed loss cost, kept apart from `ClaimParams`, the true process. Its `Sections` give each claims section's assumed frequency, severity, limit and excess switch, by the same names in the same order. `ExpectedSectionLoss` prices one section of a policy in closed form from the assumptions alone, and premium is the sum over the target loss ratio. The loss ratio emerges from sampling and from any gap between the two models; nothing forces it.

### `policy` - the book

`BookSimulator.Simulate` writes one underwriting year at a time, starting with a warm-up year before the window so the first accident year has a full book in force. A year's size counts fleets. Without a fleet book each fleet is one policy, which draws its cover dates, sum insured, risk factor and excess from its own sub-stream. With one, each fleet draws its cover dates, excess, vehicle count, median vehicle sum insured and risk factor from its own `fleet-<id>` stream, and each vehicle is then a policy that draws its sum insured and risk factor around the fleet's from its own stream and shares the fleet's cover dates and excess; `Policy.FleetID` records the fleet. Every policy is priced from the pricing block. `Policy` also carries two fields no CSV writes: `BaseSumInsured`, the sum insured in start-year dollars that a sum-insured severity is sized off, and `SectionPremiums`, the premium split by section, which the realism gate scores the scored sections against. `ProjectedSize` is the noise-free policy count, vehicles on a fleet book, the web server caps runs with.

### `claim` - claim events

A `Claim`'s life is a list of `Episode`s, each with open and close dates, its true cost (`Ultimate`), a nil flag, and the case it opens at. The first episode runs from the report date to the first close; a reopened claim has a second. Stages append episodes and never rewrite one's dates or cost, so the claim's report date, final close, initial estimate and total cost are all read off its episodes. `Record()` is the claim's `claims.csv` row.

`ClaimSimulator.Simulate` draws, for each section of each policy, a claim count, then each claim's occurrence date, report lag, ground-up loss, inflation, cap, excess, limit cap, close lag and nil flag in a fixed order, dropping claims that do not pierce the excess. A section with `NoExcess` takes no excess, so every loss is reported. A claim records its `Section` and its cover limit: the sum insured less the excess it takes for a sum-insured severity, the section's `Limit` for a limited one, and zero (unlimited) otherwise. It sorts the claims into registration order and numbers them. `InflationIndex` is the stochastic claims-inflation path, one factor per calendar year, interpolated smoothly through the year. `ReopenSimulator.Apply` is a post-pass that appends a second episode to some closed claims, with its own cost and close date.

### `transaction` - the ledger

`CaseEstimator.Apply` sets each episode's opening case around its true cost. `RunoffSimulator.Simulate` turns each claim into `ESTIMATE` and `PAYMENT` rows one episode at a time: the case moves to the episode's opening case, then interim payments and revisions in date order, then a final settlement and a release of the case to zero. Each claim is paid by its section's `Settlement`: a lump-sum episode pays its whole cost at close; otherwise its interim payments share what its settlement share leaves, and one below `RunoffParams.MinPayment` is held over to the next. With `RunoffParams.PaymentDelayDays`, every payment comes at least that many days after the last row that raised the case: each payment has a bill that many days before it, where a short case is raised, revisions between the bill and the payment cannot raise the case, and payments are spaced at least that far apart. The claim and reopen simulators keep every paying episode open that much longer (`WithPaymentDelay`), so the runoff has room. `RecoverySimulator.Apply` adds `SALVAGE` and `SUBROGATION` rows after the final close of paid claims in sections with recoveries, salvage only on total losses on a sum-insured section.

The ledger is each claim's event stream, and every measure folds from it: outstanding case is the running sum of `ESTIMATE` rows, gross paid the sum of `PAYMENT` rows, and net paid subtracts the recoveries.

### `triangle` - aggregation and realism

- `MonthlyGrid` is the one aggregation store: incremental cells, origin months down, development months across, run to full runoff, for paid, net paid, incurred and reported count. `BuildMonthlyGrid` folds the ledger into it on an `OriginBasis`, accident or underwriting.
- `Coarsen` maps both axes onto calendar periods. `AnnualTriangles` is `Coarsen(Annual, 10, true)` cumulated, the view the UI reads. The realism gate reads `Coarsen(Annual, 10, false)` cumulated, dropping development after age 10, because Schedule P values every company at age 10. New aggregate views coarsen the grid rather than re-scan the transactions.
- `ExposureByMonth` gives premium, policy-years and policy count by origin month, and `EarnedPremiumByYear` rolls the monthly premium up by year.
- `CompareToReference` scores paid and incurred age-to-age factors, paid to date at each age as a share of paid at the last age (`Triangle.DevelopmentShares`), the ultimate loss ratio and the loss-ratio drift, relative to the pool's median drift, against the P5-P95 bands across the reference companies, and returns a `Report`.
- `ReferenceCriteria` picks the reference companies: steady net premium and net-to-direct ratio, a size floor and named exclusions. `SelectReferences` applies it.

### `shared` - value objects

`Date` is a UTC-midnight date, `Month` an absolute month index, and `Money` an integer count of cents, so accumulation never drifts. `TrendYears` is the time axis both the inflation index and pricing read, so the trend priced and the trend experienced line up. `MeanOneLogNormal` is the multiplicative noise used throughout; at sigma 0 it returns 1 without drawing.

## Reference lines and realism profiles

`application.ReferenceLines` lists the Schedule P lines the gate scores against, each a `ReferenceLine{ID, Label, Criteria}`: private passenger auto (`PersonalMotorCriteria`, 45 companies) and commercial auto (`CommercialAutoCriteria`, 42). A `ReferencePool` is a line with its selected companies. A `RealismProfile{Line, Sections}` says how a line of business is scored: the reference line, and the sections scored together by name, resolved against the line of business by `SectionIndices`. Profiles are evaluation, not simulation, so they live in the preset registry, not in the YAML.

## Randomness

`random.Source` holds a SHA-256 key and a PCG generator seeded from it, with gonum supplying the distributions. `Split(label)` hashes the parent key with the label to make the child's key, so a stream's draws depend only on the seed and its label path, never on how much any other stream drew. Two conventions keep a parameter toggle from moving unrelated draws:

1. **Streams keyed by entity.** Every fleet, every policy, every policy's claims and every claim draws from a stream labelled with its ID: `fleet-<id>` on a fleet book, `policy-<id>`, `claims-policy-<id>` split further by section name, `reopen-claim-<id>`, `case-estimate-claim-<id>`, `runoff-claim-<id>`, and `recovery-claim-<id>`, split further by `SALVAGE` and `SUBROGATION`.
2. **Constant draw counts.** A stream takes the same draws whatever the parameters: the nil flag is drawn even at a nil probability of 0. `MeanOneLogNormal` at sigma 0 is the deliberate mirror image, taking no draw where none is conceptually needed, and `ReopenSimulator.Apply` takes no draws at all when the reopen probability is 0.

## Adapters

- **`config`** mirrors the domain structs field for field with `yaml` and `json` tags, so the domain stays tag-free and the same structs serve YAML loading and the web API. Decoding is strict, and `Load` maps then validates. The preset registry embeds `motor-personal.yaml` and `motor-commercial.yaml` and lists each in `presetInfos`, with its realism profile, and `presetYAML`. `TestToDomainMapsEveryField` fails if a field is not carried across.
- **`csv`** writes the five files with fixed formatting, so equal datasets give equal bytes, into a directory (`WriteDataset`, `WriteAggregates`) or one zip archive with fixed entry timestamps (`WriteZip`). Every column is numeric, an ISO-8601 date or a fixed enum, so no quoting is needed. `transactions.csv` is in claim-registration order, not date order.
- **`schedulep`** reads a CAS Schedule P file, one row per company, accident year and lag, into one reference set per company with every cell present: the paid and incurred triangles at the 2007 valuation, the incurred developed to age 10, and net and direct earned premium. `refdata` embeds the private passenger auto and commercial auto files and names each line's file in `LineFiles`; `LoadPools` reads them and selects every line's pool. The other four lines in `data/reference/schedule p/` are kept but neither embedded nor read.
- **`web`** serves the embedded single-page UI and a JSON API: `/api/lobs`, `/api/lobs/{id}/preset`, `/api/limits`, `/api/fields`, `/api/generate` (the run's analytics) and `/api/download` (the run's CSVs as a zip). Both run endpoints share one decode, check and generate path. The server holds one reference pool per line and scores a run by the preset its request names, through that preset's realism profile; a run with no preset, or whose parameters lack a scored section, is reported as not scored. It is otherwise stateless and writes no files: the browser keeps the request behind the results it shows and sends it again to download, and the same seed and parameters reproduce the run byte for byte. The parameter form is built from the `formFields` registry, which `TestFormFieldsCoverEveryParameter` keeps complete. `ServeHTTP` rejects non-local `Host` and `Origin` headers, and runs are capped in size (`checkRunSize`).

The CLI dispatches `generate` (load a `--preset`, motor personal by default, or a `--config` YAML, generate, write the five CSVs) and `ui` (load the embedded reference pools, bind `127.0.0.1:<port>`, serve).

## Invariants

- **Determinism.** The same seed and parameters produce byte-identical CSVs. `internal/application/golden_test.go` pins hashes of the dataset CSVs, the aggregate CSVs, and both presets' annual triangles the realism gate scores.
- **Ultimate-first.** A claim's true cost is fixed before its ledger is drawn: the case-estimate and runoff parameters move reserves and timing, never the amount paid.
- **The ledger folds cleanly.** Each payment releases its own case, outstanding case is never negative, and every episode ends with the case at exactly zero. Gross paid equals the claim's true cost, and no claim pays beyond its `CoverLimit`: the sum insured less the excess on a sum-insured section, the section's `Limit` on a limited one. `internal/application/invariants_test.go` checks the ledger as a state machine.
- **Recoveries stay below gross paid,** by at least a cent, and are the only rows dated after a claim's final close.
- **Every claim closes.** There is no valuation date.
- **Realism.** `TestPresetsAreRealistic` keeps every preset's scored third-party sections (property damage and injury) inside the P5-P95 bands of its line's Schedule P pool across several seeds, and `TestPresetHasNoSystematicLossRatioDrift` switches each preset's inflation and pricing noise off and requires a flat loss ratio.
- **Loopback-only UI.** A 127.0.0.1 bind, Host and Origin checks, capped request bodies, strict JSON and YAML decoding, and a front end that never assigns HTML. `docs/todo.md` lists what must change before the UI is served beyond 127.0.0.1.
- **No free text in the CSVs.** If a free-text column is ever added, switch to `encoding/csv` with formula-lead-character escaping in the same change.

The model's deliberate simplifications are listed in the README's "Assumptions and known simplifications".
