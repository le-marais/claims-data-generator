# Detailed architecture

This document is a function-level tour of `claimsgen`. It is meant to be read end to end: by the time you reach the bottom you should understand what every package contains, what each exported (and most unexported) function does, its parameters, and the algorithm behind it.

The prose style avoids em dashes in favour of spaced hyphens. Code comments quoted from the source are reproduced verbatim and may still contain em dashes.

## 1. What the app is

`claimsgen` is a local CLI (plus an optional browser UI) that generates fully synthetic insurance claims data as dummy input to reserving processes. Nothing in the output is real, so there are no data governance concerns.

One run produces three linked CSV datasets for a class of business:

- **policies.csv** - the book of policies per calendar year: cover dates, sum insured, excess, risk factor, premium.
- **claims.csv** - claim events with occurrence, report and close dates plus the initial case estimate.
- **transactions.csv** - each claim's case estimate movements, payments, and recoveries (salvage and subrogation) over its lifetime.

Generation is reproducible: the same seed plus the same parameters produce byte-identical output. There is no valuation date; every claim runs to closure, which supports out-of-sample testing of reserving methods.

## 2. Layout and dependency direction

The codebase is domain-driven and layered. Dependencies point inward: infrastructure depends on application depends on domain; the domain depends on nothing outside itself.

```
cmd/claimsgen/            CLI entry point (main), argument parsing, command dispatch
internal/
  domain/                 the simulation model, no outside dependencies
    shared/               value objects: Date, Money, distributions, RandomSource interface
    lob/                  LineOfBusiness parameter tree + validation + expected-loss pricing
    policy/               step 1: the policy book
    claim/                step 2: claim events, claims inflation, reopening
    transaction/          steps 3-4: case-estimate runoff, payments, recoveries
    triangle/             development triangles + realism comparison
  application/            use cases: GenerateDataset + analytics (summary, histogram, realism)
  infrastructure/         adapters
    config/               YAML <-> LineOfBusiness mapping, embedded preset registry
    random/               gonum-backed RandomSource implementation
    csv/                  CSV writer
    schedulep/            Schedule P reference-file reader
    web/                  HTTP server + JSON view models for the browser UI
data/reference/           embedded Schedule P reference companies + refdata package
```

The `internal/domain/shared.RandomSource` interface is the seam between the pure domain and the concrete `internal/infrastructure/random.Source`. The domain describes the randomness it needs; infrastructure supplies it.

## 3. End-to-end data flow

`application.GenerateDataset` is the composition root. It runs six ordered stages, each drawing from its own labelled random sub-stream so that toggling one stage never reshuffles another's draws:

```
seed --> random.NewSource
             |
   src.Split("book")       --> policy.BookSimulator.Simulate      --> []Policy
   src.Split("inflation")  --> claim.NewInflationIndex            --> InflationIndex
   src.Split("claims")     --> claim.ClaimSimulator.Simulate      --> []Claim   (needs book, inflation, base year, window)
   src.Split("reopening")  --> claim.ReopenSimulator.Apply        --> []Claim   (mutates claims in place)
   src.Split("runoff")     --> transaction.RunoffSimulator.Simulate --> []Transaction  (needs claims)
   src.Split("recovery")   --> transaction.RecoverySimulator.Apply --> []Transaction  (needs claims + txs)
             |
             v
   application.Dataset{Policies, Claims, Transactions}
```

Downstream, three read-only analytics consume the `Dataset`:

- `application.Summarize` - the per-year table.
- `application.ComputeDistributions` - severity and lag histograms.
- `application.EvaluateRealism` - paid/incurred triangles scored against Schedule P reference bands.

The CLI writes the three CSVs; the web UI additionally serialises the analytics as JSON for the browser.

## 4. The randomness model

Reproducibility is the backbone of the design, so it is worth understanding before the domain code.

### 4.1 The `shared.RandomSource` interface (`internal/domain/shared/random.go`)

```go
type RandomSource interface {
    Split(label string) RandomSource
    Uniform() float64            // in [0, 1)
    Bernoulli(p float64) bool
    Poisson(mean float64) int
    LogNormal(mu, sigma float64) float64
    Gamma(shape, scale float64) float64
    Pareto(xm, alpha float64) float64
    Beta(alpha, beta float64) float64
}
```

`Split(label)` derives a named, independent, reproducible child stream. The doc comment states the contract precisely: "Split derives an independent, reproducible sub-stream so that adding a consumer never reshuffles the draws of existing ones." The remaining methods are single draws from named distributions.

### 4.2 The concrete source (`internal/infrastructure/random/source.go`)

`Source` implements the interface with a hash-chained key and a `math/rand/v2` PCG generator.

- Fields: `key [32]byte` (SHA-256 stream identifier) and `rng *rand.Rand` (PCG seeded from the first 16 bytes of `key`). A compile-time assertion `var _ shared.RandomSource = (*Source)(nil)` guarantees conformance.
- `NewSource(seed uint64) *Source` - encodes the seed little-endian into 8 bytes, hashes with `sha256.Sum256`, and builds a source from the digest.
- `fromKey(key [32]byte) *Source` (unexported) - constructs the `Source`, seeding `rand.NewPCG` from `key[0:8]` and `key[8:16]`.
- `Split(label string) shared.RandomSource` - hashes `s.key` followed by the label bytes; the digest is the child key. Because the key is a hash chain (parent key + label -> SHA-256 -> child key), a stream's draws depend only on the master seed and its label path, never on how much any ancestor drew. This is what makes draw order independent.
- Distribution methods wrap gonum's `distuv`: `Uniform` = `rng.Float64()`; `Bernoulli(p)` = `rng.Float64() < p`; `Poisson` returns 0 when `mean <= 0`, else `distuv.Poisson{Lambda: mean}`; `LogNormal`, `Pareto`, `Beta` map directly; `Gamma(shape, scale)` uses `distuv.Gamma{Alpha: shape, Beta: 1/scale}` because distuv parameterises by rate, not scale.

### 4.3 The shift-free contract

Two conventions keep parameter toggles from disturbing unrelated draws:

1. **Labelled sub-streams keyed by entity ID.** Each policy draws from `src.Split("policy-<id>")`, each claim from `claims-policy-<id>`, `reopen-claim-<id>`, `runoff-claim-<id>`, `recovery-claim-<id>`, and recovery types further split by `SALVAGE`/`SUBROGATION`. Keying on the global sequential ID makes an entity's draws stable regardless of what other entities do.
2. **Constant draw counts.** `simulateClaim` always draws the nil `Bernoulli`, even when `NilProbability` is 0 (`Bernoulli(0)` still consumes one uniform and returns false), so turning nil claims off does not reshuffle later draws. `shared.MeanOneLogNormal` is the deliberate mirror image: with `sigma <= 0` it returns 1 without drawing, so a zero-volatility knob does not consume a draw where none is conceptually needed.

The one intentional exception is `ReopenSimulator.Apply`, which short-circuits entirely when reopen probability is `<= 0` (it takes no draws at all in that case).

## 5. Domain: `shared` value objects

### 5.1 `date.go` - calendar dates

`Date` wraps a `time.Time` held at UTC midnight in an unexported field, forcing all construction through `NewDate` and preserving the midnight-UTC invariant (no DST or partial-day complications).

- `NewDate(year int, month time.Month, day int) Date` - builds via `time.Date(..., time.UTC)`, inheriting `time.Date` normalisation (day 32 rolls forward).
- `(d Date) AddDays(n int) Date` - returns a new date `n` days later (`n` may be negative).
- `(d Date) Before(other Date) bool` / `After(other Date) bool` - strict ordering.
- `(d Date) Year() int` - the calendar year.
- `(d Date) IsZero() bool` - true only for the zero instant (used to signal "unset" dates like `windowEnd` and `ReopenDate`).
- `(d Date) Equal(other Date) bool` - same instant.
- `DaysBetween(a, b Date) int` - whole days from `a` to `b`, negative if `b` is earlier, via `int(b.t.Sub(a.t) / 24h)`. Exact because every date is UTC midnight.
- `(d Date) String() string` - ISO-8601 (`2006-01-02`), implementing `fmt.Stringer`; this is what lands in the CSVs.

### 5.2 `money.go` - integer-cent money

`Money` is an `int64` count of whole cents, so accumulation never drifts.

- `const OneCent Money = 1` - smallest positive amount; used as a floor throughout.
- `FromDollars(d float64) Money` - `Money(math.Round(d*100))`; returns 0 for NaN/Inf.
- `(m Money) Dollars() float64` - `float64(m)/100`.
- `(m Money) MulFloat(f float64) Money` - `Money(math.Round(float64(m)*f))`; returns 0 for NaN/Inf. Used to apply inflation and recovery/reopen shares.
- `(m Money) String() string` - formats `"[-]dollars.cc"`, negating a local copy first so the cents part never prints a negative remainder. This is the CSV amount format.

### 5.3 `distribution.go` - mean-one lognormal noise

- `MeanOneLogNormal(src RandomSource, sigma float64) float64` - a lognormal with mean exactly 1. With `sigma <= 0` it returns 1 with no draw (preserving the shift-free contract); otherwise `src.LogNormal(-sigma*sigma/2, sigma)`, where the `-sigma^2/2` offset centres the multiplicative noise on 1. This is the standard multiplicative noise used for book size, inflation, reopen estimates, and runoff revisions.

### 5.4 `random.go`

The `RandomSource` interface, covered in section 4.1.

## 6. Domain: `lob` - the parameter tree

`internal/domain/lob` defines `LineOfBusiness`, the complete parameter set that makes the engine reusable across classes of business, plus validation and deterministic expected-loss pricing.

### 6.1 The parameter structs (`lob.go`)

`LineOfBusiness{Name string, Book BookParams, Pricing PricingParams, Claims ClaimParams, Runoff RunoffParams}` is the root.

- **`BookParams`** (step 1): `GrowthFactor` (year-on-year policy-count trend), `SizeVolatility` (sigma of mean-1 size noise), `Spread` (heterogeneity knob reused for both the sum-insured lognormal sigma and the risk-factor coefficient of variation), `SumInsuredMedian`, `SumInsuredInflation` (annual median drift), `ExcessChoices []ExcessChoice`.
- **`ExcessChoice`**: `Value` (deductible dollars), `Weight` (unnormalised selection weight).
- **`PricingParams`** (premium): the insurer's assumed loss cost, independent of the claims model. `TargetLossRatio` (premium = assumed expected loss / this), `BaseFrequency`, `Severity SeverityParams`, `ReopenProbability`, `ReopenEstimateFactor`, `InflationMean` (all assumed values). Carries the `ExpectedPolicyLoss` method (in `expectedloss.go`). Defaulting these to the true claims values prices the book perfectly; deviating them models underpricing or adverse experience.
- **`ClaimParams`** (step 2): `BaseFrequency`, `ReportLagMedian`, `ReportLagSigma`, `Severity SeverityParams`, `CloseLag CloseLagParams`, `Inflation InflationParams`, `NilProbability`, `Recoveries RecoveryParams`, `Reopening ReopeningParams`.
- **`SeverityParams`**: `ThirdPartyWeight` (probability a claim is third party), `OwnDamageMedianFraction` (own-damage median as a fraction of sum insured), `OwnDamageSigma`, `ThirdPartyScale` (Pareto minimum), `ThirdPartyAlpha` (Pareto tail index, must exceed 1 for a finite mean).
- **`CloseLagParams`**: `Shape`, `MeanDays` (own-damage gamma base), `SizeThreshold`/`SizeMultiplier` (stretch the mean lag for large own-damage claims), `RiskLoading` (exponent applied to the risk factor), `ThirdPartyShape`/`ThirdPartyMeanDays` (the slower bodily-injury regime, not size-stretched).
- **`InflationParams`**: `Mean` (average annual claims-inflation factor), `Volatility` (sigma of mean-1 noise per year).
- **`RecoveryParams`**: `Salvage`, `Subrogation`, each a `RecoveryTypeParams`.
- **`RecoveryTypeParams`**: `Probability` (0 switches the type off), `MeanShare` (mean recovery as a share of gross paid), `Concentration` (Beta concentration), `LagMedianDays`, `LagSigma` (lognormal close-to-receipt lag).
- **`ReopeningParams`**: `Probability` (0 switches reopening off), `EstimateFactor` (reopen estimate as a factor of the original initial estimate), `EstimateSigma`, `LagMedianDays`, `LagSigma`.
- **`RunoffParams`** (steps 3-4): `CaseAdequacyMean` (mean of ultimate/initial estimate - systematic over/under-reserving), `CaseAdequacySigma`, `PaymentsPerYear` (Poisson intensity of interim payments), `SettlementShare` (fraction of ultimate held for the final settlement), `Concentration` (Dirichlet concentration splitting the interim remainder), `RevisionsPerYear`, `RevisionSigma` (initial revision noise, decays with age).

### 6.2 Validation (`lob.go`)

`(l LineOfBusiness) Validate() error` is the only exported validation entry point. It rejects an empty `Name`, then delegates to unexported `Book.validate()`, `Pricing.validate()`, `Claims.validate()`, `Runoff.validate()`, short-circuiting on the first error and naming the offending field in snake_case (e.g. `book.excess_choices[2].value`).

A helper `checkFinite(fields ...namedFloat) error` screens NaN and infinity first, because "every comparison with NaN is false" would otherwise let a NaN slip past ordinary range checks. `namedFloat{name, v}` pairs a field's display name with its value.

Notable per-struct rules:

- `BookParams`: growth/spread/median/inflation all `> 0`; volatility `>= 0`; `ExcessChoices` non-empty with each weight `>= 0` and a positive total weight; each value `>= 0`.
- `PricingParams`: `TargetLossRatio`, `BaseFrequency`, `InflationMean`, `ReopenEstimateFactor` all `> 0`; `ReopenProbability` in `[0, 1)`; delegates to `Severity.validate("pricing.severity")`.
- `ClaimParams`: base frequency, report-lag median/sigma `> 0`; `NilProbability` in `[0, 1)`; delegates to `Severity.validate("claims.severity")`, inflation, both recovery types (with the prefix passed in), reopening, and close lag.
- `SeverityParams.validate(prefix string)`: `ThirdPartyWeight` in `[0, 1]` (inclusive, unlike the other probabilities); own-damage fraction/sigma, third-party scale `> 0`; `ThirdPartyAlpha > 1`. The prefix names the offending field for either the pricing or claims severity block.
- `CloseLagParams`: shapes and mean days `> 0`; `SizeMultiplier >= 1`; loadings/threshold `>= 0`.
- `RecoveryTypeParams.validate(prefix string)`: `Probability` in `[0, 1)`, `MeanShare` in the open interval `(0, 1)`, `Concentration`/`LagMedianDays > 0`, `LagSigma >= 0`.
- `ReopeningParams`: `Probability` in `[0, 1)`, `EstimateFactor > 0`, sigmas/median with the usual non-negativity/positivity.
- `RunoffParams`: `SettlementShare` in `(0, 1]`; adequacy mean/concentration `> 0`; the rest `>= 0`.

### 6.3 Expected-loss pricing (`expectedloss.go`)

This file prices premium deterministically (no randomness) from the assumed loss cost in `PricingParams`. When the pricing assumptions equal the true claims values, premium tracks expected loss and accident-year loss ratios stay flat as severities inflate; when they differ, the realized loss ratio moves on its own (underpricing / adverse experience). The formula is a deliberate, separate copy of the severity model - it is the insurer's assumption, not the true process.

- `normCDF(x)` - the standard normal CDF via `0.5*math.Erfc(-x/sqrt2)`, stable in the tails.
- `stopLossLognormal(median, sigma, excess)` - `E[(X-excess)+]` for a lognormal. Mean is `median*exp(sigma^2/2)`. For `excess <= 0` it is `mean - excess`; otherwise the Black-Scholes-style `mean*Phi(d1) - excess*Phi(d2)`.
- `limitedStopLossLognormal(median, sigma, excess, cap)` - `E[(min(X,cap)-excess)+]`, i.e. the layer between `excess` and `cap`. Returns 0 when `cap <= excess`, else the difference of two stop-loss layers. This is the own-damage cover between the deductible and a total-loss cap.
- `stopLossPareto(scale, alpha, excess)` - `E[(X-excess)+]` for a Pareto. Mean is `scale*alpha/(alpha-1)`. For `excess <= scale` it is `mean - excess`; otherwise the closed form `(scale/(alpha-1))*(scale/excess)^(alpha-1)`.
- `(p PricingParams) ExpectedPolicyLoss(sumInsured, excess, riskFactor, inflationFactor, siDrift float64) float64` - the deterministic expected ultimate gross incurred loss for one policy under the pricing assumptions. It backs out the base-year sum insured (`baseSI = sumInsured/siDrift`), trends the assumed own-damage median by the claims index (`odMedian = inflationFactor*baseSI*OwnDamageMedianFraction`), prices own damage as a limited stop-loss capped at the drifted `sumInsured`, prices third party as an uncapped Pareto stop-loss on a claims-trended scale, mixes them by the assumed `ThirdPartyWeight`, applies a reopen uplift `1 + ReopenProbability*ReopenEstimateFactor`, and multiplies by the assumed `BaseFrequency*riskFactor`. Recoveries are excluded (gross basis). It draws no randomness, so pricing never perturbs a sub-stream.

## 7. Domain: `policy` - the book (step 1)

`internal/domain/policy/book.go` simulates the exposure claims arise from.

`Policy{ID, CoverStart, CoverEnd, SumInsured, Excess, RiskFactor, Premium}` is one 12-month motor policy. `CoverEnd` is always `CoverStart.AddDays(364)`. Money fields are `shared.Money`; `RiskFactor` is a `float64`.

`BookSimulator` holds `book lob.BookParams` and `pricing lob.PricingParams` (the latter drives premium pricing, independent of the claims model). `NewBookSimulator(book, pricing)` constructs it.

- `Simulate(src, startYear, years, initialSize int) []Policy` - produces the whole book. It splits a dedicated `book-size` stream for year-size noise, starts `size = initialSize` and a global `id = 1`, then for each year `y`:
  - For `y > 0`, applies growth with noise: `size = round(size * GrowthFactor * MeanOneLogNormal(sizeSrc, SizeVolatility))`, clamped to a minimum of 1. So the book trends upward but individual years can shrink.
  - Computes the drifted median sum insured `SumInsuredMedian*SumInsuredInflation^y`, the assumed pricing inflation factor `pricing.InflationMean^y`, and the sum-insured drift `SumInsuredInflation^y`.
  - Emits `size` policies, each from its own `policy-<id>` sub-stream, incrementing the global `id`.
- `simulatePolicy(src, id, year int, medianSI, inflation, siDrift float64) Policy` - one policy: cover start uniform within the calendar year (leap-year aware via `DaysBetween`), sum insured lognormal `(log(medianSI), Spread)`, risk factor a mean-1 gamma with variance `Spread^2` (`Gamma(1/spread2, spread2)`), excess via `drawExcess`, and premium `pricing.ExpectedPolicyLoss(...) / pricing.TargetLossRatio`.
- `drawExcess(src) float64` - weighted categorical draw over `ExcessChoices`: draw `u = Uniform()*totalWeight`, walk the choices subtracting weights, return the first whose running total crosses `u`; fall back to the last choice on floating-point edges.

## 8. Domain: `claim` - claim events (step 2)

`internal/domain/claim` simulates occurrence, report and close dates and the initial case estimate, plus the stochastic inflation path and the optional reopen episode.

### 8.1 `claim.go`

`Claim` fields: `ID`, `PolicyID`, `OccurrenceDate`, `ReportDate`, `CloseDate` (the final close after any reopen), `InitialEstimate` (ground-up loss minus excess), `RiskFactor` (carried from the policy), `Nil` (closes without payment; carried to runoff, never written to CSV), `OwnDamage` (drives recovery eligibility; never written to CSV), and the reopen triple `FirstCloseDate`, `ReopenDate`, `ReopenEstimate` (all zero when the claim never reopens). `(c Claim) Reopened() bool` is `ReopenDate != zero`.

`ClaimSimulator` holds `params`, an `InflationIndex`, `sumInsuredInflation`, `startYear`, and an exclusive `windowEnd`. It is built fluently:

- `NewClaimSimulator(p lob.ClaimParams) *ClaimSimulator` - sets only params (no inflation, nominal sum insured, no window).
- `WithInflation(x InflationIndex)` - sets the occurrence-year inflation index (the zero value is the identity).
- `WithBaseYear(sumInsuredInflation float64, startYear int)` - lets own-damage severity be expressed in base-year sum-insured terms.
- `WithWindow(startYear, years int)` - sets `windowEnd = Jan 1 of startYear+years` (exclusive), constraining occurrences to `[startYear, startYear+years)` so the trailing underwriting year does not spill a partial accident year into claims.csv.

Helpers:

- `baseSumInsured(pol) float64` - deflates the drifted sum insured to base-year dollars: `SumInsured / sumInsuredInflation^(coverYear-startYear)`, or the nominal value when the base-year knob is unset (`<= 0`).
- `exposedFraction(pol) float64` - the share of a policy's cover term lying inside the window, used to pro-rate frequency. Returns 1 when windowing is off or cover ends before `windowEnd`; otherwise the in-window days over the full term (guarding a zero-length term).

Core generation:

- `Simulate(src, book []policy.Policy) []Claim` - for each policy, splits a `claims-policy-<id>` stream, draws a Poisson count with mean `BaseFrequency * RiskFactor * exposedFraction(pol)`, and calls `simulateClaim` that many times (appending only reportable ones). It then stable-sorts by report date, then policy ID, then occurrence date - resembling a claims-system registration order - and assigns 1-based sequential IDs after sorting.
- `simulateClaim(src, pol) (Claim, bool)` - draws one claim in a fixed order so draw counts stay constant:
  1. Occurrence date: uniform over the cover span. When windowing caps the span at the exclusive `windowEnd`, the usual `span++` (to include the final cover day) is dropped so the exclusive boundary is honoured.
  2. Report lag: lognormal `(log(ReportLagMedian), ReportLagSigma)`, rounded to days.
  3. Ground-up loss: `drawGroundUpLoss`.
  4. Claims inflation: multiply the loss by `inflation.For(occurrenceYear)` (applies to both severity components).
  5. Own-damage cap: if own damage and the loss exceeds the drifted `SumInsured`, cap it (a total loss).
  6. Estimate = loss - excess; if `<= 0` the claim did not pierce the excess and is unreportable (`ok = false`, but the draws above were still consumed).
  7. Close date: `report + round(drawCloseLag(...))`.
  8. Nil flag: always drawn via `Bernoulli(NilProbability)`.
- `drawGroundUpLoss(src, pol) (loss float64, ownDamage bool)` - a two-component mixture that always consumes exactly two draws: with probability `ThirdPartyWeight`, a Pareto `(ThirdPartyScale, ThirdPartyAlpha)` (uncapped, `ownDamage=false`); otherwise `baseSumInsured(pol)` times a lognormal fraction `(log(OwnDamageMedianFraction), OwnDamageSigma)` (`ownDamage=true`).

Shared close-lag logic, reused by the reopen pass:

- `closeLagRegime(cl, estimate, riskFactor, ownDamage) (shape, mean float64)` - selects the gamma parameters. Own damage uses `Shape`/`MeanDays`, stretched by `SizeMultiplier` above `SizeThreshold`; third party uses the slower `ThirdPartyShape`/`ThirdPartyMeanDays` with no size stretch. Both scale the mean by `riskFactor^RiskLoading`.
- `drawCloseLag(src, cl, estimate, riskFactor, ownDamage) float64` - draws `Gamma(shape, mean/shape)`, giving expected value `mean`.

### 8.2 `inflation.go` - the claims-inflation path

`InflationIndex{startYear int, factors []float64}` maps an occurrence year to a cumulative inflation factor; the zero value is the identity (`For` returns 1.0 for every year).

- `NewInflationIndex(src, p lob.InflationParams, startYear, years int) InflationIndex` - `factors[0] = 1.0`, then each subsequent year multiplies by `Mean * MeanOneLogNormal(src, Volatility)`, so the expected annual factor is `Mean`. Returns the identity when `years < 1`.
- `(x InflationIndex) For(year int) float64` - looks up `factors[year-startYear]`, clamping out-of-range years to the first or last simulated index (no extrapolation) and returning 1.0 for the zero-value index.

### 8.3 `reopen.go` - the optional reopen episode

`ReopenSimulator{params lob.ClaimParams}`, built by `NewReopenSimulator(p)`, runs as a post-pass after claim IDs are assigned.

- `Apply(src, claims []Claim) []Claim` - mutates reopened claims in place. If reopen probability is `<= 0` it returns immediately with no draws. Otherwise, for each claim it splits a `reopen-claim-<id>` stream and draws `Bernoulli(Probability)`; a claim that does not reopen consumes exactly that one draw. A reopening claim then draws, in order: a reopen lag (lognormal, floored to 1 day), a reopen estimate (`InitialEstimate * EstimateFactor * MeanOneLogNormal`, floored to one cent), and a second close lag (via the shared `drawCloseLag`, floored to 1 day). It records `FirstCloseDate = old CloseDate`, `ReopenDate = FirstCloseDate + lag`, `ReopenEstimate`, and moves `CloseDate` to `ReopenDate + closeLag`. The day floors guarantee `ReopenDate > FirstCloseDate` and final `CloseDate > ReopenDate`.

## 9. Domain: `transaction` - runoff and recoveries (steps 3-4)

`internal/domain/transaction` turns each claim into a ledger of case-estimate movements, payments, and recoveries.

`Transaction{ID, ClaimID, Date, Type, Amount}` is one movement. `Type` is a string enum: `PAYMENT`, `ESTIMATE` (in `runoff.go`), `SALVAGE`, `SUBROGATION` (in `recovery.go`). `(t Type) IsRecovery()` is true for salvage or subrogation. The outstanding case at any time is the running sum of `ESTIMATE` amounts (signed movements), so the first `ESTIMATE` row is the opening case at report date.

### 9.1 `runoff.go` - ultimate-first case runoff

The design is ultimate-first: the true ultimate cost is drawn up front, payments split it over the claim's life, and the case estimate is a noisy view of the remaining cost that converges to zero at close. Development happens in episodes: a normal claim runs one episode; a reopened claim runs a first episode to first close, re-raises the case to the reopen estimate, then runs a second episode to final close.

`RunoffSimulator{params lob.RunoffParams}`, built by `NewRunoffSimulator(p)`:

- `Simulate(src, claims) []Transaction` - concatenates each claim's rows (from a `runoff-claim-<id>` stream, in claim order, each claim chronological), then assigns global 1-based IDs.
- `simulateClaim(src, c) []Transaction` - drives an `emitter`. It emits the opening `ESTIMATE` at report date, runs the first episode from report to first close (nil per `c.Nil`), and, if reopened, re-raises the case to `ReopenEstimate` and runs a second episode (never nil, revisions floored).
- `runEpisode(src, e, start, close, opening, isNil, floorRevisions)` - develops one episode. All offsets are report-relative (`base = DaysBetween(report, start)`). On the nil path it emits no payments: it draws revisions, moves the case toward `remaining * MeanOneLogNormal(sigma)` at each (with `sigma = RevisionSigma*(1 - offset/duration)` shrinking toward close and a one-cent floor to keep the case open), then releases the case to zero at close. On the non-nil path it draws the ultimate and interim payments, merges revisions and payments (revisions sort before payments on the same day), walks them tracking cumulative `paid`, re-centres the case on `ultimate - paid` at each revision, pays the remaining ultimate as a final settlement, and snaps the case to exactly zero at close. Because the first revision re-centres on the true remaining, incurred development carries little systematic IBNER signal after it.
- `drawUltimate(src, initial) shared.Money` - `initial * LogNormal(mu, sigma)` with `sigma = CaseAdequacySigma` and `mu = log(CaseAdequacyMean) - sigma^2/2`, so the multiplier has mean `CaseAdequacyMean`. Floored to one cent.
- `drawInterimPayments(src, ultimate, duration, years) []event` - splits `(1 - SettlementShare)` of the ultimate across `Poisson(PaymentsPerYear*years)` interior days, weighted by a Dirichlet built from `Gamma(Concentration, 1)` draws. Returns nil (settle everything at close) for `duration < 2`, zero count, degenerate weights, or rounding that would over-pay.
- `drawRevisions(src, duration, years) []event` - `Poisson(RevisionsPerYear*years)` pure-revision events on interior days (nil for `duration < 2`).
- `interiorOffset(src, duration) int` - a day strictly between report and close, `1 + int(Uniform()*(duration-1))`.
- `event{offset, kind, amount}` with `kind` in `{kindRevision=0, kindPayment=1}` is the internal merge unit.
- `emitter{claimID, report, outstanding, txs}` keeps the outstanding case non-negative by construction: `estimate(offset, movement)` appends a signed `ESTIMATE` (no-op for zero) and updates `outstanding`; `reviseTo(offset, target)` moves outstanding to `target` in one row; `pay(offset, amount)` strengthens the case up to the payment first if needed, emits the `PAYMENT`, then reduces the case by the same amount so every payment fully releases its own case.

### 9.2 `recovery.go` - salvage and subrogation

Recoveries are pure cash events on own-damage claims that paid something; they land after close and never touch the (gross) case estimate.

`RecoverySimulator{params lob.RecoveryParams}`, built by `NewRecoverySimulator(p)`:

- `Apply(src, claims, txs) []Transaction` - first sums gross paid per claim from the `PAYMENT` rows. For each claim it draws recoveries from a `recovery-claim-<id>` stream and inserts them immediately after that claim's contiguous block in `txs`, then renumbers all IDs. Returns `txs` unchanged when no recoveries were produced.
- `simulateClaim(src, c, paid) []Transaction` - eligibility requires `OwnDamage && paid > 0` (a never-reopened nil claim has `paid == 0` and is ineligible; a reopened nil claim that paid in its second episode is eligible). It iterates salvage then subrogation, each from its own `SALVAGE`/`SUBROGATION` sub-stream: skip if probability `<= 0` or the Bernoulli fails; draw the share from a `Beta(MeanShare*Concentration, (1-MeanShare)*Concentration)`; amount = `paid * share`; lag = lognormal `(log(LagMedianDays), LagSigma)` floored to 1 day. A key constraint keeps cumulative recovered strictly below gross paid: if `recovered + amount >= paid`, the amount is trimmed to `paid - recovered - OneCent`; sub-cent amounts emit no row. It stable-sorts the (at most two) rows by date so they come out chronological.

## 10. Domain: `triangle` - development triangles and realism

`internal/domain/triangle` holds the reserving concepts used both for the UI and for the realism test gate.

### 10.1 `triangle.go` - aggregation and factors

`Triangle{StartYear int, Cells [][]float64}` is a cumulative triangle indexed `[origin][dev]`; rows may be ragged.

- `PaidTriangle`, `NetPaidTriangle`, `IncurredTriangle(claims, txs, startYear, origins, devs)` - the three builders, differing only by a weight function passed to `aggregate`: paid weights `PAYMENT` as +1; net paid weights payments +1 and recoveries -1 (so net paid can develop downward late when recoveries land); incurred weights recoveries -1 and everything else (payments and estimate movements) +1. Schedule P reports paid net of recoveries, so `NetPaidTriangle` is the one the realism comparison scores.
- `aggregate(...)` (unexported) - maps each transaction to `origin = occurrenceYear - startYear` and `dev = txYear - occurrenceYear`, clamps `dev` into `[0, devs-1]` (late development folds into the last column; out-of-window origins are dropped), sums `weight * amount` into an incremental grid, then cumulates each row left to right.
- `EarnedPremiumByYear(policies, startYear, years) []float64` - spreads each policy's premium evenly across its inclusive cover term (`perDay = premium / termDays`) and sums the portion earned in each window year via `overlapDays`.
- `overlapDays(start, end, year)` (unexported) - inclusive days of `[start, end]` inside `year`.
- `(t Triangle) ATAFactors() []float64` - volume-weighted age-to-age (chain-ladder) factors: `factor[age] = sum(row[age+1]) / sum(row[age])` over rows long enough to have both cells; ages with no data are `NaN`; returns nil when the longest row has fewer than two cells.
- `(t Triangle) latestDiagonal()` (unexported) - the last cumulative value per non-empty row.

### 10.2 `compare.go` - scoring against reference bands

- `ReferenceSet{Name, Paid, Incurred, EarnedPremium}` - one reference company's observed triangles and premium. `Comparison{Paid, Incurred, EarnedPremium}` - the generated data's equivalent.
- `Band{Lo, Hi, Min, Max}` - `Lo`/`Hi` are the scored P5-P95 pass interval; `Min`/`Max` are the full observed extremes kept for display. `(b Band) contains(v)` is inclusive membership.
- Constants: `bandLoPercentile = 5`, `bandHiPercentile = 95` (the scored band), and `driftTolerance = 1.10` (the allowed loss-ratio drift between the first and second halves of the accident-year span).
- `Percentile(xs, p)` - linearly interpolated percentile (type-7), non-mutating; `NaN` for empty input.
- `bandFromValues(xs)` (unexported) - a `Band` from P5/P95 plus scanned min/max; an empty input yields a band that contains nothing.
- `ATABands(triangles) []Band` - per development age, the band of volume-weighted factors across the triangles.
- `AgeCheck{Age, Value, Band, Within}` and `Check{Value, Band, Within}` - scored results for an age and for a scalar.
- `Report{PaidATA, IncurredATA []AgeCheck, LossRatio, LossRatioDrift Check}` - the comparison outcome. `(r Report) Pass()` requires every age check plus both scalar checks to be within. `(r Report) String()` renders a human-readable, 1-indexed report.
- `usableRefs(refs)` (unexported) - a backstop that drops reference companies with no scorable signal (non-positive total earned premium or non-positive summed incurred latest diagonal).
- `CompareToReference(c Comparison, refs) Report` - the main entry point. It filters to usable refs, checks paid and incurred age factors against the reference bands (only ages present in both), scores the ultimate loss ratio against the reference band, and scores loss-ratio drift against a fixed `[1/driftTolerance, driftTolerance]` band (passing vacuously when drift cannot be computed).
- Helpers `checkAges`, `lossRatio` (total latest incurred over total earned premium), and `lossRatioDrift` (second-half over first-half aggregate loss ratio, computed from generated data only, so it is immune to reference immaturity; the middle year is excluded for odd counts).

## 11. Application layer

`internal/application` orchestrates the domain and provides read-only analytics.

### 11.1 `generate.go` - the use case

- `GenerateRequest{LOB lob.LineOfBusiness, StartYear, Years, InitialBookSize int}` and `Dataset{Policies, Claims, Transactions}`.
- `(r GenerateRequest) validate()` (unexported) - requires `Years >= 1` and `InitialBookSize >= 1`, then delegates to `LOB.Validate()` (`StartYear` is not validated).
- `GenerateDataset(src shared.RandomSource, req GenerateRequest) (Dataset, error)` - validates, then runs the six stages of section 3 over independently labelled sub-streams (`book`, `inflation`, `claims`, `reopening`, `runoff`, `recovery`), wiring the claim simulator fluently with the inflation index, base year (`SumInsuredInflation`, `StartYear`), and window. Output depends only on the master seed plus the request.

### 11.2 `summary.go` - per-year table

- `YearSummary{Year, Policies, Claims, NilClaims, EarnedPremium, Paid, Recovered, Reopened}` with `(s YearSummary) LossRatio() (float64, bool)` = `Paid/EarnedPremium` (ok false when there is no premium). Policies are grouped by cover-start year; claims, paid, recovered, reopened and nil are grouped by occurrence year. `Paid` is the ultimate and equals final incurred, gross of recoveries.
- `SummaryReport{Years []YearSummary, Total YearSummary}` (`Total.Year` unused).
- `Summarize(ds, startYear, years) SummaryReport` - allocates one row per window year, counts policies by cover-start year, assigns earned premium positionally from `EarnedPremiumByYear`, counts claims/nil/reopened by occurrence year (via a claim-ID to occurrence-year map), attributes each transaction's payment or recovery amount to its claim's occurrence year, and folds a grand total.

### 11.3 `histogram.go` - distributions

- `HistogramBin{Lo, Hi, Count}` (half-open except the last bin, which is inclusive) and `Histogram{Bins}`.
- `LinearHistogram(values, bins) Histogram` - equal-width bins over `[min, max]`, with the top boundary forced inclusive so the maximum lands in the last bin; handles the degenerate all-equal case.
- `LogHistogram(values, bins) Histogram` - log10-spaced bins (bins the log of positive values linearly, then converts bounds back to linear units); values `<= 0` are dropped.
- `minMax(values)` (unexported) - min and max (callers guard non-empty input).
- `Distributions{Severity, ReportLagDays, CloseLagDays Histogram}` and `ComputeDistributions(ds) Distributions` (with `const histogramBins = 20`) - severity is each claim's total `PAYMENT` amount on a log histogram (so zero-paid/nil claims drop out), report lag is occurrence-to-report days and close lag is report-to-close days, both linear.

### 11.4 `realism.go` - the gate

- `const developmentYears = 10` (Schedule P shape).
- `EvaluateRealism(ds, refs, startYear, years) triangle.Report` - a thin adapter: builds a `Comparison` from `NetPaidTriangle` (net of recoveries, matching Schedule P), `IncurredTriangle`, and `EarnedPremiumByYear`, then returns `CompareToReference`. Used as a test gate (`TestDefaultPresetIsRealistic`).

## 12. Infrastructure layer

### 12.1 `config` - YAML mapping and preset registry

`internal/infrastructure/config/config.go` maps YAML onto `lob.LineOfBusiness`. The `*Params` DTOs (`LOBParams`, `BookParams`, `ClaimsParams`, `SeverityParams`, `CloseLagParams`, `InflationParams`, `RecoveriesParams`, `RecoveryTypeParams`, `ReopeningParams`, `RunoffParams`, `ExcessChoiceParams`) mirror the domain structs field-for-field, each field carrying matching `yaml:` and `json:` tags. This means the same structs serve both YAML config loading (CLI) and the JSON request/response shape (web API). Decoding is strict: `KnownFields(true)` rejects unknown keys.

- `decode(r) (LOBParams, error)` (unexported) - strict YAML decode.
- `Load(r) (lob.LineOfBusiness, error)` - decode, `ToDomain()`, then `Validate()`.
- `(d LOBParams) ToDomain() lob.LineOfBusiness` and `(r RecoveryTypeParams) toDomain()` - pure struct-to-struct translation, no validation.
- Preset registry: `motorPersonalYAML []byte` (`//go:embed motor-personal.yaml`), `presetInfos []PresetInfo{ID, Name}` (currently just motor-personal), and `presetYAML map[string][]byte`. `Presets()` returns a clone of the registry; `PresetParams(id)` returns the raw (unvalidated) `LOBParams` to prefill the UI editor; `Preset(id)` returns a validated domain object.
- `LoadFile(path)`, `MotorPersonal()` - file-based and embedded-preset loaders.

The embedded `motor-personal.yaml` is the annotated personal-motor preset, calibrated so generated triangles fall within the P5-P95 Schedule P bands. Its comments flag the tightest-margin metric (paid ATA age 2-3) and explain the target-loss-ratio pricing.

### 12.2 `random` - covered in section 4.2.

### 12.3 `csv` - the writer

`internal/infrastructure/csv/writer.go` writes the three CSVs with stable formatting so identical datasets produce byte-identical files. Every column is numeric, an ISO-8601 date, or a fixed enum, so no quoting is needed and `fmt.Sprintf` is safe.

- `WriteDataset(dir, ds) error` - creates `dir` (0o755) and writes `policies.csv`, `claims.csv`, `transactions.csv`.
- `FormatRiskFactor(r) string` - fixed 6-decimal formatting for byte stability.
- `writeFile(dir, name, header, rows, row func(int) string)` (unexported) - buffered generic writer with a deferred close that surfaces a close error only when there was no prior error.

CSV headers:

```
policies.csv:     policy_id,cover_start,cover_end,sum_insured,excess,risk_factor,premium
claims.csv:       claim_id,policy_id,occurrence_date,report_date,close_date,initial_estimate
transactions.csv: transaction_id,claim_id,date,type,amount
```

`transactions.csv` is emitted in claim-registration order, not date order: all of a claim's rows are written together, and because recovery rows can post-date the close, a later claim's rows can carry earlier dates.

### 12.4 `schedulep` - the reference reader

`internal/infrastructure/schedulep/reader.go` reads the Schedule P reference companies into `triangle.ReferenceSet`s. Each company JSON carries a `ClassId`, a `PaidTriangle` and `IncurredTriangle` (each a list of `[year, [values...]]` rows), and an `EarnedPremium` list of `[year, amount]` pairs. Custom `UnmarshalJSON` methods on `triangleRow` and `premiumJSON` decode the positional pair encodings.

- `LoadFile(path)` - one company from disk; company name is the file stem.
- `LoadFS(fsys, dir)` - every `*.json` in a directory of a filesystem, sorted by name for determinism. This is what `runUI` uses with the embedded `refdata.Files`.
- `LoadDir(dir)` - the on-disk variant over `os.DirFS`, rewriting the "no reference files" sentinel to include the directory.
- `loadDirFS`, `parse`, `toTriangle` (unexported) - glob and sort names, parse each file (sorting premium and triangle rows by year), and require contiguous origin years in each triangle.

### 12.5 `web` - the server and view models

`internal/infrastructure/web/server.go` serves the UI: an embedded static page (`//go:embed static`, holding `app.js`, `index.html`, `style.css`) plus a small JSON API. The server is stateless apart from the loaded reference sets; the latest run lives in the browser.

- `Server{refs, mux}` and `NewServer(refs)` register routes: `GET /api/lobs`, `GET /api/lobs/{id}/preset`, `POST /api/generate`, and `GET /` (a file server over the embedded `static` subtree).
- `ServeHTTP` is a security front gate before dispatch (the server is loopback-only): it rejects non-local `Host` (403 "forbidden host") and, when an `Origin` header is present, non-local origins (403 "forbidden origin"), guarding against DNS rebinding and cross-site use. `localHost` accepts `127.0.0.1`, `localhost`, `::1` (with optional port); `localOrigin` parses the origin and checks its host.
- `handleLOBs` returns the preset list as `lobInfoJSON{id, name}`. `handlePreset` returns the raw `LOBParams` for a preset id (404 on unknown). `handleGenerate` caps the body at 1 MiB, decodes a strict `generateRequest{seed string, start_year, years, initial_book_size, out_dir, params}`, parses the seed, requires and absolutises `out_dir`, runs `GenerateDataset`, writes the CSVs, and returns `buildResponse` (validation and domain errors map to 400, CSV write errors to 500).
- `writeJSON`, `writeError` - JSON response helpers.

`internal/infrastructure/web/viewmodel.go` builds the `/api/generate` response DTOs. `buildResponse(req, ds, refs)` assembles a `generateResponseJSON{run, summary, triangles, distributions, realism}`: it computes paid, net-paid, and incurred triangles (10 development years) and delegates the summary, distributions and realism to the application layer, mapping each into JSON view models. Notable serialisation choices: `LossRatio` and per-age factors are pointers so they serialise as `null` when undefined or `NaN`; the `finite` helper replaces `NaN`/`Inf` band numbers with 0 so the response stays valid JSON.

The front end (`static/index.html`, `static/app.js`, `static/style.css`) is a single-page app: a sidebar form (line-of-business select, run flags, and an editable parameter panel prefilled from the preset) posts to `/api/generate` and renders four tabs - Summary, Triangles (with a Paid gross / Paid net / Incurred toggle and age-to-age factors), Distributions, and Realism.

### 12.6 `data/reference/refdata.go`

`refdata` embeds the Schedule P datasets so the binary can assess realism without repository access. `Files embed.FS` (`//go:embed "schedule p/ppauto_pos98-07/*.json"`) holds the 96 curated private-passenger-auto companies for accident years 1998-2007, and `const PersonalMotorDir = "schedule p/ppauto_pos98-07"` names the directory. `runUI` loads them via `schedulep.LoadFS(refdata.Files, refdata.PersonalMotorDir)`.

## 13. CLI (`cmd/claimsgen/main.go`)

A single verb-first binary: `claimsgen <command> [flags]`.

- `main()` calls `os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))`.
- `run(args, stdout, stderr) int` dispatches to `generate` or `ui`; anything else (or no args) prints the usage text to stderr and returns exit code 2.
- `runGenerate` parses `--config`, `--seed` (default 1), `--out` (default `output`), `--start-year` (default 1998), `--years` (default 10), `--initial-book-size` (default 20000); loads the embedded preset (or a YAML file via `--config`), runs `GenerateDataset(random.NewSource(seed), ...)`, writes the CSVs, and prints a one-line summary. Config errors and generation/write errors return exit code 1; flag-parse errors return 2.
- `runUI` parses `--port` (default 8080), loads the embedded reference data, binds a loopback listener on `127.0.0.1:<port>`, prints the URL, and serves `web.NewServer(refs)`.

## 14. Key invariants and deliberate simplifications

Invariants worth remembering:

- **Determinism.** Same seed plus same parameters produce byte-identical CSVs. Every independent decision draws from its own labelled sub-stream, and draw counts are kept constant across knob toggles, so turning a feature on or off never reshuffles unrelated draws.
- **Case releases to zero.** Every runoff episode ends with the outstanding case revised to exactly zero on the close date, and each payment fully releases its own case, so outstanding case is always the running sum of `ESTIMATE` amounts and is never negative.
- **Recoveries stay below gross paid.** A claim's cumulative recovered is strictly less than its gross paid (by at least one cent), and recovery rows are the only transactions dated after the final close.
- **All claims close.** There is no valuation date; every claim runs to closure, gross paid equals the ultimate (zero for a never-reopened nil claim).

Deliberate simplifications (from the README's assumptions): own-damage severity trends only at the claims index and is capped at the sum insured; case estimates re-centre on the true ultimate at the first revision, so incurred carries little IBNER; nil claims draw severity and probability independently of claim size; there is no seasonality, catastrophe, or event clustering; and each year's book is an independent cohort with no renewals. The insurer prices risk perfectly *by default* - the preset's `pricing` assumptions equal the true claims values, so premium tracks expected loss and loss ratios are more stable than a real book's - but this is configurable: deviating the `pricing` block from the claims values models underpricing or adverse experience, and the realized loss ratio then moves off target.
