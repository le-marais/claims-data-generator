# Independent pricing basis Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give the policy stage its own pricing basis (the insurer's assumed loss cost) so premium is priced independently of the claims model, making the realized loss ratio emergent and cutting the F1 coupling.

**Architecture:** Add a `PricingParams` value object to `LineOfBusiness`, sibling to `Book`/`Claims`/`Runoff`. Move `TargetLossRatio` and the `ExpectedPolicyLoss` method off `BookParams`/`ClaimParams` onto `PricingParams`. The book simulator prices from `PricingParams`; the claims stage is untouched and references no pricing knob. The preset's pricing assumptions default to the claims values, so output is byte-identical until someone deviates them.

**Tech Stack:** Go 1.26, gonum (distributions), yaml.v3 (config), vanilla JS UI. Tests are standard `go test`.

## Global Constraints

- Default behaviour must be byte-identical: with the preset's pricing assumptions equal to the claims values, the golden test and `TestDefaultPresetIsRealistic` pass unmodified.
- Pricing stays deterministic: no RNG draws in pricing code (the labelled sub-stream contract is untouched).
- Config decoding is strict (`KnownFields(true)`): every YAML key must map to a DTO field, and every DTO block must be present in the preset.
- Validation errors name the offending field in snake_case, matching the existing `namedFloat` pattern.
- Run commands from the repo root `C:/Users/Stephan/repos/claims data generator`. Work on branch `feature/independent-pricing-basis`.

---

### Task 1: Core refactor - move pricing onto PricingParams

This is one atomic unit: moving the type and method breaks every caller at once, so the domain change, the book/generate wiring, the config DTO, the preset YAML, and the existing test helpers all move together. The deliverable is a green suite producing byte-identical output.

**Files:**
- Modify: `internal/domain/lob/lob.go`
- Modify: `internal/domain/lob/expectedloss.go`
- Modify: `internal/domain/policy/book.go`
- Modify: `internal/application/generate.go`
- Modify: `internal/infrastructure/config/config.go`
- Modify: `internal/infrastructure/config/motor-personal.yaml`
- Test (fix existing): `internal/domain/lob/lob_test.go`, `internal/domain/lob/expectedloss_test.go`, `internal/domain/policy/book_test.go`, `internal/infrastructure/config/config_test.go`

**Interfaces:**
- Produces: `lob.PricingParams` struct with fields `TargetLossRatio float64`, `BaseFrequency float64`, `Severity SeverityParams`, `ReopenProbability float64`, `ReopenEstimateFactor float64`, `InflationMean float64`.
- Produces: method `func (p PricingParams) ExpectedPolicyLoss(sumInsured, excess, riskFactor, inflationFactor, siDrift float64) float64`.
- Produces: `lob.LineOfBusiness.Pricing PricingParams` field; `lob.BookParams` no longer has `TargetLossRatio`; `lob.ClaimParams` no longer has the `ExpectedPolicyLoss` method.
- Produces: `func (s SeverityParams) validate(prefix string) error` (was no-arg).
- Produces: `policy.NewBookSimulator(book lob.BookParams, pricing lob.PricingParams) *BookSimulator`.
- Produces: config `PricingParams` DTO and `LOBParams.Pricing` field; config `BookParams` DTO no longer has `TargetLossRatio`.

- [ ] **Step 1: Add the `PricingParams` type and `Pricing` field in `lob.go`**

In `internal/domain/lob/lob.go`, add `Pricing` to the struct (between `Book` and `Claims`):

```go
type LineOfBusiness struct {
	Name    string
	Book    BookParams
	Pricing PricingParams
	Claims  ClaimParams
	Runoff  RunoffParams
}
```

Remove the `TargetLossRatio` field (and its doc comment) from `BookParams`. Then add the new type after `BookParams` / `ExcessChoice`:

```go
// PricingParams drives premium pricing: the insurer's assumed loss cost,
// independent of the claims model. Each policy's premium is its assumed
// expected ultimate loss (ExpectedPolicyLoss) divided by TargetLossRatio.
// When these assumptions equal the true claims values the book is priced
// perfectly and the realized loss ratio lands on the target; deviating them
// models underpricing or adverse experience.
type PricingParams struct {
	// TargetLossRatio is the assumed loss ratio premium is priced to.
	TargetLossRatio float64
	// BaseFrequency is the assumed ground-up occurrence frequency at risk factor 1.
	BaseFrequency float64
	// Severity is the assumed ground-up loss mixture.
	Severity SeverityParams
	// ReopenProbability and ReopenEstimateFactor are the assumed reopen uplift
	// inputs: expected extra development is ReopenProbability * ReopenEstimateFactor.
	ReopenProbability    float64
	ReopenEstimateFactor float64
	// InflationMean is the assumed mean annual claims-inflation trend.
	InflationMean float64
}
```

- [ ] **Step 2: Move `ExpectedPolicyLoss` onto `PricingParams` in `expectedloss.go`**

In `internal/domain/lob/expectedloss.go`, change the method receiver and the reopen-uplift source. Replace the whole `ExpectedPolicyLoss` function with:

```go
// ExpectedPolicyLoss is the deterministic expected ultimate gross incurred loss
// for one policy under the pricing assumptions. Own damage is expressed in
// base-year sum-insured terms (baseSI = sumInsured / siDrift) trended by the
// claims index only, and capped at the drifted sumInsured (a total loss). Third
// party keeps the claims index. It draws no randomness, so pricing never
// perturbs a sub-stream. Recoveries are excluded (gross basis).
func (p PricingParams) ExpectedPolicyLoss(sumInsured, excess, riskFactor, inflationFactor, siDrift float64) float64 {
	s := p.Severity
	baseSI := sumInsured / siDrift
	odMedian := inflationFactor * baseSI * s.OwnDamageMedianFraction
	od := limitedStopLossLognormal(odMedian, s.OwnDamageSigma, excess, sumInsured)
	tpScale := inflationFactor * s.ThirdPartyScale
	tp := stopLossPareto(tpScale, s.ThirdPartyAlpha, excess)
	perClaim := s.ThirdPartyWeight*tp + (1-s.ThirdPartyWeight)*od
	reopenUplift := 1 + p.ReopenProbability*p.ReopenEstimateFactor
	return p.BaseFrequency * riskFactor * perClaim * reopenUplift
}
```

- [ ] **Step 3: Add `PricingParams.validate`, parameterise `SeverityParams.validate`, and wire both into `Validate`**

In `internal/domain/lob/lob.go`:

Change the `Validate` method to validate pricing after book:

```go
func (l LineOfBusiness) Validate() error {
	if l.Name == "" {
		return fmt.Errorf("name: must not be empty")
	}
	if err := l.Book.validate(); err != nil {
		return err
	}
	if err := l.Pricing.validate(); err != nil {
		return err
	}
	if err := l.Claims.validate(); err != nil {
		return err
	}
	return l.Runoff.validate()
}
```

In `BookParams.validate`, remove the `namedFloat{"book.target_loss_ratio", b.TargetLossRatio}` line from the `checkFinite` call and delete the trailing `if b.TargetLossRatio <= 0 { ... }` block.

Change `SeverityParams.validate` to take a prefix (so pricing and claims report distinct keys):

```go
func (s SeverityParams) validate(prefix string) error {
	if err := checkFinite(
		namedFloat{prefix + ".third_party_weight", s.ThirdPartyWeight},
		namedFloat{prefix + ".own_damage_median_fraction", s.OwnDamageMedianFraction},
		namedFloat{prefix + ".own_damage_sigma", s.OwnDamageSigma},
		namedFloat{prefix + ".third_party_scale", s.ThirdPartyScale},
		namedFloat{prefix + ".third_party_alpha", s.ThirdPartyAlpha},
	); err != nil {
		return err
	}
	if s.ThirdPartyWeight < 0 || s.ThirdPartyWeight > 1 {
		return fmt.Errorf("%s.third_party_weight: must be in [0, 1], got %v", prefix, s.ThirdPartyWeight)
	}
	if s.OwnDamageMedianFraction <= 0 {
		return fmt.Errorf("%s.own_damage_median_fraction: must be positive, got %v", prefix, s.OwnDamageMedianFraction)
	}
	if s.OwnDamageSigma <= 0 {
		return fmt.Errorf("%s.own_damage_sigma: must be positive, got %v", prefix, s.OwnDamageSigma)
	}
	if s.ThirdPartyScale <= 0 {
		return fmt.Errorf("%s.third_party_scale: must be positive, got %v", prefix, s.ThirdPartyScale)
	}
	if s.ThirdPartyAlpha <= 1 {
		return fmt.Errorf("%s.third_party_alpha: must exceed 1 for a finite mean, got %v", prefix, s.ThirdPartyAlpha)
	}
	return nil
}
```

In `ClaimParams.validate`, change the call `c.Severity.validate()` to `c.Severity.validate("claims.severity")`.

Add the pricing validator (place it after `BookParams.validate`):

```go
func (p PricingParams) validate() error {
	if err := checkFinite(
		namedFloat{"pricing.target_loss_ratio", p.TargetLossRatio},
		namedFloat{"pricing.base_frequency", p.BaseFrequency},
		namedFloat{"pricing.reopen_probability", p.ReopenProbability},
		namedFloat{"pricing.reopen_estimate_factor", p.ReopenEstimateFactor},
		namedFloat{"pricing.inflation_mean", p.InflationMean},
	); err != nil {
		return err
	}
	if p.TargetLossRatio <= 0 {
		return fmt.Errorf("pricing.target_loss_ratio: must be positive, got %v", p.TargetLossRatio)
	}
	if p.BaseFrequency <= 0 {
		return fmt.Errorf("pricing.base_frequency: must be positive, got %v", p.BaseFrequency)
	}
	if err := p.Severity.validate("pricing.severity"); err != nil {
		return err
	}
	if p.ReopenProbability < 0 || p.ReopenProbability >= 1 {
		return fmt.Errorf("pricing.reopen_probability: must be in [0, 1), got %v", p.ReopenProbability)
	}
	if p.ReopenEstimateFactor <= 0 {
		return fmt.Errorf("pricing.reopen_estimate_factor: must be positive, got %v", p.ReopenEstimateFactor)
	}
	if p.InflationMean <= 0 {
		return fmt.Errorf("pricing.inflation_mean: must be positive, got %v", p.InflationMean)
	}
	return nil
}
```

- [ ] **Step 4: Point the book simulator at `PricingParams` in `book.go`**

In `internal/domain/policy/book.go`, change the struct field and constructor:

```go
// BookSimulator generates the policy book for a run.
type BookSimulator struct {
	book    lob.BookParams
	pricing lob.PricingParams
}

// NewBookSimulator builds a book simulator from the book and pricing
// parameters; pricing parameters drive premium (independent of the claims
// model that generates experience).
func NewBookSimulator(book lob.BookParams, pricing lob.PricingParams) *BookSimulator {
	return &BookSimulator{book: book, pricing: pricing}
}
```

Change line 56 (`inflation := ...`) to:

```go
		inflation := math.Pow(s.pricing.InflationMean, float64(y))
```

Change line 78 (`premium := ...`) to:

```go
		premium := s.pricing.ExpectedPolicyLoss(sumInsured, excess, riskFactor, inflation, siDrift) / s.pricing.TargetLossRatio
```

- [ ] **Step 5: Update the caller in `generate.go`**

In `internal/application/generate.go`, change the book construction (line ~47):

```go
	book := policy.NewBookSimulator(req.LOB.Book, req.LOB.Pricing).
		Simulate(src.Split("book"), req.StartYear, req.Years, req.InitialBookSize)
```

- [ ] **Step 6: Add the config DTO and mapping in `config.go`**

In `internal/infrastructure/config/config.go`:

Add `Pricing` to `LOBParams` (between `Book` and `Claims`):

```go
type LOBParams struct {
	Name    string        `yaml:"name" json:"name"`
	Book    BookParams    `yaml:"book" json:"book"`
	Pricing PricingParams `yaml:"pricing" json:"pricing"`
	Claims  ClaimsParams  `yaml:"claims" json:"claims"`
	Runoff  RunoffParams  `yaml:"runoff" json:"runoff"`
}
```

Remove the `TargetLossRatio` line from the config `BookParams` struct.

Add the DTO (after `BookParams`):

```go
// PricingParams mirrors lob.PricingParams for YAML/JSON.
type PricingParams struct {
	TargetLossRatio      float64        `yaml:"target_loss_ratio" json:"target_loss_ratio"`
	BaseFrequency        float64        `yaml:"base_frequency" json:"base_frequency"`
	Severity             SeverityParams `yaml:"severity" json:"severity"`
	ReopenProbability    float64        `yaml:"reopen_probability" json:"reopen_probability"`
	ReopenEstimateFactor float64        `yaml:"reopen_estimate_factor" json:"reopen_estimate_factor"`
	InflationMean        float64        `yaml:"inflation_mean" json:"inflation_mean"`
}
```

In `ToDomain`, remove `TargetLossRatio: d.Book.TargetLossRatio,` from the `Book:` literal, and add a `Pricing:` block after the `Book:` block:

```go
		Pricing: lob.PricingParams{
			TargetLossRatio: d.Pricing.TargetLossRatio,
			BaseFrequency:   d.Pricing.BaseFrequency,
			Severity: lob.SeverityParams{
				ThirdPartyWeight:        d.Pricing.Severity.ThirdPartyWeight,
				OwnDamageMedianFraction: d.Pricing.Severity.OwnDamageMedianFraction,
				OwnDamageSigma:          d.Pricing.Severity.OwnDamageSigma,
				ThirdPartyScale:         d.Pricing.Severity.ThirdPartyScale,
				ThirdPartyAlpha:         d.Pricing.Severity.ThirdPartyAlpha,
			},
			ReopenProbability:    d.Pricing.ReopenProbability,
			ReopenEstimateFactor: d.Pricing.ReopenEstimateFactor,
			InflationMean:        d.Pricing.InflationMean,
		},
```

- [ ] **Step 7: Add the `pricing` block to the preset and remove `book.target_loss_ratio`**

In `internal/infrastructure/config/motor-personal.yaml`, delete the `target_loss_ratio` line and its two comment lines from the `book:` block (lines 19-22). Then insert a `pricing:` block between the `book:` block and the `claims:` block:

```yaml
# Pricing basis: the insurer's assumed loss cost, used only to set premium.
# Independent of the claims block, which generates the true experience. When
# these assumptions equal the claims values (as below) the book is priced
# perfectly and the realized loss ratio lands on target_loss_ratio; deviate
# them to model underpricing or adverse experience.
pricing:
  target_loss_ratio: 0.72
  base_frequency: 0.12
  severity:
    third_party_weight: 0.20
    own_damage_median_fraction: 0.12
    own_damage_sigma: 1.0
    third_party_scale: 4000
    third_party_alpha: 2.2
  reopen_probability: 0.04
  reopen_estimate_factor: 0.45
  inflation_mean: 1.04
```

- [ ] **Step 8: Fix the `lob` package tests**

In `internal/domain/lob/lob_test.go`:

In `validMotor()`, remove `TargetLossRatio: 0.72,` from the `Book:` literal and add a `Pricing:` block after it (values mirror the claims block so the set is self-consistent):

```go
		Pricing: PricingParams{
			TargetLossRatio: 0.72,
			BaseFrequency:   0.15,
			Severity: SeverityParams{
				ThirdPartyWeight:        0.15,
				OwnDamageMedianFraction: 0.15,
				OwnDamageSigma:          1.0,
				ThirdPartyScale:         5000,
				ThirdPartyAlpha:         2.0,
			},
			ReopenProbability:    0.04,
			ReopenEstimateFactor: 0.45,
			InflationMean:        1.0,
		},
```

Change the offending-field table entry `{"book.target_loss_ratio", ...}` to:

```go
		{"pricing.target_loss_ratio", func(l *LineOfBusiness) { l.Pricing.TargetLossRatio = 0 }},
```

In `internal/domain/lob/expectedloss_test.go`, replace both `ExpectedPolicyLoss` tests' `ClaimParams{...}` construction with `PricingParams{...}`. For `TestExpectedPolicyLossScalesWithRiskAndInflation`, replace the `c := ClaimParams{...}` block (and rename `c` to `p`) with:

```go
	p := PricingParams{
		BaseFrequency: 0.12,
		Severity: SeverityParams{
			ThirdPartyWeight:        0.20,
			OwnDamageMedianFraction: 0.12,
			OwnDamageSigma:          1.0,
			ThirdPartyScale:         4000,
			ThirdPartyAlpha:         2.2,
		},
		ReopenProbability:    0.04,
		ReopenEstimateFactor: 0.45,
	}
	base := p.ExpectedPolicyLoss(20000, 300, 1.0, 1.0, 1.0)
```

and update the two later `c.ExpectedPolicyLoss(...)` calls in that test to `p.ExpectedPolicyLoss(...)`.

For `TestExpectedPolicyLossRebasesAndCapsOwnDamage`, replace the `c := ClaimParams{...}` block (rename `c` to `p`) with:

```go
	p := PricingParams{
		BaseFrequency: 0.12,
		Severity: SeverityParams{
			ThirdPartyWeight:        0, // pure own damage
			OwnDamageMedianFraction: 0.12,
			OwnDamageSigma:          1.0,
			ThirdPartyScale:         4000,
			ThirdPartyAlpha:         2.2,
		},
		ReopenProbability:    0.04,
		ReopenEstimateFactor: 0.45,
	}
```

and update the three references: `p.ExpectedPolicyLoss(...)` (twice), and the ceiling line to `reopenUplift := 1 + p.ReopenProbability*p.ReopenEstimateFactor` and `ceiling := p.BaseFrequency * 1.0 * (si - excess) * reopenUplift`.

- [ ] **Step 9: Fix the `policy` package tests**

In `internal/domain/policy/book_test.go`, replace the `claimParams()` helper with a `pricingParams()` helper:

```go
func pricingParams() lob.PricingParams {
	return lob.PricingParams{
		TargetLossRatio: 0.72,
		BaseFrequency:   0.12,
		Severity: lob.SeverityParams{
			ThirdPartyWeight:        0.20,
			OwnDamageMedianFraction: 0.12,
			OwnDamageSigma:          1.0,
			ThirdPartyScale:         4000,
			ThirdPartyAlpha:         2.2,
		},
		ReopenProbability:    0.04,
		ReopenEstimateFactor: 0.45,
		InflationMean:        1.04,
	}
}
```

Replace every `policy.NewBookSimulator(<book>, claimParams())` call with `policy.NewBookSimulator(<book>, pricingParams())` (there are four: in `TestBookSizeFollowsRecursionWithoutVolatility`, `TestBookSizeCanShrinkSomeYears`, `TestPolicyIDsAreSequential`, `TestPolicyFieldConsistency`). Also remove the now-unused `TargetLossRatio: 0.72,` line from the `params()` helper's `BookParams` literal.

In `TestPolicyFieldConsistency`, replace the premium-assertion block (lines ~126-133) with:

```go
		pp := pricingParams()
		yearOffset := float64(p.CoverStart.Year() - 1998)
		infl := math.Pow(pp.InflationMean, yearOffset)
		siDrift := math.Pow(prm.SumInsuredInflation, yearOffset)
		wantPremium := pp.ExpectedPolicyLoss(p.SumInsured.Dollars(), p.Excess.Dollars(), p.RiskFactor, infl, siDrift) / pp.TargetLossRatio
		if math.Abs(p.Premium.Dollars()-wantPremium) > 0.01 {
			t.Fatalf("premium %v, want %v", p.Premium.Dollars(), wantPremium)
		}
```

- [ ] **Step 10: Fix the `config` package test fixture and add a preset guard test**

In `internal/infrastructure/config/config_test.go`, in the `validYAML` constant: remove the `  target_loss_ratio: 0.72` line from the `book:` block and insert a `pricing:` block between `book:` and `claims:`:

```yaml
pricing:
  target_loss_ratio: 0.72
  base_frequency: 0.15
  severity:
    third_party_weight: 0.15
    own_damage_median_fraction: 0.15
    own_damage_sigma: 1.0
    third_party_scale: 5000
    third_party_alpha: 2.0
  reopen_probability: 0.04
  reopen_estimate_factor: 0.45
  inflation_mean: 1.04
```

Then read the rest of `config_test.go`; if any assertion references `Book.TargetLossRatio` or `book.target_loss_ratio`, update it to the pricing equivalent. Add a guard test pinning the "priced to truth by default" preset invariant:

```go
func TestMotorPresetPricesToTruth(t *testing.T) {
	l, err := MotorPersonal()
	if err != nil {
		t.Fatalf("MotorPersonal: %v", err)
	}
	if l.Pricing.BaseFrequency != l.Claims.BaseFrequency {
		t.Errorf("pricing base frequency %v != claims %v", l.Pricing.BaseFrequency, l.Claims.BaseFrequency)
	}
	if l.Pricing.Severity != l.Claims.Severity {
		t.Errorf("pricing severity %+v != claims %+v", l.Pricing.Severity, l.Claims.Severity)
	}
	if l.Pricing.ReopenProbability != l.Claims.Reopening.Probability {
		t.Errorf("pricing reopen prob %v != claims %v", l.Pricing.ReopenProbability, l.Claims.Reopening.Probability)
	}
	if l.Pricing.ReopenEstimateFactor != l.Claims.Reopening.EstimateFactor {
		t.Errorf("pricing reopen factor %v != claims %v", l.Pricing.ReopenEstimateFactor, l.Claims.Reopening.EstimateFactor)
	}
	if l.Pricing.InflationMean != l.Claims.Inflation.Mean {
		t.Errorf("pricing inflation mean %v != claims %v", l.Pricing.InflationMean, l.Claims.Inflation.Mean)
	}
}
```

- [ ] **Step 11: Build and run the full suite**

Run: `go build ./... && go vet ./... && go test ./...`
Expected: PASS across all packages. The golden test and `TestDefaultPresetIsRealistic` must pass unmodified (byte-identical output proof). If a compile error names a residual `TargetLossRatio`/`ExpectedPolicyLoss`/`claimParams` reference the steps missed, fix it to the pricing equivalent and re-run.

- [ ] **Step 12: Commit**

```bash
git add internal/ 
git commit -m "Move premium pricing onto an independent PricingParams basis"
```

---

### Task 2: UI form - Pricing parameter group

**Files:**
- Modify: `internal/infrastructure/web/static/app.js`

**Interfaces:**
- Consumes: the `pricing.*` JSON keys produced by the config DTO in Task 1 (`target_loss_ratio`, `base_frequency`, `severity.*`, `reopen_probability`, `reopen_estimate_factor`, `inflation_mean`).

- [ ] **Step 1: Remove the target-loss-ratio field from the Book group**

In `internal/infrastructure/web/static/app.js`, delete this line from the Book group's `fields`:

```js
      { path: ["book", "target_loss_ratio"], label: "Target loss ratio", tip: "Premium = expected ultimate loss / target loss ratio." },
```

- [ ] **Step 2: Add a Pricing group after the Book group**

Insert a new group object in `FIELD_GROUPS` between the Book group's closing `},` and the Claims group's opening `{`:

```js
  {
    label: "Pricing",
    fields: [
      { path: ["pricing", "target_loss_ratio"], label: "Target loss ratio", tip: "Assumed loss ratio premium is priced to. Premium = assumed expected loss / target." },
      { path: ["pricing", "base_frequency"], label: "Assumed base frequency", tip: "Assumed ground-up frequency used for pricing (independent of the true claims frequency)." },
      { path: ["pricing", "severity", "third_party_weight"], label: "Assumed third party weight", tip: "Assumed probability a claim is third party, for pricing." },
      { path: ["pricing", "severity", "own_damage_median_fraction"], label: "Assumed own damage median fraction", tip: "Assumed median own-damage loss as a fraction of sum insured, for pricing." },
      { path: ["pricing", "severity", "own_damage_sigma"], label: "Assumed own damage sigma", tip: "Assumed sigma of the own-damage lognormal, for pricing." },
      { path: ["pricing", "severity", "third_party_scale"], label: "Assumed third party scale", tip: "Assumed Pareto scale (minimum) in dollars, for pricing." },
      { path: ["pricing", "severity", "third_party_alpha"], label: "Assumed third party alpha", tip: "Assumed Pareto tail index, for pricing; must exceed 1." },
      { path: ["pricing", "reopen_probability"], label: "Assumed reopen probability", tip: "Assumed reopen chance feeding the pricing uplift." },
      { path: ["pricing", "reopen_estimate_factor"], label: "Assumed reopen estimate factor", tip: "Assumed reopen estimate factor feeding the pricing uplift." },
      { path: ["pricing", "inflation_mean"], label: "Assumed inflation trend", tip: "Assumed mean annual claims-inflation trend used for pricing." },
    ],
  },
```

- [ ] **Step 3: Verify the form round-trips**

Run: `go test ./internal/infrastructure/web/... ./internal/infrastructure/config/...`
Expected: PASS. (The server serves `app.js` as a static asset; there is no JS unit test, so this confirms the preset JSON the form reads still loads and validates.)

Optional manual check: `go run ./cmd/claimsgen ui`, open `http://127.0.0.1:8080`, expand "Line of business parameters", confirm a "Pricing" group appears with the target loss ratio and assumed drivers prefilled, and Generate still works.

- [ ] **Step 4: Commit**

```bash
git add internal/infrastructure/web/static/app.js
git commit -m "Add Pricing parameter group to the UI form"
```

---

### Task 3: Feature test - underpricing raises the realized loss ratio

**Files:**
- Create: `internal/application/pricing_test.go`

**Interfaces:**
- Consumes: `config.MotorPersonal()`, `application.GenerateDataset`, `application.GenerateRequest`, `application.Summarize`, `SummaryReport.Total.LossRatio()`, `random.NewSource`, and the `lob.PricingParams` fields from Task 1.

- [ ] **Step 1: Write the failing test**

Create `internal/application/pricing_test.go`:

```go
package application_test

import (
	"testing"

	"github.com/le-marais/claimsgen/internal/application"
	"github.com/le-marais/claimsgen/internal/infrastructure/config"
	"github.com/le-marais/claimsgen/internal/infrastructure/random"
)

func pooledLossRatio(r application.SummaryReport) float64 {
	lr, _ := r.Total.LossRatio()
	return lr
}

// Pricing and experience are independent: leaving the true claims model
// unchanged but assuming a cheaper loss cost underprices the book, so the
// realized loss ratio rises on its own.
func TestUnderpricingRaisesRealizedLossRatio(t *testing.T) {
	base, err := config.MotorPersonal()
	if err != nil {
		t.Fatalf("MotorPersonal: %v", err)
	}
	req := application.GenerateRequest{LOB: base, StartYear: 1998, Years: 10, InitialBookSize: 4000}

	dsBase, err := application.GenerateDataset(random.NewSource(1), req)
	if err != nil {
		t.Fatalf("baseline generate: %v", err)
	}
	lrBase := pooledLossRatio(application.Summarize(dsBase, 1998, 10))

	// Underprice: assume own-damage and third-party severity at half the truth.
	// Claims params are untouched, so actual losses are identical; only premium
	// (hence earned premium) falls, lifting the loss ratio.
	under := base
	under.Pricing.Severity.OwnDamageMedianFraction *= 0.5
	under.Pricing.Severity.ThirdPartyScale *= 0.5
	reqUnder := req
	reqUnder.LOB = under

	dsUnder, err := application.GenerateDataset(random.NewSource(1), reqUnder)
	if err != nil {
		t.Fatalf("underpriced generate: %v", err)
	}
	lrUnder := pooledLossRatio(application.Summarize(dsUnder, 1998, 10))

	if lrBase < 0.6 || lrBase > 0.85 {
		t.Fatalf("baseline loss ratio %.3f not near target %.3f (pricing should equal truth)", lrBase, base.Pricing.TargetLossRatio)
	}
	if lrUnder <= lrBase {
		t.Fatalf("underpricing did not raise loss ratio: base %.3f, under %.3f", lrBase, lrUnder)
	}
	if lrUnder <= base.Pricing.TargetLossRatio+0.2 {
		t.Fatalf("underpriced loss ratio %.3f not clearly above target %.3f", lrUnder, base.Pricing.TargetLossRatio)
	}
}
```

- [ ] **Step 2: Run the test**

Run: `go test ./internal/application/ -run TestUnderpricingRaisesRealizedLossRatio -v`
Expected: PASS. (No production code changes are needed; the behaviour comes from Task 1. If `lrBase` is out of the sanity band, the pricing-equals-truth default is broken - revisit Task 1 Step 7/10.)

- [ ] **Step 3: Commit**

```bash
git add internal/application/pricing_test.go
git commit -m "Test that underpricing raises the realized loss ratio"
```

---

### Task 4: Documentation

**Files:**
- Modify: `README.md`
- Modify: `docs/stage-coupling-findings.md`

**Interfaces:**
- Consumes: the shipped behaviour from Tasks 1-3.

- [ ] **Step 1: Update the README premium description**

In `README.md`, in the "How the simulation works" section, replace the premium sentence in the **Policy book** bullet (currently "Premium is priced to a target loss ratio - each policy's premium is its expected ultimate loss divided by `target_loss_ratio` - so the accident-year loss ratio stays flat as severities inflate.") with:

```
Premium is priced from a separate pricing basis - the insurer's assumed loss cost - divided by `target_loss_ratio`, independent of the claims model that generates experience. When the pricing assumptions match the true claims parameters (the shipped default) the book is priced perfectly and the realized loss ratio lands on target; deviating them models underpricing or adverse experience, and the realized loss ratio moves on its own.
```

- [ ] **Step 2: Update the README simplifications list**

In `README.md`, in "Assumptions and known simplifications", change the "insurer prices risk perfectly" bullet to note it is now the default rather than a hard limitation:

```
- **The insurer prices risk perfectly by default, but this is configurable.** The shipped preset's `pricing` assumptions equal the true claims parameters, so premium tracks expected loss exactly and loss ratios are stable across cohorts. Set the `pricing` block away from the claims values to model underpricing, overpricing, or adverse experience; the realized loss ratio then differs from `target_loss_ratio`.
```

If the README documents the parameter layout, add a line noting the new top-level `pricing` block alongside `book`, `claims`, `runoff`.

- [ ] **Step 3: Mark F1 addressed in the findings doc**

In `docs/stage-coupling-findings.md`, add a note to the F1 finding (and the F2 mention) that it is addressed by the independent pricing basis: append to the F1 section:

```
**Status (2026-07-27):** Addressed. Premium is now priced from a policy-owned `PricingParams` block (see `docs/superpowers/specs/2026-07-27-independent-pricing-basis-design.md`); the `policy` package no longer imports `ClaimParams`, and the duplicated inflation-mean path (the F2 aspect) is now the intentional pricing-vs-truth separation.
```

Also update the summary table row for F1 to note it is done.

- [ ] **Step 4: Commit**

```bash
git add README.md docs/stage-coupling-findings.md
git commit -m "Document the independent pricing basis"
```

---

## Self-Review

**Spec coverage:** Every spec section maps to a task. Domain model + book + generate (spec 1,2) -> Task 1 Steps 1-5. Config/preset/migration (spec 3) -> Task 1 Steps 6-7. Validation (spec 4) -> Task 1 Step 3. Behaviour/realism/reproducibility (spec 5) -> Task 1 Step 11 (golden + realism unchanged). Testing (spec 6) -> Task 1 Steps 8-10 (moved/guard tests) and Task 3 (feature test). Docs (spec 7) -> Task 4. UI fan-out (spec 3) -> Task 2.

**Placeholder scan:** No TBD/TODO; every code step carries full code. The only deliberately open instruction is Task 1 Step 10 "read the rest of config_test.go and update any residual reference", which is a real verification step, not a placeholder.

**Type consistency:** `PricingParams` fields, the `ExpectedPolicyLoss(sumInsured, excess, riskFactor, inflationFactor, siDrift)` signature, `SeverityParams.validate(prefix string)`, and `NewBookSimulator(book, pricing)` are used identically across the domain change, the config DTO, the book simulator, the UI paths, and the tests. The preset guard test and the feature test both read the same `Pricing.*` field names defined in Task 1.
