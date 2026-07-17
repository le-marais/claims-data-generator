# Recoveries (salvage and subrogation) implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add salvage and subrogation recoveries - money coming back on own-damage claims after close - as two new transaction types, with net-of-recoveries triangles, UI support, and a recalibrated motor preset.

**Architecture:** Recoveries are pure cash events drawn in their own per-claim sub-stream after the runoff stage: the case estimate stays gross and every existing runoff invariant is untouched. A `RecoverySimulator` in the transaction domain appends at most one `SALVAGE` and one `SUBROGATION` row per eligible claim (own-damage, non-nil), dated after the close date. The triangle domain gains net paid aggregation; the realism gate compares net paid against Schedule P (which is net of salvage and subrogation).

**Tech Stack:** Go (stdlib + gonum/stat/distuv + yaml.v3, all already in go.mod), vanilla JS UI.

Spec: `docs/superpowers/specs/2026-07-17-recoveries-design.md`

## Global constraints

- Strict YAML decoding (`KnownFields(true)`): old config files missing the new `claims.recoveries` block must fail validation; the embedded motor preset is updated in the same change that requires the keys.
- Money is `shared.Money`, whole cents (int64). Amount comparisons in invariants are exact cent arithmetic.
- Same seed + config = byte-identical CSVs. New draws must come from new labelled sub-streams (`recovery-claim-<id>`) so existing stages never reshuffle.
- `claims.csv` and `policies.csv` schemas are unchanged. `transactions.csv` shape is unchanged; only the `type` column gains the values `SALVAGE` and `SUBROGATION`.
- Probability 0 switches a recovery type off; this must be stated in YAML comments and UI tooltips, and proven from output by a test.
- Run `go test ./...` and `go vet ./...` before every commit. `TestDefaultPresetIsRealistic` is expected to fail from Task 6 until the calibration task (Task 10) - note this in commit messages when it applies, and run the rest of the suite with `go test ./... -skip TestDefaultPresetIsRealistic` in between.
- Commit messages: plain imperative sentences, no type prefixes (match `git log`: "Add off-switch test, floor nil revisions, harden config test").
- Docs use sentence case headers and never use em dashes (use spaced hyphens ` - `).

---

### Task 1: Beta draw on RandomSource

Recovery shares are Beta-distributed (naturally bounded in (0, 1)). The domain interface gains a `Beta` method, implemented with gonum.

**Files:**
- Modify: `internal/domain/shared/random.go`
- Modify: `internal/infrastructure/random/source.go`
- Test: `internal/infrastructure/random/source_test.go` (package `random`, internal)

**Interfaces:**
- Produces: `Beta(alpha, beta float64) float64` on `shared.RandomSource`, used by Task 5 as `src.Beta(mean*conc, (1-mean)*conc)`.

- [ ] **Step 1: Write the failing test**

Append to `internal/infrastructure/random/source_test.go`:

```go
func TestBetaMeanAndBounds(t *testing.T) {
	src := NewSource(9)
	const n = 20000
	sum := 0.0
	for i := 0; i < n; i++ {
		v := src.Beta(2, 8)
		if v <= 0 || v >= 1 {
			t.Fatalf("draw %d: Beta(2, 8) = %v, want strictly in (0, 1)", i, v)
		}
		sum += v
	}
	if mean := sum / n; math.Abs(mean-0.2) > 0.01 {
		t.Errorf("mean of Beta(2, 8) draws = %v, want ~0.2", mean)
	}
}

func TestBetaIsDeterministicPerSeed(t *testing.T) {
	a, b := NewSource(11), NewSource(11)
	for i := 0; i < 50; i++ {
		if a.Beta(3, 5) != b.Beta(3, 5) {
			t.Fatalf("draw %d differs for identical seeds", i)
		}
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/infrastructure/random/`
Expected: FAIL with `src.Beta undefined`.

- [ ] **Step 3: Implement**

In `internal/domain/shared/random.go`, add to the `RandomSource` interface after `Pareto`:

```go
	Beta(alpha, beta float64) float64
```

In `internal/infrastructure/random/source.go`, add after the `Pareto` method:

```go
// Beta draws from a Beta distribution with shape parameters alpha and beta;
// values lie strictly in (0, 1).
func (s *Source) Beta(alpha, beta float64) float64 {
	return distuv.Beta{Alpha: alpha, Beta: beta, Src: s.rng}.Rand()
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./... && go vet ./...`
Expected: PASS everywhere (only one implementation of `RandomSource` exists; all domain tests use it).

- [ ] **Step 5: Commit**

```bash
git add internal/domain/shared/random.go internal/infrastructure/random/source.go internal/infrastructure/random/source_test.go
git commit -m "Add a Beta draw to the random source"
```

---

### Task 2: Recovery parameters in the lob domain

**Files:**
- Modify: `internal/domain/lob/lob.go`
- Test: `internal/domain/lob/lob_test.go` (package `lob`, internal)

**Interfaces:**
- Produces: `lob.RecoveryParams{Salvage, Subrogation lob.RecoveryTypeParams}` and `lob.RecoveryTypeParams{Probability, MeanShare, Concentration, LagMedianDays, LagSigma float64}`, reachable as `lob.ClaimParams.Recoveries`. Validation error prefixes: `claims.recoveries.salvage.*` and `claims.recoveries.subrogation.*`.

- [ ] **Step 1: Write the failing tests**

In `internal/domain/lob/lob_test.go`, extend the `validMotor()` fixture - inside the `Claims: ClaimParams{...}` literal, after `Inflation: InflationParams{Mean: 1.0, Volatility: 0.0},` add:

```go
			Recoveries: RecoveryParams{
				Salvage:     RecoveryTypeParams{Probability: 0.1, MeanShare: 0.15, Concentration: 10, LagMedianDays: 21, LagSigma: 0.5},
				Subrogation: RecoveryTypeParams{Probability: 0.2, MeanShare: 0.8, Concentration: 10, LagMedianDays: 180, LagSigma: 0.7},
			},
```

Add these cases to the `TestValidationNamesTheOffendingField` table:

```go
		{"claims.recoveries.salvage.probability", func(l *LineOfBusiness) { l.Claims.Recoveries.Salvage.Probability = 1.0 }},
		{"claims.recoveries.salvage.probability", func(l *LineOfBusiness) { l.Claims.Recoveries.Salvage.Probability = -0.1 }},
		{"claims.recoveries.salvage.mean_share", func(l *LineOfBusiness) { l.Claims.Recoveries.Salvage.MeanShare = 0 }},
		{"claims.recoveries.salvage.mean_share", func(l *LineOfBusiness) { l.Claims.Recoveries.Salvage.MeanShare = 1.0 }},
		{"claims.recoveries.salvage.concentration", func(l *LineOfBusiness) { l.Claims.Recoveries.Salvage.Concentration = 0 }},
		{"claims.recoveries.salvage.lag_median_days", func(l *LineOfBusiness) { l.Claims.Recoveries.Salvage.LagMedianDays = 0 }},
		{"claims.recoveries.salvage.lag_sigma", func(l *LineOfBusiness) { l.Claims.Recoveries.Salvage.LagSigma = -0.1 }},
		{"claims.recoveries.subrogation.probability", func(l *LineOfBusiness) { l.Claims.Recoveries.Subrogation.Probability = 1.5 }},
		{"claims.recoveries.subrogation.mean_share", func(l *LineOfBusiness) { l.Claims.Recoveries.Subrogation.MeanShare = -0.2 }},
```

And a new test function:

```go
func TestValidateAcceptsZeroRecoveryProbabilities(t *testing.T) {
	l := validMotor()
	l.Claims.Recoveries.Salvage.Probability = 0
	l.Claims.Recoveries.Subrogation.Probability = 0
	if err := l.Validate(); err != nil {
		t.Fatalf("zero recovery probabilities (the off switch): want nil, got %v", err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/domain/lob/`
Expected: FAIL to compile with `unknown field Recoveries`.

- [ ] **Step 3: Implement**

In `internal/domain/lob/lob.go`:

Add to `ClaimParams` after `NilProbability`:

```go
	// Recoveries drives salvage and subrogation: money coming back on
	// own-damage claims after they close.
	Recoveries RecoveryParams
```

Add the new types after `InflationParams`:

```go
// RecoveryParams drives salvage (selling the insured vehicle's wreck) and
// subrogation (recovering the payout from an at-fault third party). Both
// attach only to own-damage claims that paid something, as money-in
// transactions dated after the close.
type RecoveryParams struct {
	Salvage     RecoveryTypeParams
	Subrogation RecoveryTypeParams
}

// RecoveryTypeParams parameterizes one recovery type.
type RecoveryTypeParams struct {
	// Probability is the chance an own-damage claim yields this recovery;
	// 0 switches the type off.
	Probability float64
	// MeanShare is the average recovery as a share of the claim's gross paid.
	MeanShare float64
	// Concentration is the Beta concentration of the share draw; higher
	// means shares cluster tighter around MeanShare.
	Concentration float64
	// LagMedianDays is the median days from close to receiving the money.
	LagMedianDays float64
	// LagSigma is the sigma of the lognormal close-to-receipt lag.
	LagSigma float64
}
```

In `ClaimParams.validate()`, before the final `return c.CloseLag.validate()`:

```go
	if err := c.Recoveries.Salvage.validate("claims.recoveries.salvage"); err != nil {
		return err
	}
	if err := c.Recoveries.Subrogation.validate("claims.recoveries.subrogation"); err != nil {
		return err
	}
```

Add the validation method after `InflationParams.validate()`:

```go
func (r RecoveryTypeParams) validate(prefix string) error {
	if r.Probability < 0 || r.Probability >= 1 {
		return fmt.Errorf("%s.probability: must be in [0, 1), got %v", prefix, r.Probability)
	}
	if r.MeanShare <= 0 || r.MeanShare >= 1 {
		return fmt.Errorf("%s.mean_share: must be in (0, 1), got %v", prefix, r.MeanShare)
	}
	if r.Concentration <= 0 {
		return fmt.Errorf("%s.concentration: must be positive, got %v", prefix, r.Concentration)
	}
	if r.LagMedianDays <= 0 {
		return fmt.Errorf("%s.lag_median_days: must be positive, got %v", prefix, r.LagMedianDays)
	}
	if r.LagSigma < 0 {
		return fmt.Errorf("%s.lag_sigma: must not be negative, got %v", prefix, r.LagSigma)
	}
	return nil
}
```

- [ ] **Step 4: Run the package tests, not the full suite**

Run: `go test ./internal/domain/lob/`
Expected: PASS.

Do NOT expect `go test ./...` to pass yet: `config.MotorPersonal()` runs `Validate()`, and the embedded YAML has no recoveries block, so every test using the preset now fails with `claims.recoveries.salvage.mean_share: must be in (0, 1), got 0`. Strict decoding means the domain fields, DTO fields, YAML keys, and validation must land atomically - **Tasks 2 and 3 are one commit**. Proceed straight to Task 3; its steps assume Task 2's code is present but uncommitted.

---

### Task 3: Config DTOs and the motor preset block (committed together with Task 2)

**Files:**
- Modify: `internal/infrastructure/config/config.go`
- Modify: `internal/infrastructure/config/motor-personal.yaml`
- Test: `internal/infrastructure/config/config_test.go`

**Interfaces:**
- Consumes: `lob.RecoveryParams`, `lob.RecoveryTypeParams` (Task 2).
- Produces: `config.RecoveriesParams{Salvage, Subrogation RecoveryTypeParams}` DTO with yaml/json tags `recoveries`, `salvage`, `subrogation`, `probability`, `mean_share`, `concentration`, `lag_median_days`, `lag_sigma`; wired through `ClaimsParams.Recoveries` and `ToDomain()`. The web preset endpoint and generate round-trip pick these up automatically.

- [ ] **Step 1: Write the failing tests**

In `internal/infrastructure/config/config_test.go`, inside the `validYAML` string, after the `nil_probability: 0.05` line add (match the two-space indentation under `claims:`):

```yaml
  recoveries:
    salvage:
      probability: 0.1
      mean_share: 0.15
      concentration: 10
      lag_median_days: 21
      lag_sigma: 0.5
    subrogation:
      probability: 0.2
      mean_share: 0.8
      concentration: 10
      lag_median_days: 180
      lag_sigma: 0.7
```

In the test asserting loaded values (the one checking `l.Claims.NilProbability != 0.05`), add:

```go
	if l.Claims.Recoveries.Salvage.MeanShare != 0.15 {
		t.Errorf("salvage mean_share = %v, want 0.15", l.Claims.Recoveries.Salvage.MeanShare)
	}
	if l.Claims.Recoveries.Subrogation.LagMedianDays != 180 {
		t.Errorf("subrogation lag_median_days = %v, want 180", l.Claims.Recoveries.Subrogation.LagMedianDays)
	}
```

Add a new test:

```go
func TestLoadRejectsMissingRecoveriesBlock(t *testing.T) {
	bad := strings.Replace(validYAML, "  recoveries:", "  recoveries_gone:", 1)
	if _, err := Load(strings.NewReader(bad)); err == nil {
		t.Fatal("config without a recoveries block: want error, got nil")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/infrastructure/config/`
Expected: FAIL - strict decoding rejects the unknown `recoveries` key (DTO field missing).

- [ ] **Step 3: Implement**

In `internal/infrastructure/config/config.go`:

Add to `ClaimsParams` after `NilProbability`:

```go
	Recoveries RecoveriesParams `yaml:"recoveries" json:"recoveries"`
```

Add after `InflationParams`:

```go
type RecoveriesParams struct {
	Salvage     RecoveryTypeParams `yaml:"salvage" json:"salvage"`
	Subrogation RecoveryTypeParams `yaml:"subrogation" json:"subrogation"`
}

type RecoveryTypeParams struct {
	Probability   float64 `yaml:"probability" json:"probability"`
	MeanShare     float64 `yaml:"mean_share" json:"mean_share"`
	Concentration float64 `yaml:"concentration" json:"concentration"`
	LagMedianDays float64 `yaml:"lag_median_days" json:"lag_median_days"`
	LagSigma      float64 `yaml:"lag_sigma" json:"lag_sigma"`
}
```

In `ToDomain()`, after `NilProbability: d.Claims.NilProbability,`:

```go
			Recoveries: lob.RecoveryParams{
				Salvage:     d.Claims.Recoveries.Salvage.toDomain(),
				Subrogation: d.Claims.Recoveries.Subrogation.toDomain(),
			},
```

And add the helper at the bottom of the file:

```go
func (r RecoveryTypeParams) toDomain() lob.RecoveryTypeParams {
	return lob.RecoveryTypeParams{
		Probability:   r.Probability,
		MeanShare:     r.MeanShare,
		Concentration: r.Concentration,
		LagMedianDays: r.LagMedianDays,
		LagSigma:      r.LagSigma,
	}
}
```

In `internal/infrastructure/config/motor-personal.yaml`, after the `nil_probability: 0.08` line add:

```yaml
  # Recoveries: money coming back on own-damage claims after they close.
  # Salvage sells the insured vehicle's wreck; subrogation recovers the
  # payout from an at-fault third party. Each recovery is a Beta-distributed
  # share of the claim's gross paid, received a lognormal lag after close.
  # probability 0 switches a recovery type off.
  recoveries:
    salvage:
      probability: 0.10
      mean_share: 0.15
      concentration: 10
      lag_median_days: 21
      lag_sigma: 0.5
    subrogation:
      probability: 0.20
      mean_share: 0.80
      concentration: 10
      lag_median_days: 180
      lag_sigma: 0.7
```

(These are starting values; Task 10 calibrates them.)

- [ ] **Step 4: Run the full suite**

Run: `go test ./... && go vet ./...`
Expected: PASS everywhere - the preset decodes, validates, and downstream behavior is unchanged because nothing consumes `Recoveries` yet. (`TestDefaultPresetIsRealistic` still passes: no recovery transactions are generated yet.)

- [ ] **Step 5: Commit (Tasks 2 and 3 together)**

```bash
git add internal/domain/lob/lob.go internal/domain/lob/lob_test.go internal/infrastructure/config/config.go internal/infrastructure/config/config_test.go internal/infrastructure/config/motor-personal.yaml
git commit -m "Add recovery parameters for salvage and subrogation"
```

---

### Task 4: Carry the own-damage flag on Claim

**Files:**
- Modify: `internal/domain/claim/claim.go`
- Test: `internal/domain/claim/claim_test.go` (package `claim_test`)

**Interfaces:**
- Produces: `claim.Claim.OwnDamage bool` - true when the severity mixture picked the own-damage component. Never written to claims.csv. Task 5 reads it for recovery eligibility.

- [ ] **Step 1: Write the failing test**

Append to `internal/domain/claim/claim_test.go`:

```go
func TestOwnDamageFlagFollowsSeverityMixture(t *testing.T) {
	allOwn := params()
	allOwn.Severity.ThirdPartyWeight = 0
	claims := claim.NewClaimSimulator(allOwn).Simulate(random.NewSource(31), fixedBook(2000, 20000, 0, 1.0))
	if len(claims) == 0 {
		t.Fatal("expected claims")
	}
	for _, c := range claims {
		if !c.OwnDamage {
			t.Fatalf("claim %d not flagged own-damage with third_party_weight 0", c.ID)
		}
	}

	allThird := params()
	allThird.Severity.ThirdPartyWeight = 1
	claims = claim.NewClaimSimulator(allThird).Simulate(random.NewSource(32), fixedBook(2000, 20000, 0, 1.0))
	if len(claims) == 0 {
		t.Fatal("expected claims")
	}
	for _, c := range claims {
		if c.OwnDamage {
			t.Fatalf("claim %d flagged own-damage with third_party_weight 1", c.ID)
		}
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/domain/claim/`
Expected: FAIL to compile with `c.OwnDamage undefined`.

- [ ] **Step 3: Implement**

In `internal/domain/claim/claim.go`:

Add to the `Claim` struct after `Nil`:

```go
	// OwnDamage is true when the severity mixture picked the own-damage
	// component. Carried to the recovery stage (only own-damage claims
	// yield salvage or subrogation) but never written to CSV.
	OwnDamage bool
```

Change `drawGroundUpLoss` to also report the component:

```go
// drawGroundUpLoss mixes own-damage losses (lognormal, scaled by sum
// insured) with third party liability losses (Pareto, uncapped), reporting
// which component fired.
func (s *ClaimSimulator) drawGroundUpLoss(src shared.RandomSource, pol policy.Policy) (loss float64, ownDamage bool) {
	sev := s.params.Severity
	if src.Bernoulli(sev.ThirdPartyWeight) {
		return src.Pareto(sev.ThirdPartyScale, sev.ThirdPartyAlpha), false
	}
	fraction := src.LogNormal(math.Log(sev.OwnDamageMedianFraction), sev.OwnDamageSigma)
	return pol.SumInsured.Dollars() * fraction, true
}
```

In `simulateClaim`, replace `loss := s.drawGroundUpLoss(src, pol)` with:

```go
	loss, ownDamage := s.drawGroundUpLoss(src, pol)
```

and add `OwnDamage: ownDamage,` to the returned `Claim` literal after `Nil: isNil,`.

- [ ] **Step 4: Run the full suite**

Run: `go test ./... && go vet ./...`
Expected: PASS - no new draws were added, so all seeded output is byte-identical.

- [ ] **Step 5: Commit**

```bash
git add internal/domain/claim/claim.go internal/domain/claim/claim_test.go
git commit -m "Record the own-damage component on each claim"
```

---

### Task 5: The recovery simulator

**Files:**
- Create: `internal/domain/transaction/recovery.go`
- Test: `internal/domain/transaction/recovery_test.go` (package `transaction_test`)

**Interfaces:**
- Consumes: `claim.Claim.OwnDamage`/`.Nil` (Task 4), `lob.RecoveryParams` (Task 2), `src.Beta` (Task 1), existing `Transaction`, `Payment`, `shared.Money`.
- Produces:
  - `transaction.Salvage` and `transaction.Subrogation` (`Type` constants `"SALVAGE"`, `"SUBROGATION"`).
  - `func (t Type) IsRecovery() bool`.
  - `transaction.NewRecoverySimulator(p lob.RecoveryParams) *RecoverySimulator`.
  - `func (s *RecoverySimulator) Apply(src shared.RandomSource, claims []claim.Claim, txs []Transaction) []Transaction` - returns the merged, re-ID'd transaction list. Task 6 wires it into `GenerateDataset`.

- [ ] **Step 1: Write the failing tests**

Create `internal/domain/transaction/recovery_test.go`:

```go
package transaction_test

import (
	"testing"

	"github.com/le-marais/claimsgen/internal/domain/claim"
	"github.com/le-marais/claimsgen/internal/domain/lob"
	"github.com/le-marais/claimsgen/internal/domain/shared"
	"github.com/le-marais/claimsgen/internal/domain/transaction"
	"github.com/le-marais/claimsgen/internal/infrastructure/random"
)

func recoveryParams() lob.RecoveryParams {
	return lob.RecoveryParams{
		Salvage:     lob.RecoveryTypeParams{Probability: 0.5, MeanShare: 0.15, Concentration: 10, LagMedianDays: 21, LagSigma: 0.5},
		Subrogation: lob.RecoveryTypeParams{Probability: 0.5, MeanShare: 0.8, Concentration: 10, LagMedianDays: 180, LagSigma: 0.7},
	}
}

// recoveryFixture runs the runoff over a mixed book - own-damage, third
// party, and nil claims - then applies recoveries with the given params.
func recoveryFixture(t *testing.T, p lob.RecoveryParams, seed uint64) ([]claim.Claim, []transaction.Transaction) {
	t.Helper()
	claims := testClaims(300)
	for i := range claims {
		claims[i].OwnDamage = i%3 != 0 // two thirds own damage
		if i%10 == 0 {
			claims[i].Nil = true
		}
	}
	txs := transaction.NewRunoffSimulator(params()).Simulate(random.NewSource(seed), claims)
	return claims, transaction.NewRecoverySimulator(p).Apply(random.NewSource(seed), claims, txs)
}

func TestRecoveriesOnlyOnOwnDamageNonNilClaims(t *testing.T) {
	certain := recoveryParams()
	certain.Salvage.Probability = 1
	certain.Subrogation.Probability = 1
	claims, txs := recoveryFixture(t, certain, 1)

	eligible := map[int]bool{}
	for _, c := range claims {
		eligible[c.ID] = c.OwnDamage && !c.Nil
	}
	got := map[int]bool{}
	for _, tx := range txs {
		if tx.Type.IsRecovery() {
			if !eligible[tx.ClaimID] {
				t.Fatalf("recovery on ineligible claim %d", tx.ClaimID)
			}
			got[tx.ClaimID] = true
		}
	}
	for _, c := range claims {
		if eligible[c.ID] && !got[c.ID] {
			t.Fatalf("eligible claim %d has no recovery with probability 1", c.ID)
		}
	}
}

func TestRecoveryBoundsAndDates(t *testing.T) {
	claims, txs := recoveryFixture(t, recoveryParams(), 2)
	closeDate := map[int]shared.Date{}
	for _, c := range claims {
		closeDate[c.ID] = c.CloseDate
	}
	paid := map[int]shared.Money{}
	recovered := map[int]shared.Money{}
	sawRecovery := false
	for _, tx := range txs {
		switch {
		case tx.Type == transaction.Payment:
			paid[tx.ClaimID] += tx.Amount
		case tx.Type.IsRecovery():
			sawRecovery = true
			if tx.Amount <= 0 {
				t.Fatalf("recovery %d amount %v not positive", tx.ID, tx.Amount)
			}
			if !closeDate[tx.ClaimID].Before(tx.Date) {
				t.Fatalf("recovery %d on %s not strictly after close %s", tx.ID, tx.Date, closeDate[tx.ClaimID])
			}
			recovered[tx.ClaimID] += tx.Amount
		}
	}
	if !sawRecovery {
		t.Fatal("fixture produced no recoveries")
	}
	for id, r := range recovered {
		if r >= paid[id] {
			t.Fatalf("claim %d recovered %v >= gross paid %v", id, r, paid[id])
		}
	}
}

func TestRecoveryOffSwitchPerType(t *testing.T) {
	noSalvage := recoveryParams()
	noSalvage.Salvage.Probability = 0
	_, txs := recoveryFixture(t, noSalvage, 3)
	sawSubro := false
	for _, tx := range txs {
		if tx.Type == transaction.Salvage {
			t.Fatalf("salvage row %d with salvage probability 0", tx.ID)
		}
		if tx.Type == transaction.Subrogation {
			sawSubro = true
		}
	}
	if !sawSubro {
		t.Fatal("expected subrogation rows with subrogation still on")
	}
}

func TestRecoveriesOffReturnsRunoffUnchanged(t *testing.T) {
	off := lob.RecoveryParams{
		Salvage:     lob.RecoveryTypeParams{Probability: 0, MeanShare: 0.15, Concentration: 10, LagMedianDays: 21, LagSigma: 0.5},
		Subrogation: lob.RecoveryTypeParams{Probability: 0, MeanShare: 0.8, Concentration: 10, LagMedianDays: 180, LagSigma: 0.7},
	}
	claims := testClaims(100)
	for i := range claims {
		claims[i].OwnDamage = true
	}
	before := transaction.NewRunoffSimulator(params()).Simulate(random.NewSource(4), claims)
	after := transaction.NewRecoverySimulator(off).Apply(random.NewSource(4), claims, before)
	if len(after) != len(before) {
		t.Fatalf("lengths differ: %d vs %d", len(after), len(before))
	}
	for i := range after {
		if after[i] != before[i] {
			t.Fatalf("transaction %d changed with recoveries off", i)
		}
	}
}

func TestRecoveryMergeKeepsIDsSequentialAndClaimsChronological(t *testing.T) {
	claims, txs := recoveryFixture(t, recoveryParams(), 5)
	lastDate := map[int]shared.Date{}
	for _, c := range claims {
		lastDate[c.ID] = c.ReportDate
	}
	for i, tx := range txs {
		if tx.ID != i+1 {
			t.Fatalf("transaction %d has ID %d, want %d", i, tx.ID, i+1)
		}
		if tx.Date.Before(lastDate[tx.ClaimID]) {
			t.Fatalf("claim %d rows not chronological at transaction %d", tx.ClaimID, tx.ID)
		}
		lastDate[tx.ClaimID] = tx.Date
	}
}

func TestRecoveryApplyIsDeterministic(t *testing.T) {
	_, a := recoveryFixture(t, recoveryParams(), 6)
	_, b := recoveryFixture(t, recoveryParams(), 6)
	if len(a) != len(b) {
		t.Fatalf("lengths differ: %d vs %d", len(a), len(b))
	}
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("transaction %d differs between identical runs", i)
		}
	}
}

func TestSalvageArrivesSoonerThanSubrogationOnAverage(t *testing.T) {
	claims, txs := recoveryFixture(t, recoveryParams(), 7)
	closeDate := map[int]shared.Date{}
	for _, c := range claims {
		closeDate[c.ID] = c.CloseDate
	}
	var salvageSum, salvageN, subroSum, subroN float64
	for _, tx := range txs {
		lag := float64(shared.DaysBetween(closeDate[tx.ClaimID], tx.Date))
		switch tx.Type {
		case transaction.Salvage:
			salvageSum += lag
			salvageN++
		case transaction.Subrogation:
			subroSum += lag
			subroN++
		}
	}
	if salvageN == 0 || subroN == 0 {
		t.Fatalf("fixture drew %v salvage and %v subrogation rows, want both", salvageN, subroN)
	}
	if salvageSum/salvageN >= subroSum/subroN {
		t.Errorf("mean salvage lag %v days >= mean subrogation lag %v days, want salvage sooner",
			salvageSum/salvageN, subroSum/subroN)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/domain/transaction/`
Expected: FAIL to compile with `transaction.NewRecoverySimulator undefined`.

- [ ] **Step 3: Implement**

Create `internal/domain/transaction/recovery.go`:

```go
package transaction

import (
	"fmt"
	"math"
	"sort"

	"github.com/le-marais/claimsgen/internal/domain/claim"
	"github.com/le-marais/claimsgen/internal/domain/lob"
	"github.com/le-marais/claimsgen/internal/domain/shared"
)

// Salvage and Subrogation are money-in recovery transactions: the insured
// vehicle's wreck is sold, or the payout is recovered from an at-fault
// third party. Both land after the claim closes and never touch the case
// estimate, which stays gross.
const (
	Salvage     Type = "SALVAGE"
	Subrogation Type = "SUBROGATION"
)

// IsRecovery reports whether the type is money coming back on a claim.
func (t Type) IsRecovery() bool {
	return t == Salvage || t == Subrogation
}

// RecoverySimulator draws salvage and subrogation transactions for eligible
// claims after the runoff stage.
type RecoverySimulator struct {
	params lob.RecoveryParams
}

func NewRecoverySimulator(p lob.RecoveryParams) *RecoverySimulator {
	return &RecoverySimulator{params: p}
}

// Apply merges each eligible claim's recovery rows into the runoff output
// after that claim's block, renumbering IDs. Every claim draws from its own
// labelled sub-stream, so enabling recoveries never reshuffles the draws of
// other stages, and a claim's recoveries do not depend on any other claim.
func (s *RecoverySimulator) Apply(src shared.RandomSource, claims []claim.Claim, txs []Transaction) []Transaction {
	paid := make(map[int]shared.Money, len(claims))
	for _, tx := range txs {
		if tx.Type == Payment {
			paid[tx.ClaimID] += tx.Amount
		}
	}
	recoveries := map[int][]Transaction{}
	total := 0
	for _, c := range claims {
		rows := s.simulateClaim(src.Split(fmt.Sprintf("recovery-claim-%d", c.ID)), c, paid[c.ID])
		if len(rows) > 0 {
			recoveries[c.ID] = rows
			total += len(rows)
		}
	}
	if total == 0 {
		return txs
	}
	merged := make([]Transaction, 0, len(txs)+total)
	for i, tx := range txs {
		merged = append(merged, tx)
		// The runoff emits each claim's rows as one contiguous block; append
		// the claim's recoveries at the end of its block.
		if i+1 == len(txs) || txs[i+1].ClaimID != tx.ClaimID {
			merged = append(merged, recoveries[tx.ClaimID]...)
		}
	}
	for i := range merged {
		merged[i].ID = i + 1
	}
	return merged
}

// simulateClaim draws at most one salvage and one subrogation row. Only
// own-damage claims that paid something are eligible; the total recovered
// stays strictly below the claim's gross paid.
func (s *RecoverySimulator) simulateClaim(src shared.RandomSource, c claim.Claim, paid shared.Money) []Transaction {
	if !c.OwnDamage || c.Nil || paid <= 0 {
		return nil
	}
	kinds := []struct {
		t Type
		p lob.RecoveryTypeParams
	}{
		{Salvage, s.params.Salvage},
		{Subrogation, s.params.Subrogation},
	}
	var rows []Transaction
	recovered := shared.Money(0)
	for _, k := range kinds {
		if k.p.Probability <= 0 || !src.Bernoulli(k.p.Probability) {
			continue
		}
		share := src.Beta(k.p.MeanShare*k.p.Concentration, (1-k.p.MeanShare)*k.p.Concentration)
		amount := paid.MulFloat(share)
		lag := int(math.Round(src.LogNormal(math.Log(k.p.LagMedianDays), k.p.LagSigma)))
		if lag < 1 {
			lag = 1 // recoveries land strictly after close
		}
		if recovered+amount >= paid {
			amount = paid - recovered - 1 // keep total recovered strictly below gross paid
		}
		if amount < 1 {
			continue // sub-cent recovery: emit no row
		}
		rows = append(rows, Transaction{
			ClaimID: c.ID,
			Date:    c.CloseDate.AddDays(lag),
			Type:    k.t,
			Amount:  amount,
		})
		recovered += amount
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].Date.Before(rows[j].Date) })
	return rows
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/domain/transaction/ && go test ./... && go vet ./...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/domain/transaction/recovery.go internal/domain/transaction/recovery_test.go
git commit -m "Add the salvage and subrogation recovery simulator"
```

---

### Task 6: Wire recoveries into GenerateDataset and extend the invariant sweep

**Files:**
- Modify: `internal/application/generate.go`
- Modify: `internal/application/invariants_test.go`
- Create: `internal/application/recoveries_test.go`

**Interfaces:**
- Consumes: `transaction.NewRecoverySimulator(...).Apply(src.Split("recovery"), claims, txs)` (Task 5).
- Produces: datasets whose transactions include recovery rows; Tasks 7-9 consume them.

- [ ] **Step 1: Write the failing end-to-end tests**

Create `internal/application/recoveries_test.go`:

```go
package application_test

import (
	"testing"

	"github.com/le-marais/claimsgen/internal/application"
	"github.com/le-marais/claimsgen/internal/domain/transaction"
	"github.com/le-marais/claimsgen/internal/infrastructure/random"
)

// TestRecoveryProbabilitiesZeroLeaveNoRecoveries is the spec's output-level
// off-switch check: with both probabilities 0, no recovery rows exist.
func TestRecoveryProbabilitiesZeroLeaveNoRecoveries(t *testing.T) {
	req := request(t)
	req.LOB.Claims.Recoveries.Salvage.Probability = 0
	req.LOB.Claims.Recoveries.Subrogation.Probability = 0
	ds, err := application.GenerateDataset(random.NewSource(7), req)
	if err != nil {
		t.Fatal(err)
	}
	if len(ds.Transactions) == 0 {
		t.Fatal("expected transactions")
	}
	for _, tx := range ds.Transactions {
		if tx.Type.IsRecovery() {
			t.Fatalf("transaction %d is a %s with both recovery probabilities 0", tx.ID, tx.Type)
		}
	}
}

// TestDefaultPresetGeneratesBothRecoveryTypes proves the feature is on by
// default and both types appear in the output.
func TestDefaultPresetGeneratesBothRecoveryTypes(t *testing.T) {
	ds, err := application.GenerateDataset(random.NewSource(8), request(t))
	if err != nil {
		t.Fatal(err)
	}
	var salvage, subro int
	for _, tx := range ds.Transactions {
		switch tx.Type {
		case transaction.Salvage:
			salvage++
		case transaction.Subrogation:
			subro++
		}
	}
	if salvage == 0 || subro == 0 {
		t.Fatalf("default preset generated %d salvage and %d subrogation rows, want both positive", salvage, subro)
	}
}

// TestRecoveriesDoNotShiftOtherStages is the spec's sub-stream independence
// check: enabling recoveries only appends rows - policies, claims, and every
// non-recovery transaction are unchanged draw-for-draw.
func TestRecoveriesDoNotShiftOtherStages(t *testing.T) {
	off := request(t)
	off.LOB.Claims.Recoveries.Salvage.Probability = 0
	off.LOB.Claims.Recoveries.Subrogation.Probability = 0
	dsOff, err := application.GenerateDataset(random.NewSource(13), off)
	if err != nil {
		t.Fatal(err)
	}
	dsOn, err := application.GenerateDataset(random.NewSource(13), request(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(dsOn.Policies) != len(dsOff.Policies) || len(dsOn.Claims) != len(dsOff.Claims) {
		t.Fatalf("book or claims changed: %d/%d policies, %d/%d claims",
			len(dsOn.Policies), len(dsOff.Policies), len(dsOn.Claims), len(dsOff.Claims))
	}
	for i := range dsOn.Claims {
		if dsOn.Claims[i] != dsOff.Claims[i] {
			t.Fatalf("claim %d differs with recoveries on", dsOn.Claims[i].ID)
		}
	}
	var core []transaction.Transaction
	for _, tx := range dsOn.Transactions {
		if !tx.Type.IsRecovery() {
			core = append(core, tx)
		}
	}
	if len(core) != len(dsOff.Transactions) {
		t.Fatalf("non-recovery transaction count %d, want %d", len(core), len(dsOff.Transactions))
	}
	for i := range core {
		got, want := core[i], dsOff.Transactions[i]
		// IDs are renumbered by the merge; everything else must match.
		got.ID, want.ID = 0, 0
		if got != want {
			t.Fatalf("non-recovery transaction %d differs with recoveries on: %+v vs %+v", i, got, want)
		}
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/application/ -run 'TestRecover|TestDefaultPresetGenerates'`
Expected: `TestRecoveryProbabilitiesZeroLeaveNoRecoveries` and `TestRecoveriesDoNotShiftOtherStages` PASS trivially (nothing generates recoveries yet - that is fine), `TestDefaultPresetGeneratesBothRecoveryTypes` FAILS with zero counts.

- [ ] **Step 3: Wire the stage in**

In `internal/application/generate.go`, replace:

```go
	txs := transaction.NewRunoffSimulator(req.LOB.Runoff).
		Simulate(src.Split("runoff"), claims)
```

with:

```go
	txs := transaction.NewRunoffSimulator(req.LOB.Runoff).
		Simulate(src.Split("runoff"), claims)
	txs = transaction.NewRecoverySimulator(req.LOB.Claims.Recoveries).
		Apply(src.Split("recovery"), claims, txs)
```

- [ ] **Step 4: Update the invariant sweep**

Replace the body of `TestDatasetInvariants` in `internal/application/invariants_test.go` with (whole function shown; the claim map gains flags, the window check splits by recovery, and per-claim recovery checks land at the end):

```go
func TestDatasetInvariants(t *testing.T) {
	req := request(t)
	req.Years = 5
	req.InitialBookSize = 2000
	ds, err := application.GenerateDataset(random.NewSource(99), req)
	if err != nil {
		t.Fatal(err)
	}

	policies := map[int]struct {
		start, end shared.Date
		excess     shared.Money
	}{}
	for _, p := range ds.Policies {
		policies[p.ID] = struct {
			start, end shared.Date
			excess     shared.Money
		}{p.CoverStart, p.CoverEnd, p.Excess}
	}

	type claimInfo struct {
		report, close  shared.Date
		isNil, ownDmg  bool
	}
	claims := map[int]claimInfo{}
	for _, c := range ds.Claims {
		pol, ok := policies[c.PolicyID]
		if !ok {
			t.Fatalf("claim %d references missing policy %d", c.ID, c.PolicyID)
		}
		if c.OccurrenceDate.Before(pol.start) || c.OccurrenceDate.After(pol.end) {
			t.Fatalf("claim %d occurrence %s outside cover %s..%s", c.ID, c.OccurrenceDate, pol.start, pol.end)
		}
		if c.ReportDate.Before(c.OccurrenceDate) {
			t.Fatalf("claim %d reported %s before occurrence %s", c.ID, c.ReportDate, c.OccurrenceDate)
		}
		if c.CloseDate.Before(c.ReportDate) {
			t.Fatalf("claim %d closed %s before report %s", c.ID, c.CloseDate, c.ReportDate)
		}
		if c.InitialEstimate <= 0 {
			t.Fatalf("claim %d initial estimate %v not positive", c.ID, c.InitialEstimate)
		}
		claims[c.ID] = claimInfo{c.ReportDate, c.CloseDate, c.Nil, c.OwnDamage}
	}

	type state struct {
		outstanding shared.Money
		paid        shared.Money
		recovered   shared.Money
		rows        int
		first       transaction.Transaction
		last        transaction.Transaction
		lastCase    transaction.Transaction // last non-recovery row
	}
	perClaim := map[int]*state{}
	for _, tx := range ds.Transactions {
		c, ok := claims[tx.ClaimID]
		if !ok {
			t.Fatalf("transaction %d references missing claim %d", tx.ID, tx.ClaimID)
		}
		if tx.Type.IsRecovery() {
			// Recoveries are the only post-close activity, strictly after close.
			if !c.close.Before(tx.Date) {
				t.Fatalf("recovery %d on %s not strictly after close %s", tx.ID, tx.Date, c.close)
			}
		} else if tx.Date.Before(c.report) || tx.Date.After(c.close) {
			t.Fatalf("transaction %d on %s outside claim window %s..%s", tx.ID, tx.Date, c.report, c.close)
		}
		s := perClaim[tx.ClaimID]
		if s == nil {
			s = &state{first: tx}
			perClaim[tx.ClaimID] = s
		}
		if s.rows > 0 && tx.Date.Before(s.last.Date) {
			t.Fatalf("claim %d transactions out of order at transaction %d", tx.ClaimID, tx.ID)
		}
		switch tx.Type {
		case transaction.Estimate:
			s.outstanding += tx.Amount
		case transaction.Payment:
			if tx.Amount <= 0 {
				t.Fatalf("transaction %d payment amount %v not positive", tx.ID, tx.Amount)
			}
			s.paid += tx.Amount
		case transaction.Salvage, transaction.Subrogation:
			if tx.Amount <= 0 {
				t.Fatalf("transaction %d recovery amount %v not positive", tx.ID, tx.Amount)
			}
			if !c.ownDmg || c.isNil {
				t.Fatalf("recovery %d on ineligible claim %d (own damage %v, nil %v)", tx.ID, tx.ClaimID, c.ownDmg, c.isNil)
			}
			s.recovered += tx.Amount
		default:
			t.Fatalf("transaction %d has unknown type %q", tx.ID, tx.Type)
		}
		if s.outstanding < 0 {
			t.Fatalf("claim %d outstanding case went negative at transaction %d", tx.ClaimID, tx.ID)
		}
		s.rows++
		s.last = tx
		if !tx.Type.IsRecovery() {
			s.lastCase = tx
		}
	}

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
		if s.recovered > 0 && s.recovered >= s.paid {
			t.Fatalf("claim %d recovered %v >= gross paid %v", c.ID, s.recovered, s.paid)
		}
		if s.lastCase.Date != c.CloseDate {
			t.Fatalf("claim %d last case activity on %s, want close date %s", c.ID, s.lastCase.Date, c.CloseDate)
		}
	}
}
```

- [ ] **Step 5: Run the suite**

Run: `go test ./... -skip TestDefaultPresetIsRealistic && go vet ./...`
Expected: PASS. Then run `go test ./internal/application/ -run TestDefaultPresetIsRealistic` and note the result: it may pass or fail at this point (recoveries do not yet feed the triangles, so paid/incurred are still gross and unchanged - it should still PASS here; it will wobble in Task 7 and be settled in Task 10).

- [ ] **Step 6: Commit**

```bash
git add internal/application/generate.go internal/application/invariants_test.go internal/application/recoveries_test.go
git commit -m "Generate salvage and subrogation recoveries in the dataset"
```

---

### Task 7: Net paid triangles and the net realism gate

**Files:**
- Modify: `internal/domain/triangle/triangle.go`
- Modify: `internal/application/realism.go`
- Test: `internal/domain/triangle/triangle_test.go` (package `triangle_test`; it already imports claim, shared, transaction, triangle, and time)

**Interfaces:**
- Produces: `triangle.NetPaidTriangle(claims, txs, startYear, origins, devs) Triangle` - payments minus recoveries. `PaidTriangle` stays gross. `IncurredTriangle` becomes gross case + net paid (recoveries subtract). `EvaluateRealism` compares net paid (Schedule P is net of salvage and subrogation).

- [ ] **Step 1: Write the failing tests**

Append to `internal/domain/triangle/triangle_test.go` (adjust construction helpers to the file's existing style if it has them; otherwise this is self-contained):

```go
func TestNetPaidTriangleSubtractsRecoveries(t *testing.T) {
	claims := []claim.Claim{{ID: 1, OccurrenceDate: shared.NewDate(1998, time.March, 1)}}
	txs := []transaction.Transaction{
		{ID: 1, ClaimID: 1, Date: shared.NewDate(1998, time.April, 1), Type: transaction.Payment, Amount: shared.FromDollars(1000)},
		{ID: 2, ClaimID: 1, Date: shared.NewDate(1999, time.June, 1), Type: transaction.Salvage, Amount: shared.FromDollars(150)},
		{ID: 3, ClaimID: 1, Date: shared.NewDate(2000, time.June, 1), Type: transaction.Subrogation, Amount: shared.FromDollars(300)},
	}
	gross := triangle.PaidTriangle(claims, txs, 1998, 3, 3)
	if got := gross.Cells[0]; got[0] != 1000 || got[1] != 1000 || got[2] != 1000 {
		t.Fatalf("gross paid row = %v, want [1000 1000 1000]", got)
	}
	net := triangle.NetPaidTriangle(claims, txs, 1998, 3, 3)
	if got := net.Cells[0]; got[0] != 1000 || got[1] != 850 || got[2] != 550 {
		t.Fatalf("net paid row = %v, want [1000 850 550]", got)
	}
}

func TestIncurredTriangleSubtractsRecoveries(t *testing.T) {
	claims := []claim.Claim{{ID: 1, OccurrenceDate: shared.NewDate(1998, time.March, 1)}}
	txs := []transaction.Transaction{
		{ID: 1, ClaimID: 1, Date: shared.NewDate(1998, time.March, 10), Type: transaction.Estimate, Amount: shared.FromDollars(1000)},
		{ID: 2, ClaimID: 1, Date: shared.NewDate(1998, time.April, 1), Type: transaction.Payment, Amount: shared.FromDollars(1000)},
		{ID: 3, ClaimID: 1, Date: shared.NewDate(1998, time.April, 1), Type: transaction.Estimate, Amount: shared.FromDollars(-1000)},
		{ID: 4, ClaimID: 1, Date: shared.NewDate(1999, time.June, 1), Type: transaction.Salvage, Amount: shared.FromDollars(150)},
	}
	incurred := triangle.IncurredTriangle(claims, txs, 1998, 2, 2)
	if got := incurred.Cells[0]; got[0] != 1000 || got[1] != 850 {
		t.Fatalf("incurred row = %v, want [1000 850] (gross case + net paid)", got)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/domain/triangle/`
Expected: FAIL with `triangle.NetPaidTriangle undefined`.

- [ ] **Step 3: Implement**

In `internal/domain/triangle/triangle.go`, change `aggregate` to take a signed weight and rewrite the three constructors:

```go
// PaidTriangle aggregates gross payments into a cumulative triangle by
// occurrence year. Development years beyond the last column are accumulated
// into it.
func PaidTriangle(claims []claim.Claim, txs []transaction.Transaction, startYear, origins, devs int) Triangle {
	return aggregate(claims, txs, startYear, origins, devs, func(t transaction.Transaction) float64 {
		if t.Type == transaction.Payment {
			return 1
		}
		return 0
	})
}

// NetPaidTriangle aggregates payments net of recoveries: salvage and
// subrogation rows subtract, so cumulative net paid can develop downward at
// late ages. Schedule P paid losses are net of salvage and subrogation, so
// this is the triangle the realism comparison uses.
func NetPaidTriangle(claims []claim.Claim, txs []transaction.Transaction, startYear, origins, devs int) Triangle {
	return aggregate(claims, txs, startYear, origins, devs, func(t transaction.Transaction) float64 {
		switch {
		case t.Type == transaction.Payment:
			return 1
		case t.Type.IsRecovery():
			return -1
		}
		return 0
	})
}

// IncurredTriangle aggregates gross case plus net paid into a cumulative
// triangle by occurrence year: estimate movements and payments add,
// recoveries subtract.
func IncurredTriangle(claims []claim.Claim, txs []transaction.Transaction, startYear, origins, devs int) Triangle {
	return aggregate(claims, txs, startYear, origins, devs, func(t transaction.Transaction) float64 {
		if t.Type.IsRecovery() {
			return -1
		}
		return 1
	})
}

func aggregate(claims []claim.Claim, txs []transaction.Transaction, startYear, origins, devs int, weight func(transaction.Transaction) float64) Triangle {
```

and inside the loop replace:

```go
		if !include(tx) {
			continue
		}
```

with:

```go
		w := weight(tx)
		if w == 0 {
			continue
		}
```

and replace `incremental[origin][dev] += tx.Amount.Dollars()` with `incremental[origin][dev] += w * tx.Amount.Dollars()`.

In `internal/application/realism.go`, change the comparison's paid triangle:

```go
	comparison := triangle.Comparison{
		Paid:          triangle.NetPaidTriangle(ds.Claims, ds.Transactions, startYear, years, developmentYears),
		Incurred:      triangle.IncurredTriangle(ds.Claims, ds.Transactions, startYear, years, developmentYears),
		EarnedPremium: triangle.EarnedPremiumByYear(ds.Policies, startYear, years),
	}
```

and update the function's doc comment to say paid is net of recoveries to match Schedule P.

- [ ] **Step 4: Run the suite**

Run: `go test ./... -skip TestDefaultPresetIsRealistic && go vet ./...`
Expected: PASS. `TestDefaultPresetIsRealistic` may now fail (net triangles shift the metrics); that is expected until Task 10.

- [ ] **Step 5: Commit**

```bash
git add internal/domain/triangle/triangle.go internal/domain/triangle/triangle_test.go internal/application/realism.go
git commit -m "Aggregate net-of-recoveries triangles and gate realism on net paid"
```

---

### Task 8: Recovered column in the summary

**Files:**
- Modify: `internal/application/summary.go`
- Test: `internal/application/summary_test.go`

**Interfaces:**
- Produces: `application.YearSummary.Recovered float64` - salvage plus subrogation received, by occurrence year - plus the grand total. Task 9 surfaces it in the UI.

- [ ] **Step 1: Write the failing test**

Append to `internal/application/summary_test.go`:

```go
func TestSummarizeCountsRecoveredByOccurrenceYear(t *testing.T) {
	ds := tinyDataset()
	// A salvage recovery on claim 1 (occurred 1998), received in 1999: it
	// books to the occurrence year, like Paid.
	ds.Transactions = append(ds.Transactions, transaction.Transaction{
		ID: 5, ClaimID: 1, Date: shared.NewDate(1999, time.February, 1), Type: transaction.Salvage, Amount: shared.FromDollars(150),
	})
	got := application.Summarize(ds, 1998, 2)
	if got.Years[0].Recovered != 150 {
		t.Errorf("1998 recovered = %v, want 150", got.Years[0].Recovered)
	}
	if got.Years[1].Recovered != 0 {
		t.Errorf("1999 recovered = %v, want 0", got.Years[1].Recovered)
	}
	if got.Total.Recovered != 150 {
		t.Errorf("total recovered = %v, want 150", got.Total.Recovered)
	}
	// Paid stays gross.
	if got.Years[0].Paid != 1000 {
		t.Errorf("1998 paid = %v, want 1000 (gross)", got.Years[0].Paid)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/application/ -run TestSummarizeCountsRecovered`
Expected: FAIL to compile with `.Recovered undefined`.

- [ ] **Step 3: Implement**

In `internal/application/summary.go`:

Add to `YearSummary` after `Paid float64`:

```go
	// Recovered is salvage plus subrogation received, by occurrence year.
	Recovered float64
```

In `Summarize`, replace the transaction loop with:

```go
	for _, tx := range ds.Transactions {
		i := occurrenceYear[tx.ClaimID] - startYear
		if i < 0 || i >= years {
			continue
		}
		switch {
		case tx.Type == transaction.Payment:
			rows[i].Paid += tx.Amount.Dollars()
		case tx.Type.IsRecovery():
			rows[i].Recovered += tx.Amount.Dollars()
		}
	}
```

And in the total accumulation add `total.Recovered += r.Recovered`.

Also update the `YearSummary` doc comment: Paid is gross of recoveries.

- [ ] **Step 4: Run the suite**

Run: `go test ./... -skip TestDefaultPresetIsRealistic && go vet ./...`
Expected: PASS (the `TestSummarize` DeepEqual still passes - `Recovered` is zero in its fixture).

- [ ] **Step 5: Commit**

```bash
git add internal/application/summary.go internal/application/summary_test.go
git commit -m "Report recovered amounts per year in the summary"
```

---

### Task 9: Web API and UI - recoveries form group, Recovered column, net paid triangle

**Files:**
- Modify: `internal/infrastructure/web/viewmodel.go`
- Modify: `internal/infrastructure/web/static/app.js`
- Test: `internal/infrastructure/web/server_test.go` (package `web`, internal)

**Interfaces:**
- Consumes: `application.YearSummary.Recovered` (Task 8), `triangle.NetPaidTriangle` (Task 7), `config.RecoveriesParams` JSON (Task 3, flows through the preset endpoint automatically).
- Produces: generate response JSON with `summary.years[].recovered`, `summary.total.recovered`, and `triangles.net_paid`.

- [ ] **Step 1: Write the failing test**

In `internal/infrastructure/web/server_test.go`, find the generate-response test that asserts `resp.Summary.Total.NilClaims` is positive (around line 232) and add alongside it:

```go
	if resp.Summary.Total.Recovered <= 0 {
		t.Fatalf("total recovered = %v, want positive with the default preset", resp.Summary.Total.Recovered)
	}
	if len(resp.Triangles.NetPaid.Cells) == 0 {
		t.Fatal("net paid triangle missing from the generate response")
	}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/infrastructure/web/`
Expected: FAIL to compile with `resp.Summary.Total.Recovered undefined`.

- [ ] **Step 3: Implement the view model**

In `internal/infrastructure/web/viewmodel.go`:

Add to `summaryRowJSON` after `Paid`:

```go
	Recovered float64 `json:"recovered"`
```

Set it in `summaryRowView`: add `Recovered: s.Recovered,` after `Paid: s.Paid,`.

Add to `trianglesJSON`:

```go
	NetPaid triangleJSON `json:"net_paid"`
```

In `buildResponse`, add after the `incurred := ...` line:

```go
	netPaid := triangle.NetPaidTriangle(ds.Claims, ds.Transactions, req.StartYear, req.Years, developmentYears)
```

and change the triangles literal to:

```go
		Triangles: trianglesJSON{Paid: triangleView(paid), NetPaid: triangleView(netPaid), Incurred: triangleView(incurred)},
```

- [ ] **Step 4: Run the Go tests**

Run: `go test ./internal/infrastructure/web/ && go vet ./...`
Expected: PASS.

- [ ] **Step 5: Update the static UI**

In `internal/infrastructure/web/static/app.js`:

(a) Add a new field group to `FIELD_GROUPS` between the `Claims` and `Runoff` groups:

```js
  {
    label: "Recoveries",
    fields: [
      { path: ["claims", "recoveries", "salvage", "probability"], label: "Salvage probability", tip: "Chance an own-damage claim yields salvage; 0 switches salvage off." },
      { path: ["claims", "recoveries", "salvage", "mean_share"], label: "Salvage mean share", tip: "Average salvage recovery as a share of the claim's gross paid." },
      { path: ["claims", "recoveries", "subrogation", "probability"], label: "Subrogation probability", tip: "Chance an own-damage claim is subrogated; 0 switches subrogation off." },
      { path: ["claims", "recoveries", "subrogation", "mean_share"], label: "Subrogation mean share", tip: "Average subrogation recovery as a share of the claim's gross paid." },
    ],
  },
```

(b) In `renderSummary`, change the header array to:

```js
  for (const label of ["Year", "Policies", "Claims", "Nil claims", "Earned premium", "Ultimate (paid)", "Recovered", "Loss ratio"]) {
```

(c) In `summaryRow`, insert the recovered cell after paid:

```js
  const cells = [
    fmtInt.format(row.policies),
    fmtInt.format(row.claims),
    fmtInt.format(row.nil_claims),
    fmtMoney.format(row.earned_premium),
    fmtMoney.format(row.paid),
    fmtMoney.format(row.recovered),
    row.loss_ratio == null ? "n/a" : row.loss_ratio.toFixed(3),
  ];
```

(d) In `renderTriangles`, replace the two-kind toggle loop with a three-kind one (gross/net paid plus incurred):

```js
function renderTriangles(triangles) {
  const panel = $("#tab-triangles");
  panel.replaceChildren();
  const toggle = document.createElement("div");
  toggle.className = "toggle";
  let table = triangleTable(triangles.paid);
  const kinds = [["paid", "Paid (gross)"], ["net_paid", "Paid (net)"], ["incurred", "Incurred"]];
  for (const [kind, label] of kinds) {
    const btn = document.createElement("button");
    btn.type = "button";
    btn.textContent = label;
    if (kind === "paid") btn.classList.add("active");
    btn.addEventListener("click", () => {
      for (const b of toggle.children) b.classList.toggle("active", b === btn);
      const next = triangleTable(triangles[kind]);
      table.replaceWith(next);
      table = next;
    });
    toggle.append(btn);
  }
  panel.append(toggle, table);
}
```

- [ ] **Step 6: Verify in the browser**

Run: `go run ./cmd/claimsgen ui` and open `http://127.0.0.1:8080`. Check: the Recoveries group appears in the parameters form with preset values 0.10/0.15/0.20/0.80; Generate works; the summary shows a Recovered column with positive values; the triangles tab has three toggles and Paid (net) shows values at or below Paid (gross), with late-age dips possible. Stop the server.

- [ ] **Step 7: Run everything and commit**

Run: `go test ./... -skip TestDefaultPresetIsRealistic && go vet ./...`
Expected: PASS.

```bash
git add internal/infrastructure/web/viewmodel.go internal/infrastructure/web/static/app.js internal/infrastructure/web/server_test.go
git commit -m "Expose recoveries in the web UI"
```

---

### Task 10: Calibrate the motor preset against the net realism gate

**Files:**
- Modify: `internal/infrastructure/config/motor-personal.yaml`

**Interfaces:**
- Consumes: everything above. The acceptance test is `TestDefaultPresetIsRealistic` on net paid triangles.

- [ ] **Step 1: Measure**

Run: `go test ./internal/application/ -run TestDefaultPresetIsRealistic -v`

If it already passes, run the full suite (`go test ./... && go vet ./...`), commit any YAML comment-only tidy-up if needed, and this task reduces to Step 4.

If it fails, the output names each metric and band, e.g. `ultimate loss ratio: 0.61 in [0.64, 0.94] = false` or late-age ATA factors outside bands.

- [ ] **Step 2: Adjust, guided by the levers**

Recoveries subtract from net paid and net incurred, so the shipped values will typically push the loss ratio down and soften late incurred development. Iterate on `internal/infrastructure/config/motor-personal.yaml`, one change at a time, re-running the gate after each:

- Loss ratio below the band: lower `book.premium_rate_factor` (e.g. 0.035 → 0.033) - it raises the loss ratio without touching severity shapes. Prefer this lever first.
- Late paid ATA factors below 1 or outside the band (recoveries landing at late development ages): reduce `subrogation.lag_median_days` (180 → 120) so recoveries land earlier, or reduce `subrogation.mean_share` / `probability` to shrink the effect.
- Early paid ATA factors off: recoveries should not affect early ages much; if they do, salvage is landing too late - check `salvage.lag_median_days`.
- Keep the recovery knobs inside the spec's target shape: salvage probability ~0.05-0.15, mean share ~0.1-0.2; subrogation probability ~0.1-0.3, mean share ~0.6-0.9.

- [ ] **Step 3: Verify the gate and the whole suite**

Run: `go test ./... && go vet ./...`
Expected: PASS everywhere, including `TestDefaultPresetIsRealistic` and the off-switch, invariant, and determinism tests, with no `-skip` flag.

- [ ] **Step 4: Commit**

```bash
git add internal/infrastructure/config/motor-personal.yaml
git commit -m "Calibrate the motor preset for recoveries"
```

---

### Task 11: Documentation

**Files:**
- Modify: `README.md`
- Modify: `docs/roadmap.md`
- Modify: `docs/mission.md`

- [ ] **Step 1: README**

In `README.md` (sentence case headers, no em dashes - use ` - `):

- In the three-dataset list, extend the transactions line: `- **transactions.csv** - each claim's case estimate movements, payments, and recoveries (salvage and subrogation) over its lifetime`.
- In "How the simulation works", add a step 5 after the transactions step:

```markdown
5. **Recoveries** - own-damage claims can yield salvage (the wreck is sold) and subrogation (the payout is recovered from an at-fault third party). Each is a Beta-distributed share of the claim's gross paid, received a lognormal lag after the close date - subrogation typically much later than salvage. Recoveries are pure cash events: the case estimate stays gross, and a claim's total recovered is always below its gross paid. Setting a recovery type's probability to 0 switches it off.
```

- In step 4 of the same section, append to the end: `Recovery rows are the only transactions dated after a claim's close date.`
- Add a conventions note after the simulation section (or extend step 4): gross paid is the sum of `PAYMENT` rows; net paid subtracts `SALVAGE` and `SUBROGATION` rows.
- In "Browser UI", extend the description sentence to mention the Recovered summary column and the gross/net paid triangle toggle.
- In "Realism", note the paid comparison is net of recoveries, matching Schedule P.

- [ ] **Step 2: Roadmap and mission**

In `docs/roadmap.md`:
- Move recoveries from "Near term" to "Shipped" with a one-line description mirroring the other shipped entries: `- **Recoveries (salvage and subrogation)** - money coming back on own-damage claims after close, as SALVAGE and SUBROGATION transaction types; triangles and the realism gate go net of recoveries, and the triangle tab gains a gross/net toggle.`
- Update the near-term intro (three of four backlog items done; reopened claims remains) and remove the recoveries bullet from near term.

In `docs/mission.md`, mark the backlog line: `- Recoveries (salvage and subrogation) - done`.

- [ ] **Step 3: Check and commit**

Re-read both edited docs for style (sentence case, spaced hyphens, no invented claims).

```bash
git add README.md docs/roadmap.md docs/mission.md
git commit -m "Document salvage and subrogation recoveries"
```

Note: the README's UI screenshots (`docs/screenshots/*.png`) now show a stale form and summary table. Regenerating them requires driving the browser interactively - flag this to the user at the end of implementation rather than automating it here.
