# Detailed architecture

This document is a function-level tour of `claimsgen`. It is meant to be read end to end: by the time you reach the bottom you should understand what every package contains, what each exported (and most unexported) function does, its parameters, and the algorithm behind it.

The prose style avoids em dashes in favour of spaced hyphens. Code comments quoted from the source are reproduced verbatim and may still contain em dashes.

## 1. What the app is

`claimsgen` is a local CLI (plus an optional browser UI) that generates fully synthetic insurance claims data as dummy input to reserving processes. Nothing in the output is real, so there are no data governance concerns.

One run produces five linked CSV datasets for a class of business:

- **policies.csv** - the book of policies per calendar year: cover dates, sum insured, excess, risk factor, premium.
- **claims.csv** - claim events with occurrence, report and close dates plus the initial case estimate.
- **transactions.csv** - each claim's case estimate movements, payments, and recoveries (salvage and subrogation) over its lifetime.
- **triangles.csv** - incremental monthly development triangles by origin month: paid, paid net of recoveries, incurred, and reported claim counts.
- **exposure.csv** - exposure by origin month: premium, exposure units in policy-years, and a policy count that is an in-force count on the accident basis (so it does not sum to the book's policy count) and an inception count on the underwriting basis (so it does).

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
    csv/                  CSV writers: the dataset files and the aggregate files
    schedulep/            Schedule P reference-file reader
    web/                  HTTP server + JSON view models for the browser UI
data/reference/           embedded Schedule P reference companies + refdata package
```

The `internal/domain/shared.RandomSource` interface is the seam between the pure domain and the concrete `internal/infrastructure/random.Source`. The domain describes the randomness it needs; infrastructure supplies it.

## 3. End-to-end data flow

`application.GenerateDataset` is the composition root. It runs seven ordered stages, each drawing from its own labelled random sub-stream so that toggling one stage never reshuffles another's draws:

```
seed --> random.NewSource
             |
   src.Split("book")       --> policy.BookSimulator.Simulate      --> []Policy
   src.Split("inflation")  --> claim.NewInflationIndex            --> InflationIndex
   src.Split("claims")     --> claim.ClaimSimulator.Simulate      --> []Claim   (needs book, inflation, window)
   src.Split("reopening")  --> claim.ReopenSimulator.Apply        --> []Claim   (mutates claims in place)
   src.Split("case-estimate") --> transaction.CaseEstimator.Apply --> []Claim   (sets opening cases in place)
   src.Split("runoff")     --> transaction.RunoffSimulator.Simulate --> []Transaction  (needs claims)
   src.Split("recovery")   --> transaction.RecoverySimulator.Apply --> []Transaction  (needs claims + txs)
             |
             v
   application.Dataset{Policies, Claims, Transactions}
```

Downstream, three read-only passes consume the `Dataset`. `application.Summarize` builds the per-year table and `application.ComputeDistributions` the severity and lag histograms, both straight off the `Dataset`. `application.Aggregate` is the third: it builds the monthly grid, the monthly exposure, and the accident-basis annual triangles and earned premium into an `Aggregates`, which `application.EvaluateRealism` then scores against the Schedule P reference bands.

The CLI writes the three dataset CSVs plus `triangles.csv` and `exposure.csv` from the `Aggregates`; the web UI additionally serialises the analytics as JSON for the browser.

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

1. **Labelled sub-streams keyed by entity ID.** Each policy draws from `src.Split("policy-<id>")`, each claim from `claims-policy-<id>`, `reopen-claim-<id>`, `case-estimate-claim-<id>`, `runoff-claim-<id>`, `recovery-claim-<id>`, and recovery types further split by `SALVAGE`/`SUBROGATION`. Keying on the global sequential ID makes an entity's draws stable regardless of what other entities do.
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
- `TrendYears(d Date, startYear int) float64` - the date's position on the continuous axis claims inflation trends along: years since the middle of the start year, each day measured at its midpoint (leap-year aware). The inflation index and pricing both read time on it, so the trend the pricing assumes and the one the claims carry line up.
- `(d Date) String() string` - ISO-8601 (`2006-01-02`), implementing `fmt.Stringer`; this is what lands in the CSVs.

### 5.2 `money.go` - integer-cent money

`Money` is an `int64` count of whole cents, so accumulation never drifts.

- `const OneCent Money = 1` - smallest positive amount; used as a floor throughout.
- `FromDollars(d float64) Money` - `Money(math.Round(d*100))`; returns 0 for NaN/Inf.
- `(m Money) Dollars() float64` - `float64(m)/100`.
- `(m Money) MulFloat(f float64) Money` - `Money(math.Round(float64(m)*f))`; returns 0 for NaN/Inf. Used to apply inflation and recovery/reopen shares.
- `(m Money) String() string` - formats `"[-]dollars.cc"`, negating a local copy first so the cents part never prints a negative remainder. This is the CSV amount format.

### 5.3 `distribution.go` - mean-one lognormal noise

- `MeanOneLogNormal(src RandomSource, sigma float64) float64` - a lognormal with mean exactly 1. With `sigma <= 0` it returns 1 with no draw (preserving the shift-free contract); otherwise `src.LogNormal(-sigma*sigma/2, sigma)`, where the `-sigma^2/2` offset centres the multiplicative noise on 1. This is the standard multiplicative noise used for book size, inflation, reopen costs, opening case estimates, and runoff revisions.

### 5.4 `random.go`

The `RandomSource` interface, covered in section 4.1.

## 6. Domain: `lob` - the parameter tree

`internal/domain/lob` defines `LineOfBusiness`, the complete parameter set that makes the engine reusable across classes of business, plus validation and deterministic expected-loss pricing.

### 6.1 The parameter structs (`lob.go`)

`LineOfBusiness{Name string, Book BookParams, Pricing PricingParams, Claims ClaimParams, Runoff RunoffParams}` is the root.

- **`BookParams`** (step 1): `GrowthFactor` (year-on-year policy-count trend), `SizeVolatility` (sigma of mean-1 size noise), `Spread` (heterogeneity knob reused for both the sum-insured lognormal sigma and the risk-factor coefficient of variation), `SumInsuredMedian`, `SumInsuredInflation` (annual median drift), `ExcessChoices []ExcessChoice`.
- **`ExcessChoice`**: `Value` (deductible dollars), `Weight` (unnormalised selection weight).
- **`PricingParams`** (premium): the insurer's assumed loss cost, independent of the claims model. `TargetLossRatio` (premium = assumed expected loss / this), `AdequacyVolatility` (sigma of mean-one lognormal noise on each underwriting year's target loss ratio; 0 switches it off), `BaseFrequency`, `Severity SeverityParams`, `NilProbability`, `ReopenProbability`, `ReopenEstimateFactor`, `InflationMean` (all assumed values). Carries the `ExpectedPolicyLoss` and `ExpectedSectionLoss` methods (in `expectedloss.go`). The target sets premium only; the realized loss ratio emerges from the claims model. The preset starts these assumptions from the true claims values, so its loss ratio lands around the target; deviating them models underpricing or adverse experience.
- **`ClaimParams`** (step 2): `BaseFrequency`, `ReportLagMedian`, `ReportLagSigma`, `ThirdPartyReportLagMedian`/`ThirdPartyReportLagSigma` (third-party claims' own lognormal report lag; a median of 0 keeps the shared lag), `Severity SeverityParams`, `CloseLag CloseLagParams`, `Inflation InflationParams`, `NilProbability`, `Recoveries RecoveryParams`, `Reopening ReopeningParams`.
- **`SeverityParams`**: `ThirdPartyWeight` (probability a claim is third party), `OwnDamageMedianFraction` (own-damage median as a fraction of sum insured), `OwnDamageSigma`, `ThirdPartyScale` (Pareto minimum), `ThirdPartyAlpha` (Pareto tail index, must exceed 1 for a finite mean).
- **`CloseLagParams`**: `Shape`, `MeanDays` (own-damage gamma base), `SizeThreshold`/`SizeMultiplier` (stretch the mean lag for own-damage claims above the threshold in start-year dollars), `RiskLoading` (exponent applied to the risk factor), `ThirdPartyShape`/`ThirdPartyMeanDays` (the slower bodily-injury regime), `ThirdPartySizeElasticity`/`ThirdPartySizeReference` (a third-party claim costing `s` in start-year dollars has mean lag `ThirdPartyMeanDays * (s / ThirdPartySizeReference)^ThirdPartySizeElasticity`; an elasticity of 0 switches it off).
- **`InflationParams`**: `Mean` (average annual claims-inflation factor), `Volatility` (sigma of mean-1 noise per year).
- **`RecoveryParams`**: `Salvage`, `Subrogation`, each a `RecoveryTypeParams`.
- **`RecoveryTypeParams`**: `Probability` (the chance an eligible claim yields this recovery - a paid total loss for salvage, a paid own-damage claim for subrogation; 0 switches the type off), `MeanShare` (mean recovery as a share of gross paid), `Concentration` (Beta concentration), `LagMedianDays`, `LagSigma` (lognormal close-to-receipt lag).
- **`ReopeningParams`**: `Probability` (0 switches reopening off), `EstimateFactor` (the reopen's mean additional cost as a factor of the claim's ultimate, capped for own damage at the cover left), `EstimateSigma`, `LagMedianDays`, `LagSigma`.
- **`RunoffParams`** (steps 3-4): `CaseAdequacyMean` (true ultimate over the expected opening case - above 1 cases open deficient, below 1 redundant; it moves reserves, never the loss cost), `CaseAdequacySigma` (noise on each opening case), `PaymentsPerYear` (Poisson intensity of interim payments), `SettlementShare` (fraction of ultimate held for the final settlement), `Concentration` (Dirichlet concentration splitting the interim remainder), `RevisionsPerYear`, `RevisionSigma` (initial revision noise, decays with age).

### 6.2 Validation (`lob.go`)

`(l LineOfBusiness) Validate() error` is the only exported validation entry point. It rejects an empty `Name`, then delegates to unexported `Book.validate()`, `Pricing.validate()`, `Claims.validate()`, `Runoff.validate()`, short-circuiting on the first error and naming the offending field in snake_case (e.g. `book.excess_choices[2].value`).

A helper `checkFinite(fields ...namedFloat) error` screens NaN and infinity first, because "every comparison with NaN is false" would otherwise let a NaN slip past ordinary range checks. `namedFloat{name, v}` pairs a field's display name with its value.

Notable per-struct rules. A sub-block that is switched off is never read, so only its switch is validated: a YAML author does not have to invent parameters for a feature they turned off. Every field still passes the finite check.

- `BookParams`: growth/spread/median/inflation all `> 0`; volatility `>= 0`; `ExcessChoices` non-empty with each weight `>= 0` and a positive total weight; each value `>= 0`.
- `PricingParams`: `TargetLossRatio`, `BaseFrequency`, `InflationMean` all `> 0`; `AdequacyVolatility >= 0`; `NilProbability` and `ReopenProbability` in `[0, 1)`, and `ReopenEstimateFactor > 0` unless the reopen probability is 0; delegates to `Severity.validate("pricing.severity")`.
- `ClaimParams`: base frequency, report-lag median/sigma `> 0`; `NilProbability` in `[0, 1)`; delegates to `Severity.validate("claims.severity")`, inflation, both recovery types (with the prefix passed in), reopening, and close lag.
- `SeverityParams.validate(prefix string)`: `ThirdPartyWeight` in `[0, 1]` (inclusive, unlike the other probabilities); own-damage fraction/sigma `> 0` unless the weight is 1; third-party scale `> 0` and `ThirdPartyAlpha > 1` unless the weight is 0. The prefix names the offending field for either the pricing or claims severity block.
- `CloseLagParams.validate(thirdParty bool)`: shapes and mean days `> 0`; `SizeMultiplier >= 1`; loadings/threshold `>= 0`. The third-party shape and mean are skipped when the claims severity gives third-party claims no weight.
- `RecoveryTypeParams.validate(prefix string)`: `Probability` in `[0, 1)`; when it is above 0, `MeanShare` in the open interval `(0, 1)`, `Concentration`/`LagMedianDays > 0`, `LagSigma >= 0`.
- `ReopeningParams`: `Probability` in `[0, 1)`; when it is above 0, `EstimateFactor > 0`, sigmas/median with the usual non-negativity/positivity.
- `RunoffParams`: `SettlementShare` in `(0, 1]`; adequacy mean/concentration `> 0`; the rest `>= 0`.

### 6.3 Expected-loss pricing (`expectedloss.go`)

This file prices premium deterministically (no randomness) from the assumed loss cost in `PricingParams`. The formula is the exact expectation of its own assumptions, apart from the reopen cap noted below, so a gap between the pricing and claims assumptions moves the loss ratio by what it says. With the two equal, premium tracks expected loss and accident-year loss ratios do not drift as severities inflate, though the realized ratio still moves with claim sampling and the simulated inflation path. The formula is a deliberate, separate copy of the severity model - it is the insurer's assumption, not the true process.

- `normCDF(x)` - the standard normal CDF via `0.5*math.Erfc(-x/sqrt2)`, stable in the tails.
- `stopLossLognormal(median, sigma, excess)` - `E[(X-excess)+]` for a lognormal. Mean is `median*exp(sigma^2/2)`. For `excess <= 0` it is `mean - excess`; otherwise the Black-Scholes-style `mean*Phi(d1) - excess*Phi(d2)`.
- `limitedStopLossLognormal(median, sigma, excess, cap)` - `E[(min(X,cap)-excess)+]`, i.e. the layer between `excess` and `cap`. Returns 0 when `cap <= excess`, else the difference of two stop-loss layers. This is the own-damage cover between the deductible and a total-loss cap.
- `stopLossPareto(scale, alpha, excess)` - `E[(X-excess)+]` for a Pareto. Mean is `scale*alpha/(alpha-1)`. For `excess <= scale` it is `mean - excess`; otherwise the closed form `(scale/(alpha-1))*(scale/excess)^(alpha-1)`.
- `(p PricingParams) ExpectedPolicyLoss(sumInsured, excess, riskFactor, inflationFactor, siDrift float64) float64` - the deterministic expected ultimate gross incurred loss for one policy under the pricing assumptions. It backs out the base-year sum insured (`baseSI = sumInsured/siDrift`), trends the assumed own-damage median by the claims index (`odMedian = inflationFactor*baseSI*OwnDamageMedianFraction`), prices own damage as a limited stop-loss capped at the drifted `sumInsured`, prices third party as an uncapped Pareto stop-loss on a claims-trended scale, mixes them by the assumed `ThirdPartyWeight`, applies the expected payout per claim `1 - NilProbability + ReopenProbability*ReopenEstimateFactor` (a nil claim pays nothing in its first episode but pays a reopen like any other claim), and multiplies by the assumed `BaseFrequency*riskFactor`. Recoveries are excluded (gross basis). It draws no randomness, so pricing never perturbs a sub-stream.
- `(p PricingParams) ExpectedSectionLoss(...) (ownDamage, thirdParty float64)` - the same expected loss split into the policy's own-damage and third-party liability sections; `ExpectedPolicyLoss` is their sum. A section whose severity weight is zero is skipped rather than multiplied by zero, because its parameters are not validated and zero times an infinite layer cost is NaN.

## 7. Domain: `policy` - the book (step 1)

`internal/domain/policy/book.go` simulates the exposure claims arise from.

`Policy{ID, CoverStart, CoverEnd, SumInsured, Excess, RiskFactor, Premium, BaseSumInsured, ThirdPartyPremium}` is one 12-month motor policy. `BaseSumInsured` is the sum insured in start-year dollars (`SumInsured / SumInsuredInflation^y`, computed where the drift lives), which the claim stage sizes own damage off; it is never written to CSV. `ThirdPartyPremium` is the third-party liability section of `Premium`, priced the same way on that section's expected loss; the realism gate scores the liability claims against it, and it is never written to CSV. `CoverEnd` is always `CoverStart.AddDays(364)`. Money fields are `shared.Money`; `RiskFactor` is a `float64`.

`BookSimulator` holds `book lob.BookParams` and `pricing lob.PricingParams` (the latter drives premium pricing, independent of the claims model). `NewBookSimulator(book, pricing)` constructs it.

- `Simulate(src, startYear, years, initialSize int) []Policy` - produces the whole book. It splits a dedicated `book-size` stream for year-size noise and a `pricing-adequacy` stream for pricing noise and starts a global `id = 1`. It first writes a warm-up underwriting year (`y = -1`, `startYear-1`) of `warmUpSize = round(initialSize / GrowthFactor)` policies, with no size noise, so the window opens with a full book in force rather than one ramping up from nothing; the claim stage keeps only their in-window occurrences. Then `size = initialSize` for `y = 0`, and for each year `y`:
  - For `y > 0`, applies growth with noise: `size = round(size * GrowthFactor * MeanOneLogNormal(sizeSrc, SizeVolatility))`, clamped to a minimum of 1. So the book trends upward but individual years can shrink.
  - Draws the year's target loss ratio `TargetLossRatio * MeanOneLogNormal(adequacySrc, AdequacyVolatility)`; at a volatility of 0 this makes no draw. Every policy written in the year is priced to it, so cohorts scatter around the target while the expected loss ratio stays on it, and the knob moves premium only.
  - Computes the drifted median sum insured `SumInsuredMedian*SumInsuredInflation^y`, and the sum-insured drift `SumInsuredInflation^y`.
  - Emits `size` policies, each from its own `policy-<id>` sub-stream, incrementing the global `id`.
- `simulatePolicy(src, id, startYear, year int, lossRatio, medianSI, siDrift float64) Policy` - one policy: cover start uniform within the calendar year (leap-year aware via `DaysBetween`), the assumed pricing inflation factor `pricing.InflationMean^TrendYears(coverStart+182 days, startYear)` - the loss cost trended to the middle of the cover on the same time axis as the claims inflation index - sum insured lognormal `(log(medianSI), Spread)`, risk factor a mean-1 gamma with variance `Spread^2` (`Gamma(1/spread2, spread2)`), excess via `drawExcess`, and premium from `pricing.ExpectedSectionLoss(...)`: the sum of both sections over the year's `lossRatio`, with the third-party section alone giving `ThirdPartyPremium`.
- `drawExcess(src) float64` - weighted categorical draw over `ExcessChoices`: draw `u = Uniform()*totalWeight`, walk the choices subtracting weights, return the first whose running total crosses `u`; fall back to the last choice on floating-point edges.

## 8. Domain: `claim` - claim events (step 2)

`internal/domain/claim` simulates occurrence, report and close dates and the initial case estimate, plus the stochastic inflation path and the optional reopen episode.

### 8.1 `claim.go`

`Claim` embeds two structs, so the CSV surface is explicit in the type (RF-14); their fields read as `c.ID` or `c.Nil`:

- `Record` is the persisted claim, exactly the `claims.csv` columns: `ID`, `PolicyID`, `OccurrenceDate`, `ReportDate`, `CloseDate` (the final close after any reopen), and `InitialEstimate` (the opening case, set to `Ultimate` here and replaced by the case-estimate stage). The CSV writer reads only the record, and a test ties its field count to the file's columns.
- `Development` is what later stages need and no CSV writes: `Ultimate` (the true cost: ground-up loss minus excess, capped at the cover for own damage), `CoverLimit` (sum insured minus excess for own damage, zero meaning unlimited for third party), `RiskFactor` (the policy's, kept for the reopen pass's close-lag draw), `Nil` (the first episode closes without payment), `OwnDamage` (drives recovery eligibility), and the reopen fields `FirstCloseDate`, `ReopenDate`, `ReopenUltimate` (the episode's true additional cost) and `ReopenEstimate` (the case it re-opens at) - all zero when the claim never reopens.

`(c Claim) Reopened() bool` is `ReopenDate != zero`; `Cost()` is the true total paid over both episodes and `TotalLoss()` is an own-damage claim whose cost reached its cover limit.

`ClaimSimulator` holds `params`, an `InflationIndex`, a `windowStart`, and an exclusive `windowEnd`. It is built fluently:

- `NewClaimSimulator(p lob.ClaimParams) *ClaimSimulator` - sets only params (no inflation, nominal sum insured, no window).
- `WithInflation(x InflationIndex)` - sets the occurrence-date inflation index (the zero value is the identity).
- `WithWindow(startYear, years int)` - sets `windowStart = Jan 1 of startYear` and `windowEnd = Jan 1 of startYear+years` (exclusive), constraining occurrences to `[startYear, startYear+years)` so the trailing underwriting year does not spill a partial accident year into claims.csv and the warm-up underwriting year adds no claims before the window.

Helpers:

- `baseSumInsured(pol) float64` - the policy's `BaseSumInsured`, or the nominal sum insured for a hand-built policy without one.
- `occurrenceSpan(pol) (first Date, days int)` - the part of the cover claims can occur in: the cover (`CoverStart` to `CoverEnd` inclusive) clipped to `[windowStart, windowEnd)` when a window is set. `days` is zero or negative when the cover misses the window.
- `exposedFraction(pol) float64` - `occurrenceSpan` days over the 365 cover days, floored at 0, used to pro-rate frequency: 1 when windowing is off or the window holds the whole cover.

Core generation:

- `Simulate(src, book []policy.Policy) []Claim` - for each policy, splits a `claims-policy-<id>` stream, draws a Poisson count with mean `BaseFrequency * RiskFactor * exposedFraction(pol)`, and calls `simulateClaim` that many times (appending only reportable ones). It then stable-sorts by report date, then policy ID, then occurrence date - resembling a claims-system registration order - and assigns 1-based sequential IDs after sorting.
- `simulateClaim(src, pol) (Claim, bool)` - draws one claim in a fixed order so draw counts stay constant:
  1. Occurrence date: uniform over `occurrenceSpan(pol)`, one uniform draw whether or not the window clips the cover.
  2. Report lag: one normal deviate is drawn here (as `log(LogNormal(0, 1))`), before the severity draw decides the claim type, and the lag is set after it with the type's median and sigma (`ThirdPartyReportLag*` for third-party claims when set, the shared ones otherwise), rounded to days. Drawing the deviate first keeps the draw order the same for both types, so a third-party lag never moves an own-damage claim.
  3. Ground-up loss: `drawGroundUpLoss`.
  4. Claims inflation: multiply the loss by `inflation.For(occurrenceDate)` (applies to both severity components).
  5. Own-damage cap: if own damage and the loss exceeds the drifted `SumInsured`, cap it (a total loss).
  6. Cost = loss - excess; if `<= 0` the claim did not pierce the excess and is unreportable (`ok = false`, but the draws above were still consumed). Otherwise it becomes the claim's `Ultimate` (floored at one cent) and, for now, its `InitialEstimate`; own damage records `CoverLimit = SumInsured - Excess`.
  7. Close date: `report + round(drawCloseLag(...))`, sized on the cost deflated to start-year dollars by `inflation.For(occurrenceDate)`, so claims inflation does not push a growing share of claims over the size threshold.
  8. Nil flag: always drawn via `Bernoulli(NilProbability)`.
- `drawGroundUpLoss(src, pol) (loss float64, ownDamage bool)` - a two-component mixture that always consumes exactly two draws: with probability `ThirdPartyWeight`, a Pareto `(ThirdPartyScale, ThirdPartyAlpha)` (uncapped, `ownDamage=false`); otherwise `baseSumInsured(pol)` times a lognormal fraction `(log(OwnDamageMedianFraction), OwnDamageSigma)` (`ownDamage=true`).

Shared close-lag logic, reused by the reopen pass:

- `closeLagRegime(cl, baseSize, riskFactor, ownDamage) (shape, mean float64)` - selects the gamma parameters. `baseSize` is the claim's cost in start-year dollars. Own damage uses `Shape`/`MeanDays`, stretched by `SizeMultiplier` when `baseSize` exceeds `SizeThreshold`; third party uses the slower `ThirdPartyShape`/`ThirdPartyMeanDays`, the mean scaled by `(baseSize / ThirdPartySizeReference)^ThirdPartySizeElasticity` when the elasticity is set. Both scale the mean by `riskFactor^RiskLoading`.
- `drawCloseLag(src, cl, baseSize, riskFactor, ownDamage) float64` - draws `Gamma(shape, mean/shape)`, giving expected value `mean`.

### 8.2 `inflation.go` - the claims-inflation path

`InflationIndex{startYear int, factors []float64, mean float64}` maps an occurrence date to a cumulative inflation factor; the zero value is the identity (`For` returns 1.0 for every date). Time is read on the `shared.TrendYears` axis: years since the middle of the start year, each day measured at its midpoint.

- `NewInflationIndex(src, p lob.InflationParams, startYear, years int) InflationIndex` - `factors[0] = 1.0`, then each subsequent year multiplies by `Mean * MeanOneLogNormal(src, Volatility)`, so the expected annual factor is `Mean`. Returns the identity when `years < 1`.
- `(x InflationIndex) For(d shared.Date) float64` - anchors `factors[i]` at the middle of year `startYear+i` and interpolates geometrically between neighbouring anchors, so the index rises smoothly through the year instead of stepping each 1 January, and each calendar year averages close to its anchor. Before the first anchor and after the last it trends at `mean` from the nearest anchor. Returns 1.0 for the zero-value index.

### 8.3 `reopen.go` - the optional reopen episode

`ReopenSimulator{params lob.ClaimParams, inflation InflationIndex}`, built by `NewReopenSimulator(p)` and wired with `WithInflation(x)` (the zero index leaves costs nominal), runs as a post-pass after claim IDs are assigned. The second close lag is sized on the reopen's additional cost deflated by the index at the claim's occurrence date.

- `Apply(src, claims []Claim) []Claim` - mutates reopened claims in place. If reopen probability is `<= 0` it returns immediately with no draws. Otherwise, for each claim it splits a `reopen-claim-<id>` stream and draws `Bernoulli(Probability)`; a claim that does not reopen consumes exactly that one draw. A reopening claim then draws, in order: a reopen lag (lognormal, floored to 1 day), the reopen's additional cost (`Ultimate * EstimateFactor * MeanOneLogNormal`, floored to one cent, then capped for own damage at the cover left - `CoverLimit` less what the first episode pays, nothing for a nil claim; a claim with no cover left, such as a paid total loss, does not reopen), and a second close lag (via the shared `drawCloseLag`, floored to 1 day). It records `FirstCloseDate = old CloseDate`, `ReopenDate = FirstCloseDate + lag`, `ReopenUltimate` (and `ReopenEstimate` equal to it until the case-estimate stage), and moves `CloseDate` to `ReopenDate + closeLag`. The day floors guarantee `ReopenDate > FirstCloseDate` and final `CloseDate > ReopenDate`.

## 9. Domain: `transaction` - runoff and recoveries (steps 3-4)

`internal/domain/transaction` turns each claim into a ledger of case-estimate movements, payments, and recoveries.

`Transaction{ID, ClaimID, Date, Type, Amount}` is one movement. `Type` is a string enum: `PAYMENT`, `ESTIMATE` (in `runoff.go`), `SALVAGE`, `SUBROGATION` (in `recovery.go`). `(t Type) IsRecovery()` is true for salvage or subrogation. The outstanding case at any time is the running sum of `ESTIMATE` amounts (signed movements), so the first `ESTIMATE` row is the opening case at report date.

### 9.1 `runoff.go` - ultimate-first case runoff

The design is ultimate-first: the claim stage fixes the true ultimate cost, the case-estimate stage sets the case the claim opens at, payments split the ultimate over the claim's life, and the case estimate is a noisy view of the remaining cost that converges to zero at close. Development happens in episodes: a normal claim runs one episode; a reopened claim runs a first episode to first close, re-raises the case to the reopen estimate, then runs a second episode to final close.

`RunoffSimulator{params lob.RunoffParams}`, built by `NewRunoffSimulator(p)`:

- `Simulate(src, claims) []Transaction` - concatenates each claim's rows (from a `runoff-claim-<id>` stream, in claim order, each claim chronological), then assigns global 1-based IDs.
- `simulateClaim(src, c) []Transaction` - drives an `emitter`. It emits the opening `ESTIMATE` at report date, runs the first episode from report to first close (nil per `c.Nil`), and, if reopened, re-raises the case to `ReopenEstimate` and runs a second episode (never nil).
- `runEpisode(src, e, start, close, ultimate, isNil)` - develops one episode, paying exactly `ultimate` (`Ultimate` for the first episode, `ReopenUltimate` for the second). All offsets are report-relative (`base = DaysBetween(report, start)`). A paying episode draws interim payments first, then revisions; a nil episode draws revisions only. Revisions and payments merge in one loop (revisions sort before payments on the same day). At each revision, at elapsed share `u = offset/duration`, the case moves to its aim times `MeanOneLogNormal(RevisionSigma*(1-u))`, floored at one cent so the case stays open until the close date. A paying episode aims at `(ultimate - paid) * adequacyBias(u)`; a nil episode, whose handler does not know it will pay nothing, aims at the current case. A paying episode then pays the remaining ultimate as a final settlement, and the case snaps to exactly zero at close.
- `adequacyBias(u)` - `CaseAdequacyMean^(u-1)`: the case a revision aims at as a share of the true remaining cost. The case opens at about `1/CaseAdequacyMean` of the truth and the gap closes geometrically to parity at close, so incurred development carries a persistent IBNER signal; a mean of 1 makes every revision unbiased.
- `drawInterimPayments(src, ultimate, duration, years) []event` - splits `(1 - SettlementShare)` of the ultimate across `Poisson(PaymentsPerYear*years)` interior days, weighted by a Dirichlet built from `Gamma(Concentration, 1)` draws. Returns nil (settle everything at close) for `duration < 2`, zero count, degenerate weights, or rounding that would over-pay.
- `drawRevisions(src, duration, years) []event` - `Poisson(RevisionsPerYear*years)` pure-revision events on interior days (nil for `duration < 2`).
- `interiorOffset(src, duration) int` - a day strictly between report and close, `1 + int(Uniform()*(duration-1))`.
- `event{offset, kind, amount}` with `kind` in `{kindRevision=0, kindPayment=1}` is the internal merge unit.
- `emitter{claimID, report, outstanding, txs}` keeps the outstanding case non-negative by construction: `estimate(offset, movement)` appends a signed `ESTIMATE` (no-op for zero) and updates `outstanding`; `reviseTo(offset, target)` moves outstanding to `target` in one row; `pay(offset, amount)` strengthens the case up to the payment first if needed, emits the `PAYMENT`, then reduces the case by the same amount so every payment fully releases its own case.

### 9.2 `estimate.go` - the opening case estimate

`CaseEstimator{mean, sigma}`, built by `NewCaseEstimator(p lob.RunoffParams)` from `CaseAdequacyMean` and `CaseAdequacySigma`, runs after reopening and before the runoff.

- `Apply(src, claims) []Claim` - for each claim, splits a `case-estimate-claim-<id>` stream and sets `InitialEstimate = Ultimate * MeanOneLogNormal(sigma) / mean` (floored to one cent), and for a reopened claim `ReopenEstimate` from `ReopenUltimate` the same way. Across claims the true ultimate over the opening case averages `mean`. It never touches `Ultimate` or `ReopenUltimate`, so the adequacy knobs move case reserves and incurred development but no payment. A zero sigma draws nothing.

### 9.3 `recovery.go` - salvage and subrogation

Recoveries are pure cash events on own-damage claims that paid something; they land after close and never touch the (gross) case estimate.

`RecoverySimulator{params lob.RecoveryParams}`, built by `NewRecoverySimulator(p)`:

- `Apply(src, claims, txs) []Transaction` - first sums gross paid per claim from the `PAYMENT` rows. For each claim it draws recoveries from a `recovery-claim-<id>` stream and inserts them immediately after that claim's contiguous block in `txs`, then renumbers all IDs. Returns `txs` unchanged when no recoveries were produced.
- `simulateClaim(src, c, paid) []Transaction` - eligibility requires `OwnDamage && paid > 0` (a never-reopened nil claim has `paid == 0` and is ineligible; a reopened nil claim that paid in its second episode is subrogation-eligible). Salvage further requires `c.TotalLoss()` (the true cost reached the cover limit, the sum insured less excess) and a non-nil first episode, since only a written-off vehicle the claim paid for is sold; for a total loss gross paid is the vehicle's value less excess, so salvage is sized off it. It iterates salvage then subrogation, each from its own `SALVAGE`/`SUBROGATION` sub-stream, skipping salvage on an ineligible claim before drawing: skip if probability `<= 0` or the Bernoulli fails; draw the share from a `Beta(MeanShare*Concentration, (1-MeanShare)*Concentration)`; amount = `paid * share`; lag = lognormal `(log(LagMedianDays), LagSigma)` floored to 1 day. A key constraint keeps cumulative recovered strictly below gross paid: if `recovered + amount >= paid`, the amount is trimmed to `paid - recovered - OneCent`; sub-cent amounts emit no row. It stable-sorts the (at most two) rows by date so they come out chronological.

## 10. Domain: `triangle` - development triangles and realism

`internal/domain/triangle` holds the reserving concepts used both for the UI and for the realism test gate.

### 10.1 `monthly.go` - the canonical aggregate

`MonthlyGrid{Basis, StartMonth, DevPeriods, Paid, PaidNet, Incurred [][]float64, Reported [][]int, IBNR [][]float64}` is the single aggregation store. `IBNR` is pure IBNR at its true value: each claim's `Cost()` is booked in its occurrence month and released in its report month (skipped when they coincide), so its running sum at a valuation is the cost of claims occurred but not yet reported. It is not written to `triangles.csv`. Row `o` is origin month `StartMonth.Add(o)`; slice index `d` holds development period `d+1`, so index 0 is the origin month itself. Every row is `DevPeriods` wide.

Cells are **incremental**: a cell is the movement in that development month. Increments sum, so any coarser grain is a plain sum over cells and a cumulative view is a running sum along a row.

- `BuildMonthlyGrid(policies, claims, txs, startMonth, originMonths, basis)` - two passes over the input: one to size the rectangle to the widest development period any in-span claim reaches, one to place every movement. Weights match the annual triangles it replaced: paid counts `PAYMENT` only; net paid subtracts recoveries; incurred adds every case movement and payment and subtracts recoveries, so it is gross case plus net paid. Reported counts a claim in its **report** month. Development runs to full runoff, so the grid holds development after the run window ends.
- `OriginBasis` (`basis.go`) is the one configuration seam, consulted in exactly two places: a claim's origin month (occurrence month, or its policy's inception month) and a month's exposure (earned in the month, or written in it).
- `(g MonthlyGrid) Cell(measure, origin, dev)` reads a cell with a 1-based development period.

### 10.2 `coarsen.go` - every coarser grain

- `Coarsen(kind, devPeriods, foldTail)` maps both axes onto the calendar period the month falls in: `originPeriod = index(originMonth) - index(startMonth)` and `devPeriod = index(eventMonth) - index(originMonth) + 1`, for `Monthly`, `Quarterly` or `Annual`. Keying on the calendar period rather than dividing monthly development by twelve is what makes the annual result equal what the annual triangles have always measured: an accident in March 1998 paid in January 1999 is development year 2. Rows are zero-padded to `devPeriods` rather than left ragged, because `ATAFactors` counts an origin at an age only when its row reaches that far.
- `(s IncrementalSet) Cumulative(measure) Triangle` - the running-sum projection.
- `(g MonthlyGrid) AnnualTriangles(devYears) AnnualSet` - `Coarsen(Annual, devYears, true)` cumulated into the paid, net paid and incurred triangles the realism gate and the UI read, plus `TotalIncurred` (incurred plus pure IBNR), the counterpart of Schedule P total incurred.

### 10.3 `exposure.go` - exposure by month and year

- `ExposureByMonth(policies, startMonth, months, basis) []MonthExposure` - premium, exposure units in policy-years (`days / 365.25`) and a policy count per origin month, earned day pro-rata on the accident basis and landed whole at inception on the underwriting basis. The count follows the basis too: in force on the accident basis, where a policy counts in every month it covers so the column does not sum to the book's policy count, and inceptions on the underwriting basis, where it does apart from the warm-up year. Exposure outside the window is not counted. Because the book writes a warm-up underwriting year, the accident basis carries a full book in force from the first month and does not thin at either end; the underwriting basis books a whole policy year at an inception month whose claims are cut off at the window end, and does not count warm-up policies, which incept before the window.
- `EarnedPremiumByYear(policies, startYear, years) []float64` - the monthly premiums rolled up per calendar year, so the two views agree by construction.

### 10.4 `triangle.go` - the cumulative triangle and its factors

`Triangle{StartYear int, Cells [][]float64}` is a cumulative triangle indexed `[origin][dev]`; rows may be ragged.

- `(t Triangle) ATAFactors() []float64` - volume-weighted age-to-age (chain-ladder) factors: `factor[age] = sum(row[age+1]) / sum(row[age])` over rows long enough to have both cells; ages with no data are `NaN`; returns nil when the longest row has fewer than two cells.
- `(t Triangle) latestDiagonal()` (unexported) - the last cumulative value per non-empty row.

### 10.5 `compare.go` - scoring against reference bands

- `ReferenceSet{Name, Paid, Incurred, EarnedPremium, DevelopedIncurred}` - one reference company's observed triangles and premium. `DevelopedIncurred` is `Incurred` completed with the company's later reported development to the full ten ages; its zero value means none is available. `Incurred` is Schedule P total incurred (paid, case, bulk and IBNR), while the generated incurred is case incurred with no IBNR, so the incurred age-factor check compares different quantities: reference factors fall below 1 as early IBNR is released, generated ones mostly as nil claims release their case. `Comparison{Paid, Incurred, EarnedPremium}` - the generated data's equivalent.
- `Band{Lo, Hi, Min, Max}` - `Lo`/`Hi` are the scored P5-P95 pass interval; `Min`/`Max` are the full observed extremes kept for display. `(b Band) contains(v)` is inclusive membership.
- Constants: `bandLoPercentile = 5`, `bandHiPercentile = 95` (the scored band).
- `Percentile(xs, p)` - linearly interpolated percentile (type-7), non-mutating; `NaN` for empty input.
- `bandFromValues(xs)` (unexported) - a `Band` from P5/P95 plus scanned min/max; an empty input yields a band that contains nothing.
- `ATABands(triangles) []Band` - per development age, the band of volume-weighted factors across the triangles.
- `AgeCheck{Age, Value, Band, Within}` and `Check{Value, Band, Within}` - scored results for an age and for a scalar. `Age` is the 1-based development period the factor develops from, so age 1 is the factor from development period 1 to 2.
- `Report{PaidATA, IncurredATA []AgeCheck, LossRatio, LossRatioDrift Check}` - the comparison outcome. `(r Report) Pass()` requires every age check plus both scalar checks to be within. `(r Report) String()` renders a human-readable, 1-indexed report.
- `usableRefs(refs)` (unexported) - a backstop that drops reference companies with no scorable signal (non-positive total earned premium or non-positive summed incurred latest diagonal).
- `CompareToReference(c Comparison, refs) Report` - the main entry point. It filters to usable refs, checks paid and incurred age factors against the reference bands (only ages present in both), scores the ultimate loss ratio against the band of reference loss ratios on developed incurred (the generated triangles run to full development, so the reference's immature latest diagonal would not be like for like), and scores loss-ratio drift against the band of the reference companies' own drift on developed incurred (passing vacuously when drift cannot be computed). Real books drift widely (P5-P95 about 0.54 to 1.47), so this is a realism bound; the guard against systematic drift in the model is `TestPresetHasNoSystematicLossRatioDrift`, which switches the inflation path and pricing adequacy noise off and pools ten seeds.
- Helpers `checkAges`, `(r ReferenceSet) developedIncurred()` (`DevelopedIncurred`, falling back to `Incurred`), `lossRatio` (total latest incurred over total earned premium), and `lossRatioDrift` (second-half over first-half aggregate loss ratio on each origin year's latest value, applied to the generated triangles and to each reference company's developed incurred, so neither side is immature; the middle year is excluded for odd counts).

## 11. Application layer

`internal/application` orchestrates the domain and provides read-only analytics.

### 11.1 `generate.go` - the use case

- `GenerateRequest{LOB lob.LineOfBusiness, StartYear, Years, InitialBookSize int}` and `Dataset{Policies, Claims, Transactions}`.
- `(r GenerateRequest) validate()` (unexported) - requires `Years >= 1` and `InitialBookSize >= 1`, then delegates to `LOB.Validate()` (`StartYear` is not validated).
- `GenerateDataset(src shared.RandomSource, req GenerateRequest) (Dataset, error)` - validates, then runs the seven stages of section 3 over independently labelled sub-streams (`book`, `inflation`, `claims`, `reopening`, `case-estimate`, `runoff`, `recovery`), wiring the claim simulator fluently with the inflation index and window. The claim stage takes no book parameter: it reads each policy's `BaseSumInsured`. Output depends only on the master seed plus the request.

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

### 11.4 `aggregate.go` and `realism.go` - the aggregation pass and the gate

- `const developmentYears = 10` (Schedule P shape) - lives only in `aggregate.go` now.
- `Aggregate(ds, startYear, years, basis) (Aggregates, error)` - one pure aggregation pass per run: the monthly grid and exposure on the requested basis, plus the accident-basis annual triangles and earned premium. `Annual` and `EarnedPremium` are always accident-basis, because Schedule P is an accident-year presentation.
- `LiabilityComparison(ds, startYear, years) (triangle.Comparison, error)` - what the realism gate scores: the accident-basis annual net paid and total incurred (incurred plus pure IBNR) triangles and earned premium of the third-party liability section alone. `liabilitySection` narrows the dataset to the third-party claims and gives each policy its `ThirdPartyPremium` as premium; the result is then built like every other aggregate view, as the monthly grid coarsened to annual. The Schedule P private passenger auto reference is a liability line with no physical damage, so own damage is not scored. It lives with the gate rather than in `Aggregates`, so a run that does not score realism (the CLI) never builds it.
- `EvaluateRealism(ds, startYear, years, refs) (triangle.Report, error)` - a thin adapter: builds the `LiabilityComparison`, then returns `CompareToReference`. Used as a test gate (`TestDefaultPresetIsRealistic`) and by the UI.

## 12. Infrastructure layer

### 12.1 `config` - YAML mapping and preset registry

`internal/infrastructure/config/config.go` maps YAML onto `lob.LineOfBusiness`. The `*Params` DTOs (`LOBParams`, `BookParams`, `ClaimsParams`, `SeverityParams`, `CloseLagParams`, `InflationParams`, `RecoveriesParams`, `RecoveryTypeParams`, `ReopeningParams`, `RunoffParams`, `ExcessChoiceParams`) mirror the domain structs field-for-field, each field carrying matching `yaml:` and `json:` tags. This means the same structs serve both YAML config loading (CLI) and the JSON request/response shape (web API). Decoding is strict: `KnownFields(true)` rejects unknown keys.

- `decode(r) (LOBParams, error)` (unexported) - strict YAML decode.
- `Load(r) (lob.LineOfBusiness, error)` - decode, `ToDomain()`, then `Validate()`.
- `(d LOBParams) ToDomain() lob.LineOfBusiness` and `(r RecoveryTypeParams) toDomain()` - pure struct-to-struct translation, no validation.
- Preset registry: `motorPersonalYAML []byte` (`//go:embed motor-personal.yaml`), `presetInfos []PresetInfo{ID, Name}` (currently just motor-personal), and `presetYAML map[string][]byte`. `Presets()` returns a clone of the registry; `PresetParams(id)` returns the raw (unvalidated) `LOBParams` to prefill the UI editor; `Preset(id)` returns a validated domain object.
- `LoadFile(path)`, `MotorPersonal()` - file-based and embedded-preset loaders.

The embedded `motor-personal.yaml` is the annotated personal-motor preset, whose third-party section is calibrated so its triangles fall within the P5-P95 Schedule P liability bands; own-damage settlement is set by judgement as a short-tail class. Its comments explain the realism scope and the target-loss-ratio pricing.

### 12.2 `random` - covered in section 4.2.

### 12.3 `csv` - the writers

`internal/infrastructure/csv/writer.go` writes the three dataset CSVs; `internal/infrastructure/csv/monthly.go` writes the two aggregate CSVs. All five use stable formatting so identical datasets produce byte-identical files. Every column is numeric, an ISO-8601 date, or a fixed enum, so no quoting is needed and `fmt.Sprintf` is safe.

- `WriteDataset(dir, ds) error` - creates `dir` (0o755) and writes `policies.csv`, `claims.csv`, `transactions.csv`.
- `WriteAggregates(dir, ag) error` - writes `triangles.csv` (one row per grid cell, ordered by origin month then development month, zeros included) and `exposure.csv` (one row per origin month). Money is rendered at two decimal places and exposure units at six, with a guard so a value rounding to zero never prints as `-0.00`.
- `FormatRiskFactor(r) string` - fixed 6-decimal formatting for byte stability.
- `writeFile(dir, name, header, rows, row func(int) string)` (unexported, `writer.go`) - buffered generic writer with a deferred close that surfaces a close error only when there was no prior error; shared by both writers.

CSV headers:

```
policies.csv:     policy_id,cover_start,cover_end,sum_insured,excess,risk_factor,premium
claims.csv:       claim_id,policy_id,occurrence_date,report_date,close_date,initial_estimate
transactions.csv: transaction_id,claim_id,date,type,amount
triangles.csv:    origin_month,dev_month,paid,paid_net,incurred,reported_count
exposure.csv:     origin_month,premium,exposure_units,policies
```

`transactions.csv` is emitted in claim-registration order, not date order: all of a claim's rows are written together, and because recovery rows can post-date the close, a later claim's rows can carry earlier dates.

### 12.4 `schedulep` - the reference reader

`internal/infrastructure/schedulep/reader.go` reads the Schedule P reference companies into `triangle.ReferenceSet`s. Each company JSON carries a `ClassId`, a `PaidTriangle` and `IncurredTriangle` (each a list of `[year, [values...]]` rows), an `EarnedPremium` list of `[year, amount]` pairs, and `FutureIncurred`: the incurred development reported after the triangle's valuation date, as incremental `[year, [values...]]` rows that complete each origin year to ten ages. The file also carries `FuturePaid`, which nothing reads. Custom `UnmarshalJSON` methods on `triangleRow` and `premiumJSON` decode the positional pair encodings.

- `LoadFile(path)` - one company from disk; company name is the file stem.
- `LoadFS(fsys, dir)` - every `*.json` in a directory of a filesystem, sorted by name for determinism. This is what `runUI` uses with the embedded `refdata.Files`.
- `LoadDir(dir)` - the on-disk variant over `os.DirFS`, rewriting the "no reference files" sentinel to include the directory.
- `loadDirFS`, `parse`, `toTriangle` (unexported) - glob and sort names, parse each file (sorting premium and triangle rows by year), and require contiguous origin years in each triangle.
- `develop(tri, future)` (unexported) - copies the cumulative triangle and appends the running sum of each origin year's later increments, giving `DevelopedIncurred`. No later development yields the zero triangle; an origin year outside the triangle is an error.

### 12.5 `web` - the server and view models

`internal/infrastructure/web/server.go` serves the UI: an embedded static page (`//go:embed static`, holding `app.js`, `index.html`, `style.css`) plus a small JSON API. The server is stateless apart from the loaded reference sets; the latest run lives in the browser.

- `Server{refs, mux}` and `NewServer(refs)` register routes: `GET /api/lobs`, `GET /api/lobs/{id}/preset`, `GET /api/limits`, `GET /api/fields`, `POST /api/generate`, and `GET /` (a file server over the embedded `static` subtree).
- `ServeHTTP` is a security front gate before dispatch (the server is loopback-only): it rejects non-local `Host` (403 "forbidden host") and, when an `Origin` header is present, non-local origins (403 "forbidden origin"), guarding against DNS rebinding and cross-site use. `localHost` accepts `127.0.0.1`, `localhost`, `::1` (with optional port); `localOrigin` parses the origin and checks its host.
- `handleLOBs` returns the preset list as `lobInfoJSON{id, name}`. `handlePreset` returns the raw `LOBParams` for a preset id (404 on unknown). `handleGenerate` caps the body at 1 MiB, decodes a strict `generateRequest{seed string, start_year, years, initial_book_size, out_dir, origin_basis, params}` (an empty `origin_basis` defaults to accident), parses the seed, requires and absolutises `out_dir`, runs `GenerateDataset` and `WriteDataset`, then `application.Aggregate` and `WriteAggregates`, then `application.EvaluateRealism`, and returns `buildResponse` (validation and domain errors map to 400, CSV write and aggregation errors to 500).
- `handleFields` serves `formFields` (`fields.go`): the parameter form's metadata, one `formField{path, label, tip}` per numeric parameter in `fieldGroup`s in display order. `app.js` builds the form from it, so labels and tips live in one place; `TestFormFieldsCoverEveryParameter` requires exactly one entry per numeric `config.LOBParams` leaf (the excess choices table is built separately).
- `writeJSON`, `writeError` - JSON response helpers.

`internal/infrastructure/web/viewmodel.go` builds the `/api/generate` response DTOs. `buildResponse(req, ds, ag, realism)` assembles a `generateResponseJSON{run, summary, triangles, distributions, realism}`: `run.origin_basis` echoes `ag.Basis`, the triangles are `ag.Annual`'s paid, net paid and incurred for the whole book (already coarsened to 10 development years), the realism report (computed by `handleGenerate` via `EvaluateRealism`, so a grid error maps to a 500) scores the liability section only (the Realism tab says so under its banner), and the summary and distributions are delegated to the application layer, mapping each into JSON view models. Notable serialisation choices: `LossRatio` and per-age factors are pointers so they serialise as `null` when undefined or `NaN`; the `finite` helper replaces `NaN`/`Inf` band numbers with 0 so the response stays valid JSON.

The front end (`static/index.html`, `static/app.js`, `static/style.css`) is a single-page app: a sidebar form (line-of-business select, run flags including an origin-basis select, and an editable parameter panel built from `/api/fields` and prefilled from the preset) posts to `/api/generate` and renders four tabs - Summary, Triangles (with a Paid gross / Paid net / Incurred toggle and age-to-age factors), Distributions, and Realism. The tabs are unchanged: they read the annual triangles, not the monthly grid, which has no browser view.

### 12.6 `data/reference/refdata.go`

`refdata` embeds the Schedule P datasets so the binary can assess realism without repository access. `Files embed.FS` (`//go:embed "schedule p/ppauto_pos98-07/*.json"`) holds the 96 curated private-passenger-auto companies for accident years 1998-2007, and `const PersonalMotorDir = "schedule p/ppauto_pos98-07"` names the directory. `runUI` loads them via `schedulep.LoadFS(refdata.Files, refdata.PersonalMotorDir)`.

`data/reference/schedule p/` also holds curated companies for the other five Schedule P lines (commercial auto, other liability, workers compensation, products liability, medical malpractice), tracked against `data/reference/gr-code-list.md` and applied with `tools/prune-dec2025.ps1`. They are not embedded and nothing reads them today; they are staged for per-line-of-business calibration (see `docs/roadmap.md`).

## 13. CLI (`cmd/claimsgen/main.go`)

A single verb-first binary: `claimsgen <command> [flags]`.

- `main()` calls `os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))`.
- `run(args, stdout, stderr) int` dispatches to `generate` or `ui`; anything else (or no args) prints the usage text to stderr and returns exit code 2.
- `runGenerate` parses `--config`, `--seed` (default 1), `--out` (default `output`), `--start-year` (default 1998), `--years` (default 10), `--initial-book-size` (default 20000), `--origin-basis` (default `accident`, validated against `triangle.OriginBasis`); loads the embedded preset (or a YAML file via `--config`), runs `GenerateDataset(random.NewSource(seed), ...)`, writes the three dataset CSVs, runs `application.Aggregate` on the chosen basis, writes `triangles.csv` and `exposure.csv`, and prints a one-line summary including the triangle and exposure row counts. Config errors and generation/write errors return exit code 1; flag-parse errors return 2.
- `runUI` parses `--port` (default 8080), loads the embedded reference data, binds a loopback listener on `127.0.0.1:<port>`, prints the URL, and serves `web.NewServer(refs)`.

## 14. Key invariants and deliberate simplifications

Invariants worth remembering:

- **Determinism.** Same seed plus same parameters produce byte-identical CSVs. Every independent decision draws from its own labelled sub-stream, and draw counts are kept constant across knob toggles, so turning a feature on or off never reshuffles unrelated draws.
- **Case releases to zero.** Every runoff episode ends with the outstanding case revised to exactly zero on the close date, and each payment fully releases its own case, so outstanding case is always the running sum of `ESTIMATE` amounts and is never negative.
- **Recoveries stay below gross paid.** A claim's cumulative recovered is strictly less than its gross paid (by at least one cent), and recovery rows are the only transactions dated after the final close.
- **All claims close.** There is no valuation date; every claim runs to closure, gross paid equals the ultimate (zero for a never-reopened nil claim).
- **Paid never exceeds the cover.** Gross paid is exactly `Ultimate + ReopenUltimate` (just `ReopenUltimate` for a nil claim), and for own damage that never exceeds sum insured minus excess.

Deliberate simplifications (from the README's assumptions): own-damage severity trends only at the claims index and is capped at the sum insured; the case adequacy bias decays on one fixed geometric path for every claim; nil claims draw severity and probability independently of claim size; there is no seasonality, catastrophe, or event clustering; and each year's book is an independent cohort with no renewals. The preset's `pricing` assumptions start from the true claims values, so its loss ratio lands around the target and is more stable than a real book's; deviating the `pricing` block from the claims values models underpricing or adverse experience. The loss ratio is always emergent - the target sets premium, never experience.
