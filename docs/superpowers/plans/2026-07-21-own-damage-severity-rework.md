# Own-damage severity rework (SL-3 + SL-4) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Cap own-damage severity at the sum insured (SL-3) and rebase its trend to the claims index only, expressed in base-year sum-insured terms (SL-4), keeping premium priced to the target loss ratio.

**Architecture:** Own-damage ground-up loss becomes `min(fraction × baseSI × claims_inflation, SumInsured)`, where `baseSI` is the policy's sum insured with the `sum_insured_inflation` drift removed. The same math is mirrored in the deterministic `ExpectedPolicyLoss` pricing closed form (using a limited-lognormal stop-loss) so the per-year loss ratio stays flat.

**Tech Stack:** Go, standard library only. Tests are `go test`.

## Global Constraints

- Design doc: `docs/superpowers/specs/2026-07-21-own-damage-severity-rework-design.md` (copy any detail from there verbatim).
- No new random draws may be introduced in the claim path - the SL-5 shift-free contract must hold (`internal/domain/claim/claim.go:119-124`).
- Comments and docs use spaced hyphens ` - `, never em dashes.
- Run `go test ./...` and `go vet ./...` before every commit; both must pass.
- Third-party (Pareto) severity is unchanged throughout.

---

### Task 1: Limited-lognormal stop-loss helper

**Files:**
- Modify: `internal/domain/lob/expectedloss.go`
- Test: `internal/domain/lob/expectedloss_test.go`

**Interfaces:**
- Produces: `limitedStopLossLognormal(median, sigma, excess, cap float64) float64` = `E[(min(X, cap) - excess)+]` for `X` lognormal.

- [ ] **Step 1: Write the failing test**

Add to `internal/domain/lob/expectedloss_test.go`:

```go
func TestLimitedStopLossLognormal(t *testing.T) {
	const median, sigma = 5000.0, 1.0
	// With a cap it equals the difference of two stop-loss layers.
	excess, cap := 300.0, 20000.0
	want := stopLossLognormal(median, sigma, excess) - stopLossLognormal(median, sigma, cap)
	if got := limitedStopLossLognormal(median, sigma, excess, cap); math.Abs(got-want) > 1e-9 {
		t.Fatalf("layered form: got %.6f, want %.6f", got, want)
	}
	// A finite cap is strictly cheaper than the uncapped stop-loss.
	uncapped := stopLossLognormal(median, sigma, excess)
	if got := limitedStopLossLognormal(median, sigma, excess, cap); got >= uncapped {
		t.Fatalf("cap should reduce cost: capped %.6f, uncapped %.6f", got, uncapped)
	}
	// cap <= excess yields no layer.
	if got := limitedStopLossLognormal(median, sigma, 20000, 20000); got != 0 {
		t.Fatalf("cap == excess: got %.6f, want 0", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/domain/lob/ -run TestLimitedStopLossLognormal -v`
Expected: FAIL - `undefined: limitedStopLossLognormal`.

- [ ] **Step 3: Write minimal implementation**

Add to `internal/domain/lob/expectedloss.go` (after `stopLossLognormal`):

```go
// limitedStopLossLognormal returns E[(min(X, cap) - excess)+] for X lognormal
// with the given median and sigma: the expected excess-of-excess cost when
// losses are also capped at cap. For excess < cap it is the difference of two
// stop-loss layers; cap <= excess yields 0.
func limitedStopLossLognormal(median, sigma, excess, cap float64) float64 {
	if cap <= excess {
		return 0
	}
	return stopLossLognormal(median, sigma, excess) - stopLossLognormal(median, sigma, cap)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/domain/lob/ -run TestLimitedStopLossLognormal -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/domain/lob/expectedloss.go internal/domain/lob/expectedloss_test.go
git commit -m "Add limited-lognormal stop-loss helper (SL-3 pricing)"
```

---

### Task 2: Rebase and cap ExpectedPolicyLoss

**Files:**
- Modify: `internal/domain/lob/expectedloss.go`
- Modify: `internal/domain/policy/book.go:55-58,77` (pass the sum-insured drift factor)
- Test: `internal/domain/lob/expectedloss_test.go`

**Interfaces:**
- Consumes: `limitedStopLossLognormal` (Task 1).
- Produces: `func (c ClaimParams) ExpectedPolicyLoss(sumInsured, excess, riskFactor, inflationFactor, siDrift float64) float64` - the OD median uses `baseSI = sumInsured / siDrift`, and the OD stop-loss is capped at `sumInsured`. `siDrift` is `sum_insured_inflation ^ (policy underwriting-year offset)`.

- [ ] **Step 1: Write the failing test**

Add to `internal/domain/lob/expectedloss_test.go` (pure own-damage params so OD is not masked by third party):

```go
func TestExpectedPolicyLossRebasesAndCapsOwnDamage(t *testing.T) {
	c := ClaimParams{
		BaseFrequency: 0.12,
		Severity: SeverityParams{
			ThirdPartyWeight:        0, // pure own damage
			OwnDamageMedianFraction: 0.12,
			OwnDamageSigma:          1.0,
			ThirdPartyScale:         4000,
			ThirdPartyAlpha:         2.2,
		},
		Reopening: ReopeningParams{Probability: 0.04, EstimateFactor: 0.45},
	}
	// De-drift: a larger siDrift (same nominal SI) means a smaller base-year
	// severity, so the expected loss falls.
	full := c.ExpectedPolicyLoss(20000, 300, 1.0, 1.0, 1.0)
	deDrifted := c.ExpectedPolicyLoss(20000, 300, 1.0, 1.0, 2.0)
	if !(deDrifted < full) {
		t.Fatalf("siDrift should de-drift OD: siDrift=2 %.4f not < siDrift=1 %.4f", deDrifted, full)
	}
	// Cap: per-claim OD cannot exceed (sumInsured - excess); drive baseSI far
	// above the cap with a tiny siDrift and check the ceiling holds.
	const si, excess = 20000.0, 300.0
	reopenUplift := 1 + c.Reopening.Probability*c.Reopening.EstimateFactor
	ceiling := c.BaseFrequency * 1.0 * (si - excess) * reopenUplift
	if got := c.ExpectedPolicyLoss(si, excess, 1.0, 1.0, 0.01); got > ceiling {
		t.Fatalf("capped OD exceeds ceiling: got %.4f, ceiling %.4f", got, ceiling)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/domain/lob/ -run TestExpectedPolicyLossRebasesAndCapsOwnDamage -v`
Expected: FAIL - `ExpectedPolicyLoss` takes 4 args, not 5 (compile error).

- [ ] **Step 3: Update ExpectedPolicyLoss**

Replace the body of `ExpectedPolicyLoss` in `internal/domain/lob/expectedloss.go`:

```go
// ExpectedPolicyLoss is the deterministic expected ultimate gross incurred loss
// for one policy. It mirrors the severity model in the claim package so premium
// can be priced to a target loss ratio: own damage is expressed in base-year
// sum-insured terms (baseSI = sumInsured / siDrift) trended by the claims index
// only, and capped at the drifted sumInsured (a total loss). Third party keeps
// the claims index. It draws no randomness, so pricing never perturbs a
// sub-stream. Recoveries are excluded (gross basis).
func (c ClaimParams) ExpectedPolicyLoss(sumInsured, excess, riskFactor, inflationFactor, siDrift float64) float64 {
	s := c.Severity
	baseSI := sumInsured / siDrift
	odMedian := inflationFactor * baseSI * s.OwnDamageMedianFraction
	od := limitedStopLossLognormal(odMedian, s.OwnDamageSigma, excess, sumInsured)
	tpScale := inflationFactor * s.ThirdPartyScale
	tp := stopLossPareto(tpScale, s.ThirdPartyAlpha, excess)
	perClaim := s.ThirdPartyWeight*tp + (1-s.ThirdPartyWeight)*od
	reopenUplift := 1 + c.Reopening.Probability*c.Reopening.EstimateFactor
	return c.BaseFrequency * riskFactor * perClaim * reopenUplift
}
```

- [ ] **Step 4: Update the existing pricing test signature**

In `internal/domain/lob/expectedloss_test.go`, `TestExpectedPolicyLossScalesWithRiskAndInflation` calls the 4-arg form. Add a trailing `1.0` (identity drift) to each call:

```go
	base := c.ExpectedPolicyLoss(20000, 300, 1.0, 1.0, 1.0)
	...
	if got := c.ExpectedPolicyLoss(20000, 300, 2.0, 1.0, 1.0); math.Abs(got-2*base) > 1e-6 {
	...
	if got := c.ExpectedPolicyLoss(20000, 300, 1.0, 1.5, 1.0); got <= base {
```

- [ ] **Step 5: Update the book call site**

In `internal/domain/policy/book.go`, the `Simulate` loop already computes `y`. Compute the drift factor there and thread it through `simulatePolicy`:

In `Simulate`, inside the year loop (near the `medianSI`/`inflation` lines ~55-56):

```go
		medianSI := s.book.SumInsuredMedian * math.Pow(s.book.SumInsuredInflation, float64(y))
		inflation := math.Pow(s.claims.Inflation.Mean, float64(y))
		siDrift := math.Pow(s.book.SumInsuredInflation, float64(y))
		for i := 0; i < size; i++ {
			book = append(book, s.simulatePolicy(src.Split(fmt.Sprintf("policy-%d", id)), id, year, medianSI, inflation, siDrift))
			id++
		}
```

Change the `simulatePolicy` signature and its `ExpectedPolicyLoss` call:

```go
func (s *BookSimulator) simulatePolicy(src shared.RandomSource, id, year int, medianSI, inflation, siDrift float64) Policy {
	...
	premium := s.claims.ExpectedPolicyLoss(sumInsured, excess, riskFactor, inflation, siDrift) / s.book.TargetLossRatio
	...
}
```

- [ ] **Step 6: Run the lob and policy tests**

Run: `go test ./internal/domain/lob/ ./internal/domain/policy/ -v`
Expected: PASS (new rebase/cap test and the updated scaling test).

- [ ] **Step 7: Commit**

```bash
git add internal/domain/lob/expectedloss.go internal/domain/lob/expectedloss_test.go internal/domain/policy/book.go
git commit -m "Rebase and cap own-damage in ExpectedPolicyLoss (SL-3, SL-4)"
```

---

### Task 3: Base-year severity and cap in the claim simulator

**Files:**
- Modify: `internal/domain/claim/claim.go`
- Modify: `internal/application/generate.go:57-60` (wire the builder)
- Test: `internal/domain/claim/claim_test.go` (create if absent)

**Interfaces:**
- Consumes: nothing from earlier tasks (parallel domain path).
- Produces: `func (s *ClaimSimulator) WithBaseYear(sumInsuredInflation float64, startYear int) *ClaimSimulator`. Own-damage ground-up is `fraction × baseSumInsured(pol)`, then (in `simulateClaim`) `loss *= inflation`, then capped at `pol.SumInsured`. `baseSumInsured` is identity when the knob is unset.

- [ ] **Step 1: Write the failing cap-invariant test**

Create `internal/domain/claim/claim_test.go`:

```go
package claim

import (
	"testing"
	"time"

	"github.com/le-marais/claimsgen/internal/domain/lob"
	"github.com/le-marais/claimsgen/internal/domain/policy"
	"github.com/le-marais/claimsgen/internal/domain/shared"
	"github.com/le-marais/claimsgen/internal/infrastructure/random"
)

// bookForCap builds a small book of identical policies for severity tests.
func bookForCap(n int) []policy.Policy {
	var b []policy.Policy
	start := shared.NewDate(2000, time.January, 1)
	for i := 1; i <= n; i++ {
		b = append(b, policy.Policy{
			ID:         i,
			CoverStart: start,
			CoverEnd:   start.AddDays(364),
			SumInsured: shared.FromDollars(20000),
			Excess:     shared.FromDollars(300),
			RiskFactor: 1.0,
		})
	}
	return b
}

func TestOwnDamageIsCappedAtSumInsured(t *testing.T) {
	params := lob.ClaimParams{
		BaseFrequency:  2.0, // many claims per policy so the tail is exercised
		ReportLagMedian: 2,
		ReportLagSigma:  1.2,
		Severity: lob.SeverityParams{
			ThirdPartyWeight:        0,   // pure own damage
			OwnDamageMedianFraction: 0.5, // heavy, so the cap bites often
			OwnDamageSigma:          1.5,
			ThirdPartyScale:         4000,
			ThirdPartyAlpha:         2.2,
		},
		CloseLag: lob.CloseLagParams{Shape: 1.2, MeanDays: 120, SizeThreshold: 20000, SizeMultiplier: 6, RiskLoading: 0.3, ThirdPartyShape: 1.0, ThirdPartyMeanDays: 680},
	}
	claims := NewClaimSimulator(params).Simulate(random.NewSource(1), bookForCap(500))
	if len(claims) == 0 {
		t.Fatal("no claims generated")
	}
	for _, c := range claims {
		if !c.OwnDamage {
			continue
		}
		groundUp := c.InitialEstimate.Dollars() + 300 // + excess
		if groundUp > 20000+1e-6 {
			t.Fatalf("claim %d own-damage ground-up %.2f exceeds sum insured 20000", c.ID, groundUp)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/domain/claim/ -run TestOwnDamageIsCappedAtSumInsured -v`
Expected: FAIL - some own-damage ground-up exceeds 20000 (uncapped).

- [ ] **Step 3: Add the builder, base-year helper, and cap**

In `internal/domain/claim/claim.go`, add fields to `ClaimSimulator`:

```go
type ClaimSimulator struct {
	params              lob.ClaimParams
	inflation           InflationIndex
	sumInsuredInflation float64
	startYear           int
}
```

Add the builder (next to `WithInflation`):

```go
// WithBaseYear sets the sum-insured inflation rate and run start year so
// own-damage severity can be expressed in base-year sum-insured terms (SL-4):
// the drifted sum insured is deflated by sum_insured_inflation raised to the
// policy's underwriting-year offset before scaling the loss. Unset (the zero
// value) leaves severity scaled by the nominal sum insured.
func (s *ClaimSimulator) WithBaseYear(sumInsuredInflation float64, startYear int) *ClaimSimulator {
	s.sumInsuredInflation = sumInsuredInflation
	s.startYear = startYear
	return s
}

// baseSumInsured deflates the policy's drifted sum insured back to base-year
// dollars. Identity when the base-year knob is unset.
func (s *ClaimSimulator) baseSumInsured(pol policy.Policy) float64 {
	if s.sumInsuredInflation <= 0 {
		return pol.SumInsured.Dollars()
	}
	offset := pol.CoverStart.Year() - s.startYear
	return pol.SumInsured.Dollars() / math.Pow(s.sumInsuredInflation, float64(offset))
}
```

Change `drawGroundUpLoss` own-damage return to use base-year SI:

```go
	fraction := src.LogNormal(math.Log(sev.OwnDamageMedianFraction), sev.OwnDamageSigma)
	return s.baseSumInsured(pol) * fraction, true
```

In `simulateClaim`, cap the own-damage loss after the existing inflation line:

```go
	loss *= s.inflation.For(occurrence.Year())
	if ownDamage {
		if cap := pol.SumInsured.Dollars(); loss > cap {
			loss = cap
		}
	}
	estimate := loss - pol.Excess.Dollars()
```

Update the comment block at `claim.go:107-110` to state own damage is expressed in base-year sum-insured terms, trended by the claims index only, and capped at the drifted sum insured (a total loss).

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/domain/claim/ -run TestOwnDamageIsCappedAtSumInsured -v`
Expected: PASS.

- [ ] **Step 5: Wire the builder in generate.go**

In `internal/application/generate.go`, add `WithBaseYear` to the claim simulator chain:

```go
	claims := claim.NewClaimSimulator(req.LOB.Claims).
		WithInflation(inflation).
		WithBaseYear(req.LOB.Book.SumInsuredInflation, req.StartYear).
		Simulate(src.Split("claims"), book)
```

- [ ] **Step 6: Run the claim and application build**

Run: `go test ./internal/domain/claim/ ./internal/application/ -run 'Cap|Links' -v && go vet ./...`
Expected: PASS / no vet errors.

- [ ] **Step 7: Commit**

```bash
git add internal/domain/claim/claim.go internal/domain/claim/claim_test.go internal/application/generate.go
git commit -m "Express own-damage severity in base-year SI and cap at sum insured (SL-3, SL-4)"
```

---

### Task 4: Rebase and shift-free behavioural tests

**Files:**
- Test: `internal/application/severity_test.go` (create)

**Interfaces:**
- Consumes: `application.GenerateDataset`, the `request(t)` helper (`internal/application/generate_test.go:13`), `random.NewSource`.

- [ ] **Step 1: Write the rebase-trend test**

Create `internal/application/severity_test.go`:

```go
package application_test

import (
	"testing"

	"github.com/le-marais/claimsgen/internal/application"
	"github.com/le-marais/claimsgen/internal/infrastructure/random"
)

// meanOwnDamageBySeverityYear returns mean own-damage InitialEstimate for the
// first and last accident year of the run.
func meanOwnDamageByYear(ds application.Dataset, firstYear, lastYear int) (first, last float64) {
	var fs, ls, fn, ln float64
	for _, c := range ds.Claims {
		if !c.OwnDamage {
			continue
		}
		switch c.OccurrenceDate.Year() {
		case firstYear:
			fs += c.InitialEstimate.Dollars()
			fn++
		case lastYear:
			ls += c.InitialEstimate.Dollars()
			ln++
		}
	}
	if fn > 0 {
		first = fs / fn
	}
	if ln > 0 {
		last = ls / ln
	}
	return first, last
}

// TestOwnDamageTrendIsClaimsIndexNotProduct proves SL-4: with the claims index
// held at 1.0, own-damage severity does NOT trend even though the sum insured
// drifts at 3%/yr. Before the rebase, severity trended at ~sum_insured_inflation.
func TestOwnDamageTrendIsClaimsIndexNotProduct(t *testing.T) {
	req := request(t)
	req.StartYear = 1998
	req.Years = 8
	req.InitialBookSize = 20000
	req.LOB.Claims.Inflation.Mean = 1.0        // claims index off
	req.LOB.Claims.Inflation.Volatility = 0.0
	req.LOB.Book.SumInsuredInflation = 1.10    // strong SI drift
	ds, err := application.GenerateDataset(random.NewSource(1), req)
	if err != nil {
		t.Fatal(err)
	}
	first, last := meanOwnDamageByYear(ds, 1998, 2005)
	if first == 0 || last == 0 {
		t.Fatal("missing own-damage claims in a boundary year")
	}
	ratio := last / first
	// Rebased: ~flat. Old double-count would be ~1.10^7 ≈ 1.95.
	if ratio < 0.90 || ratio > 1.15 {
		t.Fatalf("own-damage severity trended with SI drift: ratio %.3f, want ~1.0", ratio)
	}
}
```

- [ ] **Step 2: Run test to verify it passes**

Run: `go test ./internal/application/ -run TestOwnDamageTrendIsClaimsIndexNotProduct -v`
Expected: PASS. (If it fails high, the base-year rebase in Task 3 is wrong.)

- [ ] **Step 3: Verify the SL-5 shift-free contract still holds**

The SL-3 cap is a deterministic `min()` with no `src` call, so it cannot shift
the RNG stream - this is guaranteed structurally and verified by reading the
diff (no new draw in the claim path). The behavioural guarantee that toggling a
real knob does not reshuffle later claims is already covered by the existing
`TestNilClaimsDoNotShiftOtherStages` (`internal/application/nil_test.go`). Run it
here to confirm the rework did not regress it:

Run: `go test ./internal/application/ -run TestNilClaimsDoNotShiftOtherStages -v`
Expected: PASS.

(No separate cap-toggle test is written: there is no cap knob to toggle, and
un-binding the cap by changing the sum insured also changes the estimate, which
legitimately moves reportability and the close-lag size regime - those are model
effects, not RNG shifts, so such a test would assert a false invariant.)

- [ ] **Step 4: Commit**

```bash
git add internal/application/severity_test.go
git commit -m "Test own-damage rebase; confirm shift-free contract intact (SL-3, SL-4)"
```

---

### Task 5: Recalibrate the preset and refresh the golden fixture

**Files:**
- Modify (if needed): `internal/infrastructure/config/motor-personal.yaml`
- Modify: `internal/application/golden_test.go:19` (`wantHash`)
- Modify (docs): `README.md` inflation/severity note, if the trend description changed

**Interfaces:**
- Consumes: the full generation path (Tasks 2-3).

- [ ] **Step 1: Run the realism gate**

Run: `go test ./internal/application/ -run TestDefaultPresetIsRealistic -v`
Expected: may FAIL - own-damage trend dropped 7%→4% and the tail is capped, so the triangles moved.

- [ ] **Step 2: Recalibrate if red**

If the gate fails, adjust `internal/infrastructure/config/motor-personal.yaml` and re-run until green on all three seeds (1, 42, 7). Guidance:
- The tightest metric is **paid ATA age 2-3** (per the config comment) - watch it.
- Capping severity removes extreme own-damage payouts that settled fast; if early paid development is now too light, nudge `close_lag.mean_days` down slightly or `own_damage_median_fraction`/`own_damage_sigma` to restore the paid pattern.
- The loss-ratio band should be unaffected (pricing mirrors severity), but re-check the per-year drift.
- Change one knob at a time; re-run `TestDefaultPresetIsRealistic` after each.

Record the final knob changes in the commit message.

- [ ] **Step 3: Refresh the golden hash**

Run: `go test ./internal/application/ -run TestGoldenCSVBytes -v`
Expected: FAIL with a printed `got:` hash. Copy that hash into `wantHash` at `internal/application/golden_test.go:19`.

- [ ] **Step 4: Full suite**

Run: `go test ./... && go vet ./...`
Expected: PASS / clean.

- [ ] **Step 5: Commit**

```bash
git add internal/infrastructure/config/motor-personal.yaml internal/application/golden_test.go README.md
git commit -m "Recalibrate preset and refresh golden after own-damage rework (SL-3, SL-4)"
```

---

## Self-Review

- **Spec coverage:** SL-3 cap → Tasks 1-3, invariant test Task 3; SL-4 rebase → Tasks 2-3, trend test Task 4; pricing mirror → Task 2; shift-free contract confirmed (not newly tested - no cap knob exists) → Task 4 Step 3 via the existing nil no-shift test; recalibration + golden → Task 5. Salvage coupling explicitly out of scope (design doc). All covered.
- **Placeholders:** none - every step has concrete code or an exact command. Task 5 recalibration is inherently iterative but has a concrete procedure and pass criterion.
- **Type consistency:** `ExpectedPolicyLoss(..., siDrift)` 5-arg form used in Task 2 and the book call; `WithBaseYear(sumInsuredInflation, startYear)` and `baseSumInsured(pol)` used consistently in Task 3; `limitedStopLossLognormal` signature matches Task 1 and its use in Task 2.
