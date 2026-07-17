# Reopened claims implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let a closed claim reopen once - case re-raised after a lag, a second smaller runoff episode develops and pays an additional amount, final close recorded in claims.csv - with UI support and the realism gate as acceptance.

**Architecture:** A `ReopenSimulator` post-pass in the claim domain decides reopens after claim IDs are assigned (per-claim labelled sub-streams, like recoveries), mutating the claim's dates: `CloseDate` becomes the final close, `FirstCloseDate`/`ReopenDate`/`ReopenEstimate` are carried internally. The runoff simulator is refactored into an episode function: episode 1 reproduces today's behavior byte-for-byte against the first close; for reopened claims a re-raise `ESTIMATE` row lands on the reopen date and episode 2 runs the same machinery to the final close. Recoveries need only one relaxation: a reopened nil claim that paid becomes recovery-eligible.

**Tech Stack:** Go (stdlib + gonum + yaml.v3, already in go.mod), vanilla JS UI, Node + puppeteer-core for the screenshot script.

Spec: `docs/superpowers/specs/2026-07-17-reopened-claims-design.md`

## Global constraints

- claims.csv schema unchanged: `close_date` becomes the final close; `FirstCloseDate`, `ReopenDate`, `ReopenEstimate` are never written to CSV. policies.csv and transactions.csv shapes unchanged (no new transaction types).
- Same seed + config = byte-identical CSVs. Reopen draws come only from new labelled sub-streams (`src.Split("reopening")`, then `reopen-claim-<id>`); with `reopening.probability: 0` no draw happens at all and output is byte-identical to today's. Enabling reopening leaves the book, inflation, claim events (before the post-pass), and every non-reopened claim's transactions byte-identical.
- Strict YAML decoding: domain fields, DTO fields, YAML keys, and validation land atomically (one commit), with the embedded motor preset updated in the same change.
- At most one reopen per claim. Reopen date strictly after first close; second close strictly after reopen date.
- Invariants: outstanding exactly zero at the first close and at the final close, never negative; the reopen row is an `ESTIMATE` with strictly positive amount on the reopen date; non-recovery transactions live in [report, final close] with the last case activity on the final close date; a nil claim that never reopens pays nothing; a reopened nil claim pays only in episode 2; recoveries stay strictly after the final close and strictly below total gross paid.
- Probability 0 switches reopening off; YAML comment and UI tooltip both state it; a test proves it from the output.
- Run `go test ./...` and `go vet ./...` before every commit. `TestDefaultPresetIsRealistic` may fail between the wiring task and the calibration task; where that is expected, run `go test ./... -skip TestDefaultPresetIsRealistic` and report the gate separately.
- Commit messages: plain imperative sentences, no type prefixes.
- Docs use sentence case headers and spaced hyphens ` - `, never em dashes.

---

### Task 1: Reopening parameters - lob domain, config DTOs, and the motor preset (one commit)

Strict decoding plus domain validation means these land atomically, like the recoveries parameter task.

**Files:**
- Modify: `internal/domain/lob/lob.go`
- Modify: `internal/infrastructure/config/config.go`
- Modify: `internal/infrastructure/config/motor-personal.yaml`
- Test: `internal/domain/lob/lob_test.go` (package `lob`, internal)
- Test: `internal/infrastructure/config/config_test.go`

**Interfaces:**
- Produces: `lob.ReopeningParams{Probability, EstimateFactor, EstimateSigma, LagMedianDays, LagSigma float64}` reachable as `lob.ClaimParams.Reopening`; validation error prefix `claims.reopening.*`; `config.ReopeningParams` DTO with yaml/json tags `probability`, `estimate_factor`, `estimate_sigma`, `lag_median_days`, `lag_sigma` under `claims.reopening`.

- [ ] **Step 1: Write the failing lob tests**

In `internal/domain/lob/lob_test.go`, extend `validMotor()` - inside `Claims: ClaimParams{...}` after the `Recoveries: RecoveryParams{...},` entry add:

```go
			Reopening: ReopeningParams{Probability: 0.04, EstimateFactor: 0.45, EstimateSigma: 0.5, LagMedianDays: 90, LagSigma: 0.7},
```

Add to the `TestValidationNamesTheOffendingField` table:

```go
		{"claims.reopening.probability", func(l *LineOfBusiness) { l.Claims.Reopening.Probability = 1.0 }},
		{"claims.reopening.probability", func(l *LineOfBusiness) { l.Claims.Reopening.Probability = -0.1 }},
		{"claims.reopening.estimate_factor", func(l *LineOfBusiness) { l.Claims.Reopening.EstimateFactor = 0 }},
		{"claims.reopening.estimate_sigma", func(l *LineOfBusiness) { l.Claims.Reopening.EstimateSigma = -0.1 }},
		{"claims.reopening.lag_median_days", func(l *LineOfBusiness) { l.Claims.Reopening.LagMedianDays = 0 }},
		{"claims.reopening.lag_sigma", func(l *LineOfBusiness) { l.Claims.Reopening.LagSigma = -0.1 }},
```

And a new test:

```go
func TestValidateAcceptsZeroReopeningProbability(t *testing.T) {
	l := validMotor()
	l.Claims.Reopening.Probability = 0
	if err := l.Validate(); err != nil {
		t.Fatalf("zero reopening probability (the off switch): want nil, got %v", err)
	}
}
```

- [ ] **Step 2: Run the lob tests to verify they fail**

Run: `go test ./internal/domain/lob/`
Expected: FAIL to compile with `unknown field Reopening`.

- [ ] **Step 3: Implement the domain params**

In `internal/domain/lob/lob.go`:

Add to `ClaimParams` after `Recoveries`:

```go
	// Reopening drives the single optional reopen episode: a closed claim
	// can reopen once, develop further, and close again.
	Reopening ReopeningParams
```

Add the type after `RecoveryTypeParams`:

```go
// ReopeningParams parameterizes the single optional reopen episode.
type ReopeningParams struct {
	// Probability is the chance a closed claim reopens once; 0 switches
	// reopening off.
	Probability float64
	// EstimateFactor is the mean of the reopen case estimate as a factor of
	// the claim's original initial estimate; it may exceed 1.
	EstimateFactor float64
	// EstimateSigma is the sigma of the mean-1 lognormal noise on the
	// reopen estimate.
	EstimateSigma float64
	// LagMedianDays is the median days from first close to reopen.
	LagMedianDays float64
	// LagSigma is the sigma of the lognormal close-to-reopen lag.
	LagSigma float64
}
```

In `ClaimParams.validate()`, after the two recovery validations and before `return c.CloseLag.validate()`:

```go
	if err := c.Reopening.validate(); err != nil {
		return err
	}
```

Add the validation method after `RecoveryTypeParams.validate`:

```go
func (r ReopeningParams) validate() error {
	if r.Probability < 0 || r.Probability >= 1 {
		return fmt.Errorf("claims.reopening.probability: must be in [0, 1), got %v", r.Probability)
	}
	if r.EstimateFactor <= 0 {
		return fmt.Errorf("claims.reopening.estimate_factor: must be positive, got %v", r.EstimateFactor)
	}
	if r.EstimateSigma < 0 {
		return fmt.Errorf("claims.reopening.estimate_sigma: must not be negative, got %v", r.EstimateSigma)
	}
	if r.LagMedianDays <= 0 {
		return fmt.Errorf("claims.reopening.lag_median_days: must be positive, got %v", r.LagMedianDays)
	}
	if r.LagSigma < 0 {
		return fmt.Errorf("claims.reopening.lag_sigma: must not be negative, got %v", r.LagSigma)
	}
	return nil
}
```

Verify: `go test ./internal/domain/lob/` passes. The full suite now fails (preset lacks the block) - expected until Step 6.

- [ ] **Step 4: Write the failing config tests**

In `internal/infrastructure/config/config_test.go`, inside `validYAML` after the `recoveries:` block (match the two-space indentation under `claims:`):

```yaml
  reopening:
    probability: 0.04
    estimate_factor: 0.45
    estimate_sigma: 0.5
    lag_median_days: 90
    lag_sigma: 0.7
```

In the decoded-values test (the one asserting `l.Claims.Recoveries.Salvage.MeanShare != 0.15`), add:

```go
	if l.Claims.Reopening.Probability != 0.04 {
		t.Errorf("reopening probability = %v, want 0.04", l.Claims.Reopening.Probability)
	}
	if l.Claims.Reopening.LagMedianDays != 90 {
		t.Errorf("reopening lag_median_days = %v, want 90", l.Claims.Reopening.LagMedianDays)
	}
```

Add:

```go
func TestLoadRejectsMissingReopeningBlock(t *testing.T) {
	bad := strings.Replace(validYAML, "  reopening:", "  reopening_gone:", 1)
	if _, err := Load(strings.NewReader(bad)); err == nil {
		t.Fatal("config without a reopening block: want error, got nil")
	}
}
```

Run: `go test ./internal/infrastructure/config/` - FAIL (strict decoding rejects the unknown `reopening` key).

- [ ] **Step 5: Implement the DTOs and the preset block**

In `internal/infrastructure/config/config.go`:

Add to `ClaimsParams` after `Recoveries`:

```go
	Reopening ReopeningParams `yaml:"reopening" json:"reopening"`
```

Add after `RecoveryTypeParams`:

```go
type ReopeningParams struct {
	Probability    float64 `yaml:"probability" json:"probability"`
	EstimateFactor float64 `yaml:"estimate_factor" json:"estimate_factor"`
	EstimateSigma  float64 `yaml:"estimate_sigma" json:"estimate_sigma"`
	LagMedianDays  float64 `yaml:"lag_median_days" json:"lag_median_days"`
	LagSigma       float64 `yaml:"lag_sigma" json:"lag_sigma"`
}
```

In `ToDomain()`, after the `Recoveries: lob.RecoveryParams{...},` entry:

```go
			Reopening: lob.ReopeningParams{
				Probability:    d.Claims.Reopening.Probability,
				EstimateFactor: d.Claims.Reopening.EstimateFactor,
				EstimateSigma:  d.Claims.Reopening.EstimateSigma,
				LagMedianDays:  d.Claims.Reopening.LagMedianDays,
				LagSigma:       d.Claims.Reopening.LagSigma,
			},
```

In `internal/infrastructure/config/motor-personal.yaml`, after the `recoveries:` block (still under `claims:`):

```yaml
  # Reopening: a closed claim can reopen once - the case is re-raised a
  # lognormal lag after the first close, a second episode develops and pays
  # an additional amount (the reopen estimate is `estimate_factor` times the
  # original initial estimate, with lognormal noise), then the claim closes
  # for good. claims.csv shows the final close date. probability 0 switches
  # reopening off.
  reopening:
    probability: 0.04
    estimate_factor: 0.45
    estimate_sigma: 0.5
    lag_median_days: 90
    lag_sigma: 0.7
```

(Starting values; Task 7 checks the realism gate.)

- [ ] **Step 6: Run the full suite**

Run: `go test ./... && go vet ./...`
Expected: PASS everywhere - nothing consumes the new params yet, so generated output is unchanged and the realism gate still passes.

- [ ] **Step 7: Commit**

```bash
git add internal/domain/lob/lob.go internal/domain/lob/lob_test.go internal/infrastructure/config/config.go internal/infrastructure/config/config_test.go internal/infrastructure/config/motor-personal.yaml
git commit -m "Add reopening parameters"
```

---

### Task 2: Claim reopen fields and the reopen post-pass

**Files:**
- Modify: `internal/domain/claim/claim.go`
- Create: `internal/domain/claim/reopen.go`
- Test: `internal/domain/claim/reopen_test.go` (package `claim_test`)

**Interfaces:**
- Consumes: `lob.ClaimParams.Reopening` and `lob.CloseLagParams` (Task 1), `shared.MeanOneLogNormal`.
- Produces:
  - `claim.Claim` fields `FirstCloseDate shared.Date`, `ReopenDate shared.Date`, `ReopenEstimate shared.Money` (zero values = never reopens; `CloseDate` stays the final close and the CSV column) and method `func (c Claim) Reopened() bool`.
  - `claim.NewReopenSimulator(p lob.ClaimParams) *ReopenSimulator` with `func (s *ReopenSimulator) Apply(src shared.RandomSource, claims []Claim) []Claim`.
  - Package-level `drawCloseLag(src shared.RandomSource, cl lob.CloseLagParams, estimate, riskFactor float64) float64` (extracted from the method; the ClaimSimulator keeps calling it identically, so no seeded output changes).

- [ ] **Step 1: Write the failing tests**

Create `internal/domain/claim/reopen_test.go`:

```go
package claim_test

import (
	"testing"

	"github.com/le-marais/claimsgen/internal/domain/claim"
	"github.com/le-marais/claimsgen/internal/domain/lob"
	"github.com/le-marais/claimsgen/internal/infrastructure/random"
)

func reopeningParams() lob.ClaimParams {
	p := params()
	p.Reopening = lob.ReopeningParams{Probability: 0.5, EstimateFactor: 0.45, EstimateSigma: 0.5, LagMedianDays: 90, LagSigma: 0.7}
	return p
}

// reopenFixture simulates a book of claims and applies the reopen pass.
func reopenFixture(t *testing.T, p lob.ClaimParams, seed uint64) []claim.Claim {
	t.Helper()
	claims := claim.NewClaimSimulator(p).Simulate(random.NewSource(seed), fixedBook(3000, 20000, 0, 1.0))
	if len(claims) == 0 {
		t.Fatal("expected claims")
	}
	return claim.NewReopenSimulator(p).Apply(random.NewSource(seed), claims)
}

func TestReopenZeroProbabilityIsANoOp(t *testing.T) {
	p := reopeningParams()
	p.Reopening.Probability = 0
	before := claim.NewClaimSimulator(p).Simulate(random.NewSource(41), fixedBook(1000, 20000, 0, 1.0))
	after := claim.NewReopenSimulator(p).Apply(random.NewSource(41), append([]claim.Claim(nil), before...))
	for i := range after {
		if after[i] != before[i] {
			t.Fatalf("claim %d changed with reopening probability 0", after[i].ID)
		}
		if after[i].Reopened() {
			t.Fatalf("claim %d reopened with probability 0", after[i].ID)
		}
	}
}

func TestReopenDatesAndEstimates(t *testing.T) {
	claims := reopenFixture(t, reopeningParams(), 42)
	reopened := 0
	for _, c := range claims {
		if !c.Reopened() {
			if c.FirstCloseDate != (claim.Claim{}).FirstCloseDate || c.ReopenEstimate != 0 {
				t.Fatalf("claim %d not reopened but carries reopen fields: %+v", c.ID, c)
			}
			continue
		}
		reopened++
		if !c.ReopenDate.After(c.FirstCloseDate) {
			t.Fatalf("claim %d reopen %s not strictly after first close %s", c.ID, c.ReopenDate, c.FirstCloseDate)
		}
		if !c.CloseDate.After(c.ReopenDate) {
			t.Fatalf("claim %d final close %s not strictly after reopen %s", c.ID, c.CloseDate, c.ReopenDate)
		}
		if c.FirstCloseDate.Before(c.ReportDate) {
			t.Fatalf("claim %d first close %s before report %s", c.ID, c.FirstCloseDate, c.ReportDate)
		}
		if c.ReopenEstimate <= 0 {
			t.Fatalf("claim %d reopen estimate %v not positive", c.ID, c.ReopenEstimate)
		}
	}
	if reopened == 0 {
		t.Fatal("fixture produced no reopened claims at probability 0.5")
	}
}

func TestReopenIsDeterministicPerSeed(t *testing.T) {
	a := reopenFixture(t, reopeningParams(), 7)
	b := reopenFixture(t, reopeningParams(), 7)
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("claim %d differs between identical runs", a[i].ID)
		}
	}
}

func TestReopenLeavesNonReopenedClaimsUntouched(t *testing.T) {
	p := reopeningParams()
	base := claim.NewClaimSimulator(p).Simulate(random.NewSource(9), fixedBook(2000, 20000, 0, 1.0))
	applied := claim.NewReopenSimulator(p).Apply(random.NewSource(9), append([]claim.Claim(nil), base...))
	for i := range applied {
		if applied[i].Reopened() {
			continue
		}
		if applied[i] != base[i] {
			t.Fatalf("non-reopened claim %d changed by the reopen pass", applied[i].ID)
		}
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/domain/claim/`
Expected: FAIL to compile with `claim.NewReopenSimulator undefined`.

- [ ] **Step 3: Implement**

In `internal/domain/claim/claim.go`:

Add to the `Claim` struct after `OwnDamage`:

```go
	// FirstCloseDate, ReopenDate and ReopenEstimate describe the single
	// optional reopen episode: the claim closed once, the case was re-raised
	// after a lag, and CloseDate above is the final close. Zero values mean
	// the claim never reopens. Carried to the runoff stage but never written
	// to CSV.
	FirstCloseDate shared.Date
	ReopenDate     shared.Date
	ReopenEstimate shared.Money
```

Add the predicate after the struct:

```go
// Reopened reports whether the claim has a reopen episode.
func (c Claim) Reopened() bool {
	return c.ReopenDate != (shared.Date{})
}
```

Change `drawCloseLag` from a method into a package-level function, and update the one call site:

```go
// drawCloseLag draws a report-to-close (or reopen-to-second-close) delay in
// days: gamma distributed, with the mean stretched for large claims and
// risky policyholders.
func drawCloseLag(src shared.RandomSource, cl lob.CloseLagParams, estimate, riskFactor float64) float64 {
	mean := cl.MeanDays
	if estimate > cl.SizeThreshold {
		mean *= cl.SizeMultiplier
	}
	mean *= math.Pow(riskFactor, cl.RiskLoading)
	return src.Gamma(cl.Shape, mean/cl.Shape)
}
```

and in `simulateClaim` replace `s.drawCloseLag(src, estimate, pol.RiskFactor)` with `drawCloseLag(src, s.params.CloseLag, estimate, pol.RiskFactor)`. The draw order is unchanged, so seeded output is byte-identical.

Create `internal/domain/claim/reopen.go`:

```go
package claim

import (
	"fmt"
	"math"

	"github.com/le-marais/claimsgen/internal/domain/lob"
	"github.com/le-marais/claimsgen/internal/domain/shared"
)

// ReopenSimulator decides which closed claims reopen once. It runs as a
// post-pass after claim IDs are assigned, drawing from a labelled
// sub-stream per claim so that enabling reopening never reshuffles the
// draws of any other stage.
type ReopenSimulator struct {
	params lob.ClaimParams
}

func NewReopenSimulator(p lob.ClaimParams) *ReopenSimulator {
	return &ReopenSimulator{params: p}
}

// Apply mutates reopened claims in place: CloseDate becomes the final
// close and the reopen episode is recorded on the claim. A probability of
// 0 makes no draw at all. Non-reopened claims are returned unchanged.
func (s *ReopenSimulator) Apply(src shared.RandomSource, claims []Claim) []Claim {
	r := s.params.Reopening
	if r.Probability <= 0 {
		return claims
	}
	for i := range claims {
		c := &claims[i]
		stream := src.Split(fmt.Sprintf("reopen-claim-%d", c.ID))
		if !stream.Bernoulli(r.Probability) {
			continue
		}
		lag := int(math.Round(stream.LogNormal(math.Log(r.LagMedianDays), r.LagSigma)))
		if lag < 1 {
			lag = 1 // the reopen is strictly after the first close
		}
		estimate := c.InitialEstimate.MulFloat(r.EstimateFactor * shared.MeanOneLogNormal(stream, r.EstimateSigma))
		if estimate < 1 {
			estimate = 1
		}
		closeLag := int(math.Round(drawCloseLag(stream, s.params.CloseLag, estimate.Dollars(), c.RiskFactor)))
		if closeLag < 1 {
			closeLag = 1 // the second close is strictly after the reopen
		}
		c.FirstCloseDate = c.CloseDate
		c.ReopenDate = c.CloseDate.AddDays(lag)
		c.ReopenEstimate = estimate
		c.CloseDate = c.ReopenDate.AddDays(closeLag)
	}
	return claims
}
```

- [ ] **Step 4: Run the full suite**

Run: `go test ./... && go vet ./...`
Expected: PASS - nothing calls the new simulator yet, and the `drawCloseLag` extraction changes no draws.

- [ ] **Step 5: Commit**

```bash
git add internal/domain/claim/claim.go internal/domain/claim/reopen.go internal/domain/claim/reopen_test.go
git commit -m "Add the reopen post-pass to the claim domain"
```

---

### Task 3: Runoff episodes

Refactor the runoff into an episode function that reproduces today's behavior byte-for-byte, then run a second episode for reopened claims.

**Files:**
- Modify: `internal/domain/transaction/runoff.go`
- Test: `internal/domain/transaction/runoff_test.go` (package `transaction_test`; `params()`, `testClaims`, `byClaim` helpers exist)

**Interfaces:**
- Consumes: `claim.Claim.Reopened()`, `.FirstCloseDate`, `.ReopenDate`, `.ReopenEstimate` (Task 2).
- Produces: unchanged public API (`RunoffSimulator.Simulate`); reopened claims' transactions contain a re-raise `ESTIMATE` on the reopen date and a second episode ending in zero outstanding at the final close.

- [ ] **Step 1: Write the failing tests**

Append to `internal/domain/transaction/runoff_test.go`:

```go
// reopenedClaim builds one claim with a reopen episode.
func reopenedClaim(isNil bool) claim.Claim {
	return claim.Claim{
		ID:              1,
		PolicyID:        1,
		OccurrenceDate:  shared.NewDate(2000, time.January, 1),
		ReportDate:      shared.NewDate(2000, time.January, 5),
		FirstCloseDate:  shared.NewDate(2000, time.June, 1),
		ReopenDate:      shared.NewDate(2000, time.September, 1),
		CloseDate:       shared.NewDate(2001, time.February, 1),
		InitialEstimate: shared.FromDollars(8000),
		ReopenEstimate:  shared.FromDollars(3000),
		RiskFactor:      1.0,
		Nil:             isNil,
	}
}

func TestReopenedClaimRunsTwoEpisodes(t *testing.T) {
	c := reopenedClaim(false)
	txs := transaction.NewRunoffSimulator(params()).Simulate(random.NewSource(11), []claim.Claim{c})

	outstanding := shared.Money(0)
	outstandingAtFirstClose := shared.Money(-1)
	var reopenRow *transaction.Transaction
	for i, tx := range txs {
		if tx.Type == transaction.Estimate {
			outstanding += tx.Amount
		}
		if !tx.Date.After(c.FirstCloseDate) {
			outstandingAtFirstClose = outstanding
		} else if reopenRow == nil {
			reopenRow = &txs[i]
		}
	}
	if outstandingAtFirstClose != 0 {
		t.Fatalf("outstanding at first close = %v, want 0", outstandingAtFirstClose)
	}
	if reopenRow == nil {
		t.Fatal("no transactions after the first close")
	}
	if reopenRow.Type != transaction.Estimate || reopenRow.Amount != c.ReopenEstimate || reopenRow.Date != c.ReopenDate {
		t.Fatalf("re-raise row %+v, want ESTIMATE %v on %s", *reopenRow, c.ReopenEstimate, c.ReopenDate)
	}
	if outstanding != 0 {
		t.Fatalf("outstanding at final close = %v, want 0", outstanding)
	}
	if last := txs[len(txs)-1]; last.Date != c.CloseDate {
		t.Fatalf("last transaction on %s, want final close %s", last.Date, c.CloseDate)
	}
}

func TestReopenedNilClaimPaysOnlyInEpisodeTwo(t *testing.T) {
	c := reopenedClaim(true)
	txs := transaction.NewRunoffSimulator(params()).Simulate(random.NewSource(12), []claim.Claim{c})

	paidBeforeReopen := shared.Money(0)
	paidAfterReopen := shared.Money(0)
	for _, tx := range txs {
		if tx.Type != transaction.Payment {
			continue
		}
		if tx.Date.Before(c.ReopenDate) {
			paidBeforeReopen += tx.Amount
		} else {
			paidAfterReopen += tx.Amount
		}
	}
	if paidBeforeReopen != 0 {
		t.Fatalf("reopened nil claim paid %v before the reopen, want 0", paidBeforeReopen)
	}
	if paidAfterReopen <= 0 {
		t.Fatalf("reopened nil claim paid %v in episode 2, want positive", paidAfterReopen)
	}
}

func TestReopenedClaimRowsChronological(t *testing.T) {
	claims := testClaims(50)
	for i := range claims {
		if i%4 == 0 {
			claims[i].FirstCloseDate = claims[i].CloseDate
			claims[i].ReopenDate = claims[i].CloseDate.AddDays(60)
			claims[i].ReopenEstimate = shared.FromDollars(2000)
			claims[i].CloseDate = claims[i].ReopenDate.AddDays(90)
		}
	}
	txs := transaction.NewRunoffSimulator(params()).Simulate(random.NewSource(13), claims)
	for id, rows := range byClaim(txs) {
		for i := 1; i < len(rows); i++ {
			if rows[i].Date.Before(rows[i-1].Date) {
				t.Fatalf("claim %d rows out of order", id)
			}
		}
	}
}

func TestTinyReopenEstimateStillClosesOnFinalCloseDate(t *testing.T) {
	c := reopenedClaim(false)
	c.ReopenEstimate = shared.Money(2) // two cents over a five-month episode
	for seed := uint64(1); seed <= 25; seed++ {
		txs := transaction.NewRunoffSimulator(params()).Simulate(random.NewSource(seed), []claim.Claim{c})
		outstanding := shared.Money(0)
		for _, tx := range txs {
			if tx.Type == transaction.Estimate {
				outstanding += tx.Amount
			}
		}
		if outstanding != 0 {
			t.Fatalf("seed %d: outstanding at final close = %v, want 0", seed, outstanding)
		}
		if last := txs[len(txs)-1]; last.Date != c.CloseDate {
			t.Fatalf("seed %d: last transaction on %s, want final close %s", seed, last.Date, c.CloseDate)
		}
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/domain/transaction/`
Expected: the new tests FAIL (the runoff runs the whole claim as one episode against `CloseDate`, so outstanding at `FirstCloseDate` is not zero and there is no re-raise row). Existing tests still pass.

- [ ] **Step 3: Refactor into episodes**

In `internal/domain/transaction/runoff.go`, replace `simulateClaim` and `simulateNilClaim` with an episode-based structure. The episode function must preserve today's draw order exactly (ultimate, interim payments, revisions, then revision noise during emission):

```go
func (s *RunoffSimulator) simulateClaim(src shared.RandomSource, c claim.Claim) []Transaction {
	e := &emitter{claimID: c.ID, report: c.ReportDate}
	e.estimate(0, c.InitialEstimate)

	firstClose := c.CloseDate
	if c.Reopened() {
		firstClose = c.FirstCloseDate
	}
	s.runEpisode(src, e, c.ReportDate, firstClose, c.InitialEstimate, c.Nil, false)

	if c.Reopened() {
		// The case is re-raised on the reopen date, then a second, smaller
		// episode develops and pays the reopen estimate.
		e.reviseTo(shared.DaysBetween(c.ReportDate, c.ReopenDate), c.ReopenEstimate)
		s.runEpisode(src, e, c.ReopenDate, c.CloseDate, c.ReopenEstimate, false, true)
	}
	return e.txs
}

// runEpisode develops one open-close episode: interim payments and pure
// revisions between start and close, a final settlement at close, and the
// outstanding case released to exactly zero. A nil episode emits no
// payments. floorRevisions keeps every revision target at least one cent
// (the nil path's guard), used for reopen episodes whose opening estimates
// can be tiny.
func (s *RunoffSimulator) runEpisode(src shared.RandomSource, e *emitter, start, close shared.Date, opening shared.Money, isNil, floorRevisions bool) {
	base := shared.DaysBetween(e.report, start)
	duration := shared.DaysBetween(start, close)
	years := float64(duration) / 365

	if isNil {
		revisions := s.drawRevisions(src, duration, years)
		sort.SliceStable(revisions, func(i, j int) bool {
			return revisions[i].offset < revisions[j].offset
		})
		for _, ev := range revisions {
			remaining := e.outstanding.Dollars()
			sigma := s.params.RevisionSigma * (1 - float64(ev.offset)/float64(duration))
			target := shared.FromDollars(remaining * shared.MeanOneLogNormal(src, sigma))
			if target < 1 {
				target = 1 // keep the case open so the terminal release lands on the close date
			}
			e.reviseTo(base+ev.offset, target)
		}
		e.reviseTo(base+duration, 0)
		return
	}

	ultimate := s.drawUltimate(src, opening)
	interims := s.drawInterimPayments(src, ultimate, duration, years)
	events := append(s.drawRevisions(src, duration, years), interims...)
	sort.SliceStable(events, func(i, j int) bool {
		if events[i].offset != events[j].offset {
			return events[i].offset < events[j].offset
		}
		return events[i].kind < events[j].kind
	})

	paid := shared.Money(0)
	for _, ev := range events {
		if ev.kind == 1 {
			e.pay(base+ev.offset, ev.amount)
			paid += ev.amount
			continue
		}
		remaining := (ultimate - paid).Dollars()
		sigma := s.params.RevisionSigma * (1 - float64(ev.offset)/float64(duration))
		target := shared.FromDollars(remaining * shared.MeanOneLogNormal(src, sigma))
		if floorRevisions && target < 1 {
			target = 1
		}
		e.reviseTo(base+ev.offset, target)
	}

	// Final settlement clears the remaining ultimate, then the case snaps
	// to exactly zero.
	e.pay(base+duration, ultimate-paid)
	e.reviseTo(base+duration, 0)
}
```

Delete the old `simulateNilClaim`. Keep `drawUltimate`, `drawInterimPayments`, `drawRevisions`, `interiorOffset`, and the `emitter` unchanged. Update the package doc comment's runoff description to mention episodes (one sentence).

Note the exact-preservation requirements for a non-reopened claim (`base` is 0, one episode): the emitted rows and the draw sequence are identical to today's code - the first `estimate` row is emitted in `simulateClaim` before the episode, matching the old flow.

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/domain/transaction/` then `go test ./... && go vet ./...`
Expected: all PASS, including every pre-existing runoff, recovery, application, and CLI determinism test - which proves the refactor is byte-neutral for non-reopened claims.

- [ ] **Step 5: Commit**

```bash
git add internal/domain/transaction/runoff.go internal/domain/transaction/runoff_test.go
git commit -m "Run the case estimate runoff in episodes and reopen claims"
```

---

### Task 4: Wire reopening into GenerateDataset, relax recovery eligibility, extend the invariant sweep

**Files:**
- Modify: `internal/application/generate.go`
- Modify: `internal/domain/transaction/recovery.go` (one-line eligibility relaxation + comment)
- Modify: `internal/application/invariants_test.go`
- Create: `internal/application/reopening_test.go`

**Interfaces:**
- Consumes: `claim.NewReopenSimulator(req.LOB.Claims).Apply(src.Split("reopening"), claims)` (Task 2), episode runoff (Task 3).
- Produces: datasets with reopened claims; Tasks 5-6 consume them.

- [ ] **Step 1: Write the failing end-to-end tests**

Create `internal/application/reopening_test.go`:

```go
package application_test

import (
	"testing"

	"github.com/le-marais/claimsgen/internal/application"
	"github.com/le-marais/claimsgen/internal/domain/shared"
	"github.com/le-marais/claimsgen/internal/domain/transaction"
	"github.com/le-marais/claimsgen/internal/infrastructure/random"
)

// TestReopeningProbabilityZeroMeansOneRelease is the spec's output-level
// off-switch check: with reopening off, once a claim's outstanding case
// reaches zero it stays there - no case activity ever follows a release.
func TestReopeningProbabilityZeroMeansOneRelease(t *testing.T) {
	req := request(t)
	req.LOB.Claims.Reopening.Probability = 0
	ds, err := application.GenerateDataset(random.NewSource(17), req)
	if err != nil {
		t.Fatal(err)
	}
	outstanding := map[int]shared.Money{}
	released := map[int]bool{}
	for _, tx := range ds.Transactions {
		if tx.Type.IsRecovery() {
			continue
		}
		if released[tx.ClaimID] {
			t.Fatalf("claim %d has case activity after its release to zero with reopening off", tx.ClaimID)
		}
		if tx.Type == transaction.Estimate {
			outstanding[tx.ClaimID] += tx.Amount
			if outstanding[tx.ClaimID] == 0 {
				released[tx.ClaimID] = true
			}
		}
	}
	for _, c := range ds.Claims {
		if c.Reopened() {
			t.Fatalf("claim %d reopened with probability 0", c.ID)
		}
	}
}

// TestDefaultPresetGeneratesReopenedClaims proves the feature is on by
// default, including the nil-reopen pattern.
func TestDefaultPresetGeneratesReopenedClaims(t *testing.T) {
	req := request(t)
	req.InitialBookSize = 2000
	ds, err := application.GenerateDataset(random.NewSource(18), req)
	if err != nil {
		t.Fatal(err)
	}
	reopened, nilReopened := 0, 0
	for _, c := range ds.Claims {
		if c.Reopened() {
			reopened++
			if c.Nil {
				nilReopened++
			}
		}
	}
	if reopened == 0 {
		t.Fatal("default preset generated no reopened claims")
	}
	if nilReopened == 0 {
		t.Fatal("default preset generated no reopened nil claims (expected some at 8% nil x 4% reopen with 2000 policies x 3 years)")
	}
}

// TestReopeningDoesNotShiftOtherStages is the spec's sub-stream
// independence check: enabling reopening leaves the book and every
// non-reopened claim (and its transactions) byte-identical.
func TestReopeningDoesNotShiftOtherStages(t *testing.T) {
	off := request(t)
	off.LOB.Claims.Reopening.Probability = 0
	dsOff, err := application.GenerateDataset(random.NewSource(19), off)
	if err != nil {
		t.Fatal(err)
	}
	dsOn, err := application.GenerateDataset(random.NewSource(19), request(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(dsOn.Policies) != len(dsOff.Policies) || len(dsOn.Claims) != len(dsOff.Claims) {
		t.Fatalf("book or claim count changed: %d/%d policies, %d/%d claims",
			len(dsOn.Policies), len(dsOff.Policies), len(dsOn.Claims), len(dsOff.Claims))
	}
	reopened := map[int]bool{}
	for i := range dsOn.Claims {
		if dsOn.Claims[i].Reopened() {
			reopened[dsOn.Claims[i].ID] = true
			continue
		}
		if dsOn.Claims[i] != dsOff.Claims[i] {
			t.Fatalf("non-reopened claim %d differs with reopening on", dsOn.Claims[i].ID)
		}
	}
	if len(reopened) == 0 {
		t.Fatal("expected reopened claims with the default preset")
	}
	byClaimOn := map[int][]transaction.Transaction{}
	for _, tx := range dsOn.Transactions {
		byClaimOn[tx.ClaimID] = append(byClaimOn[tx.ClaimID], tx)
	}
	byClaimOff := map[int][]transaction.Transaction{}
	for _, tx := range dsOff.Transactions {
		byClaimOff[tx.ClaimID] = append(byClaimOff[tx.ClaimID], tx)
	}
	for id, offRows := range byClaimOff {
		if reopened[id] {
			continue
		}
		onRows := byClaimOn[id]
		if len(onRows) != len(offRows) {
			t.Fatalf("non-reopened claim %d has %d rows with reopening on, %d off", id, len(onRows), len(offRows))
		}
		for i := range onRows {
			a, b := onRows[i], offRows[i]
			a.ID, b.ID = 0, 0 // IDs shift because reopened claims add rows
			if a != b {
				t.Fatalf("non-reopened claim %d row %d differs with reopening on: %+v vs %+v", id, i, a, b)
			}
		}
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/application/ -run 'TestReopening|TestDefaultPresetGeneratesReopened'`
Expected: `TestReopeningProbabilityZeroMeansOneRelease` PASSES trivially (nothing reopens yet), `TestDefaultPresetGeneratesReopenedClaims` FAILS with "no reopened claims", and `TestReopeningDoesNotShiftOtherStages` FAILS at "expected reopened claims with the default preset".

Known edge in the off-switch test: in principle a mid-life revision can round the outstanding case to exactly zero (the sub-cent edge the nil runoff guards against), which would trip "case activity after its release to zero" without any reopen. If that happens on this seed, report it and tighten the check to releases at the claim's final case row only - do not delete the test.

- [ ] **Step 3: Wire the stage in**

In `internal/application/generate.go`, after the claim simulation and before the runoff:

```go
	claims := claim.NewClaimSimulator(req.LOB.Claims).
		WithInflation(inflation).
		Simulate(src.Split("claims"), book)
	claims = claim.NewReopenSimulator(req.LOB.Claims).
		Apply(src.Split("reopening"), claims)
```

- [ ] **Step 4: Relax recovery eligibility**

In `internal/domain/transaction/recovery.go`, `simulateClaim` currently gates:

```go
	if !c.OwnDamage || c.Nil || paid <= 0 {
		return nil
	}
```

Replace with:

```go
	if !c.OwnDamage || paid <= 0 {
		return nil
	}
```

and extend the function's doc comment: a nil claim that never reopens has paid 0 and stays ineligible through the paid check; a reopened nil claim that paid is recovery-eligible like any other paying own-damage claim.

Existing recovery tests keep passing: their nil fixtures never pay, so the paid check excludes them exactly as before.

- [ ] **Step 5: Extend the invariant sweep**

In `internal/application/invariants_test.go`:

(a) Extend `claimInfo` and its population:

```go
	type claimInfo struct {
		report, close shared.Date
		firstClose    shared.Date
		reopen        shared.Date
		reopened      bool
		isNil, ownDmg bool
	}
	claims := map[int]claimInfo{}
	for _, c := range ds.Claims {
		// ... existing policy/date/estimate checks unchanged ...
		if c.Reopened() {
			if !c.ReopenDate.After(c.FirstCloseDate) {
				t.Fatalf("claim %d reopen %s not strictly after first close %s", c.ID, c.ReopenDate, c.FirstCloseDate)
			}
			if !c.CloseDate.After(c.ReopenDate) {
				t.Fatalf("claim %d final close %s not strictly after reopen %s", c.ID, c.CloseDate, c.ReopenDate)
			}
			if c.ReopenEstimate <= 0 {
				t.Fatalf("claim %d reopen estimate %v not positive", c.ID, c.ReopenEstimate)
			}
			if c.FirstCloseDate.Before(c.ReportDate) {
				t.Fatalf("claim %d first close %s before report %s", c.ID, c.FirstCloseDate, c.ReportDate)
			}
		}
		firstClose := c.CloseDate
		if c.Reopened() {
			firstClose = c.FirstCloseDate
		}
		claims[c.ID] = claimInfo{c.ReportDate, c.CloseDate, firstClose, c.ReopenDate, c.Reopened(), c.Nil, c.OwnDamage}
	}
```

(b) In the `state` struct add `afterReopen bool`, and in the transaction loop, immediately after the window check (before the type switch), enforce the first-close release and the re-raise row:

```go
		s := perClaim[tx.ClaimID]
		if s == nil {
			s = &state{first: tx}
			perClaim[tx.ClaimID] = s
		}
		if !tx.Type.IsRecovery() && c.reopened && !s.afterReopen && tx.Date.After(c.firstClose) {
			if s.outstanding != 0 {
				t.Fatalf("claim %d outstanding at first close = %v, want 0", tx.ClaimID, s.outstanding)
			}
			if tx.Type != transaction.Estimate || tx.Amount <= 0 || tx.Date != c.reopen {
				t.Fatalf("claim %d first row after first close %+v is not a positive re-raise on the reopen date %s", tx.ClaimID, tx, c.reopen)
			}
			s.afterReopen = true
		}
		if c.isNil && tx.Type == transaction.Payment && !tx.Date.After(c.firstClose) {
			t.Fatalf("nil claim %d paid %v on %s, before its first close %s", tx.ClaimID, tx.Amount, tx.Date, c.firstClose)
		}
```

(c) In the type switch, the recovery case drops the nil condition:

```go
		case transaction.Salvage, transaction.Subrogation:
			if tx.Amount <= 0 {
				t.Fatalf("transaction %d recovery amount %v not positive", tx.ID, tx.Amount)
			}
			if !c.ownDmg {
				t.Fatalf("recovery %d on non-own-damage claim %d", tx.ID, tx.ClaimID)
			}
			s.recovered += tx.Amount
```

(d) In the final per-claim loop, the nil check becomes reopen-aware and reopened claims must have seen their re-raise:

```go
		if c.Nil && !c.Reopened() {
			if s.paid != 0 {
				t.Fatalf("nil claim %d total paid %v, want 0", c.ID, s.paid)
			}
		} else if s.paid <= 0 {
			t.Fatalf("claim %d total paid %v not positive", c.ID, s.paid)
		}
		if c.Reopened() && !s.afterReopen {
			t.Fatalf("reopened claim %d has no transactions after its first close", c.ID)
		}
```

Everything else (first row, outstanding never negative, zero at final close, recovered below paid, last case activity on `CloseDate`) is already correct because `c.close`/`c.CloseDate` is the final close.

- [ ] **Step 6: Run the suite**

Run: `go test ./... -skip TestDefaultPresetIsRealistic && go vet ./...`
Expected: PASS. Then run the gate separately (`go test ./internal/application/ -run TestDefaultPresetIsRealistic -count=1`) and record the result in your report; it may fail (reopens add paid and incurred), which Task 7 resolves - do not modify the preset here.

- [ ] **Step 7: Commit**

```bash
git add internal/application/generate.go internal/domain/transaction/recovery.go internal/application/invariants_test.go internal/application/reopening_test.go
git commit -m "Generate reopened claims in the dataset"
```

---

### Task 5: Reopened count in the summary

**Files:**
- Modify: `internal/application/summary.go`
- Test: `internal/application/summary_test.go`

**Interfaces:**
- Consumes: `claim.Claim.Reopened()` (Task 2).
- Produces: `application.YearSummary.Reopened int` - reopened claims by occurrence year - plus the grand total. Task 6 surfaces it.

- [ ] **Step 1: Write the failing test**

Append to `internal/application/summary_test.go`:

```go
func TestSummarizeCountsReopenedByOccurrenceYear(t *testing.T) {
	ds := tinyDataset()
	// Mark claim 1 (occurred 1998) as reopened.
	ds.Claims[0].FirstCloseDate = ds.Claims[0].CloseDate
	ds.Claims[0].ReopenDate = shared.NewDate(1999, time.February, 1)
	ds.Claims[0].ReopenEstimate = shared.FromDollars(500)
	ds.Claims[0].CloseDate = shared.NewDate(1999, time.June, 1)
	got := application.Summarize(ds, 1998, 2)
	if got.Years[0].Reopened != 1 {
		t.Errorf("1998 reopened = %d, want 1", got.Years[0].Reopened)
	}
	if got.Years[1].Reopened != 0 {
		t.Errorf("1999 reopened = %d, want 0 (booked by occurrence year)", got.Years[1].Reopened)
	}
	if got.Total.Reopened != 1 {
		t.Errorf("total reopened = %d, want 1", got.Total.Reopened)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/application/ -run TestSummarizeCountsReopened`
Expected: FAIL to compile with `.Reopened undefined`.

- [ ] **Step 3: Implement**

In `internal/application/summary.go`: add to `YearSummary` after `NilClaims`:

```go
	// Reopened counts claims with a reopen episode, by occurrence year.
	Reopened int
```

In the claims loop of `Summarize`, after the `NilClaims` increment:

```go
			if c.Reopened() {
				rows[i].Reopened++
			}
```

In the total accumulation: `total.Reopened += r.Reopened`. Update the `YearSummary` doc comment: `NilClaims` counts claims closed without payment at their first close.

- [ ] **Step 4: Run the suite**

Run: `go test ./... -skip TestDefaultPresetIsRealistic && go vet ./...`
Expected: PASS (`TestSummarize`'s DeepEqual fixture has zero reopens).

- [ ] **Step 5: Commit**

```bash
git add internal/application/summary.go internal/application/summary_test.go
git commit -m "Count reopened claims per year in the summary"
```

---

### Task 6: Web API and UI - reopening fields and the Reopened column

**Files:**
- Modify: `internal/infrastructure/web/viewmodel.go`
- Modify: `internal/infrastructure/web/static/app.js`
- Test: `internal/infrastructure/web/server_test.go` (package `web`, internal)

**Interfaces:**
- Consumes: `application.YearSummary.Reopened` (Task 5); preset JSON already carries `claims.reopening.*` through the DTOs (Task 1).
- Produces: generate response JSON with `summary.years[].reopened` and `summary.total.reopened`.

- [ ] **Step 1: Write the failing test**

In `internal/infrastructure/web/server_test.go`, next to the existing assertions on `resp.Summary.Total.NilClaims` and `resp.Summary.Total.Recovered`, add:

```go
	if resp.Summary.Total.Reopened <= 0 {
		t.Fatalf("total reopened = %d, want positive with the default preset", resp.Summary.Total.Reopened)
	}
```

Note: that test's fixture is small, and reopens are rarer than nils (4%). The result is deterministic per seed - if the count is genuinely 0 for the fixture's seed, raise the fixture's `initial_book_size` (it is part of the request body in that test) until it is positive, and say so in your report; do not weaken the assertion.

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/infrastructure/web/`
Expected: FAIL to compile with `resp.Summary.Total.Reopened undefined`.

- [ ] **Step 3: Implement the view model**

In `internal/infrastructure/web/viewmodel.go`: add to `summaryRowJSON` after `NilClaims`:

```go
	Reopened int `json:"reopened"`
```

and set `Reopened: s.Reopened,` in `summaryRowView` after `NilClaims: s.NilClaims,`.

- [ ] **Step 4: Update the static UI**

In `internal/infrastructure/web/static/app.js`:

(a) In the `Claims` field group of `FIELD_GROUPS`, after the `nil_probability` entry:

```js
      { path: ["claims", "reopening", "probability"], label: "Reopen probability", tip: "Chance a closed claim reopens once; 0 switches reopening off." },
      { path: ["claims", "reopening", "estimate_factor"], label: "Reopen estimate factor", tip: "Mean reopen case estimate as a factor of the original initial estimate." },
```

(b) In `renderSummary`, the header array becomes:

```js
  for (const label of ["Year", "Policies", "Claims", "Nil claims", "Reopened", "Earned premium", "Ultimate (paid)", "Recovered", "Loss ratio"]) {
```

(c) In `summaryRow`, insert after the `nil_claims` cell:

```js
    fmtInt.format(row.reopened),
```

(so the cells array order is: policies, claims, nil_claims, reopened, earned_premium, paid, recovered, loss_ratio - 8 data cells + the label column = 9 columns, matching the header).

- [ ] **Step 5: Verify headlessly and run the suite**

Run `node --check internal/infrastructure/web/static/app.js`. Then `go test ./... -skip TestDefaultPresetIsRealistic && go vet ./...` - PASS. Optionally start `go run ./cmd/claimsgen ui --port 8094`, POST a generate request via curl using the preset params, and confirm `summary.total.reopened > 0`; kill the server afterwards.

- [ ] **Step 6: Commit**

```bash
git add internal/infrastructure/web/viewmodel.go internal/infrastructure/web/static/app.js internal/infrastructure/web/server_test.go
git commit -m "Expose reopened claims in the web UI"
```

---

### Task 7: Realism gate check and calibration

**Files:**
- Modify (only if needed): `internal/infrastructure/config/motor-personal.yaml`

- [ ] **Step 1: Measure**

Run: `go test ./internal/application/ -run TestDefaultPresetIsRealistic -v -count=1`

If it passes, run the full suite with no `-skip` (`go test ./... && go vet ./...`) and this task is done with no commit (note that in the report).

- [ ] **Step 2: Adjust if it fails**

Reopens add roughly `probability x estimate_factor` (~2%) to total ultimate, landing at late development ages. Iterate one change at a time, re-running the gate after each:

- Loss ratio above the band: raise `book.premium_rate_factor` slightly (0.035 upward) or trim `reopening.estimate_factor` toward 0.35.
- Late paid or incurred ATA factors above their bands: shorten `reopening.lag_median_days` (90 toward 60) so episode 2 lands earlier, or trim `reopening.probability` toward 0.03.
- Keep the reopening knobs inside the spec's target shape: probability 0.03-0.06, estimate factor 0.3-0.6, lag median 60-120 days.

- [ ] **Step 3: Verify and commit (only if the preset changed)**

Run: `go test ./... && go vet ./...` - all PASS with no `-skip`.

```bash
git add internal/infrastructure/config/motor-personal.yaml
git commit -m "Calibrate the motor preset for reopened claims"
```

---

### Task 8: Documentation and the screenshot script

**Files:**
- Modify: `README.md`
- Modify: `docs/roadmap.md`
- Modify: `docs/mission.md`
- Create: `tools/screenshots/screenshots.js`
- Create: `tools/screenshots/package.json`
- Modify: `docs/screenshots/*.png` (regenerated)

- [ ] **Step 1: README**

Style rules: sentence case headers, spaced hyphens ` - `, never em dashes, no invented claims.

- In "How the simulation works", add a step 6:

```markdown
6. **Reopened claims** - a closed claim can reopen once: the case is re-raised a lognormal lag after the first close, a second episode develops and pays an additional amount, and the claim closes for good. The reopen estimate is a configurable factor of the original initial estimate, and a nil claim that reopens pays in its second episode. claims.csv shows the final close date; the reopen is visible in transactions as the case re-raised after a release to zero. Setting the reopen probability to 0 switches reopening off.
```

- In step 2, change "A share of reported claims are nil - they close without any payment." to "A share of reported claims are nil - they close without any payment at their first close."
- In step 4, change "Recovery rows are the only transactions dated after a claim's close date." to "Recovery rows are the only transactions dated after a claim's final close date."
- In "Browser UI", extend the summary description to mention the Reopened column and the two reopening parameters in the form.

- [ ] **Step 2: Roadmap and mission**

`docs/roadmap.md`:
- Add to Shipped: `- **Reopened claims** - a closed claim can reopen once and develop a second episode; claims.csv shows the final close date and the reopen appears in transactions as a case re-raised after a release to zero, with a reopen_probability off switch.`
- The near-term section is now empty: replace it with a short note that the real-claims-data backlog is complete, pointing at the mid-term second line of business as the next candidate.

`docs/mission.md`: mark the backlog line `- Reopened claims - done`.

- [ ] **Step 3: Screenshot script**

Create `tools/screenshots/package.json`:

```json
{
  "name": "claimsgen-screenshots",
  "private": true,
  "description": "Regenerates the README UI screenshots. Usage: start `claimsgen ui --port 8093`, then `npm install && node screenshots.js`.",
  "dependencies": {
    "puppeteer-core": "^24.0.0"
  }
}
```

Create `tools/screenshots/screenshots.js`:

```js
"use strict";
// Regenerates the README UI screenshots against a running claimsgen UI.
// Usage: `claimsgen ui --port 8093` in one shell, then `npm install &&
// node screenshots.js` here. Requires a local Chrome install.
const puppeteer = require("puppeteer-core");
const path = require("path");

const OUT = path.join(__dirname, "..", "..", "docs", "screenshots");
const URL = process.env.CLAIMSGEN_URL || "http://127.0.0.1:8093";
const CHROME = process.env.CHROME_PATH || "C:\\Program Files\\Google\\Chrome\\Application\\chrome.exe";

async function generateAndWait(page) {
  await page.click("#generate-btn");
  await page.waitForFunction(
    () => !document.querySelector("#generate-btn").disabled &&
          !document.querySelector("#results").hidden &&
          document.querySelector("#error-banner").hidden,
    { timeout: 120000 }
  );
}

async function shootResults(page, file) {
  const el = await page.$("main.results");
  await el.screenshot({ path: path.join(OUT, file) });
}

async function selectTab(page, tab) {
  await page.click(`#tabs button[data-tab="${tab}"]`);
  await new Promise((r) => setTimeout(r, 300));
}

(async () => {
  const browser = await puppeteer.launch({
    executablePath: CHROME,
    headless: "new",
    args: ["--force-device-scale-factor=1"],
  });
  const page = await browser.newPage();
  await page.setViewport({ width: 1384, height: 905, deviceScaleFactor: 1 });
  await page.goto(URL, { waitUntil: "networkidle0" });
  await page.waitForFunction(() => document.querySelectorAll("#lob-select option").length > 0);

  await generateAndWait(page);
  await new Promise((r) => setTimeout(r, 300));
  await page.screenshot({ path: path.join(OUT, "ui-summary.png") });

  await selectTab(page, "triangles");
  await shootResults(page, "ui-triangles.png");

  await selectTab(page, "distributions");
  await shootResults(page, "ui-distributions.png");

  await selectTab(page, "realism");
  await shootResults(page, "ui-realism-pass.png");

  // Failing run for the README: base frequency 0.5 pushes the loss ratio
  // outside its band.
  await page.evaluate(() => {
    const input = [...document.querySelectorAll("#params-form input[data-path]")]
      .find((i) => i.dataset.path === JSON.stringify(["claims", "base_frequency"]));
    input.value = "0.5";
    input.dispatchEvent(new Event("input", { bubbles: true }));
    input.dispatchEvent(new Event("change", { bubbles: true }));
  });
  await generateAndWait(page);
  await selectTab(page, "realism");
  await shootResults(page, "ui-realism-fail.png");

  await browser.close();
  console.log("done");
})().catch((e) => { console.error(e); process.exit(1); });
```

Add a line to README's Development section: screenshots are regenerated with `tools/screenshots` (start the UI on port 8093, `npm install`, `node screenshots.js`).

- [ ] **Step 4: Regenerate the screenshots**

```powershell
go build -o "$env:TEMP\claimsgen-ui.exe" ./cmd/claimsgen
Start-Process -FilePath "$env:TEMP\claimsgen-ui.exe" -ArgumentList 'ui','--port','8093' -WindowStyle Hidden -PassThru
cd tools/screenshots; npm install; node screenshots.js
# stop the claimsgen-ui.exe process afterwards
```

Verify each regenerated PNG (view them): the summary shows the Reopened column with positive counts, and the sidebar form is unchanged in structure. `git status` should show only the five PNGs, the three docs, and the two new tools files.

- [ ] **Step 5: Final check and commit**

Re-read the edited docs for style (sentence case, spaced hyphens, no invented claims). Run `go test ./... && go vet ./...` one last time.

```bash
git add README.md docs/roadmap.md docs/mission.md docs/screenshots tools/screenshots
git commit -m "Document reopened claims and add the screenshot script"
```
