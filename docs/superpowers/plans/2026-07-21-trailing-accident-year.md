# Trailing partial accident year (MF-2) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Stop generating claims whose occurrence falls outside the run window, so `claims.csv`, the summary, the triangles, and the UI header all count the same claims.

**Architecture:** The claim simulator learns the run window (`startYear`, `years`). Each policy's Poisson frequency is pro-rated by its in-window exposed fraction of the cover term, and each occurrence is drawn uniformly over the in-window portion of the cover. No occurrence can land on or after Jan 1 of `startYear+years`.

**Tech Stack:** Go, standard library only. Tests are `go test`.

## Global Constraints

- Design doc: `docs/superpowers/specs/2026-07-21-trailing-accident-year-design.md`.
- Comments and docs use spaced hyphens ` - `, never em dashes.
- Run `go test ./...` and `go vet ./...` before every commit; both must pass.
- Per-policy RNG split streams (`"claims-policy-%d"`) must stay untouched, so determinism holds and only tail policies change.
- **Ordering:** if the own-damage severity rework plan (`2026-07-21-own-damage-severity-rework.md`) is also being executed, run these two plans sequentially; whichever lands second regenerates the golden hash. This plan assumes it may run before or after - Task 3 always refreshes the hash.

---

### Task 1: Window the claim simulator

**Files:**
- Modify: `internal/domain/claim/claim.go`
- Test: `internal/domain/claim/window_test.go` (create)

**Interfaces:**
- Produces: `func (s *ClaimSimulator) WithWindow(startYear, years int) *ClaimSimulator`. When set, `Simulate` pro-rates each policy's Poisson mean by `exposedFraction = in-window cover days / term days`, and `simulateClaim` draws the occurrence uniformly over `[CoverStart, min(CoverEnd, windowEnd)]`, `windowEnd = Jan 1 of startYear+years`. Unset (zero value) leaves the current full-term behaviour.

- [ ] **Step 1: Write the failing test**

Create `internal/domain/claim/window_test.go`:

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

func windowParams() lob.ClaimParams {
	return lob.ClaimParams{
		BaseFrequency:   3.0, // high, so tail policies would spill without windowing
		ReportLagMedian: 2,
		ReportLagSigma:  1.2,
		Severity: lob.SeverityParams{
			ThirdPartyWeight:        0.2,
			OwnDamageMedianFraction: 0.12,
			OwnDamageSigma:          1.0,
			ThirdPartyScale:         4000,
			ThirdPartyAlpha:         2.2,
		},
		CloseLag: lob.CloseLagParams{Shape: 1.2, MeanDays: 120, SizeThreshold: 20000, SizeMultiplier: 6, RiskLoading: 0.3, ThirdPartyShape: 1.0, ThirdPartyMeanDays: 680},
	}
}

// lateBook writes policies deep in the final underwriting year, whose 12-month
// cover spills into the year after the window.
func lateBook(startYear, years, n int) []policy.Policy {
	var b []policy.Policy
	lastUY := startYear + years - 1
	start := shared.NewDate(lastUY, time.December, 1) // cover runs into lastUY+1
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

func TestWindowedOccurrencesStayInWindow(t *testing.T) {
	const startYear, years = 1998, 10
	windowEnd := shared.NewDate(startYear+years, time.January, 1)
	claims := NewClaimSimulator(windowParams()).
		WithWindow(startYear, years).
		Simulate(random.NewSource(1), lateBook(startYear, years, 2000))
	if len(claims) == 0 {
		t.Fatal("no claims generated")
	}
	for _, c := range claims {
		if !c.OccurrenceDate.Before(windowEnd) {
			t.Fatalf("claim %d occurred %s, on/after window end %s", c.ID, c.OccurrenceDate, windowEnd)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/domain/claim/ -run TestWindowedOccurrencesStayInWindow -v`
Expected: FAIL - `WithWindow` undefined (compile error).

- [ ] **Step 3: Add the window field, builder, and exposed-fraction helper**

In `internal/domain/claim/claim.go`, extend `ClaimSimulator`:

```go
type ClaimSimulator struct {
	params    lob.ClaimParams
	inflation InflationIndex
	windowEnd shared.Date // zero value means no windowing
}
```

(If the severity-rework plan already added `sumInsuredInflation`/`startYear` fields, keep those and add `windowEnd` alongside.)

Add the builder and helper:

```go
// WithWindow constrains claim occurrences to the run window [startYear, startYear+years):
// each policy's frequency is pro-rated by its in-window exposed fraction of the
// cover term, and occurrences are drawn only over the in-window portion of the
// cover. This stops the trailing underwriting year from spilling a partial,
// out-of-window accident year into claims.csv (MF-2). Unset leaves full-term
// behaviour.
func (s *ClaimSimulator) WithWindow(startYear, years int) *ClaimSimulator {
	s.windowEnd = shared.NewDate(startYear+years, time.January, 1)
	return s
}

// exposedFraction is the share of a policy's cover term that lies inside the
// window; 1 when the window is unset or the cover ends before window end.
func (s *ClaimSimulator) exposedFraction(pol policy.Policy) float64 {
	if s.windowEnd.IsZero() {
		return 1
	}
	end := pol.CoverEnd
	if s.windowEnd.Before(end) {
		end = s.windowEnd
	}
	term := shared.DaysBetween(pol.CoverStart, pol.CoverEnd)
	if term <= 0 {
		return 1
	}
	inWindow := shared.DaysBetween(pol.CoverStart, end)
	return float64(inWindow) / float64(term)
}
```

Add the `time` import to `claim.go` if not already present.

- [ ] **Step 4: Pro-rate the frequency in Simulate**

In `Simulate`, multiply the Poisson mean by the exposed fraction:

```go
	for _, pol := range book {
		stream := src.Split(fmt.Sprintf("claims-policy-%d", pol.ID))
		n := stream.Poisson(s.params.BaseFrequency * pol.RiskFactor * s.exposedFraction(pol))
		for i := 0; i < n; i++ {
			if c, ok := s.simulateClaim(stream, pol); ok {
				claims = append(claims, c)
			}
		}
	}
```

- [ ] **Step 5: Constrain the occurrence draw in simulateClaim**

Replace the occurrence lines at the top of `simulateClaim`:

```go
	end := pol.CoverEnd
	if !s.windowEnd.IsZero() && s.windowEnd.Before(end) {
		end = s.windowEnd
	}
	span := shared.DaysBetween(pol.CoverStart, end)
	occurrence := pol.CoverStart.AddDays(int(src.Uniform() * float64(span+1)))
```

- [ ] **Step 6: Run test to verify it passes**

Run: `go test ./internal/domain/claim/ -run TestWindowedOccurrencesStayInWindow -v`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/domain/claim/claim.go internal/domain/claim/window_test.go
git commit -m "Window claim occurrences and pro-rate tail frequency (MF-2)"
```

---

### Task 2: Wire the window and simplify the inflation span

**Files:**
- Modify: `internal/application/generate.go:52-60`
- Test: `internal/application/window_test.go` (create)

**Interfaces:**
- Consumes: `ClaimSimulator.WithWindow` (Task 1).
- Produces: `GenerateDataset` produces datasets where every claim occurs inside the window and `len(ds.Claims)` equals the summary claim total.

- [ ] **Step 1: Write the failing counts-agree test**

Create `internal/application/window_test.go`:

```go
package application_test

import (
	"testing"
	"time"

	"github.com/le-marais/claimsgen/internal/application"
	"github.com/le-marais/claimsgen/internal/domain/shared"
	"github.com/le-marais/claimsgen/internal/infrastructure/random"
)

// TestNoClaimsOutsideWindow proves MF-2: no generated claim occurs on or after
// Jan 1 of startYear+years, so the CSV cannot carry a trailing partial year.
func TestNoClaimsOutsideWindow(t *testing.T) {
	req := request(t)
	req.StartYear = 1998
	req.Years = 10
	req.InitialBookSize = 4000
	ds, err := application.GenerateDataset(random.NewSource(1), req)
	if err != nil {
		t.Fatal(err)
	}
	windowEnd := shared.NewDate(req.StartYear+req.Years, time.January, 1)
	for _, c := range ds.Claims {
		if !c.OccurrenceDate.Before(windowEnd) {
			t.Fatalf("claim %d occurred %s, outside window", c.ID, c.OccurrenceDate)
		}
	}
}

// TestHeaderCountMatchesSummary proves the header (len(ds.Claims)) and the
// per-year summary agree once no claim falls outside the window.
func TestHeaderCountMatchesSummary(t *testing.T) {
	req := request(t)
	req.StartYear = 1998
	req.Years = 10
	req.InitialBookSize = 4000
	ds, err := application.GenerateDataset(random.NewSource(1), req)
	if err != nil {
		t.Fatal(err)
	}
	report := application.Summarize(ds, req.StartYear, req.Years)
	if len(ds.Claims) != report.Total.Claims {
		t.Fatalf("header count %d != summary total %d", len(ds.Claims), report.Total.Claims)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/application/ -run 'TestNoClaimsOutsideWindow|TestHeaderCountMatchesSummary' -v`
Expected: FAIL - claims still spill past the window; counts differ.

- [ ] **Step 3: Wire WithWindow and simplify the inflation span**

In `internal/application/generate.go`, add `WithWindow` to the chain and drop the `+1` from the inflation span (occurrences can no longer spill past the window):

```go
	// Occurrences are constrained to the window (MF-2), so the inflation index
	// only needs to span the window years; the For clamp stays as a defensive
	// fallback.
	inflation := claim.NewInflationIndex(src.Split("inflation"), req.LOB.Claims.Inflation, req.StartYear, req.Years)
	claims := claim.NewClaimSimulator(req.LOB.Claims).
		WithInflation(inflation).
		WithWindow(req.StartYear, req.Years).
		Simulate(src.Split("claims"), book)
```

(If the severity-rework plan also adds `WithBaseYear` to this chain, keep both builder calls.)

Update the comment above the inflation line (currently describing the `Years+1` span) to the new wording.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/application/ -run 'TestNoClaimsOutsideWindow|TestHeaderCountMatchesSummary' -v`
Expected: PASS.

- [ ] **Step 5: Confirm determinism is intact**

Run: `go test ./internal/application/ -run 'TestGenerateDatasetLinksTheThreeDatasets|TestNilClaimsDoNotShiftOtherStages' -v`
Expected: PASS (per-policy streams untouched).

- [ ] **Step 6: Commit**

```bash
git add internal/application/generate.go internal/application/window_test.go
git commit -m "Wire claim window and trim inflation span (MF-2)"
```

---

### Task 3: Refresh the golden fixture and confirm realism

**Files:**
- Modify: `internal/application/golden_test.go:19` (`wantHash`)
- Modify (docs): `README.md` - remove any note about the trailing partial accident year in claims.csv, if present

**Interfaces:**
- Consumes: the full windowed generation path (Tasks 1-2).

- [ ] **Step 1: Confirm realism still passes**

Run: `go test ./internal/application/ -run TestDefaultPresetIsRealistic -v`
Expected: PASS - dropping a partial-exposure trailing year does not change the mature development factors. If it fails, stop and investigate before touching the golden hash.

- [ ] **Step 2: Refresh the golden hash**

Run: `go test ./internal/application/ -run TestGoldenCSVBytes -v`
Expected: FAIL with a printed `got:` hash (the tail claims are gone, so bytes changed). Copy that hash into `wantHash` at `internal/application/golden_test.go:19`.

- [ ] **Step 3: Check the README**

Search `README.md` for any statement that claims.csv contains an extra/trailing accident year the in-app views drop. If present, remove or correct it (claims are now windowed).

Run: `grep -ni "trailing\|partial.*year\|extra.*accident" README.md`

- [ ] **Step 4: Full suite**

Run: `go test ./... && go vet ./...`
Expected: PASS / clean.

- [ ] **Step 5: Commit**

```bash
git add internal/application/golden_test.go README.md
git commit -m "Refresh golden and docs after windowing claims (MF-2)"
```

---

## Self-Review

- **Spec coverage:** window occurrences → Task 1 (occurrence constraint) + Task 2 (wiring); pro-rate tail frequency → Task 1 Step 4; no out-of-window claims → Task 1/Task 2 tests; counts agree → Task 2 test; inflation span simplification → Task 2 Step 3; golden + docs → Task 3. All design sections covered.
- **Placeholders:** none - every step has concrete code or an exact command.
- **Type consistency:** `WithWindow(startYear, years int)`, `exposedFraction(pol)`, and the `windowEnd shared.Date` field are used consistently across Task 1 and Task 2; `Summarize(ds, startYear, years)` matches `internal/application/summary.go:42`.
