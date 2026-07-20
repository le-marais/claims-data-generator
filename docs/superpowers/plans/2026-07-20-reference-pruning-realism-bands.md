# Reference-data pruning follow-through and SL-1 resolution Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make code, tests, and docs consistent with the pruned single-vintage Schedule P reference data (96 dec2025 ppauto companies) and fully resolve review finding SL-1 by replacing near-vacuous min/max realism bands with P5-P95 percentile bands plus a mechanical degeneracy backstop.

**Architecture:** The realism band logic lives in the domain package (`internal/domain/triangle/compare.go`). We change how bands are computed (percentile + filter), keep the full min/max range for display, recalibrate the shipped preset so it lands inside the tighter bands, surface both bands in the web UI, delete the now-dead multi-vintage loader, and update the docs. TDD throughout: failing test, minimal implementation, green, commit.

**Tech Stack:** Go 1.x (standard library plus gonum for distributions), a vanilla-JS web front end, PowerShell tooling, Markdown docs. Tests are Go's `testing` package.

## Global Constraints

- Writing style in docs and comments: sentence case headers; never use em dashes, use spaced hyphens ` - `; do not invent content.
- Go: `go test ./...` and `go vet ./...` must both be green at the end of every task.
- Reference company names, band percentiles, and counts must match exactly across code, tests, and docs (96 companies; P5 and P95).
- Commit and push as separate commands; never chain them. This plan only commits (no pushes).
- End every commit message with the trailer: `Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>`

---

## File structure

- `internal/domain/triangle/compare.go` (modify) - percentile helper, `Band` with scored + outer range, `usableRefs` filter, rewritten `ATABands` and `CompareToReference`, SL-8 fix.
- `internal/domain/triangle/triangle_test.go` (modify) - update `TestBandsAcrossReferenceSets` and `TestCompareToReferencePassesInsideBands`; add percentile / filter / SL-8 tests.
- `internal/application/realism_test.go` (modify) - multi-seed gate; later, single-dir loader call.
- `internal/infrastructure/config/motor-personal.yaml` (modify) - recalibrate so the preset stays inside the tighter bands.
- `internal/infrastructure/web/viewmodel.go` (modify) - carry scored + outer band in the realism JSON.
- `internal/infrastructure/web/static/app.js` (modify) - draw outer faint + inner solid band, update wording/tooltip.
- `internal/infrastructure/web/static/style.css` (modify) - `.band-outer` style.
- `internal/infrastructure/schedulep/reader.go` (modify) - collapse to single-dir loading, drop vintage qualification.
- `internal/infrastructure/schedulep/reader_test.go` (modify) - fix count, remove multi-vintage tests.
- `data/reference/refdata.go` (modify) - single dir constant.
- `cmd/claimsgen/main.go` (modify) - single-dir loader call.
- `README.md`, `docs/roadmap.md`, `data/reference/gr-code-list.md`, the two sep2011 docs (modify) - documentation.

---

## Task 1: Percentile bands, degeneracy filter, and preset recalibration

**Files:**
- Modify: `internal/domain/triangle/compare.go`
- Test: `internal/domain/triangle/triangle_test.go`
- Modify: `internal/application/realism_test.go`
- Modify: `internal/infrastructure/config/motor-personal.yaml`

**Interfaces:**
- Consumes: `Triangle.ATAFactors() []float64`, `Triangle.latestDiagonal() []float64` (unexported, same package), `lossRatio(Triangle, []float64) (float64, bool)` (existing).
- Produces:
  - `type Band struct { Lo, Hi, Min, Max float64 }` - `Lo`/`Hi` are the scored P5-P95 band; `Min`/`Max` the full observed range. `contains(v)` scores against `Lo`/`Hi`.
  - `ATABands(triangles []Triangle) []Band` - unchanged signature, now percentile-based.
  - `CompareToReference(c Comparison, refs []ReferenceSet) Report` - unchanged signature; filters refs, scores against P5-P95, propagates the loss-ratio `ok` flag.
  - `Check` and `AgeCheck` keep their existing fields (`Value`, `Band`, `Within`; `AgeCheck` also `Age`); the `Band` they carry now has the four fields above.

- [ ] **Step 1: Write the failing percentile test**

Add to `internal/domain/triangle/triangle_test.go`:

```go
func TestPercentileInterpolates(t *testing.T) {
	// Values 10..50; Percentile sorts in place, so pass a fresh slice each call.
	cases := []struct {
		p    float64
		want float64
	}{
		{5, 12},  // rank 0.2 -> 10 + 0.2*(20-10)
		{50, 30}, // rank 2.0 -> xs[2]
		{95, 48}, // rank 3.8 -> 40 + 0.8*(50-40)
	}
	for _, c := range cases {
		if got := triangle.Percentile([]float64{50, 10, 30, 20, 40}, c.p); !approx(got, c.want) {
			t.Errorf("Percentile(p=%v) = %v, want %v", c.p, got, c.want)
		}
	}
	if got := triangle.Percentile([]float64{7}, 5); !approx(got, 7) {
		t.Errorf("Percentile single = %v, want 7", got)
	}
	if got := triangle.Percentile(nil, 5); !math.IsNaN(got) {
		t.Errorf("Percentile(nil) = %v, want NaN", got)
	}
}
```

Note: `Percentile` is exported so the external `triangle_test` package can call it directly. Ensure the test file imports `math` (it already imports `testing`; add `math` if missing).

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/domain/triangle/ -run TestPercentileInterpolates -v`
Expected: FAIL - `triangle.Percentile` undefined (build error).

- [ ] **Step 3: Implement the percentile helper and the new Band**

In `internal/domain/triangle/compare.go`, add `"sort"` to the import block, then replace the `Band` type and its `contains` method:

```go
// Band is the range of an age-to-age factor or loss ratio observed across
// reference companies. Lo and Hi are the scored percentile bounds (P5-P95);
// Min and Max are the full observed extremes, kept for display context.
type Band struct {
	Lo, Hi   float64
	Min, Max float64
}

func (b Band) contains(v float64) bool {
	return v >= b.Lo && v <= b.Hi
}

// bandLoPercentile and bandHiPercentile define the scored band. Widening them
// (towards 0 and 100) loosens the realism gate.
const (
	bandLoPercentile = 5.0
	bandHiPercentile = 95.0
)

// Percentile returns the linearly-interpolated p-th percentile (p in [0,100])
// of xs, where p=0 is the minimum and p=100 the maximum. xs is sorted in place.
// Returns NaN for empty xs.
func Percentile(xs []float64, p float64) float64 {
	if len(xs) == 0 {
		return math.NaN()
	}
	sort.Float64s(xs)
	if len(xs) == 1 {
		return xs[0]
	}
	rank := p / 100 * float64(len(xs)-1)
	lo := int(math.Floor(rank))
	hi := int(math.Ceil(rank))
	return xs[lo] + (rank-float64(lo))*(xs[hi]-xs[lo])
}

// bandFromValues builds a Band from the values observed for one metric: the
// scored P5-P95 range plus the full min/max. Values with fewer than one entry
// yield a NaN scored band that contains nothing.
func bandFromValues(xs []float64) Band {
	min, max := math.Inf(1), math.Inf(-1)
	for _, v := range xs {
		if v < min {
			min = v
		}
		if v > max {
			max = v
		}
	}
	return Band{
		Lo:  Percentile(xs, bandLoPercentile),
		Hi:  Percentile(xs, bandHiPercentile),
		Min: min,
		Max: max,
	}
}
```

- [ ] **Step 4: Run the percentile test to verify it passes**

Run: `go test ./internal/domain/triangle/ -run TestPercentileInterpolates -v`
Expected: PASS.

- [ ] **Step 5: Rewrite ATABands to collect values then band them**

In `internal/domain/triangle/compare.go`, replace the body of `ATABands`:

```go
// ATABands returns, per development age, the band of volume-weighted
// age-to-age factors observed across the given triangles.
func ATABands(triangles []Triangle) []Band {
	var perAge [][]float64
	for _, t := range triangles {
		for age, f := range t.ATAFactors() {
			if math.IsNaN(f) {
				continue
			}
			for age >= len(perAge) {
				perAge = append(perAge, nil)
			}
			perAge[age] = append(perAge[age], f)
		}
	}
	bands := make([]Band, len(perAge))
	for age, xs := range perAge {
		bands[age] = bandFromValues(xs)
	}
	return bands
}
```

- [ ] **Step 6: Update TestBandsAcrossReferenceSets for the new band shape**

In `internal/domain/triangle/triangle_test.go`, replace `TestBandsAcrossReferenceSets`'s assertions (the two companies give age-0 factors {1.5, 2.0} and age-1 factors {1.1, 1.05}):

```go
	bands := triangle.ATABands(paids)
	if len(bands) != 2 {
		t.Fatalf("got %d bands, want 2", len(bands))
	}
	// Full range is the min/max; the scored band is P5-P95 (interpolated).
	if !approx(bands[0].Min, 1.5) || !approx(bands[0].Max, 2.0) {
		t.Errorf("age 0 min/max = %+v, want [1.5, 2.0]", bands[0])
	}
	if !approx(bands[0].Lo, 1.525) || !approx(bands[0].Hi, 1.975) {
		t.Errorf("age 0 scored = [%v, %v], want [1.525, 1.975]", bands[0].Lo, bands[0].Hi)
	}
	if !approx(bands[1].Min, 1.05) || !approx(bands[1].Max, 1.1) {
		t.Errorf("age 1 min/max = %+v, want [1.05, 1.1]", bands[1])
	}
```

- [ ] **Step 7: Add the degeneracy-filter and SL-8 tests (failing)**

Add to `internal/domain/triangle/triangle_test.go`:

```go
func TestCompareFiltersDegenerateReferences(t *testing.T) {
	// Two healthy companies plus one with zero earned premium carrying an
	// extreme paid factor. The zero-premium company must not widen the band.
	refs := []triangle.ReferenceSet{
		{Name: "good1", Paid: triangle.Triangle{Cells: [][]float64{{100, 150}}},
			Incurred: triangle.Triangle{Cells: [][]float64{{140, 150}}}, EarnedPremium: []float64{200}},
		{Name: "good2", Paid: triangle.Triangle{Cells: [][]float64{{100, 160}}},
			Incurred: triangle.Triangle{Cells: [][]float64{{150, 160}}}, EarnedPremium: []float64{250}},
		{Name: "zeroEP", Paid: triangle.Triangle{Cells: [][]float64{{100, 500}}}, // ATA 5.0
			Incurred: triangle.Triangle{Cells: [][]float64{{100, 500}}}, EarnedPremium: []float64{0}},
	}
	c := triangle.Comparison{
		Paid:          triangle.Triangle{Cells: [][]float64{{100, 155}}},
		Incurred:      triangle.Triangle{Cells: [][]float64{{145, 155}}},
		EarnedPremium: []float64{220},
	}
	report := triangle.CompareToReference(c, refs)
	if len(report.PaidATA) == 0 {
		t.Fatal("no paid ATA checks")
	}
	if report.PaidATA[0].Band.Max > 2.0 {
		t.Errorf("paid band max = %v; zero-premium company was not filtered out", report.PaidATA[0].Band.Max)
	}
}

func TestCompareFailsWhenGeneratedHasNoPremium(t *testing.T) {
	refs := []triangle.ReferenceSet{
		{Name: "a", Incurred: triangle.Triangle{Cells: [][]float64{{140, 150}}}, EarnedPremium: []float64{200}},
		{Name: "b", Incurred: triangle.Triangle{Cells: [][]float64{{210, 200}}}, EarnedPremium: []float64{250}},
	}
	c := triangle.Comparison{
		Incurred:      triangle.Triangle{Cells: [][]float64{{180, 180}}},
		EarnedPremium: []float64{0}, // no premium -> loss ratio undefined
	}
	report := triangle.CompareToReference(c, refs)
	if report.LossRatio.Within {
		t.Error("loss ratio scored as within despite zero generated premium")
	}
}
```

- [ ] **Step 8: Run to verify the new tests fail**

Run: `go test ./internal/domain/triangle/ -run 'TestCompareFilters|TestCompareFailsWhen' -v`
Expected: FAIL - `TestCompareFiltersDegenerateReferences` sees the max at 5.0, and `TestCompareFailsWhenGeneratedHasNoPremium` still scores `Within == true` (old behaviour discards the `ok` flag).

- [ ] **Step 9: Add the filter and rewrite CompareToReference**

In `internal/domain/triangle/compare.go`, add the filter and replace `CompareToReference`:

```go
// usableRefs drops reference companies that carry no scorable signal: no
// earned premium, or an all-zero incurred triangle. Percentile bands handle
// ordinary outliers; this is a backstop for degenerate data (for example
// future un-curated per-line-of-business references).
func usableRefs(refs []ReferenceSet) []ReferenceSet {
	out := make([]ReferenceSet, 0, len(refs))
	for _, r := range refs {
		totalEP := 0.0
		for _, ep := range r.EarnedPremium {
			totalEP += ep
		}
		if totalEP <= 0 {
			continue
		}
		latest := 0.0
		for _, v := range r.Incurred.latestDiagonal() {
			latest += v
		}
		if latest <= 0 {
			continue
		}
		out = append(out, r)
	}
	return out
}

// CompareToReference scores the generated aggregates against the P5-P95 bands
// observed across the usable reference companies: volume-weighted age-to-age
// factors for paid and incurred, and the overall ultimate loss ratio. Only
// ages present in both generated and reference data are checked.
func CompareToReference(c Comparison, refs []ReferenceSet) Report {
	refs = usableRefs(refs)
	paidRef := make([]Triangle, len(refs))
	incRef := make([]Triangle, len(refs))
	for i, r := range refs {
		paidRef[i] = r.Paid
		incRef[i] = r.Incurred
	}
	report := Report{
		PaidATA:     checkAges(c.Paid.ATAFactors(), ATABands(paidRef)),
		IncurredATA: checkAges(c.Incurred.ATAFactors(), ATABands(incRef)),
	}

	var lrs []float64
	for _, r := range refs {
		if lr, ok := lossRatio(r.Incurred, r.EarnedPremium); ok {
			lrs = append(lrs, lr)
		}
	}
	lrBand := bandFromValues(lrs)
	value, ok := lossRatio(c.Incurred, c.EarnedPremium)
	report.LossRatio = Check{Value: value, Band: lrBand, Within: ok && lrBand.contains(value)}
	return report
}
```

Also update the `String()` method's loss-ratio and ATA lines to print the scored band (they currently read `c.Band.Min`, `c.Band.Max`); change them to `c.Band.Lo`, `c.Band.Hi` so the failure report shows the scored bounds:

```go
	writeChecks := func(name string, checks []AgeCheck) {
		for _, c := range checks {
			fmt.Fprintf(&b, "%s ATA age %d-%d: %.4f in [%.4f, %.4f] = %v\n",
				name, c.Age+1, c.Age+2, c.Value, c.Band.Lo, c.Band.Hi, c.Within)
		}
	}
	writeChecks("paid", r.PaidATA)
	writeChecks("incurred", r.IncurredATA)
	fmt.Fprintf(&b, "ultimate loss ratio: %.4f in [%.4f, %.4f] = %v\n",
		r.LossRatio.Value, r.LossRatio.Band.Lo, r.LossRatio.Band.Hi, r.LossRatio.Within)
```

- [ ] **Step 10: Update TestCompareToReferencePassesInsideBands, then run the domain package**

The existing `TestCompareToReferencePassesInsideBands` still holds under P5-P95 (inside LR 0.78 within [0.7525, 0.7975]; outside paid ATA 3.0 outside [1.525, 1.975]). No assertion change is required, but confirm it passes. Run the whole package:

Run: `go test ./internal/domain/triangle/ -v`
Expected: PASS for all tests (percentile, bands, filter, SL-8, compare, and the untouched aggregation tests).

- [ ] **Step 11: Make the realism gate multi-seed**

In `internal/application/realism_test.go`, replace `TestDefaultPresetIsRealistic` so it runs across several seeds (a recalibration that only fits seed 42 must not pass):

```go
func TestDefaultPresetIsRealistic(t *testing.T) {
	refs, err := schedulep.LoadFS(refdata.Files, refdata.PersonalMotorDirs...)
	if err != nil {
		t.Fatal(err)
	}
	for _, seed := range []uint64{1, 42, 7} {
		req := request(t)
		req.StartYear = 1998
		req.Years = 10
		req.InitialBookSize = 10000
		ds, err := application.GenerateDataset(random.NewSource(seed), req)
		if err != nil {
			t.Fatal(err)
		}
		report := application.EvaluateRealism(ds, refs, req.StartYear, req.Years)
		if !report.Pass() {
			t.Errorf("seed %d: generated data outside Schedule P bands:\n%s", seed, report)
		}
	}
}
```

- [ ] **Step 12: Run the gate and recalibrate the preset until green**

Run: `go test ./internal/application/ -run TestDefaultPresetIsRealistic -v`

Expected initially: possibly FAIL - the tighter P5-P95 bands may exclude one or more default-preset metrics. The failure report (from `String()`) names each metric, its value, and the `[Lo, Hi]` band it missed.

Recalibrate `internal/infrastructure/config/motor-personal.yaml` to re-centre the failing metrics inside their bands. Guidance on which knob moves which metric:
- Ultimate loss ratio too high/low: adjust `book.premium_rate_factor` (higher premium rate lowers the loss ratio) or the severity level (`claims.severity.*`, `claims.inflation.mean`).
- Paid/incurred ATA factors developing too fast/slow: adjust the close-lag and payment-pattern knobs (`claims.close_lag.*`, `runoff.*`) that control how quickly cumulative amounts mature.

Change the smallest number of knobs by the smallest amount that brings every metric inside its band, re-running the gate after each change.

**Stop-and-report guardrail:** if bringing the preset inside the bands requires moving any single knob by more than ~15% of its current value, or changing more than two knobs, do not force it - stop and report the failing metrics, the bands, and the knob changes considered, so a human can decide whether the bands or the model need rethinking (this is the recalibration-risk checkpoint flagged in the spec).

Re-run until: PASS across all three seeds.

- [ ] **Step 13: Update the preset calibration comment**

In `internal/infrastructure/config/motor-personal.yaml`, update the top calibration comment to describe the curated single-vintage pool and the percentile bands:

```yaml
# Personal motor insurance preset.
# Values are calibrated so that generated development triangles fall within
# the P5-P95 bands observed across the curated Schedule P private passenger
# auto reference data (data/reference/schedule p/dec2025, 96 hand-curated
# companies).
```

- [ ] **Step 14: Run the full suite and vet**

Run: `go test ./... && go vet ./...`
Expected: PASS everywhere (the web viewmodel still compiles - `Band.Min`/`Band.Max` still exist).

- [ ] **Step 15: Commit**

```bash
git add internal/domain/triangle/compare.go internal/domain/triangle/triangle_test.go internal/application/realism_test.go internal/infrastructure/config/motor-personal.yaml
git commit -m "$(cat <<'EOF'
Score realism against P5-P95 bands over filtered references

Resolves SL-1: replace min/max realism bands (which a single degenerate
company could set) with P5-P95 percentile bands over reference companies
that carry scorable signal, keep min/max for display, propagate the
loss-ratio ok flag (SL-8), run the gate across three seeds, and recalibrate
the motor preset to stay inside the tighter bands.

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
EOF
)"
```

---

## Task 2: Surface both bands in the web UI

**Files:**
- Modify: `internal/infrastructure/web/viewmodel.go:91-104` (`ageCheckJSON`, `checkJSON`), `:184-204` (`realismView`, `ageChecksView`)
- Modify: `internal/infrastructure/web/static/app.js:436-484` (realism section text and `bandCard`)
- Modify: `internal/infrastructure/web/static/style.css:161` (`.band` and new `.band-outer`)

**Interfaces:**
- Consumes: `triangle.Report` with `Check`/`AgeCheck` carrying a `Band{Lo, Hi, Min, Max}` (from Task 1).
- Produces: realism JSON where each check has `lo`, `hi` (scored band), `min`, `max` (outer band), `value`, `within`; `ageCheckJSON` also `age`. app.js renders a faint outer rect and a solid inner rect per check.

- [ ] **Step 1: Add scored-band fields to the JSON DTOs**

In `internal/infrastructure/web/viewmodel.go`, extend the two structs:

```go
type ageCheckJSON struct {
	Age    int     `json:"age"`
	Value  float64 `json:"value"`
	Lo     float64 `json:"lo"`
	Hi     float64 `json:"hi"`
	Min    float64 `json:"min"`
	Max    float64 `json:"max"`
	Within bool    `json:"within"`
}

type checkJSON struct {
	Value  float64 `json:"value"`
	Lo     float64 `json:"lo"`
	Hi     float64 `json:"hi"`
	Min    float64 `json:"min"`
	Max    float64 `json:"max"`
	Within bool    `json:"within"`
}
```

- [ ] **Step 2: Populate the new fields**

In `internal/infrastructure/web/viewmodel.go`, update `realismView` and `ageChecksView`:

```go
func realismView(r triangle.Report) realismJSON {
	return realismJSON{
		Pass:        r.Pass(),
		PaidATA:     ageChecksView(r.PaidATA),
		IncurredATA: ageChecksView(r.IncurredATA),
		LossRatio: checkJSON{
			Value:  r.LossRatio.Value,
			Lo:     r.LossRatio.Band.Lo,
			Hi:     r.LossRatio.Band.Hi,
			Min:    r.LossRatio.Band.Min,
			Max:    r.LossRatio.Band.Max,
			Within: r.LossRatio.Within,
		},
	}
}

func ageChecksView(checks []triangle.AgeCheck) []ageCheckJSON {
	out := make([]ageCheckJSON, len(checks))
	for i, c := range checks {
		out[i] = ageCheckJSON{
			Age: c.Age, Value: c.Value,
			Lo: c.Band.Lo, Hi: c.Band.Hi, Min: c.Band.Min, Max: c.Band.Max,
			Within: c.Within,
		}
	}
	return out
}
```

- [ ] **Step 3: Run the web package tests**

Run: `go test ./internal/infrastructure/web/ -v`
Expected: PASS - `server_test.go` decodes `min`/`max`/`within` (still present) and only asserts `len(PaidATA)` and `LossRatio.Value`.

- [ ] **Step 4: Draw both bands in app.js**

In `internal/infrastructure/web/static/app.js`, in `bandCard`, replace the single band rect with an outer (min/max) faint rect behind an inner (lo/hi) solid rect, and update the tooltip:

```js
    const outer = svgEl("rect", {
      x: x(c.min), y: cy - 5, width: Math.max(x(c.max) - x(c.min), 1), height: 10, rx: 5, class: "band-outer",
    });
    const band = svgEl("rect", {
      x: x(c.lo), y: cy - 4, width: Math.max(x(c.hi) - x(c.lo), 1), height: 8, rx: 4, class: "band",
    });
    const dot = svgEl("circle", { cx: x(c.value), cy, r: 5, class: c.within ? "dot" : "dot dot-out" });
    attachTooltip(dot, [
      c.value.toFixed(4),
      `P5-P95 ${c.lo.toFixed(4)} to ${c.hi.toFixed(4)}`,
      `min/max ${c.min.toFixed(4)} to ${c.max.toFixed(4)}`,
    ]);
```

Then update the `svg.append(...)` line in the same loop to include the outer rect, drawn first so it sits behind:

```js
    svg.append(label, outer, band, dot, status);
```

- [ ] **Step 5: Update the realism wording**

In `internal/infrastructure/web/static/app.js`, update the pass/fail summary text and the three `bandCard` titles (around lines 436-442) to name the scored band:

```js
    ? "✓ Pass - every metric inside the Schedule P P5-P95 reference band"
    : "✗ Fail - some metrics fall outside the Schedule P P5-P95 reference band";
```

```js
    bandCard("Paid age-to-age factors vs reference P5-P95 (min/max faint)", r.paid_ata || []),
    bandCard("Incurred age-to-age factors vs reference P5-P95 (min/max faint)", r.incurred_ata || []),
    bandCard("Ultimate loss ratio vs reference P5-P95 (min/max faint)", [{ ...r.loss_ratio, label: "ULR" }]),
```

- [ ] **Step 6: Add the outer-band style**

In `internal/infrastructure/web/static/style.css`, replace the `.band` line with two rules:

```css
.band-outer { fill: var(--grid); opacity: 0.4; }
.band { fill: var(--grid); }
```

- [ ] **Step 7: Run the web tests and vet**

Run: `go test ./internal/infrastructure/web/ && go vet ./...`
Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/infrastructure/web/viewmodel.go internal/infrastructure/web/static/app.js internal/infrastructure/web/static/style.css
git commit -m "$(cat <<'EOF'
Show P5-P95 scored band and min/max range in the realism UI

The realism JSON now carries the scored percentile band (lo/hi) alongside
the full observed range (min/max); the band card draws the min/max as a faint
outer bar behind the solid P5-P95 bar and names both in the tooltip.

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
EOF
)"
```

---

## Task 3: Simplify out the multi-vintage loader

**Files:**
- Modify: `internal/infrastructure/schedulep/reader.go:95-150` (`LoadFS`, `LoadDir`, `loadDirFS`)
- Modify: `data/reference/refdata.go`
- Modify: `cmd/claimsgen/main.go:116`
- Modify: `internal/application/realism_test.go:16,35`
- Test: `internal/infrastructure/schedulep/reader_test.go`

**Interfaces:**
- Consumes: `refdata.Files embed.FS`.
- Produces:
  - `refdata.PersonalMotorDir string` (was `PersonalMotorDirs []string`) - the single embedded dir.
  - `schedulep.LoadFS(fsys fs.FS, dir string) ([]triangle.ReferenceSet, error)` - single dir; company names are bare gr codes (no `vintage/` prefix).
  - `schedulep.LoadDir(dir string) ([]triangle.ReferenceSet, error)` - unchanged signature, names bare.

- [ ] **Step 1: Update the reader to single-dir, bare names**

In `internal/infrastructure/schedulep/reader.go`:

Replace `LoadFS` (drop the variadic and the vintage prefix):

```go
// LoadFS reads every reference company file in dir of fsys, files sorted by
// name for determinism. Company names are the bare file stem (for example
// "10007").
func LoadFS(fsys fs.FS, dir string) ([]triangle.ReferenceSet, error) {
	return loadDirFS(fsys, dir)
}
```

Replace `LoadDir`:

```go
// LoadDir reads every reference company file in a directory on disk, sorted by
// file name for determinism, with bare company names.
func LoadDir(dir string) ([]triangle.ReferenceSet, error) {
	clean := filepath.Clean(dir)
	refs, err := loadDirFS(os.DirFS(clean), ".")
	if errors.Is(err, errNoReferenceFiles) {
		return nil, fmt.Errorf("%w in %s", errNoReferenceFiles, dir)
	}
	return refs, err
}
```

Replace `loadDirFS` (drop the `vintage` parameter and the name qualification):

```go
func loadDirFS(fsys fs.FS, dir string) ([]triangle.ReferenceSet, error) {
	names, err := fs.Glob(fsys, path.Join(dir, "*.json"))
	if err != nil {
		return nil, err
	}
	if len(names) == 0 {
		return nil, fmt.Errorf("%w in %s", errNoReferenceFiles, dir)
	}
	sort.Strings(names)
	refs := make([]triangle.ReferenceSet, 0, len(names))
	for _, n := range names {
		b, err := fs.ReadFile(fsys, n)
		if err != nil {
			return nil, fmt.Errorf("reading reference file: %w", err)
		}
		ref, err := parse(path.Base(n), b)
		if err != nil {
			return nil, err
		}
		refs = append(refs, ref)
	}
	return refs, nil
}
```

Update the `LoadFile` doc comment (it references vintage qualification):

```go
// LoadFile reads one reference company file from disk, with a bare company
// name (the file stem).
```

Note: after this, `path.Dir`/`path.Clean` may become unused in `LoadFS`; `filepath` and `path` are still used by `LoadDir`/`loadDirFS`. Run `go build` and remove any now-unused import the compiler flags.

- [ ] **Step 2: Update refdata to a single dir constant**

Replace `data/reference/refdata.go` lines 10-14:

```go
// PersonalMotorDir is the embedded dataset backing the personal motor
// reference pool. dec2025 spans accident years 1998-2007; the companies are
// hand-curated (see data/reference/gr-code-list.md).
const PersonalMotorDir = "schedule p/dec2025/ppauto_pos98-07"
```

- [ ] **Step 3: Update the two production/test callers**

In `cmd/claimsgen/main.go:116`:

```go
	refs, err := schedulep.LoadFS(refdata.Files, refdata.PersonalMotorDir)
```

In `internal/application/realism_test.go`, both `schedulep.LoadFS(refdata.Files, refdata.PersonalMotorDirs...)` calls (in `TestDefaultPresetIsRealistic` and `TestEvaluateRealismProducesChecksAtEveryAge`) become:

```go
	refs, err := schedulep.LoadFS(refdata.Files, refdata.PersonalMotorDir)
```

- [ ] **Step 4: Rewrite the reader tests for single-vintage**

In `internal/infrastructure/schedulep/reader_test.go`:

Fix the count in `TestLoadDirReadsAllCompanies`:

```go
func TestLoadDirReadsAllCompanies(t *testing.T) {
	refs, err := schedulep.LoadDir(refDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 96 {
		t.Fatalf("loaded %d reference companies, want 96", len(refs))
	}
}
```

In `TestLoadKnownCompany`, the loaded name is already bare (`LoadFile` was always bare), so no change is needed there.

Rewrite `TestLoadFSEmbeddedMatchesDisk` for the single dir and bare names:

```go
func TestLoadFSEmbeddedMatchesDisk(t *testing.T) {
	embedded, err := schedulep.LoadFS(refdata.Files, refdata.PersonalMotorDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(embedded) != 96 {
		t.Fatalf("embedded reference sets = %d, want 96", len(embedded))
	}
	disk, err := schedulep.LoadDir(filepath.Join("../../../data/reference", refdata.PersonalMotorDir))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(embedded, disk) {
		t.Fatal("embedded reference sets differ from disk")
	}
}
```

Delete these now-obsolete tests entirely: `TestLoadFSMergesDirsWithQualifiedNames`, `TestLoadFSErrorsWhenAnyDirIsEmpty`, `TestLoadDirQualifiesNamesByVintage`, and the `minimalRef` constant they use.

Keep `TestLoadDirEmptyNamesDirectory` and `TestLoadFSErrorsOnNoDirs`? `TestLoadFSErrorsOnNoDirs` called `LoadFS(fstest.MapFS{})` with no dirs; `LoadFS` now takes a single required dir, so replace it with a missing-dir error test:

```go
func TestLoadFSErrorsOnEmptyDir(t *testing.T) {
	_, err := schedulep.LoadFS(fstest.MapFS{}, "schedule p/dec2025/ppauto_pos98-07")
	if err == nil {
		t.Fatal("LoadFS on an empty FS: want error, got nil")
	}
}
```

After the edits, `fstest` is still imported (used above); `strings` is still used by `TestLoadDirEmptyNamesDirectory`; `triangle` is no longer referenced in the test file (the old `TestLoadFSEmbeddedMatchesDisk` built a `[]triangle.ReferenceSet`), so remove the `triangle` import. Run `go build` / `go vet` and fix any unused imports the compiler reports.

- [ ] **Step 5: Run the schedulep and application tests**

Run: `go test ./internal/infrastructure/schedulep/ ./internal/application/ -v`
Expected: PASS - 96-company counts hold, the multi-vintage tests are gone, the gate still passes.

- [ ] **Step 6: Run the full suite and vet**

Run: `go test ./... && go vet ./...`
Expected: PASS everywhere.

- [ ] **Step 7: Commit**

```bash
git add internal/infrastructure/schedulep/reader.go internal/infrastructure/schedulep/reader_test.go data/reference/refdata.go cmd/claimsgen/main.go internal/application/realism_test.go
git commit -m "$(cat <<'EOF'
Collapse the reference loader to a single vintage

With the sep2011 vintage removed, drop the multi-dir merge and vintage-
qualified naming: LoadFS takes one dir, company names are bare gr codes,
refdata exposes a single PersonalMotorDir. Fix the reader company count to
96 and remove the two-vintage tests.

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
EOF
)"
```

---

## Task 4: Documentation

**Files:**
- Modify: `README.md:69,71-73` (Realism section and the "no code changes" sentence context around reference data)
- Modify: `docs/roadmap.md:10`
- Modify: `data/reference/gr-code-list.md:1` (add a header)
- Modify: `docs/superpowers/specs/2026-07-17-sep2011-reference-pool-design.md:1`, `docs/superpowers/plans/2026-07-17-sep2011-reference-pool.md:1` (superseded notes)

**Interfaces:** none (docs only).

- [ ] **Step 1: Rewrite the README Realism section**

In `README.md`, replace the Realism paragraph (line ~73):

```markdown
## Realism

Generated data is checked against 96 hand-curated Schedule P private passenger
auto reference companies (`data/reference/schedule p/dec2025/ppauto_pos98-07/`,
accident years 1998-2007). The companies were curated from the full Schedule P
extract via `data/reference/gr-code-list.md` and `tools/prune-dec2025.ps1` to
remove low-volume and degenerate companies. Paid and incurred age-to-age
development factors and the ultimate loss ratio must fall inside the P5-P95
bands observed across those companies, with a backstop filter that drops any
company carrying no scorable signal; the full min/max range is shown for
context. The paid comparison is net of recoveries, matching how Schedule P
reports paid losses. This runs as a test gate (`TestDefaultPresetIsRealistic`,
across several seeds).
```

- [ ] **Step 2: Check the UI-description line for stale reference wording**

In `README.md`, confirm line ~36 ("The Schedule P reference data is embedded in the binary.") and line ~49 ("bands observed across the Schedule P reference companies") are still accurate - they are (no vintage or count claim). No change unless a "two vintages"/"289" phrase is present; if found, delete the vintage/count clause. Do not edit otherwise.

- [ ] **Step 3: Fix the roadmap count**

In `docs/roadmap.md:10`, replace the realism-gate bullet:

```markdown
- **Realism gate** - generated motor data is scored against the 96 hand-curated Schedule P private passenger auto reference companies; the shipped preset must land inside the observed P5-P95 bands (`TestDefaultPresetIsRealistic`).
```

- [ ] **Step 4: Add a header to the keep-list**

At the top of `data/reference/gr-code-list.md`, insert:

```markdown
<!--
Hand-curated keep-list for the Schedule P dec2025 reference data. One
"<lob>: <grcode>" entry per line, across all six Schedule P lines of business.
Low-volume and degenerate companies were removed by judgement so the realism
bands reflect typical experience. Only the ppauto entries are embedded and used
by the realism gate today; the other lines are kept for future per-line-of-
business calibration. Apply the list with `tools/prune-dec2025.ps1` (dry-run by
default; pass -Apply to delete).
-->

```

Leave the existing `comauto: 10022` ... lines untouched below the comment.

- [ ] **Step 5: Mark the two sep2011 docs superseded**

At the very top of `docs/superpowers/specs/2026-07-17-sep2011-reference-pool-design.md`, insert:

```markdown
> **Superseded (2026-07-20):** the sep2011 vintage was removed and the reference pool is now single-vintage (dec2025, hand-curated). See `docs/superpowers/specs/2026-07-20-reference-pruning-realism-bands-design.md`. Kept for historical context.

```

At the very top of `docs/superpowers/plans/2026-07-17-sep2011-reference-pool.md`, insert:

```markdown
> **Superseded (2026-07-20):** the sep2011 vintage was removed; this plan is historical. See `docs/superpowers/plans/2026-07-20-reference-pruning-realism-bands.md`.

```

- [ ] **Step 6: Verify no stale references remain**

Run: `grep -rn "sep2011\|two vintage\|two-vintage\|289\|~143" README.md docs/roadmap.md`
Expected: no matches in `README.md` or `docs/roadmap.md` (matches inside the historical `docs/superpowers/specs|plans/*sep2011*` files and `docs/code-review-2026-07-18.md` are fine and expected).

- [ ] **Step 7: Final full verification**

Run: `go test ./... && go vet ./...`
Expected: PASS everywhere.

- [ ] **Step 8: Commit**

```bash
git add README.md docs/roadmap.md data/reference/gr-code-list.md docs/superpowers/specs/2026-07-17-sep2011-reference-pool-design.md docs/superpowers/plans/2026-07-17-sep2011-reference-pool.md
git commit -m "$(cat <<'EOF'
Update docs for the curated single-vintage reference pool

README and roadmap now describe 96 hand-curated dec2025 ppauto companies and
the P5-P95 realism bands (was 289 across two vintages / min-max). Document the
keep-list and prune tool, and mark the sep2011 design and plan superseded.

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
EOF
)"
```

---

## Self-review notes

- **Spec coverage:** Task 1 covers spec sections 1 (bands + filter + SL-8) and 3 (recalibration) and 5 (multi-seed gate, new unit tests); Task 2 covers section 2 (UI); Task 3 covers section 4 (simplify loader) and the broken-test fix in section 5; Task 4 covers section 6 (docs). Section 7 (verification) is the final step of Tasks 1, 3, and 4.
- **Ordering rationale:** Task 1 keeps `Band.Min`/`Band.Max`, so the web viewmodel compiles unchanged and every package stays green before Task 2 wires the new `lo`/`hi` fields. Task 3's loader signature change touches `realism_test.go`, which Task 1 already rewrote for multi-seed - sequential edits, no conflict.
- **Recalibration is empirical, not a placeholder:** Step 12 gives the exact command, the failure-diagnostic source, the knob-to-metric mapping, and a stop-and-report threshold. The specific YAML values cannot be pre-computed; the loop and guardrail are the deliverable.
