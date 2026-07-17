# Claims inflation and nil claims implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add stochastic occurrence-year claims inflation and nil claims (closed without payment) to the claimsgen engine, motor personal first, keeping every invariant and the Schedule P realism gate.

**Architecture:** Inflation is a simulated per-calendar-year index in the `claim` domain package, built from its own `src.Split("inflation")` sub-stream and applied to each claim's ground-up loss by occurrence year. Nil claims are a per-claim Bernoulli draw flagged on `claim.Claim` and handled by a dedicated no-payment path in the `transaction` runoff simulator. New parameters flow through the `lob` domain object, the config DTOs/YAML, and the web form. The engine stays byte-identical until a final calibration task flips the preset from identity values to the real defaults and re-passes the realism gate.

**Tech Stack:** Go stdlib plus the existing `gonum`-backed random source; no new dependencies.

**Spec:** `docs/superpowers/specs/2026-07-16-claims-inflation-and-nil-claims-design.md`

## Global constraints

- No new Go module dependencies.
- No change to the CSV output schema: the `Nil` flag is in-memory only, never written to `claims.csv`.
- Inflation applies to the whole ground-up loss (own damage and third party), scaled by the claim's occurrence-year index.
- Inflation is stochastic: one simulated annual factor per calendar year from `src.Split("inflation")`; the user-facing knob is the mean, the volatility is a per-LoB YAML parameter not shown in the UI form.
- `nil_probability` in `[0, 1)`; `0` switches nil claims off, stated in both the YAML comment and the UI tooltip; when `0`, no Bernoulli draw is made so the off state is a true no-op on the random stream.
- Same seed + same config = byte-identical output (existing determinism guarantee).
- Reproducibility isolation: adding a consumer must not reshuffle existing sub-streams. The inflation sub-stream is independent; the nil draw lives inside each claim's existing per-claim sub-stream and is only drawn when `nil_probability > 0`.
- `go test ./...` and `go vet ./...` must be clean before every commit.
- Commit style: imperative subject, ending with `Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>`.

**Green-keeping strategy:** Tasks 1-6 leave the shipped motor preset at *identity* inflation/nil values (`inflation.mean: 1.0`, `inflation.volatility: 0.0`, `nil_probability: 0.0`), so wiring the features in changes no output and the realism gate stays green. Task 7 flips the preset to the real defaults, recalibrates, and updates the invariant sweep. Task 8 is the UI.

---

### Task 1: Parameters, validation, config DTOs, and preset (identity values)

Add the new parameters to the `lob` domain object with validation, mirror them in the config DTOs, and add them to the motor preset at identity values so no behavior changes yet.

**Files:**
- Modify: `internal/domain/lob/lob.go`
- Modify: `internal/domain/lob/lob_test.go`
- Modify: `internal/infrastructure/config/config.go`
- Modify: `internal/infrastructure/config/motor-personal.yaml`
- Test: `internal/domain/lob/lob_test.go`, `internal/infrastructure/config/config_test.go`

**Interfaces:**
- Consumes: existing `lob.ClaimParams`, `config.ClaimsParams`, `config.LOBParams.ToDomain`.
- Produces:
  - `lob.InflationParams{ Mean, Volatility float64 }`
  - `lob.ClaimParams` gains `Inflation InflationParams` and `NilProbability float64`
  - `config.InflationParams{ Mean, Volatility float64 }` with yaml+json tags
  - `config.ClaimsParams` gains `Inflation InflationParams` and `NilProbability float64`

Note on the test file: `internal/domain/lob/lob_test.go` is `package lob` (internal), so it uses the domain types unqualified (`InflationParams`, not `lob.InflationParams`) and already has a helper `validMotor() LineOfBusiness` that returns a valid parameter set. The new tests below use `validMotor()`.

- [ ] **Step 1: Write the failing validation tests**

Append to `internal/domain/lob/lob_test.go`:

```go
func TestValidateRejectsNonPositiveInflationMean(t *testing.T) {
	l := validMotor()
	l.Claims.Inflation.Mean = 0
	if err := l.Validate(); err == nil {
		t.Fatal("inflation mean 0: want error, got nil")
	}
}

func TestValidateRejectsNegativeInflationVolatility(t *testing.T) {
	l := validMotor()
	l.Claims.Inflation.Volatility = -0.1
	if err := l.Validate(); err == nil {
		t.Fatal("negative inflation volatility: want error, got nil")
	}
}

func TestValidateRejectsNilProbabilityOutOfRange(t *testing.T) {
	for _, p := range []float64{-0.01, 1.0, 1.5} {
		l := validMotor()
		l.Claims.NilProbability = p
		if err := l.Validate(); err == nil {
			t.Fatalf("nil_probability %v: want error, got nil", p)
		}
	}
}

func TestValidateAcceptsIdentityInflationAndZeroNil(t *testing.T) {
	l := validMotor()
	l.Claims.Inflation = InflationParams{Mean: 1.0, Volatility: 0.0}
	l.Claims.NilProbability = 0
	if err := l.Validate(); err != nil {
		t.Fatalf("identity inflation and zero nil: want nil, got %v", err)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/domain/lob/`
Expected: FAIL to compile with `l.Claims.Inflation undefined` and `l.Claims.NilProbability undefined`.

Then, so `validMotor()` still returns a valid set once the new validation requires a positive inflation mean, add the identity inflation to its `Claims` block in `internal/domain/lob/lob_test.go` (leave `NilProbability` unset - its zero value is valid):

```go
			CloseLag: CloseLagParams{
				Shape:          1.5,
				MeanDays:       60,
				SizeThreshold:  20000,
				SizeMultiplier: 4,
				RiskLoading:    0.5,
			},
			Inflation: InflationParams{Mean: 1.0, Volatility: 0.0},
		},
```

- [ ] **Step 3: Add the domain params and validation**

In `internal/domain/lob/lob.go`, add `Inflation` and `NilProbability` to `ClaimParams`:

```go
// ClaimParams drives step 2, claim event simulation.
type ClaimParams struct {
	// BaseFrequency is expected reported claims per policy-year (risk
	// factor 1).
	BaseFrequency float64
	// ReportLagMedian is the median occurrence-to-report lag in days.
	ReportLagMedian float64
	// ReportLagSigma is the sigma of the lognormal report lag.
	ReportLagSigma float64
	Severity       SeverityParams
	CloseLag       CloseLagParams
	// Inflation is the stochastic claims-inflation path applied by
	// occurrence year to every claim's ground-up loss.
	Inflation InflationParams
	// NilProbability is the chance a reported claim closes without payment;
	// 0 switches nil claims off.
	NilProbability float64
}

// InflationParams is the stochastic annual claims-inflation path: each
// calendar year's factor is Mean times mean-1 lognormal noise of sigma
// Volatility, compounded from an index of 1.0 in the start year.
type InflationParams struct {
	// Mean is the average annual claims inflation factor (1.0 = flat prices).
	Mean float64
	// Volatility is the sigma of the mean-1 lognormal noise on each year's
	// factor.
	Volatility float64
}
```

Extend `ClaimParams.validate()` (add before its final `return c.CloseLag.validate()` so the existing severity/close-lag checks still run; keep close-lag last):

```go
func (c ClaimParams) validate() error {
	if c.BaseFrequency <= 0 {
		return fmt.Errorf("claims.base_frequency: must be positive, got %v", c.BaseFrequency)
	}
	if c.ReportLagMedian <= 0 {
		return fmt.Errorf("claims.report_lag_median: must be positive, got %v", c.ReportLagMedian)
	}
	if c.ReportLagSigma <= 0 {
		return fmt.Errorf("claims.report_lag_sigma: must be positive, got %v", c.ReportLagSigma)
	}
	if err := c.Severity.validate(); err != nil {
		return err
	}
	if err := c.Inflation.validate(); err != nil {
		return err
	}
	if c.NilProbability < 0 || c.NilProbability >= 1 {
		return fmt.Errorf("claims.nil_probability: must be in [0, 1), got %v", c.NilProbability)
	}
	return c.CloseLag.validate()
}

func (i InflationParams) validate() error {
	if i.Mean <= 0 {
		return fmt.Errorf("claims.inflation.mean: must be positive, got %v", i.Mean)
	}
	if i.Volatility < 0 {
		return fmt.Errorf("claims.inflation.volatility: must not be negative, got %v", i.Volatility)
	}
	return nil
}
```

- [ ] **Step 4: Run the lob tests**

Run: `go test ./internal/domain/lob/`
Expected: PASS.

- [ ] **Step 5: Add the config DTOs and mapping**

In `internal/infrastructure/config/config.go`, add fields to `ClaimsParams` and a new `InflationParams` DTO:

```go
type ClaimsParams struct {
	BaseFrequency   float64         `yaml:"base_frequency" json:"base_frequency"`
	ReportLagMedian float64         `yaml:"report_lag_median" json:"report_lag_median"`
	ReportLagSigma  float64         `yaml:"report_lag_sigma" json:"report_lag_sigma"`
	Severity        SeverityParams  `yaml:"severity" json:"severity"`
	CloseLag        CloseLagParams  `yaml:"close_lag" json:"close_lag"`
	Inflation       InflationParams `yaml:"inflation" json:"inflation"`
	NilProbability  float64         `yaml:"nil_probability" json:"nil_probability"`
}

type InflationParams struct {
	Mean       float64 `yaml:"mean" json:"mean"`
	Volatility float64 `yaml:"volatility" json:"volatility"`
}
```

In `ToDomain`, extend the `Claims` mapping to carry the new fields. Find the `Claims: lob.ClaimParams{...}` block and add the two fields after the existing `CloseLag:` mapping:

```go
			CloseLag: lob.CloseLagParams{
				Shape:          d.Claims.CloseLag.Shape,
				MeanDays:       d.Claims.CloseLag.MeanDays,
				SizeThreshold:  d.Claims.CloseLag.SizeThreshold,
				SizeMultiplier: d.Claims.CloseLag.SizeMultiplier,
				RiskLoading:    d.Claims.CloseLag.RiskLoading,
			},
			Inflation: lob.InflationParams{
				Mean:       d.Claims.Inflation.Mean,
				Volatility: d.Claims.Inflation.Volatility,
			},
			NilProbability: d.Claims.NilProbability,
```

- [ ] **Step 6: Add the keys to the motor preset (identity values)**

In `internal/infrastructure/config/motor-personal.yaml`, under the `claims:` block (after the `close_lag:` block, at the same indentation as `base_frequency`), add:

```yaml
  # Claims inflation applied by occurrence year to every claim's ground-up
  # loss. Stochastic: each year's factor is `mean` times lognormal noise of
  # sigma `volatility`, compounded from 1.0 in the start year. Identity here
  # (mean 1.0, volatility 0.0) until calibration sets the real trend.
  inflation:
    mean: 1.0
    volatility: 0.0
  # Probability a reported claim closes without payment. 0 switches nil
  # claims off.
  nil_probability: 0.0
```

- [ ] **Step 7: Run the full suite**

Run: `go test ./...` and `go vet ./...`
Expected: PASS, clean. (The config round-trip picks up the new keys; nothing consumes them yet, so the realism gate is unchanged.)

- [ ] **Step 8: Commit**

```bash
git add internal/domain/lob/ internal/infrastructure/config/
git commit -m "Add claims inflation and nil probability parameters

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 2: Shared mean-one lognormal helper and the inflation index

Move the existing `meanOneLogNormal` helper into `shared` (it is now needed by two packages) and add the `InflationIndex` value object that simulates the compounding annual path.

**Files:**
- Create: `internal/domain/shared/distribution.go`
- Modify: `internal/domain/transaction/runoff.go`
- Create: `internal/domain/claim/inflation.go`
- Test: `internal/domain/shared/distribution_test.go`, `internal/domain/claim/inflation_test.go`

**Interfaces:**
- Consumes: `shared.RandomSource`, `lob.InflationParams`.
- Produces:
  - `func shared.MeanOneLogNormal(src RandomSource, sigma float64) float64`
  - `type claim.InflationIndex` with `func claim.NewInflationIndex(src shared.RandomSource, p lob.InflationParams, startYear, years int) InflationIndex` and `func (x InflationIndex) For(year int) float64`
  - The zero-value `InflationIndex{}` returns `1.0` from `For` for any year (identity), so callers that never set it are unaffected.

- [ ] **Step 1: Write the failing shared helper test**

Create `internal/domain/shared/distribution_test.go`:

```go
package shared_test

import (
	"math"
	"testing"

	"github.com/le-marais/claimsgen/internal/domain/shared"
	"github.com/le-marais/claimsgen/internal/infrastructure/random"
)

func TestMeanOneLogNormalZeroSigmaIsOne(t *testing.T) {
	if got := shared.MeanOneLogNormal(random.NewSource(1), 0); got != 1 {
		t.Fatalf("sigma 0: got %v, want 1", got)
	}
}

func TestMeanOneLogNormalMeanIsApproximatelyOne(t *testing.T) {
	src := random.NewSource(7)
	sum := 0.0
	const n = 20000
	for i := 0; i < n; i++ {
		sum += shared.MeanOneLogNormal(src, 0.3)
	}
	if mean := sum / n; math.Abs(mean-1) > 0.02 {
		t.Fatalf("empirical mean %v, want approximately 1", mean)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/domain/shared/`
Expected: FAIL with `undefined: shared.MeanOneLogNormal`.

- [ ] **Step 3: Add the shared helper and refactor transaction to use it**

Create `internal/domain/shared/distribution.go`:

```go
package shared

// MeanOneLogNormal draws from a lognormal distribution with mean exactly 1;
// a sigma of 0 or less degenerates to the constant 1 with no draw.
func MeanOneLogNormal(src RandomSource, sigma float64) float64 {
	if sigma <= 0 {
		return 1
	}
	return src.LogNormal(-sigma*sigma/2, sigma)
}
```

In `internal/domain/transaction/runoff.go`, delete the local `meanOneLogNormal` function (the block at the bottom of the file). It has a single call site, inside `simulateClaim`; change that line to call the shared helper (the `shared` package is already imported):

```go
		target := shared.FromDollars(remaining * shared.MeanOneLogNormal(src, sigma))
```

- [ ] **Step 4: Run the shared and transaction tests**

Run: `go test ./internal/domain/shared/ ./internal/domain/transaction/`
Expected: PASS (the refactor is byte-identical: same math, same RNG calls).

- [ ] **Step 5: Write the failing inflation index test**

Create `internal/domain/claim/inflation_test.go`:

```go
package claim_test

import (
	"math"
	"testing"

	"github.com/le-marais/claimsgen/internal/domain/claim"
	"github.com/le-marais/claimsgen/internal/domain/lob"
	"github.com/le-marais/claimsgen/internal/infrastructure/random"
)

func TestInflationIndexStartYearIsOne(t *testing.T) {
	idx := claim.NewInflationIndex(random.NewSource(1), lob.InflationParams{Mean: 1.05, Volatility: 0.02}, 1998, 5)
	if got := idx.For(1998); got != 1.0 {
		t.Fatalf("start-year index = %v, want 1.0", got)
	}
}

func TestInflationIndexIdentityWhenMeanOneNoVol(t *testing.T) {
	idx := claim.NewInflationIndex(random.NewSource(1), lob.InflationParams{Mean: 1.0, Volatility: 0.0}, 1998, 10)
	for y := 1998; y < 2008; y++ {
		if got := idx.For(y); got != 1.0 {
			t.Fatalf("identity index For(%d) = %v, want 1.0", y, got)
		}
	}
}

func TestInflationIndexCompoundsAtMeanWithoutVol(t *testing.T) {
	idx := claim.NewInflationIndex(random.NewSource(1), lob.InflationParams{Mean: 1.04, Volatility: 0.0}, 2000, 4)
	for i, want := range []float64{1.0, 1.04, 1.04 * 1.04, 1.04 * 1.04 * 1.04} {
		if got := idx.For(2000 + i); math.Abs(got-want) > 1e-9 {
			t.Fatalf("For(%d) = %v, want %v", 2000+i, got, want)
		}
	}
}

func TestInflationIndexZeroValueIsIdentity(t *testing.T) {
	var idx claim.InflationIndex
	if got := idx.For(2003); got != 1.0 {
		t.Fatalf("zero-value index = %v, want 1.0 (identity)", got)
	}
}

func TestInflationIndexClampsOutOfRange(t *testing.T) {
	idx := claim.NewInflationIndex(random.NewSource(1), lob.InflationParams{Mean: 1.04, Volatility: 0.0}, 2000, 3)
	// Years before the window read as the start-year index (1.0); years after
	// read as the last simulated index.
	if got := idx.For(1990); got != 1.0 {
		t.Fatalf("For(before window) = %v, want 1.0", got)
	}
	if got, want := idx.For(2050), idx.For(2002); got != want {
		t.Fatalf("For(after window) = %v, want last index %v", got, want)
	}
}
```

- [ ] **Step 6: Run to verify it fails**

Run: `go test ./internal/domain/claim/`
Expected: FAIL with `undefined: claim.NewInflationIndex` / `claim.InflationIndex`.

- [ ] **Step 7: Implement the inflation index**

Create `internal/domain/claim/inflation.go`:

```go
package claim

import (
	"github.com/le-marais/claimsgen/internal/domain/lob"
	"github.com/le-marais/claimsgen/internal/domain/shared"
)

// InflationIndex maps a claim's occurrence year to a cumulative
// claims-inflation factor. The index is 1.0 in the start year and compounds
// a simulated annual factor for each subsequent year of the run window. The
// zero value is the identity index: For returns 1.0 for every year.
type InflationIndex struct {
	startYear int
	// factors[i] is the cumulative index for startYear+i; factors[0] is 1.0.
	factors []float64
}

// NewInflationIndex simulates the inflation path over the run window. Each
// year past the first multiplies the running index by Mean times mean-1
// lognormal noise of sigma Volatility.
func NewInflationIndex(src shared.RandomSource, p lob.InflationParams, startYear, years int) InflationIndex {
	if years < 1 {
		return InflationIndex{}
	}
	factors := make([]float64, years)
	factors[0] = 1.0
	for i := 1; i < years; i++ {
		annual := p.Mean * shared.MeanOneLogNormal(src, p.Volatility)
		factors[i] = factors[i-1] * annual
	}
	return InflationIndex{startYear: startYear, factors: factors}
}

// For returns the cumulative inflation factor for an occurrence year. Years
// before the window clamp to the start-year index (1.0); years after clamp
// to the last simulated index. The zero-value index returns 1.0 everywhere.
func (x InflationIndex) For(year int) float64 {
	if len(x.factors) == 0 {
		return 1.0
	}
	i := year - x.startYear
	if i < 0 {
		i = 0
	}
	if i >= len(x.factors) {
		i = len(x.factors) - 1
	}
	return x.factors[i]
}
```

- [ ] **Step 8: Run the full suite**

Run: `go test ./...` and `go vet ./...`
Expected: PASS, clean.

- [ ] **Step 9: Commit**

```bash
git add internal/domain/shared/ internal/domain/transaction/runoff.go internal/domain/claim/inflation.go internal/domain/claim/inflation_test.go
git commit -m "Add shared mean-one lognormal and the claims inflation index

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 3: Apply inflation in the claim simulator and wire it in GenerateDataset

Give the claim simulator an optional inflation index (identity by default, so existing tests are untouched) and scale each claim's ground-up loss by its occurrence-year factor. Build and pass the index in `GenerateDataset`.

**Files:**
- Modify: `internal/domain/claim/claim.go`
- Modify: `internal/application/generate.go`
- Test: `internal/domain/claim/claim_test.go`

**Interfaces:**
- Consumes: `claim.InflationIndex`, `claim.NewInflationIndex` (Task 2), `lob.ClaimParams.Inflation`.
- Produces:
  - `func (s *ClaimSimulator) WithInflation(x InflationIndex) *ClaimSimulator` (returns the same simulator for chaining)
  - `ClaimSimulator.Simulate` signature is unchanged, so existing callers keep working with the identity index.

- [ ] **Step 1: Write the failing test**

Append to `internal/domain/claim/claim_test.go` (it already imports `random`, `lob`, `policy`; the helpers `params()` and `fixedBook(sumInsured, ..., excess, risk)` exist - check their exact signatures at the top of the file and match them):

```go
func TestInflationScalesGroundUpLoss(t *testing.T) {
	// A homogeneous book with no report/close randomness knobs still yields a
	// higher total initial estimate once inflation is applied, because every
	// claim's ground-up loss is multiplied by its occurrence-year factor.
	book := fixedBook(30000, 20000, 0, 1.0)

	base := claim.NewClaimSimulator(params()).
		Simulate(random.NewSource(11), book)

	inflated := claim.NewClaimSimulator(params()).
		WithInflation(claim.NewInflationIndex(random.NewSource(11), lob.InflationParams{Mean: 2.0, Volatility: 0.0}, book[0].CoverStart.Year(), 3)).
		Simulate(random.NewSource(11), book)

	if len(base) == 0 || len(inflated) == 0 {
		t.Fatal("expected claims in both runs")
	}
	var baseTotal, inflatedTotal int64
	for _, c := range base {
		baseTotal += int64(c.InitialEstimate)
	}
	for _, c := range inflated {
		inflatedTotal += int64(c.InitialEstimate)
	}
	if inflatedTotal <= baseTotal {
		t.Fatalf("inflated total estimate %d not greater than base %d", inflatedTotal, baseTotal)
	}
}

func TestNoInflationMatchesIdentity(t *testing.T) {
	book := fixedBook(30000, 20000, 0, 1.0)
	withoutCall := claim.NewClaimSimulator(params()).Simulate(random.NewSource(12), book)
	withIdentity := claim.NewClaimSimulator(params()).
		WithInflation(claim.NewInflationIndex(random.NewSource(99), lob.InflationParams{Mean: 1.0, Volatility: 0.0}, book[0].CoverStart.Year(), 3)).
		Simulate(random.NewSource(12), book)
	if len(withoutCall) != len(withIdentity) {
		t.Fatalf("identity inflation changed claim count: %d vs %d", len(withoutCall), len(withIdentity))
	}
	for i := range withoutCall {
		if withoutCall[i] != withIdentity[i] {
			t.Fatalf("identity inflation changed claim %d", i)
		}
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/domain/claim/`
Expected: FAIL with `sim.WithInflation undefined`.

- [ ] **Step 3: Add the inflation field, setter, and scaling**

In `internal/domain/claim/claim.go`, add the field and setter and apply the scaling. Update the struct and constructor area:

```go
// ClaimSimulator generates claim events for a policy book.
type ClaimSimulator struct {
	params    lob.ClaimParams
	inflation InflationIndex
}

func NewClaimSimulator(p lob.ClaimParams) *ClaimSimulator {
	return &ClaimSimulator{params: p}
}

// WithInflation sets the occurrence-year inflation index. The zero-value
// index (the default) is the identity, so a simulator built without this
// call applies no inflation.
func (s *ClaimSimulator) WithInflation(x InflationIndex) *ClaimSimulator {
	s.inflation = x
	return s
}
```

In `simulateClaim`, scale the ground-up loss by the occurrence-year factor before subtracting the excess:

```go
	loss := s.drawGroundUpLoss(src, pol)
	loss *= s.inflation.For(occurrence.Year())
	estimate := loss - pol.Excess.Dollars()
	if estimate <= 0 {
		return Claim{}, false
	}
```

- [ ] **Step 4: Run the claim tests**

Run: `go test ./internal/domain/claim/`
Expected: PASS.

- [ ] **Step 5: Wire the index into GenerateDataset**

In `internal/application/generate.go`, build the index from a dedicated sub-stream and pass it to the claim simulator:

```go
	book := policy.NewBookSimulator(req.LOB.Book).
		Simulate(src.Split("book"), req.StartYear, req.Years, req.InitialBookSize)
	inflation := claim.NewInflationIndex(src.Split("inflation"), req.LOB.Claims.Inflation, req.StartYear, req.Years)
	claims := claim.NewClaimSimulator(req.LOB.Claims).
		WithInflation(inflation).
		Simulate(src.Split("claims"), book)
	txs := transaction.NewRunoffSimulator(req.LOB.Runoff).
		Simulate(src.Split("runoff"), claims)
```

- [ ] **Step 6: Run the full suite**

Run: `go test ./...` and `go vet ./...`
Expected: PASS, clean. The preset is still at `inflation.mean: 1.0, volatility: 0.0`, so the index is all `1.0` and every output is byte-identical - the realism and determinism gates stay green.

- [ ] **Step 7: Commit**

```bash
git add internal/domain/claim/claim.go internal/domain/claim/claim_test.go internal/application/generate.go
git commit -m "Apply occurrence-year claims inflation in the claim simulator

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 4: Flag nil claims in the claim simulator

Add the `Nil` field to `Claim` and draw it per claim, guarded so a probability of 0 makes no draw.

**Files:**
- Modify: `internal/domain/claim/claim.go`
- Test: `internal/domain/claim/claim_test.go`

**Interfaces:**
- Consumes: `lob.ClaimParams.NilProbability`, `shared.RandomSource.Bernoulli`.
- Produces: `claim.Claim` gains `Nil bool`.

- [ ] **Step 1: Write the failing test**

Append to `internal/domain/claim/claim_test.go`:

```go
func TestNilProbabilityZeroFlagsNoClaims(t *testing.T) {
	p := params()
	p.NilProbability = 0
	claims := claim.NewClaimSimulator(p).Simulate(random.NewSource(21), fixedBook(30000, 20000, 0, 1.0))
	if len(claims) == 0 {
		t.Fatal("expected claims")
	}
	for _, c := range claims {
		if c.Nil {
			t.Fatalf("claim %d flagged nil with probability 0", c.ID)
		}
	}
}

func TestNilProbabilityHighFlagsMostClaims(t *testing.T) {
	p := params()
	p.NilProbability = 0.9
	claims := claim.NewClaimSimulator(p).Simulate(random.NewSource(22), fixedBook(30000, 20000, 0, 1.0))
	if len(claims) < 20 {
		t.Fatalf("expected a meaningful number of claims, got %d", len(claims))
	}
	nils := 0
	for _, c := range claims {
		if c.Nil {
			nils++
		}
	}
	if frac := float64(nils) / float64(len(claims)); frac < 0.7 {
		t.Fatalf("nil fraction %v, want most claims nil at probability 0.9", frac)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/domain/claim/`
Expected: FAIL with `c.Nil undefined`.

- [ ] **Step 3: Add the field and the guarded draw**

In `internal/domain/claim/claim.go`, add the field to `Claim`:

```go
type Claim struct {
	ID              int
	PolicyID        int
	OccurrenceDate  shared.Date
	ReportDate      shared.Date
	CloseDate       shared.Date
	InitialEstimate shared.Money
	// RiskFactor is carried from the policy for downstream stages.
	RiskFactor float64
	// Nil is true when the claim closes without any payment. It is carried
	// to the runoff stage but never written to CSV.
	Nil bool
}
```

In `simulateClaim`, draw the nil flag (guarded) and set it on the returned claim. Add the draw after the close date is computed:

```go
	close := report.AddDays(int(math.Round(s.drawCloseLag(src, estimate, pol.RiskFactor))))

	isNil := s.params.NilProbability > 0 && src.Bernoulli(s.params.NilProbability)

	return Claim{
		PolicyID:        pol.ID,
		OccurrenceDate:  occurrence,
		ReportDate:      report,
		CloseDate:       close,
		InitialEstimate: shared.FromDollars(estimate),
		RiskFactor:      pol.RiskFactor,
		Nil:             isNil,
	}, true
```

- [ ] **Step 4: Run the claim tests**

Run: `go test ./internal/domain/claim/`
Expected: PASS.

- [ ] **Step 5: Run the full suite**

Run: `go test ./...` and `go vet ./...`
Expected: PASS, clean. The preset has `nil_probability: 0.0`, so no draw is made and output is byte-identical.

- [ ] **Step 6: Commit**

```bash
git add internal/domain/claim/claim.go internal/domain/claim/claim_test.go
git commit -m "Flag nil claims in the claim simulator

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 5: Nil-claim runoff path

Give the runoff simulator a dedicated path for nil claims: initial estimate, interim pure revisions around the outstanding reserve, then a single release to zero at close, with no payments ever.

**Files:**
- Modify: `internal/domain/transaction/runoff.go`
- Test: `internal/domain/transaction/runoff_test.go`

**Interfaces:**
- Consumes: `claim.Claim.Nil` (Task 4), existing `emitter`, `drawRevisions`, `shared.MeanOneLogNormal` (Task 2).
- Produces: no new exported surface; `simulateClaim` branches to `simulateNilClaim` when the claim is nil.

- [ ] **Step 1: Write the failing test**

Append to `internal/domain/transaction/runoff_test.go` (it already imports `claim`, `shared`, `random`; match the existing style - the file builds `claim.Claim` literals directly, e.g. around line 160):

```go
func TestNilClaimHasNoPaymentsAndClosesToZero(t *testing.T) {
	c := claim.Claim{
		ID:              1,
		PolicyID:        1,
		OccurrenceDate:  shared.NewDate(2000, time.January, 1),
		ReportDate:      shared.NewDate(2000, time.January, 10),
		CloseDate:       shared.NewDate(2001, time.June, 1),
		InitialEstimate: shared.FromDollars(5000),
		RiskFactor:      1.0,
		Nil:             true,
	}
	sim := transaction.NewRunoffSimulator(params())
	txs := sim.Simulate(random.NewSource(1), []claim.Claim{c})

	if len(txs) == 0 {
		t.Fatal("expected transactions for the nil claim")
	}
	outstanding := shared.Money(0)
	paid := shared.Money(0)
	for _, tx := range txs {
		switch tx.Type {
		case transaction.Payment:
			paid += tx.Amount
		case transaction.Estimate:
			outstanding += tx.Amount
		}
	}
	if paid != 0 {
		t.Fatalf("nil claim paid %v, want 0", paid)
	}
	if outstanding != 0 {
		t.Fatalf("nil claim outstanding at close %v, want 0", outstanding)
	}
	first := txs[0]
	if first.Type != transaction.Estimate || first.Amount != c.InitialEstimate || first.Date != c.ReportDate {
		t.Fatalf("first row %+v is not the initial estimate on the report date", first)
	}
	if last := txs[len(txs)-1]; last.Date != c.CloseDate {
		t.Fatalf("last row on %s, want close date %s", last.Date, c.CloseDate)
	}
}
```

The test uses the existing `params()` helper in `runoff_test.go` (which returns a valid `lob.RunoffParams`) and the existing imports - `time`, `claim`, `shared`, `random`, and `transaction` are all already imported at the top of the file, so no import changes are needed.

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/domain/transaction/`
Expected: FAIL - the current runoff always pays at close, so `paid != 0`.

- [ ] **Step 3: Add the nil branch**

In `internal/domain/transaction/runoff.go`, branch at the top of `simulateClaim`:

```go
func (s *RunoffSimulator) simulateClaim(src shared.RandomSource, c claim.Claim) []Transaction {
	if c.Nil {
		return s.simulateNilClaim(src, c)
	}
	duration := shared.DaysBetween(c.ReportDate, c.CloseDate)
	years := float64(duration) / 365
	// ... existing body unchanged ...
```

Add the nil path (place it just after `simulateClaim`):

```go
// simulateNilClaim runs off a claim that closes without payment: the initial
// case estimate, interim pure revisions as noise around the outstanding
// reserve, then a single release to zero at close. No payments are emitted.
func (s *RunoffSimulator) simulateNilClaim(src shared.RandomSource, c claim.Claim) []Transaction {
	duration := shared.DaysBetween(c.ReportDate, c.CloseDate)
	years := float64(duration) / 365

	revisions := s.drawRevisions(src, duration, years)
	sort.SliceStable(revisions, func(i, j int) bool {
		return revisions[i].offset < revisions[j].offset
	})

	emitter := &emitter{claimID: c.ID, report: c.ReportDate}
	emitter.estimate(0, c.InitialEstimate)

	for _, e := range revisions {
		remaining := emitter.outstanding.Dollars()
		sigma := s.params.RevisionSigma * (1 - float64(e.offset)/float64(duration))
		target := shared.FromDollars(remaining * shared.MeanOneLogNormal(src, sigma))
		emitter.reviseTo(e.offset, target)
	}

	emitter.reviseTo(duration, 0)
	return emitter.txs
}
```

- [ ] **Step 4: Run the transaction tests**

Run: `go test ./internal/domain/transaction/`
Expected: PASS.

- [ ] **Step 5: Run the full suite**

Run: `go test ./...` and `go vet ./...`
Expected: PASS, clean. Preset `nil_probability` is still 0, so no nil claims reach the runoff in end-to-end runs; the path is covered by the unit test above.

- [ ] **Step 6: Commit**

```bash
git add internal/domain/transaction/runoff.go internal/domain/transaction/runoff_test.go
git commit -m "Add no-payment runoff path for nil claims

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 6: Nil-claim count in the summary view model

Add a per-year nil-claim count to `Summarize` for the UI summary tab.

**Files:**
- Modify: `internal/application/summary.go`
- Test: `internal/application/summary_test.go`

**Interfaces:**
- Consumes: `claim.Claim.Nil`.
- Produces: `application.YearSummary` gains `NilClaims int`.

- [ ] **Step 1: Write the failing test**

Append to `internal/application/summary_test.go`. It uses the `tinyDataset()` helper from the existing tests; add a second helper dataset with a nil claim (a nil claim has no payment transactions):

```go
func TestSummarizeCountsNilClaims(t *testing.T) {
	ds := application.Dataset{
		Policies: []policy.Policy{
			{ID: 1, CoverStart: shared.NewDate(1998, time.January, 1), CoverEnd: shared.NewDate(1998, time.December, 31), Premium: shared.FromDollars(365)},
		},
		Claims: []claim.Claim{
			{ID: 1, PolicyID: 1, OccurrenceDate: shared.NewDate(1998, time.March, 1), ReportDate: shared.NewDate(1998, time.March, 11), CloseDate: shared.NewDate(1998, time.June, 1)},
			{ID: 2, PolicyID: 1, OccurrenceDate: shared.NewDate(1998, time.April, 1), ReportDate: shared.NewDate(1998, time.April, 11), CloseDate: shared.NewDate(1998, time.July, 1), Nil: true},
		},
		Transactions: []transaction.Transaction{
			{ID: 1, ClaimID: 1, Date: shared.NewDate(1998, time.March, 11), Type: transaction.Estimate, Amount: shared.FromDollars(1000)},
			{ID: 2, ClaimID: 1, Date: shared.NewDate(1998, time.June, 1), Type: transaction.Payment, Amount: shared.FromDollars(1000)},
			{ID: 3, ClaimID: 2, Date: shared.NewDate(1998, time.April, 11), Type: transaction.Estimate, Amount: shared.FromDollars(800)},
			{ID: 4, ClaimID: 2, Date: shared.NewDate(1998, time.July, 1), Type: transaction.Estimate, Amount: shared.FromDollars(-800)},
		},
	}
	got := application.Summarize(ds, 1998, 1)
	if got.Years[0].Claims != 2 {
		t.Fatalf("claims = %d, want 2", got.Years[0].Claims)
	}
	if got.Years[0].NilClaims != 1 {
		t.Fatalf("nil claims = %d, want 1", got.Years[0].NilClaims)
	}
	if got.Total.NilClaims != 1 {
		t.Fatalf("total nil claims = %d, want 1", got.Total.NilClaims)
	}
}
```

Make sure the test file imports `claim` (`github.com/le-marais/claimsgen/internal/domain/claim`); it already imports `policy`, `shared`, `transaction`, `time`.

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/application/`
Expected: FAIL with `got.Years[0].NilClaims undefined`.

- [ ] **Step 3: Add the field and the count**

In `internal/application/summary.go`, add `NilClaims` to `YearSummary`:

```go
type YearSummary struct {
	Year          int
	Policies      int
	Claims        int
	NilClaims     int
	EarnedPremium float64
	Paid          float64
}
```

In `Summarize`, count nils by occurrence year in the existing claims loop, and add them into the total:

```go
	occurrenceYear := make(map[int]int, len(ds.Claims))
	for _, c := range ds.Claims {
		occurrenceYear[c.ID] = c.OccurrenceDate.Year()
		if i := c.OccurrenceDate.Year() - startYear; i >= 0 && i < years {
			rows[i].Claims++
			if c.Nil {
				rows[i].NilClaims++
			}
		}
	}
```

and in the totals loop:

```go
	var total YearSummary
	for _, r := range rows {
		total.Policies += r.Policies
		total.Claims += r.Claims
		total.NilClaims += r.NilClaims
		total.EarnedPremium += r.EarnedPremium
		total.Paid += r.Paid
	}
```

- [ ] **Step 4: Run the application tests**

Run: `go test ./internal/application/`
Expected: PASS. (The existing `TestSummarize` omits `NilClaims` in its expected literals, which is the zero value, so it still matches.)

- [ ] **Step 5: Run the full suite**

Run: `go test ./...` and `go vet ./...`
Expected: PASS, clean.

- [ ] **Step 6: Commit**

```bash
git add internal/application/summary.go internal/application/summary_test.go
git commit -m "Count nil claims per year in the summary view model

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 7: Calibrate the preset to the real defaults

Flip the motor preset from identity values to the real inflation and nil defaults, recalibrate the severity/premium so the Schedule P realism gate passes, and extend the invariant sweep to allow nil claims.

**Files:**
- Modify: `internal/infrastructure/config/motor-personal.yaml`
- Modify: `internal/application/invariants_test.go`
- Test: `internal/application/realism_test.go` (the gate, unchanged), `internal/application/invariants_test.go`

**Interfaces:**
- Consumes: everything above.
- Produces: a shipped preset with realistic inflation and nil rates that still passes `TestDefaultPresetIsRealistic`.

- [ ] **Step 1: Update the invariant sweep to allow nil claims**

In `internal/application/invariants_test.go`, the final per-claim loop asserts `s.paid <= 0` is a failure for every claim. Nil claims pay nothing, so branch on the claim's `Nil` flag. Replace the paid assertion block:

```go
	for _, c := range ds.Claims {
		s := perClaim[c.ID]
		if s == nil {
			t.Fatalf("claim %d has no transactions", c.ID)
		}
		if s.first.Type != transaction.Estimate || s.first.Amount != c.InitialEstimate || s.first.Date != c.ReportDate {
			t.Fatalf("claim %d first transaction %+v is not the initial estimate on the report date", c.ID, s.first)
		}
		if s.outstanding != 0 {
			t.Fatalf("claim %d outstanding at close = %v, want 0", c.ID, s.outstanding)
		}
		if c.Nil {
			if s.paid != 0 {
				t.Fatalf("nil claim %d total paid %v, want 0", c.ID, s.paid)
			}
		} else if s.paid <= 0 {
			t.Fatalf("claim %d total paid %v not positive", c.ID, s.paid)
		}
		if s.last.Date != c.CloseDate {
			t.Fatalf("claim %d last transaction on %s, want close date %s", c.ID, s.last.Date, c.CloseDate)
		}
	}
```

- [ ] **Step 2: Set the real preset values**

In `internal/infrastructure/config/motor-personal.yaml`, set the inflation and nil defaults to their target values, and update the comment to reflect that these are now the real trend:

```yaml
  # Claims inflation applied by occurrence year to every claim's ground-up
  # loss. Stochastic: each year's factor is `mean` times lognormal noise of
  # sigma `volatility`, compounded from 1.0 in the start year.
  inflation:
    mean: 1.04
    volatility: 0.015
  # Probability a reported claim closes without payment. 0 switches nil
  # claims off.
  nil_probability: 0.08
```

- [ ] **Step 3: Run the realism gate to see the miss**

Run: `go test ./internal/application/ -run TestDefaultPresetIsRealistic -v`
Expected: likely FAIL - inflation compounds on top of sum insured drift, pushing the ultimate loss ratio (and possibly the incurred level) outside the reference band. The failure message prints each metric with its band, which drives the next step. (If it happens to pass on the first try, skip to Step 5.)

- [ ] **Step 4: Recalibrate to pass the gate**

Adjust the preset to bring the failing metrics back inside their bands, re-running the gate after each change. Inflation applies uniformly across a claim's life, so it does not change the paid/incurred age-to-age factors within an origin year; it moves the overall loss-ratio level, and nil claims soften late incurred development. The two effective levers for the loss ratio are:

- `claims.severity.own_damage_median_fraction` (currently `0.12`): lowering it reduces incurred and the loss ratio.
- `book.premium_rate_factor` (currently `0.035`): raising it increases premium and lowers the loss ratio.

Nil claims (8%) pull the loss ratio down, partly offsetting inflation, so the net adjustment is smaller than the raw ~1.15x mid-decade inflation factor. Change one lever at a time in small steps (e.g. `own_damage_median_fraction` toward `0.10-0.11`, or `premium_rate_factor` toward `0.038-0.042`), re-running:

Run: `go test ./internal/application/ -run TestDefaultPresetIsRealistic -v`

after each change until it passes. Keep changes minimal - adjust only these two values; do not touch the age-to-age-driving parameters (report/close lags, runoff) since inflation does not affect those bands.

- [ ] **Step 5: Run the full application suite**

Run: `go test ./internal/application/ -v`
Expected: PASS - `TestDefaultPresetIsRealistic`, `TestDatasetInvariants` (now with nil claims present and allowed), and the summary/histogram tests all green.

- [ ] **Step 6: Run the full suite**

Run: `go test ./...` and `go vet ./...`
Expected: PASS, clean.

- [ ] **Step 7: Commit**

```bash
git add internal/infrastructure/config/motor-personal.yaml internal/application/invariants_test.go
git commit -m "Calibrate motor preset for claims inflation and nil claims

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 8: Expose the new parameters and nil count in the web UI

Add the two new form fields (claims inflation, nil claim probability) and a Nil claims column to the summary tab.

**Files:**
- Modify: `internal/infrastructure/web/viewmodel.go`
- Modify: `internal/infrastructure/web/static/app.js`
- Test: `internal/infrastructure/web/server_test.go`

**Interfaces:**
- Consumes: `application.YearSummary.NilClaims` (Task 6), the `config.ClaimsParams` inflation/nil JSON fields (Task 1).
- Produces: `summaryRowJSON` gains `NilClaims int json:"nil_claims"`; the summary API response carries per-year and total nil counts; the form gains two inputs.

- [ ] **Step 1: Write the failing test**

In `internal/infrastructure/web/server_test.go`, extend the generate round-trip to assert the nil count is present in the summary. Add a focused test (reusing the `newTestServer`, `do`, and `generateBody` helpers already in the file):

```go
func TestGenerateResponseIncludesNilCount(t *testing.T) {
	outDir := t.TempDir()
	rec := do(t, newTestServer(t), "POST", "/api/generate", generateBody(t, outDir))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Summary struct {
			Years []struct {
				Claims    int `json:"claims"`
				NilClaims int `json:"nil_claims"`
			} `json:"years"`
			Total struct {
				NilClaims int `json:"nil_claims"`
			} `json:"total"`
		} `json:"summary"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Summary.Years) == 0 {
		t.Fatal("no summary years")
	}
	// The default preset generates nils at ~8%, so the total should be positive
	// and never exceed total claims.
	if resp.Summary.Total.NilClaims <= 0 {
		t.Fatalf("total nil claims = %d, want positive with the default preset", resp.Summary.Total.NilClaims)
	}
}
```

Confirm the test file already imports `encoding/json` and `net/http` (it does, from the existing round-trip tests).

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/infrastructure/web/ -run TestGenerateResponseIncludesNilCount`
Expected: FAIL - `nil_claims` is absent from the JSON, so `Total.NilClaims` is 0.

- [ ] **Step 3: Add the field to the view model**

In `internal/infrastructure/web/viewmodel.go`, add `NilClaims` to `summaryRowJSON` and map it in `summaryRowView`:

```go
type summaryRowJSON struct {
	Year          int      `json:"year"`
	Policies      int      `json:"policies"`
	Claims        int      `json:"claims"`
	NilClaims     int      `json:"nil_claims"`
	EarnedPremium float64  `json:"earned_premium"`
	Paid          float64  `json:"paid"`
	LossRatio     *float64 `json:"loss_ratio"`
}
```

```go
func summaryRowView(s application.YearSummary) summaryRowJSON {
	row := summaryRowJSON{
		Year:          s.Year,
		Policies:      s.Policies,
		Claims:        s.Claims,
		NilClaims:     s.NilClaims,
		EarnedPremium: s.EarnedPremium,
		Paid:          s.Paid,
	}
	if lr, ok := s.LossRatio(); ok {
		row.LossRatio = &lr
	}
	return row
}
```

- [ ] **Step 4: Run the web test**

Run: `go test ./internal/infrastructure/web/`
Expected: PASS.

- [ ] **Step 5: Add the summary column and form fields in the frontend**

In `internal/infrastructure/web/static/app.js`:

Add a "Nil claims" column to the summary table. In `renderSummary`, add the header after "Claims":

```js
  for (const label of ["Year", "Policies", "Claims", "Nil claims", "Earned premium", "Ultimate (paid)", "Loss ratio"]) {
    head.append(th(label));
  }
```

and in `summaryRow`, add the nil cell after the claims cell:

```js
  const cells = [
    fmtInt.format(row.policies),
    fmtInt.format(row.claims),
    fmtInt.format(row.nil_claims),
    fmtMoney.format(row.earned_premium),
    fmtMoney.format(row.paid),
    row.loss_ratio == null ? "n/a" : row.loss_ratio.toFixed(3),
  ];
```

Add the two parameter fields to the Claims group in `FIELD_GROUPS`. Insert these at the end of the Claims group's `fields` array (after the close-lag fields):

```js
      { path: ["claims", "inflation", "mean"], label: "Claims inflation", tip: "Average annual claims inflation factor, applied by occurrence year (1.0 = flat)." },
      { path: ["claims", "nil_probability"], label: "Nil claim probability", tip: "Probability a claim closes without payment; 0 switches nil claims off." },
```

(The inflation volatility is intentionally not a form field - it rides through on the preset default via `structuredClone(preset)` in `collectParams`.)

- [ ] **Step 6: Verify the frontend end to end**

Since the embedded JS changed, rebuild and drive the real endpoints:

```bash
go build ./cmd/claimsgen
./claimsgen ui --port 8934 &
sleep 1
# preset carries the new claims params
curl -s http://127.0.0.1:8934/api/lobs/motor-personal/preset | grep -o '"inflation":[^}]*}'
curl -s http://127.0.0.1:8934/api/lobs/motor-personal/preset | grep -o '"nil_probability":[0-9.]*'
# a generate run reports nil counts in the summary
curl -s -X POST http://127.0.0.1:8934/api/generate \
  -H 'Content-Type: application/json' \
  -d "{\"lob_id\":\"motor-personal\",\"seed\":1,\"start_year\":1998,\"years\":3,\"initial_book_size\":2000,\"out_dir\":\"$(mktemp -d)\",\"params\":$(curl -s http://127.0.0.1:8934/api/lobs/motor-personal/preset)}" \
  | grep -o '"nil_claims":[0-9]*' | head -3
kill %1
```

Expected: the preset shows `"inflation":{"mean":1.04,"volatility":0.015}` and `"nil_probability":0.08`, and the generate response contains `"nil_claims":` entries. If a browser is available, also load `http://127.0.0.1:8934`, generate, and confirm the summary tab shows a Nil claims column and the Claims parameters section shows the two new fields with their tooltips; otherwise note that the visual check is deferred.

- [ ] **Step 7: Run the full suite**

Run: `go test ./...` and `go vet ./...`
Expected: PASS, clean.

- [ ] **Step 8: Commit**

```bash
git add internal/infrastructure/web/
git commit -m "Expose claims inflation and nil claims in the web UI

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```
