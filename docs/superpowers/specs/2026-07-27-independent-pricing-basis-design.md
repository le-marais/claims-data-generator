> **OUT OF CONTEXT - do not read (2026-08-09):** historical design record, kept for provenance only. Agents must not load this file into context or treat it as a source of truth; it records decisions as of its own date and may not match current behaviour. For how the system works today see `README.md`, `AGENTS.md`, and `docs/detailed-architecture.md`.

# Independent pricing basis: isolate premium pricing from claims experience

Date: 2026-07-27
Addresses: F1 (and the F2 aspect) from `docs/stage-coupling-findings.md`.

## Problem

Premium is priced off the exact same expected-loss model that generates
claims. `BookSimulator` holds `lob.ClaimParams` and prices each policy as
`ExpectedPolicyLoss(...) / target_loss_ratio`
(`internal/domain/policy/book.go:78`), where `ExpectedPolicyLoss` reads the
claims severity, base frequency, reopening, and inflation mean
(`internal/domain/lob/expectedloss.go`). Because pricing and experience share
one model, the realized loss ratio equals the target by construction, and the
policy stage cannot run without the claims parameters.

In practice an insurer's pricing loss ratio is an *assumption*; actual claims
experience is a separate process. The two are independent, and the realized
loss ratio is whatever falls out. The current design collapses them, which is
also why the README lists "the insurer prices risk perfectly" as a known
simplification.

## Goal

Give the policy stage its own pricing basis - the insurer's assumed loss cost -
independent of the claims model. The claims stage generates the true experience
with no reference to any pricing knob. The realized loss ratio becomes emergent,
so a book can be priced at one loss ratio and run at another (underpricing,
adverse experience, the pricing cycle). This also cuts the F1 coupling: the
policy stage no longer imports `ClaimParams`.

The change must default to today's behaviour: with the assumed pricing basis set
equal to the true claims values, output is byte-identical to the current engine
and the realism gate stays green. Deviation is opt-in.

## Design

### 1. Domain model

Add a `Pricing` concern to `LineOfBusiness`, sibling to `Book`, `Claims`,
`Runoff` (`internal/domain/lob/lob.go`):

```go
type LineOfBusiness struct {
    Name    string
    Book    BookParams
    Pricing PricingParams // new
    Claims  ClaimParams
    Runoff  RunoffParams
}

type PricingParams struct {
    TargetLossRatio      float64        // moved out of BookParams
    BaseFrequency        float64        // assumed loss-cost drivers
    Severity             SeverityParams // reuses the existing type
    ReopenProbability    float64
    ReopenEstimateFactor float64
    InflationMean        float64        // assumed pricing trend
}
```

`PricingParams` carries exactly the inputs `ExpectedPolicyLoss` consumes today:
base frequency, the five severity params, the reopen uplift inputs
(probability and estimate factor), and the mean inflation trend. `Severity`
reuses `SeverityParams` so the expected-loss formula is unchanged.

- `TargetLossRatio` moves from `BookParams` to `PricingParams`.
- `ExpectedPolicyLoss` moves from a method on `ClaimParams` to a method on
  `PricingParams`, reading the assumed values. The stop-loss helpers
  (`normCDF`, `stopLossLognormal`, `limitedStopLossLognormal`,
  `stopLossPareto`) stay in the `lob` package unchanged.
- The reopen uplift becomes `1 + p.ReopenProbability * p.ReopenEstimateFactor`
  (was `Reopening.Probability * Reopening.EstimateFactor`).
- `siDrift` continues to come from `BookParams.SumInsuredInflation`: it is
  exposure data (the actual sum-insured drift written on the book), not a loss
  assumption. (F3 - the claims stage also reading `SumInsuredInflation` - is a
  separate finding, out of scope here.)

### 2. Book simulator

`NewBookSimulator` takes `(BookParams, PricingParams)` instead of
`(BookParams, ClaimParams)` (`internal/domain/policy/book.go:31-35`):

- Field renamed from `claims lob.ClaimParams` to `pricing lob.PricingParams`.
- `book.go:56`: `inflation := math.Pow(s.pricing.InflationMean, float64(y))`.
- `book.go:78`: `premium := s.pricing.ExpectedPolicyLoss(sumInsured, excess,
  riskFactor, inflation, siDrift) / s.pricing.TargetLossRatio`.

`internal/application/generate.go:47` wires
`policy.NewBookSimulator(req.LOB.Book, req.LOB.Pricing)`. The claims stage
(`generate.go:52-57`) is untouched and references nothing from pricing.

After this, the `policy` package imports `PricingParams` only, never
`ClaimParams`. F1 is cut.

### 3. Config, preset, and UI fan-out

Follow the existing per-parameter fan-out pattern (RF-13); no refactor of the
fan-out mechanism here.

- `internal/infrastructure/config/config.go`: add a `PricingParams` DTO with
  matching `yaml`/`json` tags mirroring the domain struct (reusing the existing
  `SeverityParams` DTO), and map it in `ToDomain`.
- `internal/infrastructure/config/motor-personal.yaml`: add a `pricing:` block
  whose assumed values equal the claims values -
  `target_loss_ratio: 0.72`, `base_frequency: 0.12`, the same `severity`,
  `reopen_probability: 0.04`, `reopen_estimate_factor: 0.45`,
  `inflation_mean: 1.04` - and remove `target_loss_ratio` from `book:`.
- `internal/infrastructure/web/static/app.js`: add a Pricing group to the
  preset-driven form.

**Migration.** Moving `target_loss_ratio` out of `book:` is a breaking YAML
change: strict decoding (`KnownFields(true)`) rejects a config that still has
`book.target_loss_ratio` or lacks `pricing`. Acceptable given a single embedded
preset and a single maintainer; noted so it is a deliberate break.

### 4. Validation

`PricingParams.validate()` (unexported, called from `LineOfBusiness.Validate`):
`TargetLossRatio > 0`, `BaseFrequency > 0`, `Severity.validate()`,
`InflationMean > 0`, `ReopenProbability` in `[0, 1)`,
`ReopenEstimateFactor > 0`, plus `checkFinite` on the scalar fields. Reuses the
existing `SeverityParams.validate()`. Remove the `TargetLossRatio` check from
`BookParams.validate()`.

### 5. Behaviour, realism gate, reproducibility

- **Default is a no-op.** With pricing assumptions equal to the claims values,
  `ExpectedPolicyLoss` returns the identical number, premiums are numerically
  identical, and no RNG draw changes, so output is byte-identical. The golden
  test must pass unmodified - this is the acceptance bar.
- **The feature.** Assumed drivers below the truth underprice the book and lift
  realized LR above target (adverse experience); above the truth, the reverse.
  A difference between assumed and true *inflation trend* makes the LR drift
  across accident years rather than shift level.
- **Realism gate - no code changes.** Default lands realized LR ≈ 0.72, inside
  the reference band. A deliberate deviation can push it out of band (the
  intended "unrealistic scenario" signal); the default stays green. Because
  both trends carry inflation, a level deviation keeps the LR flat across years
  (drift check passes), while a trend deviation surfaces as drift - so the
  existing drift gate now doubles as a pricing-vs-experience trend validator,
  for free.
- **Reproducibility.** Pricing is deterministic (no RNG). The labelled
  sub-stream contract and stream labels are untouched.

### 6. Testing

- Move the `ExpectedPolicyLoss` Monte-Carlo tie test onto `PricingParams`,
  using pricing = claims, asserting priced expected loss ties to the simulated
  mean (the formula is tied to the model only when assumptions match).
- Update `internal/domain/policy/book_test.go` premium assertions to read
  pricing params.
- `TestDefaultPresetIsRealistic` unchanged - must still pass.
- Golden test unchanged - must still pass (byte-identical proof).
- New feature test: a config with assumed severity (or frequency) below the
  true value yields a realized loss ratio meaningfully above `target_loss_ratio`.
- Optional guard test: the shipped motor preset's pricing assumptions equal its
  claims values, documenting the "perfect pricing by default" intent.

### 7. Docs

- README "How the simulation works" premium bullet: pricing off an assumed loss
  cost, independent of true experience; realized LR is emergent.
- README "Assumptions and known simplifications": the "insurer prices risk
  perfectly" line becomes "prices perfectly by default, configurable" - the
  simplification is now opt-out.
- README parameters section: document the `pricing` block.
- `docs/stage-coupling-findings.md`: mark F1 (and the F2 aspect) addressed.

## Out of scope

- F3 (claims stage reading `Book.SumInsuredInflation`), F4-F8 from the findings
  doc. Separate changes.
- Deeper reduced-form pricing (findings Approach B) and a single adequacy knob
  (Approach C) were considered and rejected in favour of the independent
  assumed-basis model (Approach A).
- Frequency/severity/development apportionment of the experience gap: not
  modelled explicitly; the gap emerges from the difference between assumed and
  true drivers.
