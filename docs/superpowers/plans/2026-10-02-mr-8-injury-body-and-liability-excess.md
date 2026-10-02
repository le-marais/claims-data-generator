> **OUT OF CONTEXT - do not read (2026-10-02):** implementation plan, deleted once its work ships. Agents must not load this file into context or treat it as a source of truth. For how the system works today see `README.md`, `AGENTS.md`, and `docs/architecture.md`.

# MR-8 fix implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Close MR-8: a spliced `lognormal_pareto` severity kind for injury in both presets, and no excess on personal motor's liability, with both presets recalibrated.

**Architecture:** The spliced distribution lives in `internal/domain/lob/lognormalpareto.go` (continuity-set tail share, quantile, stop-loss). Pricing calls its stop-loss; the claim stage draws one uniform and calls its quantile through `SeverityParams.LognormalParetoLoss`. The presets change in YAML only.

**Tech Stack:** Go 1.26, standard library `math`.

## Global constraints

- Same seed and config give byte-identical output; every severity kind takes exactly one draw per claim.
- All four golden hashes move by design in Task 2; understand each move before pasting.
- Both presets pass `TestPresetsAreRealistic`, the drift guard and the loss-ratio test; calibration checks seeds 1-30.
- Docs and comments: sentence case headers, no em dashes, concise.
- Run `gofmt -l .`, `go vet ./...`, `go test ./...` before each commit; `golangci-lint run ./...` before the PR.

---

### Task 1: The `lognormal_pareto` severity kind

**Files:**
- Create: `internal/domain/lob/lognormalpareto.go`, `internal/domain/lob/lognormalpareto_test.go`
- Modify: `internal/domain/lob/lob.go` (kind constant, docs, validation), `internal/domain/lob/expectedloss.go` (pricing case)
- Modify: `internal/domain/claim/claim.go` (`drawGroundUpLoss`), `internal/domain/claim/claim_test.go`
- Modify: `internal/infrastructure/web/fields.go`, `internal/infrastructure/web/fields_internal_test.go`
- Modify: `internal/domain/lob/lob_test.go` (validation cases)

**Interfaces:**
- Produces: `lob.LognormalPareto SeverityKind = "lognormal_pareto"`; `(SeverityParams).LognormalParetoLoss(u float64) float64`; unexported `newLognormalPareto(median, sigma, scale, alpha float64) lognormalPareto` with `tail`, `quantile(u)`, `stopLoss(t)`.

- [ ] **Step 1: Failing tests** in `lognormalpareto_test.go` (package `lob`):

```go
func TestLognormalParetoTailShareKeepsTheDensityContinuous(t *testing.T) {
	d := newLognormalPareto(4000, 1.0, 25000, 2.0)
	if d.tail < 0.035 || d.tail > 0.039 {
		t.Fatalf("tail share = %v, want about 0.037", d.tail)
	}
	// Density either side of scale from a fine uniform grid: equal-width
	// bins just below and just above scale hold about as many losses.
	const n = 2_000_000
	below, above := 0, 0
	for i := 0; i < n; i++ {
		x := d.quantile((float64(i) + 0.5) / n)
		switch {
		case x >= 24500 && x < 25000:
			below++
		case x >= 25000 && x < 25500:
			above++
		}
	}
	if rel := math.Abs(float64(below-above)) / float64(above); rel > 0.05 {
		t.Fatalf("bins below/above scale hold %d/%d losses, want about equal", below, above)
	}
	// The tail holds its share of losses.
	tail := 0
	for i := 0; i < n; i++ {
		if d.quantile((float64(i)+0.5)/n) > 25000 {
			tail++
		}
	}
	if got := float64(tail) / n; math.Abs(got-d.tail) > 1e-4 {
		t.Fatalf("share above scale = %v, want %v", got, d.tail)
	}
}

func TestLognormalParetoStopLossMatchesItsDraws(t *testing.T) {
	d := newLognormalPareto(4000, 1.0, 25000, 2.0)
	const n = 2_000_000
	for _, excess := range []float64{0, 500, 10000, 25000, 60000} {
		sum := 0.0
		for i := 0; i < n; i++ {
			if x := d.quantile((float64(i)+0.5)/n) - excess; x > 0 {
				sum += x
			}
		}
		want := sum / n
		if got := d.stopLoss(excess); math.Abs(got-want) > 0.01*want {
			t.Errorf("stopLoss(%v) = %.2f, draws give %.2f", excess, got, want)
		}
	}
}

func TestExpectedSectionLossPricesALognormalPareto(t *testing.T) {
	p := motorPricing()
	p.Sections[1].Severity = SeverityParams{Kind: LognormalPareto, Median: 4000, Sigma: 1.0, Scale: 25000, Alpha: 2.0}
	p.Sections[1].Limit = 100000
	d := newLognormalPareto(1.1*4000, 1.0, 1.1*25000, 2.0)
	payout := 1 - p.NilProbability + p.ReopenProbability*p.ReopenEstimateFactor
	want := p.Sections[1].BaseFrequency * 1.3 * payout * (d.stopLoss(300) - d.stopLoss(100300))
	if got := p.ExpectedSectionLoss(1, 20000, 300, 1.3, 1.1, 1.05); math.Abs(got-want) > 1e-9*want {
		t.Fatalf("section loss = %v, want %v", got, want)
	}
}
```

  Plus, in `lob_test.go`, validation cases naming `claims.sections[1].severity.median`, `.sigma`, `.scale` and `.alpha` for a `lognormal_pareto` section missing each; in `claim_test.go`, `TestLognormalParetoClaimsHaveABody`: a `no_excess` section of the kind reports claims below half its scale and a share above scale near the tail share.
- [ ] **Step 2: Run** `go test ./internal/domain/...` - fails to compile.
- [ ] **Step 3: Implement.** `lognormalpareto.go`:

```go
// lognormalPareto is the spliced loss of the LognormalPareto kind: a
// lognormal body truncated at scale, and a Pareto tail from scale up, joined
// so the density is continuous at scale.
type lognormalPareto struct {
	mu, sigma, scale, alpha float64
	// bodyMass is the body lognormal's probability below scale.
	bodyMass float64
	// tail is the share of losses above scale, which continuity sets.
	tail float64
}

func newLognormalPareto(median, sigma, scale, alpha float64) lognormalPareto {
	mu := math.Log(median)
	z := (math.Log(scale) - mu) / sigma
	bodyMass := normCDF(z)
	r := math.Exp(-z*z/2) / math.Sqrt(2*math.Pi) / (sigma * alpha * bodyMass)
	return lognormalPareto{mu: mu, sigma: sigma, scale: scale, alpha: alpha, bodyMass: bodyMass, tail: r / (1 + r)}
}

// quantile is the loss at cumulative probability u in [0, 1).
func (d lognormalPareto) quantile(u float64) float64 {
	if u < 1-d.tail {
		p := u / (1 - d.tail) * d.bodyMass
		return math.Exp(d.mu + d.sigma*math.Sqrt2*math.Erfinv(2*p-1))
	}
	return d.scale * math.Pow((1-u)/d.tail, -1/d.alpha)
}

// stopLoss is E[(X - t)+] for t >= 0: the tail's expected excess over t, and
// below scale the integral of the body's survival from t to scale.
func (d lognormalPareto) stopLoss(t float64) float64 {
	tailExcess := func(t float64) float64 {
		return d.tail * math.Pow(d.scale, d.alpha) * math.Pow(t, 1-d.alpha) / (d.alpha - 1)
	}
	if t >= d.scale {
		return tailExcess(t)
	}
	cdf := func(a float64) float64 { return normCDF((math.Log(a) - d.mu) / d.sigma) }
	partial := func(a float64) float64 { // E[X; X <= a] for the body lognormal
		return math.Exp(d.mu+d.sigma*d.sigma/2) * normCDF((math.Log(a)-d.mu-d.sigma*d.sigma)/d.sigma)
	}
	tF := 0.0
	if t > 0 {
		tF = t * cdf(t)
	}
	integral := d.scale*d.bodyMass - tF - (partial(d.scale) - partial(t))
	return (d.scale - t) - (1-d.tail)/d.bodyMass*integral + tailExcess(d.scale)
}

// LognormalParetoLoss is the LognormalPareto loss at cumulative probability
// u in [0, 1), in start-year dollars: one uniform draw gives one loss.
func (s SeverityParams) LognormalParetoLoss(u float64) float64 {
	return newLognormalPareto(s.Median, s.Sigma, s.Scale, s.Alpha).quantile(u)
}
```

  `lob.go`: `LognormalPareto SeverityKind = "lognormal_pareto"` documented; `SeverityParams` doc names Median, Sigma, Scale and Alpha for it; `validate` case requires median, sigma, scale positive and alpha above 1; the kind error lists four kinds. `expectedloss.go` adds the case trending `Median` and `Scale` by `inflationFactor`. `claim.go`: `case lob.LognormalPareto: return sev.LognormalParetoLoss(src.Uniform())`. `fields.go` adds four `severityFields` with `Kind: "lognormal_pareto"`; `fields_internal_test.go` accepts the kind.
- [ ] **Step 4: Run** `go test ./...` - passes, golden hashes unchanged (no preset uses the kind yet).
- [ ] **Step 5: Commit** "Add a lognormal_pareto severity kind: a lognormal body under a Pareto tail".

### Task 2: Personal motor's liability takes no excess, and both presets' injury moves to the new kind

**Files:** `internal/infrastructure/config/motor-personal.yaml`, `internal/infrastructure/config/motor-commercial.yaml`, `internal/application/golden_test.go`.

- [ ] **Step 1:** Personal: `no_excess: true` on `third_party_property` and `third_party_injury` in both blocks; injury severity `{kind: lognormal_pareto, median: 5000, sigma: 1.0, scale: 30000, alpha: 1.8}` in both blocks. Commercial: injury severity `{kind: lognormal_pareto, ...}` with a mean near 20k under the $1m limit.
- [ ] **Step 2: Calibrate** with an uncommitted sweep test over seeds 1-30 at each preset's gate book size, printing each metric's min, median and max against its band and the failing seeds. Re-tune each preset's injury `close_lag` (and frequencies to keep about three property damage claims per injury claim in personal) until all 30 seeds pass with margin.
- [ ] **Step 3:** Rewrite both YAMLs' comments for the new values and why. Run `go test ./...`; paste the four printed golden hashes after checking each move is from the preset change.
- [ ] **Step 4: Commit** "Recalibrate both presets on the lognormal_pareto injury body, and take no excess on personal liability (MR-8)".

### Task 3: Docs and hand-over

- [ ] README: the kind in the claim events diagram and the section text, the YAML severity kinds, personal liability taking no excess, the injury limits paragraph.
- [ ] `docs/architecture.md`: the severity kinds in the `lob` section.
- [ ] `docs/review.md`: delete MR-8, renumber MR-19, MR-20, MR-12 to 1-3.
- [ ] Delete this plan; run all checks; push; open the PR naming MR-8 as closed; stop.
