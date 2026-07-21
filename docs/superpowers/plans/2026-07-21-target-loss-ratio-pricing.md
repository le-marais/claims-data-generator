# Target loss ratio pricing Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the constant `premium_rate_factor` with a `target_loss_ratio` knob that prices each policy on its expected ultimate loss, so the accident-year loss ratio stays flat, and add a per-year drift guard to the realism gate.

**Architecture:** A deterministic `ExpectedPolicyLoss` method on the claim parameters mirrors the severity model; the book simulator divides that expectation by `target_loss_ratio` to set premium. The realism gate gains a self-consistency check comparing the first-half and second-half aggregate loss ratios.

**Tech Stack:** Go (standard library only), YAML preset, vanilla-JS web UI.

## Global Constraints

- Writing style in docs/comments: sentence case headers; no em dashes (use ` - `).
- No new third-party dependencies; standard library only.
- Pricing must add no random draws - the labelled sub-stream reproducibility contract must be preserved. Verify with the existing determinism test.
- `go vet ./...` clean and `go test ./...` green at the end of every task.

---

### Task 1: Expected-loss closed forms in the lob package

**Files:**
- Create: `internal/domain/lob/expectedloss.go`
- Test: `internal/domain/lob/expectedloss_test.go`

**Interfaces:**
- Produces: `func (c ClaimParams) ExpectedPolicyLoss(sumInsured, excess, riskFactor, inflationFactor float64) float64`
- Produces (unexported, same package): `stopLossLognormal(median, sigma, excess float64) float64`, `stopLossPareto(scale, alpha, excess float64) float64`

- [ ] **Step 1: Write the failing test**

Create `internal/domain/lob/expectedloss_test.go`:

```go
package lob

import (
	"math"
	"testing"
)

// monteCarloStopLoss estimates E[(X-excess)+] by sampling, to pin the closed
// forms to the distributions they model.
func monteCarloStopLoss(draw func() float64, excess float64, n int) float64 {
	sum := 0.0
	for i := 0; i < n; i++ {
		if x := draw() - excess; x > 0 {
			sum += x
		}
	}
	return sum / float64(n)
}

func TestStopLossLognormalMatchesMonteCarlo(t *testing.T) {
	median, sigma, excess := 3000.0, 1.0, 500.0
	mu := math.Log(median)
	// Deterministic LCG so the test never flakes.
	var state uint64 = 88172645463325252
	next := func() float64 {
		state ^= state << 13
		state ^= state >> 7
		state ^= state << 17
		return float64(state>>11) / float64(1<<53)
	}
	normal := func() float64 {
		u1, u2 := next(), next()
		if u1 < 1e-12 {
			u1 = 1e-12
		}
		return math.Sqrt(-2*math.Log(u1)) * math.Cos(2*math.Pi*u2)
	}
	draw := func() float64 { return math.Exp(mu + sigma*normal()) }
	want := monteCarloStopLoss(draw, excess, 2_000_000)
	got := stopLossLognormal(median, sigma, excess)
	if rel := math.Abs(got-want) / want; rel > 0.02 {
		t.Fatalf("stopLossLognormal = %.2f, monte carlo = %.2f (rel %.3f)", got, want, rel)
	}
}

func TestStopLossParetoClosedForm(t *testing.T) {
	scale, alpha := 4000.0, 2.2
	mean := scale * alpha / (alpha - 1)
	// Excess below the Pareto minimum: every loss exceeds it.
	if got := stopLossPareto(scale, alpha, 1000); math.Abs(got-(mean-1000)) > 1e-6 {
		t.Fatalf("excess below scale: got %.4f, want %.4f", got, mean-1000)
	}
	// Excess above the minimum: closed-form tail integral.
	excess := 10000.0
	want := (scale / (alpha - 1)) * math.Pow(scale/excess, alpha-1)
	if got := stopLossPareto(scale, alpha, excess); math.Abs(got-want) > 1e-6 {
		t.Fatalf("excess above scale: got %.4f, want %.4f", got, want)
	}
}

func TestExpectedPolicyLossScalesWithRiskAndInflation(t *testing.T) {
	c := ClaimParams{
		BaseFrequency: 0.12,
		Severity: SeverityParams{
			ThirdPartyWeight:        0.20,
			OwnDamageMedianFraction: 0.12,
			OwnDamageSigma:          1.0,
			ThirdPartyScale:         4000,
			ThirdPartyAlpha:         2.2,
		},
		Reopening: ReopeningParams{Probability: 0.04, EstimateFactor: 0.45},
	}
	base := c.ExpectedPolicyLoss(20000, 300, 1.0, 1.0)
	if base <= 0 {
		t.Fatalf("expected positive loss, got %v", base)
	}
	// Doubling the risk factor doubles the expectation.
	if got := c.ExpectedPolicyLoss(20000, 300, 2.0, 1.0); math.Abs(got-2*base) > 1e-6 {
		t.Fatalf("risk scaling: got %v, want %v", got, 2*base)
	}
	// Higher inflation raises the expectation.
	if got := c.ExpectedPolicyLoss(20000, 300, 1.0, 1.5); got <= base {
		t.Fatalf("inflation scaling: got %v, want > %v", got, base)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/domain/lob/ -run 'StopLoss|ExpectedPolicyLoss' -v`
Expected: compile failure - `undefined: stopLossLognormal`, `undefined: stopLossPareto`, `ExpectedPolicyLoss` not defined.

- [ ] **Step 3: Write the implementation**

Create `internal/domain/lob/expectedloss.go`:

```go
package lob

import "math"

// normCDF is the standard normal cumulative distribution function.
func normCDF(x float64) float64 {
	return 0.5 * math.Erfc(-x/math.Sqrt2)
}

// stopLossLognormal returns E[(X-excess)+] for X lognormal with the given
// median and sigma (ln X ~ Normal(ln median, sigma^2)). This is the standard
// undiscounted stop-loss form with the forward equal to the mean.
func stopLossLognormal(median, sigma, excess float64) float64 {
	mean := median * math.Exp(sigma*sigma/2)
	if excess <= 0 {
		return mean - excess
	}
	d1 := (math.Log(mean/excess) + sigma*sigma/2) / sigma
	d2 := d1 - sigma
	return mean*normCDF(d1) - excess*normCDF(d2)
}

// stopLossPareto returns E[(X-excess)+] for X Pareto with the given scale
// (minimum) and alpha > 1. Below the minimum every loss exceeds the excess;
// above it, the closed-form tail integral applies.
func stopLossPareto(scale, alpha, excess float64) float64 {
	mean := scale * alpha / (alpha - 1)
	if excess <= scale {
		return mean - excess
	}
	return (scale / (alpha - 1)) * math.Pow(scale/excess, alpha-1)
}

// ExpectedPolicyLoss is the deterministic expected ultimate gross incurred loss
// for one policy at the given claims-inflation factor (Inflation.Mean raised to
// the policy's year offset). It mirrors the severity model in the claim package
// so premium can be priced to a target loss ratio. It draws no randomness, so
// pricing never perturbs a sub-stream. Recoveries are excluded (gross basis).
func (c ClaimParams) ExpectedPolicyLoss(sumInsured, excess, riskFactor, inflationFactor float64) float64 {
	s := c.Severity
	odMedian := inflationFactor * sumInsured * s.OwnDamageMedianFraction
	od := stopLossLognormal(odMedian, s.OwnDamageSigma, excess)
	tpScale := inflationFactor * s.ThirdPartyScale
	tp := stopLossPareto(tpScale, s.ThirdPartyAlpha, excess)
	perClaim := s.ThirdPartyWeight*tp + (1-s.ThirdPartyWeight)*od
	reopenUplift := 1 + c.Reopening.Probability*c.Reopening.EstimateFactor
	return c.BaseFrequency * riskFactor * perClaim * reopenUplift
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/domain/lob/ -run 'StopLoss|ExpectedPolicyLoss' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/domain/lob/expectedloss.go internal/domain/lob/expectedloss_test.go
git commit -m "Add expected-loss closed forms for target-loss-ratio pricing"
```

---

### Task 2: Switch pricing to the target loss ratio knob

This is one atomic change: removing `PremiumRateFactor` breaks compilation until every touch point is updated, so all edits land together and the task ends green.

**Files:**
- Modify: `internal/domain/lob/lob.go` (field, comment, validation)
- Modify: `internal/domain/policy/book.go` (constructor + pricing)
- Modify: `internal/application/generate.go:47` (constructor call)
- Modify: `internal/infrastructure/config/config.go` (DTO field + ToDomain)
- Modify: `internal/infrastructure/config/motor-personal.yaml`
- Modify: `internal/infrastructure/web/static/app.js:16` (form field metadata)
- Test/modify: `internal/domain/policy/book_test.go`, `internal/domain/lob/lob_test.go`, `internal/infrastructure/config/config_test.go`

**Interfaces:**
- Consumes: `ClaimParams.ExpectedPolicyLoss` (Task 1)
- Produces: `BookParams.TargetLossRatio float64`; `policy.NewBookSimulator(book lob.BookParams, claims lob.ClaimParams) *BookSimulator`

- [ ] **Step 1: Swap the domain knob and validation in `internal/domain/lob/lob.go`**

Replace the field (lines 34-35):

```go
	// TargetLossRatio prices premium: each policy's premium is its expected
	// ultimate loss (ClaimParams.ExpectedPolicyLoss) divided by this target.
	TargetLossRatio float64
```

In `BookParams.Validate` (the `namedFloat` list near line 217) replace the `book.premium_rate_factor` entry with:

```go
		namedFloat{"book.target_loss_ratio", b.TargetLossRatio},
```

Replace the positivity check (lines 258-259) with:

```go
	if b.TargetLossRatio <= 0 {
		return fmt.Errorf("book.target_loss_ratio: must be positive, got %v", b.TargetLossRatio)
	}
```

- [ ] **Step 2: Reprice in `internal/domain/policy/book.go`**

Change the struct and constructor (lines 26-33):

```go
// BookSimulator generates the policy book for a run.
type BookSimulator struct {
	book   lob.BookParams
	claims lob.ClaimParams
}

// NewBookSimulator builds a book simulator from the book and claim
// parameters; claim parameters drive expected-loss pricing.
func NewBookSimulator(book lob.BookParams, claims lob.ClaimParams) *BookSimulator {
	return &BookSimulator{book: book, claims: claims}
}
```

In `Simulate`, rename `s.params` to `s.book` throughout, and compute the per-year inflation factor, passing it into `simulatePolicy`. The loop body becomes:

```go
	for y := 0; y < years; y++ {
		if y > 0 {
			noise := shared.MeanOneLogNormal(sizeSrc, s.book.SizeVolatility)
			size = int(math.Round(float64(size) * s.book.GrowthFactor * noise))
			if size < 1 {
				size = 1
			}
		}
		year := startYear + y
		medianSI := s.book.SumInsuredMedian * math.Pow(s.book.SumInsuredInflation, float64(y))
		inflation := math.Pow(s.claims.Inflation.Mean, float64(y))
		for i := 0; i < size; i++ {
			book = append(book, s.simulatePolicy(src.Split(fmt.Sprintf("policy-%d", id)), id, year, medianSI, inflation))
			id++
		}
	}
```

Rewrite `simulatePolicy` (lines 62-82). Draw the excess into a variable at the same point in the RNG stream (after the risk factor gamma) so the sub-stream is unchanged, then price off it:

```go
func (s *BookSimulator) simulatePolicy(src shared.RandomSource, id, year int, medianSI, inflation float64) Policy {
	yearStart := shared.NewDate(year, time.January, 1)
	daysInYear := shared.DaysBetween(yearStart, shared.NewDate(year+1, time.January, 1))
	start := yearStart.AddDays(int(src.Uniform() * float64(daysInYear)))

	sumInsured := src.LogNormal(math.Log(medianSI), s.book.Spread)

	// Gamma with mean 1 and standard deviation equal to the spread knob.
	spread2 := s.book.Spread * s.book.Spread
	riskFactor := src.Gamma(1/spread2, spread2)

	excess := s.drawExcess(src)
	premium := s.claims.ExpectedPolicyLoss(sumInsured, excess, riskFactor, inflation) / s.book.TargetLossRatio

	return Policy{
		ID:         id,
		CoverStart: start,
		CoverEnd:   start.AddDays(364),
		SumInsured: shared.FromDollars(sumInsured),
		Excess:     shared.FromDollars(excess),
		RiskFactor: riskFactor,
		Premium:    shared.FromDollars(premium),
	}
}
```

Also rename `s.params.ExcessChoices` to `s.book.ExcessChoices` in `drawExcess`.

- [ ] **Step 3: Update the constructor call in `internal/application/generate.go`**

Change lines 47-48 to:

```go
	book := policy.NewBookSimulator(req.LOB.Book, req.LOB.Claims).
		Simulate(src.Split("book"), req.StartYear, req.Years, req.InitialBookSize)
```

- [ ] **Step 4: Update the config DTO and mapping in `internal/infrastructure/config/config.go`**

Replace the DTO field (line 40):

```go
	TargetLossRatio     float64              `yaml:"target_loss_ratio" json:"target_loss_ratio"`
```

Replace the `ToDomain` mapping (line 214):

```go
			TargetLossRatio:     d.Book.TargetLossRatio,
```

- [ ] **Step 5: Update the preset `internal/infrastructure/config/motor-personal.yaml`**

Replace line 19 (`premium_rate_factor: 0.035`) with (calibrated in Step 9):

```yaml
  # Premium is priced to this target loss ratio: each policy's premium is its
  # expected ultimate loss divided by the target. Keeps the accident-year loss
  # ratio flat as severities inflate.
  target_loss_ratio: 0.72
```

- [ ] **Step 6: Update the web form metadata `internal/infrastructure/web/static/app.js`**

Replace line 16:

```js
      { path: ["book", "target_loss_ratio"], label: "Target loss ratio", tip: "Premium = expected ultimate loss / target loss ratio." },
```

- [ ] **Step 7: Update the affected tests**

In `internal/domain/policy/book_test.go`:

Replace the `params()` return (line 27 area) so the fixture uses the new knob, and add a claim-params fixture:

```go
func params() lob.BookParams {
	return lob.BookParams{
		GrowthFactor:        1.05,
		SizeVolatility:      0.06,
		Spread:              0.4,
		SumInsuredMedian:    20000,
		SumInsuredInflation: 1.03,
		ExcessChoices: []lob.ExcessChoice{
			{Value: 0, Weight: 0.1},
			{Value: 100, Weight: 0.2},
			{Value: 300, Weight: 0.3},
			{Value: 500, Weight: 0.3},
			{Value: 1000, Weight: 0.1},
		},
		TargetLossRatio: 0.72,
	}
}

func claimParams() lob.ClaimParams {
	return lob.ClaimParams{
		BaseFrequency: 0.12,
		Severity: lob.SeverityParams{
			ThirdPartyWeight:        0.20,
			OwnDamageMedianFraction: 0.12,
			OwnDamageSigma:          1.0,
			ThirdPartyScale:         4000,
			ThirdPartyAlpha:         2.2,
		},
		Inflation: lob.InflationParams{Mean: 1.04},
		Reopening: lob.ReopeningParams{Probability: 0.04, EstimateFactor: 0.45},
	}
}
```

Update every `policy.NewBookSimulator(params())` call to `policy.NewBookSimulator(params(), claimParams())` and every `policy.NewBookSimulator(p)` (where `p` is a mutated `params()`) to `policy.NewBookSimulator(p, claimParams())`.

Replace the premium assertion in `TestPolicyFieldConsistency` (lines 111-114). The year offset for a policy is `p.CoverStart.Year() - 1998`:

```go
			cp := claimParams()
			infl := math.Pow(cp.Inflation.Mean, float64(p.CoverStart.Year()-1998))
			wantPremium := cp.ExpectedPolicyLoss(p.SumInsured.Dollars(), p.Excess.Dollars(), p.RiskFactor, infl) / prm.TargetLossRatio
			if math.Abs(p.Premium.Dollars()-wantPremium) > 0.01 {
				t.Fatalf("premium %v, want %v", p.Premium.Dollars(), wantPremium)
			}
```

In `internal/domain/lob/lob_test.go`: replace `PremiumRateFactor: 0.03,` (line 26) with `TargetLossRatio: 0.72,`, and replace the validation case at line 87:

```go
		{"book.target_loss_ratio", func(l *LineOfBusiness) { l.Book.TargetLossRatio = 0 }},
```

In `internal/infrastructure/config/config_test.go`: replace `premium_rate_factor: 0.03` (line 22) with `target_loss_ratio: 0.72`.

- [ ] **Step 8: Build and run the full suite (some realism assertions may need calibration)**

Run: `go build ./... && go vet ./...`
Expected: builds clean.

Run: `go test ./internal/domain/... ./internal/infrastructure/config/...`
Expected: PASS.

- [ ] **Step 9: Calibrate the target so the realism gate passes**

Run: `go test ./internal/application/ -run TestDefaultPresetIsRealistic -v`

If it fails on the loss ratio, temporarily print the report to read the net loss ratio and its band:

```bash
go test ./internal/application/ -run TestDefaultPresetIsRealistic -v 2>&1 | grep -i "loss ratio"
```

The `Report.String()` line reads `ultimate loss ratio: <value> in [<lo>, <hi>]`. Set the new target with:

```
new_target = 0.72 * (value / ((lo + hi) / 2))
```

i.e. scale the current target by the ratio of the achieved loss ratio to the band midpoint, so the achieved loss ratio moves to the middle of the band. Update `target_loss_ratio` in `motor-personal.yaml` (and the `0.72` literals in the three test fixtures from Step 7) to the rounded value, then re-run until the gate passes across all its seeds.

Run: `go test ./... `
Expected: PASS (including the determinism test - pricing added no draws).

- [ ] **Step 10: Commit**

```bash
git add -A
git commit -m "Price premium to a target loss ratio (MF-1, MF-7)"
```

---

### Task 3: Per-year loss-ratio drift guard in the realism gate

**Files:**
- Modify: `internal/domain/triangle/compare.go` (drift metric, Report field, Pass, String, CompareToReference)
- Test: `internal/domain/triangle/compare_test.go`

**Interfaces:**
- Consumes: `Triangle.latestDiagonal()`, `Comparison.Incurred`, `Comparison.EarnedPremium`
- Produces: `Report.LossRatioDrift Check`; unexported `lossRatioDrift(incurred Triangle, earnedPremium []float64) (float64, bool)`; `driftTolerance` const

- [ ] **Step 1: Write the failing test**

Add to `internal/domain/triangle/compare_test.go` (create the file with `package triangle` if it does not exist):

```go
// flatTriangle builds a fully-developed incurred triangle whose every origin
// year has the same single cumulative value, and a matching earned premium.
func flatTriangle(years int, incurred, premium float64) (Triangle, []float64) {
	cells := make([][]float64, years)
	ep := make([]float64, years)
	for i := range cells {
		cells[i] = []float64{incurred}
		ep[i] = premium
	}
	return Triangle{StartYear: 1998, Cells: cells}, ep
}

func TestLossRatioDriftFlatIsNearOne(t *testing.T) {
	tri, ep := flatTriangle(10, 700, 1000)
	d, ok := lossRatioDrift(tri, ep)
	if !ok {
		t.Fatal("expected a drift value")
	}
	if math.Abs(d-1) > 1e-9 {
		t.Fatalf("flat drift = %v, want 1", d)
	}
}

func TestLossRatioDriftClimbingExceedsTolerance(t *testing.T) {
	years := 10
	cells := make([][]float64, years)
	ep := make([]float64, years)
	for i := range cells {
		// Loss ratio climbs from 0.70 to 1.06 across the decade.
		cells[i] = []float64{700 + float64(i)*40}
		ep[i] = 1000
	}
	tri := Triangle{StartYear: 1998, Cells: cells}
	d, ok := lossRatioDrift(tri, ep)
	if !ok {
		t.Fatal("expected a drift value")
	}
	if d <= driftTolerance {
		t.Fatalf("climbing drift = %v, want > %v", d, driftTolerance)
	}
}
```

Add `"math"` to the test file imports if not already present.

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/domain/triangle/ -run LossRatioDrift -v`
Expected: compile failure - `undefined: lossRatioDrift`, `undefined: driftTolerance`.

- [ ] **Step 3: Implement the drift metric and wire it into the report**

In `internal/domain/triangle/compare.go`, add the tolerance next to the percentile constants (near line 42):

```go
// driftTolerance bounds systematic loss-ratio drift: the second-half loss
// ratio must stay within [1/driftTolerance, driftTolerance] of the first-half
// loss ratio. Tightening it toward 1 makes the drift gate stricter.
const driftTolerance = 1.10
```

Add the `LossRatioDrift` field to `Report` (after `LossRatio Check`):

```go
	LossRatioDrift Check
```

Update `Report.Pass()` to include it - change the final `return`:

```go
	return r.LossRatio.Within && r.LossRatioDrift.Within
```

Add a line to `Report.String()` after the loss-ratio line:

```go
	fmt.Fprintf(&b, "loss ratio drift (2nd half / 1st half): %.4f in [%.4f, %.4f] = %v\n",
		r.LossRatioDrift.Value, r.LossRatioDrift.Band.Lo, r.LossRatioDrift.Band.Hi, r.LossRatioDrift.Within)
```

Add the metric function (near `lossRatio`, around line 234):

```go
// lossRatioDrift measures systematic loss-ratio drift across accident years:
// the ratio of the second-half aggregate loss ratio to the first-half one. A
// value near 1 means a flat loss-ratio trend. It uses the generated data only
// (no reference), so it is immune to reference immaturity. ok is false when
// there are too few years or no first-half signal.
func lossRatioDrift(incurred Triangle, earnedPremium []float64) (float64, bool) {
	latest := incurred.latestDiagonal()
	n := len(latest)
	if n < 2 || len(earnedPremium) < n {
		return 0, false
	}
	half := n / 2
	sum := func(lo, hi int) (inc, ep float64) {
		for i := lo; i < hi; i++ {
			inc += latest[i]
			ep += earnedPremium[i]
		}
		return
	}
	inc1, ep1 := sum(0, half)
	inc2, ep2 := sum(n-half, n)
	if ep1 <= 0 || ep2 <= 0 || inc1 <= 0 {
		return 0, false
	}
	return (inc2 / ep2) / (inc1 / ep1), true
}
```

Wire it into `CompareToReference`, just before `return report`:

```go
	drift, driftOK := lossRatioDrift(c.Incurred, c.EarnedPremium)
	driftBand := Band{Lo: 1 / driftTolerance, Hi: driftTolerance, Min: 1 / driftTolerance, Max: driftTolerance}
	report.LossRatioDrift = Check{Value: drift, Band: driftBand, Within: !driftOK || driftBand.contains(drift)}
```

- [ ] **Step 4: Run the drift tests and the full triangle suite**

Run: `go test ./internal/domain/triangle/ -run LossRatioDrift -v`
Expected: PASS.

Run: `go test ./internal/domain/triangle/ ./internal/application/`
Expected: PASS - the calibrated preset is flat, so `TestDefaultPresetIsRealistic` still passes with the new drift check active.

If `TestDefaultPresetIsRealistic` fails on drift, the preset still drifts: re-check Task 2's pricing (the inflation factor should use `Inflation.Mean`, matching the loss trend). Do not loosen `driftTolerance` to force a pass without confirming the pricing is correct.

- [ ] **Step 5: Commit**

```bash
git add internal/domain/triangle/compare.go internal/domain/triangle/compare_test.go
git commit -m "Guard against per-year loss-ratio drift in the realism gate"
```

---

### Task 4: Surface the drift check in the web realism tab

**Files:**
- Modify: `internal/infrastructure/web/viewmodel.go` (realismJSON field + realismView)
- Modify: `internal/infrastructure/web/static/app.js` (renderRealism)
- Test: `internal/infrastructure/web/server_test.go` (assert the new JSON field is present)

**Interfaces:**
- Consumes: `triangle.Report.LossRatioDrift`
- Produces: JSON field `realism.loss_ratio_drift` (a `checkJSON`)

- [ ] **Step 1: Write the failing test**

Find the existing realism response test in `internal/infrastructure/web/server_test.go` (search for `"realism"` or `loss_ratio`). Add an assertion that the generate response JSON contains `loss_ratio_drift` under `realism` with a numeric `value`. If the test decodes into a struct, add a `LossRatioDrift` field mirroring the loss-ratio one and assert `resp.Realism.LossRatioDrift.Value > 0`. If it decodes into a `map[string]any`, assert:

```go
	realism := body["realism"].(map[string]any)
	drift := realism["loss_ratio_drift"].(map[string]any)
	if _, ok := drift["value"].(float64); !ok {
		t.Fatal("expected realism.loss_ratio_drift.value in response")
	}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/infrastructure/web/ -run Realism -v` (adjust `-run` to the matched test name)
Expected: FAIL - `loss_ratio_drift` key missing.

- [ ] **Step 3: Add the field to the viewmodel**

In `internal/infrastructure/web/viewmodel.go`, add to `realismJSON` (after `LossRatio checkJSON`):

```go
	LossRatioDrift checkJSON `json:"loss_ratio_drift"`
```

In `realismView`, add after the `LossRatio` field:

```go
		LossRatioDrift: checkJSON{
			Value:  finite(r.LossRatioDrift.Value),
			Lo:     finite(r.LossRatioDrift.Band.Lo),
			Hi:     finite(r.LossRatioDrift.Band.Hi),
			Min:    finite(r.LossRatioDrift.Band.Min),
			Max:    finite(r.LossRatioDrift.Band.Max),
			Within: r.LossRatioDrift.Within,
		},
```

- [ ] **Step 4: Render it in the UI**

In `internal/infrastructure/web/static/app.js`, `renderRealism`, add a fourth card to the `panel.append(...)` call:

```js
    bandCard("Loss-ratio drift 2nd half / 1st half (flat = 1)", [{ ...r.loss_ratio_drift, label: "Drift" }]),
```

- [ ] **Step 5: Run the test to verify it passes**

Run: `go test ./internal/infrastructure/web/ -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/infrastructure/web/viewmodel.go internal/infrastructure/web/static/app.js internal/infrastructure/web/server_test.go
git commit -m "Show the loss-ratio drift check in the realism tab"
```

---

### Task 5: Update the README

**Files:**
- Modify: `README.md`

- [ ] **Step 1: Update the pricing documentation**

Search `README.md` for `premium_rate_factor` and for any note describing the loss ratio drifting across the ten years (the MF-1 wording). Replace the `premium_rate_factor` description with a `target_loss_ratio` description: "Premium is priced to a target loss ratio - each policy's premium is its expected ultimate loss divided by `target_loss_ratio` - so the accident-year loss ratio stays flat as severities inflate." Remove or rewrite the drift note to say the drift is now priced out and guarded by a per-year drift check in the realism gate. Keep sentence-case headers and use ` - ` instead of em dashes.

- [ ] **Step 2: Verify the whole suite once more**

Run: `go vet ./... && go test ./...`
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add README.md
git commit -m "Document target-loss-ratio pricing in the README"
```

---

## Self-review notes

- Spec coverage: pricing model (Tasks 1-2), placement on `ClaimParams` (Task 1), config/knob swap + fan-out incl. app.js (Task 2), calibration (Task 2 Step 9), per-year drift check (Task 3), UI surfacing (Task 4), docs (Task 5). Testing bullets from the spec map to Task 1 (closed forms vs Monte Carlo), Task 2 (premium assertion, realism gate), Task 3 (drift on flat vs climbing).
- Determinism: excess is drawn at the same point in the sub-stream and no new draws are added, so the existing full-dataset determinism test guards the reproducibility contract (Task 2 Step 9).
- Out of scope, per spec: SL-3 own-damage cap, SL-4 inflation rebase, SL-2 reference immaturity, recovery netting in the target basis.
