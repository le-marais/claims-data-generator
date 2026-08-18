# Monthly triangles and monthly exposure implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Aggregate a generated dataset into incremental monthly triangles (paid, paid net of recoveries, incurred, reported claim counts) plus monthly exposure, write both to CSV, and re-express the existing annual triangles as a coarsened view of the same monthly grid.

**Architecture:** A new `shared.Month` value type gives month arithmetic. `triangle.MonthlyGrid` becomes the single aggregation store: incremental cells, one row per origin month, development running to full runoff. `MonthlyGrid.Coarsen` aggregates it onto any coarser calendar grain (monthly, quarterly, annual) by keying both axes on the calendar period the month falls in; the annual triangles the realism gate and UI read are `Coarsen(Annual, 10, foldTail)` cumulated, so they stay identical cell for cell. `application.Aggregates` builds the grid once per run and hands out the annual triangles, the monthly grid and the exposure; `csv.WriteAggregates` writes `triangles.csv` and `exposure.csv`.

**Tech Stack:** Go 1.26, standard library only for this work. Tests are table-driven `_test.go` files beside the code, external test packages (`package foo_test`) by default.

## Global constraints

- Spec: `docs/superpowers/specs/2026-08-18-monthly-triangles-design.md`. Read it before starting; it carries the rationale for every decision below.
- Module path is `github.com/le-marais/claimsgen`. Import paths are `github.com/le-marais/claimsgen/internal/...`.
- **Do not add dependencies.** Standard library only.
- **Reproducibility is a hard invariant.** Everything in this plan is pure aggregation over an already-generated dataset. Do not touch the generation path, do not draw randomness, do not introduce wall-clock time or map iteration order into any output.
- **Dependency direction:** `internal/domain` depends on nothing outside itself; `internal/application` orchestrates the domain; `internal/infrastructure` adapts the outside world. Never import infrastructure from domain.
- **Byte-stable output.** Identical inputs must produce byte-identical CSV files. Fixed-precision float formatting only, deterministic row order, no map iteration in output paths.
- Run `go test ./...` and `go vet ./...` before claiming any task done, and both again before opening the PR.
- Work on branch `feature/monthly-triangles` (already created). Never commit to `main`. Commit at the end of every task.
- Writing style for docs and comments: sentence case headers, no em dashes (use spaced hyphens ` - `), concise and factual.
- Development period numbering is **1-based** everywhere it is surfaced to a user or an API: CSV columns, JSON fields, UI labels. Raw Go slices stay 0-indexed, with slice index `d` holding development period `d+1`, stated in a doc comment.

## File structure

**Created:**

| File | Responsibility |
|---|---|
| `internal/domain/shared/month.go` | `Month` value type: calendar-month arithmetic, formatting, quarter and year accessors |
| `internal/domain/shared/month_test.go` | Tests for the above |
| `internal/domain/triangle/basis.go` | `OriginBasis` enum and its validation |
| `internal/domain/triangle/exposure.go` | `MonthExposure`, `ExposureByMonth`, `EarnedPremiumByYear` (moved here), the shared `overlapDays` helper |
| `internal/domain/triangle/exposure_test.go` | Tests for exposure on both bases |
| `internal/domain/triangle/monthly.go` | `Measure`, `MonthlyGrid`, `BuildMonthlyGrid` - the canonical incremental store |
| `internal/domain/triangle/monthly_test.go` | Tests for grid construction |
| `internal/domain/triangle/coarsen.go` | `PeriodKind`, `IncrementalSet`, `Coarsen`, `Cumulative`, `AnnualSet`, `AnnualTriangles` |
| `internal/domain/triangle/coarsen_test.go` | Tests for coarsening and cumulation |
| `internal/application/aggregate.go` | `Aggregates` and `Aggregate` - one aggregation pass per run |
| `internal/application/aggregate_test.go` | Oracle test against the pre-refactor annual aggregation, plus reconciliation tests |
| `internal/infrastructure/csv/monthly.go` | `WriteAggregates` - `triangles.csv` and `exposure.csv` |
| `internal/infrastructure/csv/monthly_test.go` | Tests for the two new files |

**Modified:**

| File | Change |
|---|---|
| `internal/domain/triangle/triangle.go` | `EarnedPremiumByYear` and `overlapDays` move out; the three annual constructors become grid-backed, then are deleted in Task 6 |
| `internal/domain/triangle/triangle_test.go` | The five tests calling the annual constructors move onto `AnnualTriangles` |
| `internal/domain/triangle/compare.go` | `AgeCheck.Age` becomes 1-based; `Report.String()` stops adding 1 |
| `internal/application/realism.go` | `EvaluateRealism` takes an `Aggregates`; the duplicate `developmentYears` constant goes |
| `internal/application/realism_test.go` | Call sites updated |
| `internal/infrastructure/csv/writer.go` | Package doc says five files; `formatAmount` helper shared with `monthly.go` |
| `internal/infrastructure/csv/golden` (in `internal/application/golden_test.go`) | Second hash pinning the two new files |
| `cmd/claimsgen/main.go` | `--origin-basis` flag, `WriteAggregates` call, extended success line, usage text |
| `cmd/claimsgen/main_test.go` | Coverage for the flag and the new files |
| `internal/infrastructure/web/server.go` | `origin_basis` in the request, basis validation, `WriteAggregates` call |
| `internal/infrastructure/web/viewmodel.go` | `buildResponse` reads the aggregate; local `developmentYears` deleted |
| `internal/infrastructure/web/server_test.go` | Coverage for `origin_basis` |
| `internal/infrastructure/web/static/index.html` | Origin basis select |
| `internal/infrastructure/web/static/app.js` | Sends `origin_basis`, shows it in the run line, 1-based band labels |
| `README.md`, `AGENTS.md`, `docs/roadmap.md`, `docs/todo.md`, `docs/detailed-architecture.md` | Documentation |

---

### Task 1: `shared.Month`

A calendar-month value type. Held as an absolute month index so differences and offsets are integer arithmetic with no date construction, and so two `Month` values compare with `==`.

**Files:**
- Create: `internal/domain/shared/month.go`
- Create: `internal/domain/shared/month_test.go`

**Interfaces:**
- Consumes: `shared.Date` and `shared.NewDate` from `internal/domain/shared/date.go`.
- Produces: `shared.Month` (comparable struct), `shared.NewMonth(year int, m time.Month) Month`, `(Date) Month() Month`, `(Month) Year() int`, `(Month) Month() time.Month`, `(Month) Quarter() int`, `(Month) Add(n int) Month`, `(Month) Start() Date`, `(Month) End() Date`, `(Month) String() string`, `shared.MonthsBetween(a, b Month) int`.

- [ ] **Step 1: Write the failing test**

Create `internal/domain/shared/month_test.go`:

```go
package shared_test

import (
	"testing"
	"time"

	"github.com/le-marais/claimsgen/internal/domain/shared"
)

func TestMonthStringIsZeroPadded(t *testing.T) {
	cases := []struct {
		m    shared.Month
		want string
	}{
		{shared.NewMonth(1998, time.January), "1998-01"},
		{shared.NewMonth(1998, time.October), "1998-10"},
		{shared.NewMonth(2007, time.December), "2007-12"},
	}
	for _, c := range cases {
		if got := c.m.String(); got != c.want {
			t.Errorf("String() = %q, want %q", got, c.want)
		}
	}
}

func TestDateMonth(t *testing.T) {
	got := shared.NewDate(1998, time.March, 15).Month()
	if want := shared.NewMonth(1998, time.March); got != want {
		t.Errorf("Month() = %v, want %v", got, want)
	}
}

func TestMonthAddCrossesYearBoundaries(t *testing.T) {
	cases := []struct {
		from shared.Month
		n    int
		want shared.Month
	}{
		{shared.NewMonth(1998, time.December), 1, shared.NewMonth(1999, time.January)},
		{shared.NewMonth(1999, time.January), -1, shared.NewMonth(1998, time.December)},
		{shared.NewMonth(1998, time.January), 24, shared.NewMonth(2000, time.January)},
		{shared.NewMonth(1998, time.March), 0, shared.NewMonth(1998, time.March)},
	}
	for _, c := range cases {
		if got := c.from.Add(c.n); got != c.want {
			t.Errorf("%v.Add(%d) = %v, want %v", c.from, c.n, got, c.want)
		}
	}
}

func TestMonthsBetween(t *testing.T) {
	cases := []struct {
		a, b shared.Month
		want int
	}{
		{shared.NewMonth(1998, time.January), shared.NewMonth(1999, time.January), 12},
		{shared.NewMonth(1998, time.March), shared.NewMonth(1999, time.January), 10},
		{shared.NewMonth(1999, time.January), shared.NewMonth(1998, time.January), -12},
		{shared.NewMonth(1998, time.June), shared.NewMonth(1998, time.June), 0},
	}
	for _, c := range cases {
		if got := shared.MonthsBetween(c.a, c.b); got != c.want {
			t.Errorf("MonthsBetween(%v, %v) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestMonthYearMonthAndQuarter(t *testing.T) {
	cases := []struct {
		m       shared.Month
		year    int
		month   time.Month
		quarter int
	}{
		{shared.NewMonth(1998, time.January), 1998, time.January, 1},
		{shared.NewMonth(1998, time.March), 1998, time.March, 1},
		{shared.NewMonth(1998, time.April), 1998, time.April, 2},
		{shared.NewMonth(1998, time.September), 1998, time.September, 3},
		{shared.NewMonth(2007, time.December), 2007, time.December, 4},
	}
	for _, c := range cases {
		if got := c.m.Year(); got != c.year {
			t.Errorf("%v.Year() = %d, want %d", c.m, got, c.year)
		}
		if got := c.m.Month(); got != c.month {
			t.Errorf("%v.Month() = %v, want %v", c.m, got, c.month)
		}
		if got := c.m.Quarter(); got != c.quarter {
			t.Errorf("%v.Quarter() = %d, want %d", c.m, got, c.quarter)
		}
	}
}

func TestMonthStartAndEnd(t *testing.T) {
	// February 2000 is a leap February, so End must be the 29th.
	m := shared.NewMonth(2000, time.February)
	if got, want := m.Start().String(), "2000-02-01"; got != want {
		t.Errorf("Start() = %s, want %s", got, want)
	}
	if got, want := m.End().String(), "2000-02-29"; got != want {
		t.Errorf("End() = %s, want %s", got, want)
	}
	dec := shared.NewMonth(1998, time.December)
	if got, want := dec.End().String(), "1998-12-31"; got != want {
		t.Errorf("Start() = %s, want %s", got, want)
	}
}

func TestNewMonthNormalisesOutOfRangeMonths(t *testing.T) {
	if got, want := shared.NewMonth(1998, time.Month(13)), shared.NewMonth(1999, time.January); got != want {
		t.Errorf("NewMonth(1998, 13) = %v, want %v", got, want)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/domain/shared/ -run TestMonth -v`
Expected: compile failure - `undefined: shared.NewMonth`.

- [ ] **Step 3: Write the implementation**

Create `internal/domain/shared/month.go`:

```go
package shared

import (
	"fmt"
	"time"
)

// Month is a calendar month, held as an absolute month index so offsets and
// differences are integer arithmetic and two months compare with ==.
type Month struct {
	index int // year*12 + int(month) - 1
}

// NewMonth builds a calendar month. The month number is normalised, so
// NewMonth(1998, time.Month(13)) is January 1999.
func NewMonth(year int, m time.Month) Month {
	return Month{index: year*12 + int(m) - 1}
}

// Month returns the calendar month the date falls in.
func (d Date) Month() Month {
	return NewMonth(d.t.Year(), d.t.Month())
}

// Year returns the calendar year.
func (m Month) Year() int { return m.index / 12 }

// Month returns the month of the year.
func (m Month) Month() time.Month { return time.Month(m.index%12 + 1) }

// Quarter returns the calendar quarter, 1 to 4.
func (m Month) Quarter() int { return m.index%12/3 + 1 }

// Add returns the month n months later; n may be negative.
func (m Month) Add(n int) Month { return Month{index: m.index + n} }

// Start returns the first day of the month.
func (m Month) Start() Date { return NewDate(m.Year(), m.Month(), 1) }

// End returns the last day of the month.
func (m Month) End() Date { return m.Add(1).Start().AddDays(-1) }

// String formats the month as "1998-01".
func (m Month) String() string { return fmt.Sprintf("%04d-%02d", m.Year(), int(m.Month())) }

// MonthsBetween returns the number of whole months from a to b, negative when
// b is earlier than a.
func MonthsBetween(a, b Month) int { return b.index - a.index }
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/domain/shared/ -v` then `go vet ./internal/domain/shared/`
Expected: all PASS, vet clean.

- [ ] **Step 5: Commit**

```bash
git add internal/domain/shared/month.go internal/domain/shared/month_test.go
git commit -m "Add a calendar month value type to shared"
```

---

### Task 2: Origin basis and monthly exposure

`OriginBasis` is the one configuration seam. Monthly exposure is the first thing that consumes it, and `EarnedPremiumByYear` is rebuilt on top of the monthly function so the two views agree by construction rather than by coincidence.

**Files:**
- Create: `internal/domain/triangle/basis.go`
- Create: `internal/domain/triangle/exposure.go`
- Create: `internal/domain/triangle/exposure_test.go`
- Modify: `internal/domain/triangle/triangle.go` - delete `EarnedPremiumByYear` (lines 100-118) and `overlapDays` (lines 120-135), and drop the now-unused `time`, `policy` and `shared` imports.

**Interfaces:**
- Consumes: `shared.Month`, `shared.NewMonth`, `shared.MonthsBetween`, `(Date) Month()` from Task 1; `policy.Policy`, `shared.DaysBetween`.
- Produces: `triangle.OriginBasis` with constants `triangle.AccidentMonth` (`"accident"`) and `triangle.UnderwritingMonth` (`"underwriting"`), and `(OriginBasis) Validate() error`; `triangle.MonthExposure{Month shared.Month, Premium float64, ExposureUnits float64, Policies int}`; `triangle.ExposureByMonth(policies []policy.Policy, startMonth shared.Month, months int, basis OriginBasis) []MonthExposure`; `triangle.EarnedPremiumByYear(policies []policy.Policy, startYear, years int) []float64` (unchanged signature and values); package-private `overlapDays(start, end, rangeStart, rangeEnd shared.Date) int`.

- [ ] **Step 1: Write the failing test**

Create `internal/domain/triangle/exposure_test.go`:

```go
package triangle_test

import (
	"testing"
	"time"

	"github.com/le-marais/claimsgen/internal/domain/policy"
	"github.com/le-marais/claimsgen/internal/domain/shared"
	"github.com/le-marais/claimsgen/internal/domain/triangle"
)

// dollarADayPolicy covers 365 days from 1 October 1998 for a premium of $365,
// so a day of cover earns exactly one dollar and every expected figure below
// is a day count.
func dollarADayPolicy() policy.Policy {
	start := shared.NewDate(1998, time.October, 1)
	return policy.Policy{
		ID:         1,
		CoverStart: start,
		CoverEnd:   start.AddDays(364),
		Premium:    shared.FromDollars(365),
	}
}

func TestExposureByMonthEarnsPremiumByDay(t *testing.T) {
	exposure := triangle.ExposureByMonth(
		[]policy.Policy{dollarADayPolicy()},
		shared.NewMonth(1998, time.January), 24, triangle.AccidentMonth)

	if len(exposure) != 24 {
		t.Fatalf("got %d months, want 24", len(exposure))
	}
	// Index 8 is September 1998, before cover starts; 9 is October (31 days).
	if !approx(exposure[8].Premium, 0) {
		t.Errorf("September 1998 premium = %v, want 0", exposure[8].Premium)
	}
	if !approx(exposure[9].Premium, 31) {
		t.Errorf("October 1998 premium = %v, want 31", exposure[9].Premium)
	}
	if !approx(exposure[10].Premium, 30) {
		t.Errorf("November 1998 premium = %v, want 30", exposure[10].Premium)
	}
	// Index 20 is September 1999, cover's last month: 30 days to the 30th.
	if !approx(exposure[20].Premium, 30) {
		t.Errorf("September 1999 premium = %v, want 30", exposure[20].Premium)
	}
	if !approx(exposure[21].Premium, 0) {
		t.Errorf("October 1999 premium = %v, want 0", exposure[21].Premium)
	}
	if got, want := exposure[9].Month, shared.NewMonth(1998, time.October); got != want {
		t.Errorf("index 9 month = %v, want %v", got, want)
	}
}

func TestExposureByMonthCountsUnitsAndPolicies(t *testing.T) {
	exposure := triangle.ExposureByMonth(
		[]policy.Policy{dollarADayPolicy()},
		shared.NewMonth(1998, time.January), 24, triangle.AccidentMonth)

	if !approx(exposure[9].ExposureUnits, 31/365.25) {
		t.Errorf("October 1998 exposure units = %v, want %v", exposure[9].ExposureUnits, 31/365.25)
	}
	if exposure[9].Policies != 1 {
		t.Errorf("October 1998 policies = %d, want 1", exposure[9].Policies)
	}
	if exposure[8].Policies != 0 {
		t.Errorf("September 1998 policies = %d, want 0", exposure[8].Policies)
	}
}

func TestExposureByMonthUnderwritingBasisLandsEverythingAtInception(t *testing.T) {
	exposure := triangle.ExposureByMonth(
		[]policy.Policy{dollarADayPolicy()},
		shared.NewMonth(1998, time.January), 24, triangle.UnderwritingMonth)

	if !approx(exposure[9].Premium, 365) {
		t.Errorf("October 1998 premium = %v, want 365 (whole premium at inception)", exposure[9].Premium)
	}
	if !approx(exposure[9].ExposureUnits, 365/365.25) {
		t.Errorf("October 1998 exposure units = %v, want %v", exposure[9].ExposureUnits, 365/365.25)
	}
	if exposure[9].Policies != 1 {
		t.Errorf("October 1998 policies = %d, want 1", exposure[9].Policies)
	}
	for _, i := range []int{10, 11, 12, 20} {
		if !approx(exposure[i].Premium, 0) {
			t.Errorf("index %d premium = %v, want 0 on the underwriting basis", i, exposure[i].Premium)
		}
		if exposure[i].Policies != 0 {
			t.Errorf("index %d policies = %d, want 0 on the underwriting basis", i, exposure[i].Policies)
		}
	}
}

func TestExposureByMonthClipsToTheWindow(t *testing.T) {
	// Cover starts before the window and ends inside it: only the overlap counts.
	p := policy.Policy{
		ID:         1,
		CoverStart: shared.NewDate(1997, time.December, 1),
		CoverEnd:   shared.NewDate(1998, time.January, 30),
		Premium:    shared.FromDollars(61), // 61 cover days, one dollar each
	}
	exposure := triangle.ExposureByMonth(
		[]policy.Policy{p}, shared.NewMonth(1998, time.January), 12, triangle.AccidentMonth)
	if !approx(exposure[0].Premium, 30) {
		t.Errorf("January 1998 premium = %v, want 30", exposure[0].Premium)
	}
	if exposure[0].Policies != 1 {
		t.Errorf("January 1998 policies = %d, want 1", exposure[0].Policies)
	}
	total := 0.0
	for _, e := range exposure {
		total += e.Premium
	}
	if !approx(total, 30) {
		t.Errorf("total premium in window = %v, want 30", total)
	}
}

func TestExposureByMonthAddsUpPoliciesInForce(t *testing.T) {
	a := dollarADayPolicy()
	b := dollarADayPolicy()
	b.ID = 2
	exposure := triangle.ExposureByMonth(
		[]policy.Policy{a, b}, shared.NewMonth(1998, time.January), 24, triangle.AccidentMonth)
	if exposure[9].Policies != 2 {
		t.Errorf("October 1998 policies = %d, want 2", exposure[9].Policies)
	}
	if !approx(exposure[9].Premium, 62) {
		t.Errorf("October 1998 premium = %v, want 62", exposure[9].Premium)
	}
}

func TestEarnedPremiumByYearIsTheMonthlySumsRolledUp(t *testing.T) {
	policies := []policy.Policy{dollarADayPolicy()}
	yearly := triangle.EarnedPremiumByYear(policies, 1998, 2)
	monthly := triangle.ExposureByMonth(policies, shared.NewMonth(1998, time.January), 24, triangle.AccidentMonth)
	for y := 0; y < 2; y++ {
		sum := 0.0
		for i := y * 12; i < (y+1)*12; i++ {
			sum += monthly[i].Premium
		}
		if !approx(yearly[y], sum) {
			t.Errorf("year %d: yearly = %v, monthly sum = %v", 1998+y, yearly[y], sum)
		}
	}
}

func TestOriginBasisValidate(t *testing.T) {
	for _, b := range []triangle.OriginBasis{triangle.AccidentMonth, triangle.UnderwritingMonth} {
		if err := b.Validate(); err != nil {
			t.Errorf("Validate(%q) = %v, want nil", b, err)
		}
	}
	if err := triangle.OriginBasis("policy").Validate(); err == nil {
		t.Error("Validate(\"policy\") = nil, want an error naming the allowed values")
	}
}
```

`approx` already exists in `internal/domain/triangle/triangle_test.go`; both files are in `package triangle_test`, so it is shared.

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/domain/triangle/ -run 'TestExposure|TestEarnedPremiumByYearIs|TestOriginBasis' -v`
Expected: compile failure - `undefined: triangle.ExposureByMonth`, `undefined: triangle.AccidentMonth`.

- [ ] **Step 3: Write the origin basis**

Create `internal/domain/triangle/basis.go`:

```go
package triangle

import "fmt"

// OriginBasis selects which period a claim's origin is keyed on, and with it
// which exposure measure pairs with that origin.
type OriginBasis string

const (
	// AccidentMonth keys a claim on the month it occurred, and exposure on
	// the exposure earned in each month.
	AccidentMonth OriginBasis = "accident"
	// UnderwritingMonth keys a claim on the inception month of its policy,
	// and exposure on the exposure written in each month, so a policy's whole
	// premium and whole term land in its inception month.
	UnderwritingMonth OriginBasis = "underwriting"
)

// Validate reports whether the basis is one of the known values.
func (b OriginBasis) Validate() error {
	switch b {
	case AccidentMonth, UnderwritingMonth:
		return nil
	default:
		return fmt.Errorf("origin basis: must be %q or %q, got %q", AccidentMonth, UnderwritingMonth, string(b))
	}
}
```

- [ ] **Step 4: Write the exposure implementation**

Create `internal/domain/triangle/exposure.go`:

```go
package triangle

import (
	"time"

	"github.com/le-marais/claimsgen/internal/domain/policy"
	"github.com/le-marais/claimsgen/internal/domain/shared"
)

// daysPerYear converts policy-days into the policy-years an exposure unit is
// measured in.
const daysPerYear = 365.25

// MonthExposure is one origin month's exposure. On the accident basis the
// figures are earned in the month, day pro-rata; on the underwriting basis
// they are written in it, so a policy's whole premium, whole term and its
// count land in its inception month.
type MonthExposure struct {
	Month         shared.Month
	Premium       float64
	ExposureUnits float64 // policy-years
	Policies      int
}

// ExposureByMonth returns the exposure of each of the origin months starting
// at startMonth. Exposure falling outside that span is not counted, so the
// last months of a run window are thin.
func ExposureByMonth(policies []policy.Policy, startMonth shared.Month, months int, basis OriginBasis) []MonthExposure {
	out := make([]MonthExposure, months)
	for i := range out {
		out[i].Month = startMonth.Add(i)
	}
	for _, p := range policies {
		termDays := shared.DaysBetween(p.CoverStart, p.CoverEnd) + 1
		if termDays <= 0 {
			continue
		}
		if basis == UnderwritingMonth {
			i := shared.MonthsBetween(startMonth, p.CoverStart.Month())
			if i < 0 || i >= months {
				continue
			}
			out[i].Premium += p.Premium.Dollars()
			out[i].ExposureUnits += float64(termDays) / daysPerYear
			out[i].Policies++
			continue
		}
		// Accident basis: spread the premium over the cover days and credit
		// each month with the days it holds. Only the months the cover can
		// touch are visited.
		perDay := p.Premium.Dollars() / float64(termDays)
		first := shared.MonthsBetween(startMonth, p.CoverStart.Month())
		if first < 0 {
			first = 0
		}
		last := shared.MonthsBetween(startMonth, p.CoverEnd.Month())
		if last > months-1 {
			last = months - 1
		}
		for i := first; i <= last; i++ {
			m := startMonth.Add(i)
			days := overlapDays(p.CoverStart, p.CoverEnd, m.Start(), m.End())
			if days <= 0 {
				continue
			}
			out[i].Premium += perDay * float64(days)
			out[i].ExposureUnits += float64(days) / daysPerYear
			out[i].Policies++
		}
	}
	return out
}

// EarnedPremiumByYear spreads each policy's premium over its cover period and
// sums the portion earned in each calendar year of the window. It rolls up
// ExposureByMonth on the accident basis, so the yearly and monthly views agree
// by construction.
func EarnedPremiumByYear(policies []policy.Policy, startYear, years int) []float64 {
	monthly := ExposureByMonth(policies, shared.NewMonth(startYear, time.January), years*12, AccidentMonth)
	earned := make([]float64, years)
	for i, m := range monthly {
		earned[i/12] += m.Premium
	}
	return earned
}

// overlapDays counts the days of [start, end] falling inside
// [rangeStart, rangeEnd], all bounds inclusive.
func overlapDays(start, end, rangeStart, rangeEnd shared.Date) int {
	if start.Before(rangeStart) {
		start = rangeStart
	}
	if end.After(rangeEnd) {
		end = rangeEnd
	}
	days := shared.DaysBetween(start, end) + 1
	if days < 0 {
		return 0
	}
	return days
}
```

- [ ] **Step 5: Remove the moved code from `triangle.go`**

Delete the `EarnedPremiumByYear` function and the old year-based `overlapDays` from `internal/domain/triangle/triangle.go`. The import block there becomes:

```go
import (
	"math"

	"github.com/le-marais/claimsgen/internal/domain/claim"
	"github.com/le-marais/claimsgen/internal/domain/transaction"
)
```

- [ ] **Step 6: Run the tests to verify they pass**

Run: `go test ./internal/domain/triangle/ -v` then `go vet ./...`
Expected: all PASS, including the pre-existing `TestEarnedPremiumSplitsAcrossCalendarYears` (92 and 273) which now runs through the monthly path unchanged. vet clean.

- [ ] **Step 7: Commit**

```bash
git add internal/domain/triangle/basis.go internal/domain/triangle/exposure.go \
        internal/domain/triangle/exposure_test.go internal/domain/triangle/triangle.go
git commit -m "Add an origin basis and monthly exposure to the triangle package"
```

---

### Task 3: The monthly grid

The canonical incremental store. Two passes over the input: one to size the rectangle, one to fill it.

**Files:**
- Create: `internal/domain/triangle/monthly.go`
- Create: `internal/domain/triangle/monthly_test.go`

**Interfaces:**
- Consumes: `shared.Month`, `shared.NewMonth`, `shared.MonthsBetween`, `(Date) Month()`; `triangle.OriginBasis`, `AccidentMonth`, `UnderwritingMonth` from Task 2; `claim.Claim`, `policy.Policy`, `transaction.Transaction`, `transaction.Payment`, `(Type) IsRecovery()`, `shared.Money.Dollars()`.
- Produces: `triangle.Measure` with constants `MeasurePaid`, `MeasurePaidNet`, `MeasureIncurred`, `MeasureReported`; `triangle.MonthlyGrid{Basis OriginBasis, StartMonth shared.Month, DevPeriods int, Paid, PaidNet, Incurred [][]float64, Reported [][]int}`; `(MonthlyGrid) Origins() int`; `(MonthlyGrid) Cell(m Measure, origin, dev int) float64` (1-based `dev`); `triangle.BuildMonthlyGrid(policies []policy.Policy, claims []claim.Claim, txs []transaction.Transaction, startMonth shared.Month, originMonths int, basis OriginBasis) (MonthlyGrid, error)`.

- [ ] **Step 1: Write the failing test**

Create `internal/domain/triangle/monthly_test.go`:

```go
package triangle_test

import (
	"strings"
	"testing"
	"time"

	"github.com/le-marais/claimsgen/internal/domain/claim"
	"github.com/le-marais/claimsgen/internal/domain/policy"
	"github.com/le-marais/claimsgen/internal/domain/shared"
	"github.com/le-marais/claimsgen/internal/domain/transaction"
	"github.com/le-marais/claimsgen/internal/domain/triangle"
)

// jan1998 is the start month every grid test keys from.
var jan1998 = shared.NewMonth(1998, time.January)

// gridFixture is one claim occurring in March 1998 under a policy incepting
// in January 1998: reported the same month, $600 paid in June 1998 and $500
// paid at close in February 1999.
func gridFixture() ([]policy.Policy, []claim.Claim, []transaction.Transaction) {
	policies := []policy.Policy{{
		ID:         1,
		CoverStart: shared.NewDate(1998, time.January, 15),
		CoverEnd:   shared.NewDate(1998, time.January, 15).AddDays(364),
		Premium:    shared.FromDollars(365),
	}}
	claims := []claim.Claim{{
		ID: 1, PolicyID: 1,
		OccurrenceDate:  shared.NewDate(1998, time.March, 1),
		ReportDate:      shared.NewDate(1998, time.March, 3),
		CloseDate:       shared.NewDate(1999, time.February, 1),
		InitialEstimate: shared.FromDollars(1000),
	}}
	txs := []transaction.Transaction{
		{ID: 1, ClaimID: 1, Date: shared.NewDate(1998, time.March, 3), Type: transaction.Estimate, Amount: shared.FromDollars(1000)},
		{ID: 2, ClaimID: 1, Date: shared.NewDate(1998, time.June, 1), Type: transaction.Payment, Amount: shared.FromDollars(600)},
		{ID: 3, ClaimID: 1, Date: shared.NewDate(1998, time.June, 1), Type: transaction.Estimate, Amount: shared.FromDollars(-600)},
		{ID: 4, ClaimID: 1, Date: shared.NewDate(1999, time.February, 1), Type: transaction.Payment, Amount: shared.FromDollars(500)},
		{ID: 5, ClaimID: 1, Date: shared.NewDate(1999, time.February, 1), Type: transaction.Estimate, Amount: shared.FromDollars(-400)},
	}
	return policies, claims, txs
}

func buildGrid(t *testing.T, basis triangle.OriginBasis) triangle.MonthlyGrid {
	t.Helper()
	policies, claims, txs := gridFixture()
	g, err := triangle.BuildMonthlyGrid(policies, claims, txs, jan1998, 24, basis)
	if err != nil {
		t.Fatal(err)
	}
	return g
}

func TestMonthlyGridCellsAreIncrementalAndOneBased(t *testing.T) {
	g := buildGrid(t, triangle.AccidentMonth)
	// The claim occurs in March 1998: origin row 2. Development period 1 is
	// March itself, so June 1998 is period 4 and February 1999 is period 12.
	if got := g.Cell(triangle.MeasurePaid, 2, 4); !approx(got, 600) {
		t.Errorf("paid at origin 2 dev 4 = %v, want 600", got)
	}
	if got := g.Cell(triangle.MeasurePaid, 2, 12); !approx(got, 500) {
		t.Errorf("paid at origin 2 dev 12 = %v, want 500 (incremental, not 1100)", got)
	}
	if got := g.Cell(triangle.MeasurePaid, 2, 1); !approx(got, 0) {
		t.Errorf("paid at origin 2 dev 1 = %v, want 0", got)
	}
	// Nothing lands in any other origin row.
	for o := 0; o < 24; o++ {
		if o == 2 {
			continue
		}
		for d := 1; d <= g.DevPeriods; d++ {
			if got := g.Cell(triangle.MeasurePaid, o, d); got != 0 {
				t.Fatalf("paid at origin %d dev %d = %v, want 0", o, d, got)
			}
		}
	}
}

func TestMonthlyGridIncurredIsCaseMovementsPlusPayments(t *testing.T) {
	g := buildGrid(t, triangle.AccidentMonth)
	// Dev 1: the initial estimate of 1000 is raised in March.
	if got := g.Cell(triangle.MeasureIncurred, 2, 1); !approx(got, 1000) {
		t.Errorf("incurred at dev 1 = %v, want 1000", got)
	}
	// Dev 4: pay 600 and release 600 of case, so incurred does not move.
	if got := g.Cell(triangle.MeasureIncurred, 2, 4); !approx(got, 0) {
		t.Errorf("incurred at dev 4 = %v, want 0", got)
	}
	// Dev 12: pay 500 and release 400, so incurred strengthens by 100.
	if got := g.Cell(triangle.MeasureIncurred, 2, 12); !approx(got, 100) {
		t.Errorf("incurred at dev 12 = %v, want 100", got)
	}
}

func TestMonthlyGridCountsClaimsByReportMonth(t *testing.T) {
	// A claim occurring in December 1998 but reported in January 1999 counts
	// in origin December at development period 2.
	claims := []claim.Claim{{
		ID: 1, PolicyID: 1,
		OccurrenceDate: shared.NewDate(1998, time.December, 20),
		ReportDate:     shared.NewDate(1999, time.January, 15),
	}}
	g, err := triangle.BuildMonthlyGrid(nil, claims, nil, jan1998, 24, triangle.AccidentMonth)
	if err != nil {
		t.Fatal(err)
	}
	if got := g.Cell(triangle.MeasureReported, 11, 1); got != 0 {
		t.Errorf("reported at origin 11 dev 1 = %v, want 0", got)
	}
	if got := g.Cell(triangle.MeasureReported, 11, 2); !approx(got, 1) {
		t.Errorf("reported at origin 11 dev 2 = %v, want 1", got)
	}
}

func TestMonthlyGridNetsRecoveriesOffPaidAndIncurred(t *testing.T) {
	claims := []claim.Claim{{
		ID: 1, PolicyID: 1,
		OccurrenceDate: shared.NewDate(1998, time.March, 1),
		ReportDate:     shared.NewDate(1998, time.March, 1),
	}}
	txs := []transaction.Transaction{
		{ID: 1, ClaimID: 1, Date: shared.NewDate(1998, time.April, 1), Type: transaction.Payment, Amount: shared.FromDollars(1000)},
		{ID: 2, ClaimID: 1, Date: shared.NewDate(1999, time.June, 1), Type: transaction.Salvage, Amount: shared.FromDollars(150)},
	}
	g, err := triangle.BuildMonthlyGrid(nil, claims, txs, jan1998, 24, triangle.AccidentMonth)
	if err != nil {
		t.Fatal(err)
	}
	// April 1998 is development period 2 for a March origin; June 1999 is 16.
	if got := g.Cell(triangle.MeasurePaid, 2, 2); !approx(got, 1000) {
		t.Errorf("gross paid at dev 2 = %v, want 1000", got)
	}
	if got := g.Cell(triangle.MeasurePaid, 2, 16); !approx(got, 0) {
		t.Errorf("gross paid at dev 16 = %v, want 0 (recoveries are not payments)", got)
	}
	if got := g.Cell(triangle.MeasurePaidNet, 2, 16); !approx(got, -150) {
		t.Errorf("net paid at dev 16 = %v, want -150", got)
	}
	if got := g.Cell(triangle.MeasureIncurred, 2, 16); !approx(got, -150) {
		t.Errorf("incurred at dev 16 = %v, want -150", got)
	}
}

func TestMonthlyGridIsRectangularAndSizedToTheLastDevelopment(t *testing.T) {
	g := buildGrid(t, triangle.AccidentMonth)
	// The last movement is February 1999, development period 12 of a March
	// 1998 origin, so the grid is 12 wide.
	if g.DevPeriods != 12 {
		t.Errorf("DevPeriods = %d, want 12", g.DevPeriods)
	}
	if g.Origins() != 24 {
		t.Errorf("Origins() = %d, want 24", g.Origins())
	}
	for o, row := range g.Paid {
		if len(row) != g.DevPeriods {
			t.Fatalf("paid row %d is %d wide, want %d", o, len(row), g.DevPeriods)
		}
	}
	for o, row := range g.Reported {
		if len(row) != g.DevPeriods {
			t.Fatalf("reported row %d is %d wide, want %d", o, len(row), g.DevPeriods)
		}
	}
}

func TestMonthlyGridUnderwritingBasisKeysOnInception(t *testing.T) {
	g := buildGrid(t, triangle.UnderwritingMonth)
	// The policy incepts in January 1998, so the origin row is 0 and the
	// development periods count from January: June 1998 is period 6.
	if got := g.Cell(triangle.MeasurePaid, 0, 6); !approx(got, 600) {
		t.Errorf("paid at origin 0 dev 6 = %v, want 600", got)
	}
	if got := g.Cell(triangle.MeasurePaid, 2, 4); !approx(got, 0) {
		t.Errorf("paid at accident origin 2 dev 4 = %v, want 0 on the underwriting basis", got)
	}
	if got := g.Cell(triangle.MeasureReported, 0, 3); !approx(got, 1) {
		t.Errorf("reported at origin 0 dev 3 = %v, want 1 (reported in March)", got)
	}
}

func TestMonthlyGridUnderwritingBasisRejectsAMissingPolicy(t *testing.T) {
	_, claims, txs := gridFixture()
	_, err := triangle.BuildMonthlyGrid(nil, claims, txs, jan1998, 24, triangle.UnderwritingMonth)
	if err == nil {
		t.Fatal("want an error when a claim's policy is not in the book")
	}
	if !strings.Contains(err.Error(), "policy 1") {
		t.Errorf("error = %v, want it to name the missing policy", err)
	}
}

func TestMonthlyGridSkipsOriginsOutsideTheSpan(t *testing.T) {
	claims := []claim.Claim{
		{ID: 1, PolicyID: 1, OccurrenceDate: shared.NewDate(1997, time.June, 1), ReportDate: shared.NewDate(1997, time.June, 1)},
		{ID: 2, PolicyID: 1, OccurrenceDate: shared.NewDate(2001, time.June, 1), ReportDate: shared.NewDate(2001, time.June, 1)},
	}
	txs := []transaction.Transaction{
		{ID: 1, ClaimID: 1, Date: shared.NewDate(1998, time.June, 1), Type: transaction.Payment, Amount: shared.FromDollars(900)},
		{ID: 2, ClaimID: 2, Date: shared.NewDate(2001, time.July, 1), Type: transaction.Payment, Amount: shared.FromDollars(900)},
	}
	// A 24-month span from January 1998 excludes both claims, so nothing is
	// placed and neither claim stretches the grid.
	g, err := triangle.BuildMonthlyGrid(nil, claims, txs, jan1998, 24, triangle.AccidentMonth)
	if err != nil {
		t.Fatal(err)
	}
	if g.DevPeriods != 1 {
		t.Errorf("DevPeriods = %d, want 1 when no claim is in the span", g.DevPeriods)
	}
	total := 0.0
	for _, row := range g.Paid {
		for _, v := range row {
			total += v
		}
	}
	if !approx(total, 0) {
		t.Errorf("total paid = %v, want 0", total)
	}
}

func TestMonthlyGridClampsDevelopmentBelowOne(t *testing.T) {
	// Hand-built input only: a payment before the claim's origin month cannot
	// arise in generated data, but must not index out of range.
	claims := []claim.Claim{{
		ID: 1, PolicyID: 1,
		OccurrenceDate: shared.NewDate(1998, time.June, 1),
		ReportDate:     shared.NewDate(1998, time.June, 1),
	}}
	txs := []transaction.Transaction{
		{ID: 1, ClaimID: 1, Date: shared.NewDate(1998, time.February, 1), Type: transaction.Payment, Amount: shared.FromDollars(100)},
	}
	g, err := triangle.BuildMonthlyGrid(nil, claims, txs, jan1998, 24, triangle.AccidentMonth)
	if err != nil {
		t.Fatal(err)
	}
	if got := g.Cell(triangle.MeasurePaid, 5, 1); !approx(got, 100) {
		t.Errorf("paid at origin 5 dev 1 = %v, want 100 (clamped)", got)
	}
}

func TestMonthlyGridRejectsBadArguments(t *testing.T) {
	if _, err := triangle.BuildMonthlyGrid(nil, nil, nil, jan1998, 0, triangle.AccidentMonth); err == nil {
		t.Error("want an error for zero origin months")
	}
	if _, err := triangle.BuildMonthlyGrid(nil, nil, nil, jan1998, 12, triangle.OriginBasis("policy")); err == nil {
		t.Error("want an error for an unknown origin basis")
	}
}

func TestMonthlyGridEmptyDatasetIsOneColumnWide(t *testing.T) {
	g, err := triangle.BuildMonthlyGrid(nil, nil, nil, jan1998, 12, triangle.AccidentMonth)
	if err != nil {
		t.Fatal(err)
	}
	if g.DevPeriods != 1 || g.Origins() != 12 {
		t.Fatalf("grid is %dx%d, want 12x1", g.Origins(), g.DevPeriods)
	}
	if got := g.Cell(triangle.MeasurePaid, 0, 1); got != 0 {
		t.Errorf("cell = %v, want 0", got)
	}
}

func TestMonthlyGridCellOutOfRangeIsZero(t *testing.T) {
	g := buildGrid(t, triangle.AccidentMonth)
	for _, c := range []struct{ origin, dev int }{{-1, 1}, {99, 1}, {2, 0}, {2, 999}} {
		if got := g.Cell(triangle.MeasurePaid, c.origin, c.dev); got != 0 {
			t.Errorf("Cell(origin=%d, dev=%d) = %v, want 0", c.origin, c.dev, got)
		}
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/domain/triangle/ -run TestMonthlyGrid -v`
Expected: compile failure - `undefined: triangle.BuildMonthlyGrid`, `undefined: triangle.MeasurePaid`.

- [ ] **Step 3: Write the implementation**

Create `internal/domain/triangle/monthly.go`:

```go
package triangle

import (
	"fmt"

	"github.com/le-marais/claimsgen/internal/domain/claim"
	"github.com/le-marais/claimsgen/internal/domain/policy"
	"github.com/le-marais/claimsgen/internal/domain/shared"
	"github.com/le-marais/claimsgen/internal/domain/transaction"
)

// Measure names one of the quantities a triangle carries.
type Measure int

const (
	MeasurePaid     Measure = iota // payments, gross of recoveries
	MeasurePaidNet                 // payments net of salvage and subrogation
	MeasureIncurred                // gross case movements plus net paid
	MeasureReported                // claim counts, by report month
)

// MonthlyGrid holds incremental monthly triangles: row o is origin month
// StartMonth.Add(o), and slice index d holds development period d+1, so index
// 0 is development period 1 - the origin month itself. Every row is DevPeriods
// wide.
//
// Cells are incremental, not cumulative: a cell is the movement in that
// development month. Increments sum, so any coarser origin or development
// grain is a plain sum over cells (see Coarsen) and a cumulative view is a
// running sum along a row.
type MonthlyGrid struct {
	Basis      OriginBasis
	StartMonth shared.Month
	DevPeriods int
	Paid       [][]float64
	PaidNet    [][]float64
	Incurred   [][]float64
	Reported   [][]int
}

// Origins returns the number of origin months in the grid.
func (g MonthlyGrid) Origins() int { return len(g.Paid) }

// Cell reads one cell with a 1-based development period. Reported counts
// convert to float64. Indices outside the grid read as zero.
func (g MonthlyGrid) Cell(m Measure, origin, dev int) float64 {
	if m == MeasureReported {
		if origin < 0 || origin >= len(g.Reported) || dev < 1 || dev > len(g.Reported[origin]) {
			return 0
		}
		return float64(g.Reported[origin][dev-1])
	}
	var cells [][]float64
	switch m {
	case MeasurePaidNet:
		cells = g.PaidNet
	case MeasureIncurred:
		cells = g.Incurred
	default:
		cells = g.Paid
	}
	if origin < 0 || origin >= len(cells) || dev < 1 || dev > len(cells[origin]) {
		return 0
	}
	return cells[origin][dev-1]
}

// BuildMonthlyGrid aggregates a dataset into incremental monthly triangles.
// originMonths origin months are keyed from startMonth; a claim whose origin
// falls outside that span is skipped, along with all of its movements.
//
// Development runs to full runoff: DevPeriods is the widest development period
// any in-span claim reaches, so no movement is dropped, including development
// after the run window ends. A caller wanting a valuation-date view filters
// cells where origin + dev - 1 exceeds the valuation month.
func BuildMonthlyGrid(
	policies []policy.Policy,
	claims []claim.Claim,
	txs []transaction.Transaction,
	startMonth shared.Month,
	originMonths int,
	basis OriginBasis,
) (MonthlyGrid, error) {
	if err := basis.Validate(); err != nil {
		return MonthlyGrid{}, err
	}
	if originMonths < 1 {
		return MonthlyGrid{}, fmt.Errorf("origin months: must be at least 1, got %d", originMonths)
	}
	rows, err := originRows(policies, claims, basis, startMonth, originMonths)
	if err != nil {
		return MonthlyGrid{}, err
	}

	// Pass one sizes the rectangle: the widest development period reached by
	// any movement of any in-span claim, floored at one column.
	devPeriods := 1
	widen := func(claimID int, event shared.Month) {
		row, ok := rows[claimID]
		if !ok {
			return
		}
		if d := devPeriod(startMonth, row, event); d > devPeriods {
			devPeriods = d
		}
	}
	for _, c := range claims {
		widen(c.ID, c.ReportDate.Month())
	}
	for _, tx := range txs {
		widen(tx.ClaimID, tx.Date.Month())
	}

	g := MonthlyGrid{
		Basis:      basis,
		StartMonth: startMonth,
		DevPeriods: devPeriods,
		Paid:       newFloatCells(originMonths, devPeriods),
		PaidNet:    newFloatCells(originMonths, devPeriods),
		Incurred:   newFloatCells(originMonths, devPeriods),
		Reported:   newIntCells(originMonths, devPeriods),
	}

	// Pass two places every movement. The weights match the annual triangles
	// this grid replaces: paid counts payments only; net paid subtracts
	// recoveries; incurred adds every case movement and payment and subtracts
	// recoveries, so it is gross case plus net paid.
	for _, c := range claims {
		row, ok := rows[c.ID]
		if !ok {
			continue
		}
		g.Reported[row][devPeriod(startMonth, row, c.ReportDate.Month())-1]++
	}
	for _, tx := range txs {
		row, ok := rows[tx.ClaimID]
		if !ok {
			continue
		}
		d := devPeriod(startMonth, row, tx.Date.Month()) - 1
		amount := tx.Amount.Dollars()
		switch {
		case tx.Type == transaction.Payment:
			g.Paid[row][d] += amount
			g.PaidNet[row][d] += amount
			g.Incurred[row][d] += amount
		case tx.Type.IsRecovery():
			g.PaidNet[row][d] -= amount
			g.Incurred[row][d] -= amount
		default:
			g.Incurred[row][d] += amount
		}
	}
	return g, nil
}

// originRows maps each claim to its grid row, dropping claims whose origin
// falls outside the grid. On the underwriting basis a claim whose policy is
// absent from the book is an error rather than a silent drop.
func originRows(policies []policy.Policy, claims []claim.Claim, basis OriginBasis, startMonth shared.Month, originMonths int) (map[int]int, error) {
	var coverStart map[int]shared.Month
	if basis == UnderwritingMonth {
		coverStart = make(map[int]shared.Month, len(policies))
		for _, p := range policies {
			coverStart[p.ID] = p.CoverStart.Month()
		}
	}
	rows := make(map[int]int, len(claims))
	for _, c := range claims {
		origin := c.OccurrenceDate.Month()
		if basis == UnderwritingMonth {
			m, ok := coverStart[c.PolicyID]
			if !ok {
				return nil, fmt.Errorf("claim %d: policy %d is not in the book", c.ID, c.PolicyID)
			}
			origin = m
		}
		row := shared.MonthsBetween(startMonth, origin)
		if row < 0 || row >= originMonths {
			continue
		}
		rows[c.ID] = row
	}
	return rows, nil
}

// devPeriod returns the 1-based development month of an event for the origin
// row, clamped to at least 1. The clamp cannot fire on generated data - a
// policy's cover start precedes its claims' occurrences, which precede their
// report dates, which precede their transactions - but this is a public domain
// function and must not index out of range on hand-built input.
func devPeriod(startMonth shared.Month, row int, event shared.Month) int {
	d := shared.MonthsBetween(startMonth.Add(row), event) + 1
	if d < 1 {
		return 1
	}
	return d
}

func newFloatCells(rows, cols int) [][]float64 {
	cells := make([][]float64, rows)
	for i := range cells {
		cells[i] = make([]float64, cols)
	}
	return cells
}

func newIntCells(rows, cols int) [][]int {
	cells := make([][]int, rows)
	for i := range cells {
		cells[i] = make([]int, cols)
	}
	return cells
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/domain/triangle/ -v` then `go vet ./internal/domain/triangle/`
Expected: all PASS, vet clean.

- [ ] **Step 5: Commit**

```bash
git add internal/domain/triangle/monthly.go internal/domain/triangle/monthly_test.go
git commit -m "Add the incremental monthly triangle grid"
```

---

### Task 4: Coarsening, and the annual triangles derived from the grid

The proof of this task is that **every existing test passes unchanged**. The three exported annual constructors keep their signatures and behaviour but are re-implemented over the grid, so the existing `triangle_test.go`, `realism_test.go` and `TestDefaultPresetIsRealistic` become the behaviour-preservation evidence.

**Files:**
- Create: `internal/domain/triangle/coarsen.go`
- Create: `internal/domain/triangle/coarsen_test.go`
- Modify: `internal/domain/triangle/triangle.go` - `PaidTriangle`, `NetPaidTriangle` and `IncurredTriangle` become grid-backed shims; the `aggregate` function is deleted.

**Interfaces:**
- Consumes: `MonthlyGrid`, `Measure` constants, `devPeriod` from Task 3; `shared.Month`, `shared.NewMonth`, `shared.MonthsBetween`; the existing `Triangle{StartYear int, Cells [][]float64}`.
- Produces: `triangle.PeriodKind` with constants `Monthly`, `Quarterly`, `Annual`; `triangle.IncrementalSet{Basis OriginBasis, Kind PeriodKind, StartMonth shared.Month, Paid, PaidNet, Incurred [][]float64, Reported [][]int}`; `(MonthlyGrid) Coarsen(kind PeriodKind, devPeriods int, foldTail bool) IncrementalSet`; `(IncrementalSet) Cumulative(m Measure) Triangle`; `triangle.AnnualSet{Paid, NetPaid, Incurred Triangle}`; `(MonthlyGrid) AnnualTriangles(devYears int) AnnualSet`.

- [ ] **Step 1: Write the failing test**

Create `internal/domain/triangle/coarsen_test.go`:

```go
package triangle_test

import (
	"testing"
	"time"

	"github.com/le-marais/claimsgen/internal/domain/claim"
	"github.com/le-marais/claimsgen/internal/domain/shared"
	"github.com/le-marais/claimsgen/internal/domain/transaction"
	"github.com/le-marais/claimsgen/internal/domain/triangle"
)

// marchToJanuary is the case that separates calendar-period development from
// dividing monthly development by twelve: an accident in March 1998 paid in
// January 1999 is development year 2, because the payment falls in the next
// calendar year - ten months of development, but two calendar years.
func marchToJanuary(t *testing.T) triangle.MonthlyGrid {
	t.Helper()
	claims := []claim.Claim{{
		ID: 1, PolicyID: 1,
		OccurrenceDate: shared.NewDate(1998, time.March, 1),
		ReportDate:     shared.NewDate(1998, time.March, 1),
	}}
	txs := []transaction.Transaction{
		{ID: 1, ClaimID: 1, Date: shared.NewDate(1998, time.April, 1), Type: transaction.Payment, Amount: shared.FromDollars(100)},
		{ID: 2, ClaimID: 1, Date: shared.NewDate(1999, time.January, 15), Type: transaction.Payment, Amount: shared.FromDollars(500)},
	}
	g, err := triangle.BuildMonthlyGrid(nil, claims, txs, jan1998, 24, triangle.AccidentMonth)
	if err != nil {
		t.Fatal(err)
	}
	return g
}

func TestCoarsenAnnualKeysOnTheCalendarYearOfTheEvent(t *testing.T) {
	set := marchToJanuary(t).Coarsen(triangle.Annual, 3, true)
	if len(set.Paid) != 2 {
		t.Fatalf("got %d origin years, want 2", len(set.Paid))
	}
	// Origin year 1998: 100 in development year 1, 500 in development year 2.
	// Dividing ten months of development by twelve would wrongly put the 500
	// in development year 1.
	if !approx(set.Paid[0][0], 100) {
		t.Errorf("1998 dev 1 = %v, want 100", set.Paid[0][0])
	}
	if !approx(set.Paid[0][1], 500) {
		t.Errorf("1998 dev 2 = %v, want 500", set.Paid[0][1])
	}
	if !approx(set.Paid[0][2], 0) {
		t.Errorf("1998 dev 3 = %v, want 0", set.Paid[0][2])
	}
}

func TestCoarsenPadsEveryRowToDevPeriods(t *testing.T) {
	set := marchToJanuary(t).Coarsen(triangle.Annual, 5, true)
	for i, row := range set.Paid {
		if len(row) != 5 {
			t.Errorf("row %d is %d wide, want 5", i, len(row))
		}
	}
	for i, row := range set.Reported {
		if len(row) != 5 {
			t.Errorf("reported row %d is %d wide, want 5", i, len(row))
		}
	}
}

func TestCoarsenFoldsOrDropsTheTail(t *testing.T) {
	folded := marchToJanuary(t).Coarsen(triangle.Annual, 1, true)
	if !approx(folded.Paid[0][0], 600) {
		t.Errorf("folded 1998 dev 1 = %v, want 600", folded.Paid[0][0])
	}
	dropped := marchToJanuary(t).Coarsen(triangle.Annual, 1, false)
	if !approx(dropped.Paid[0][0], 100) {
		t.Errorf("truncated 1998 dev 1 = %v, want 100", dropped.Paid[0][0])
	}
}

func TestCoarsenQuarterlyUsesCalendarQuarters(t *testing.T) {
	// March 1998 is Q1 1998; January 1999 is Q1 1999, four quarters later, so
	// development quarter 5. April 1998 is Q2, development quarter 2.
	set := marchToJanuary(t).Coarsen(triangle.Quarterly, 0, false)
	if !approx(set.Paid[0][1], 100) {
		t.Errorf("Q1 1998 dev 2 = %v, want 100", set.Paid[0][1])
	}
	if !approx(set.Paid[0][4], 500) {
		t.Errorf("Q1 1998 dev 5 = %v, want 500", set.Paid[0][4])
	}
	if len(set.Paid) != 8 {
		t.Errorf("got %d origin quarters, want 8 for a 24-month grid", len(set.Paid))
	}
}

func TestCoarsenMonthlyIsTheGridItself(t *testing.T) {
	g := marchToJanuary(t)
	set := g.Coarsen(triangle.Monthly, 0, false)
	if len(set.Paid) != g.Origins() {
		t.Fatalf("got %d origins, want %d", len(set.Paid), g.Origins())
	}
	for o := range set.Paid {
		for d := range set.Paid[o] {
			if !approx(set.Paid[o][d], g.Paid[o][d]) {
				t.Fatalf("cell (%d, %d) = %v, want %v", o, d, set.Paid[o][d], g.Paid[o][d])
			}
		}
	}
}

func TestCumulativeRunsTheRunningSum(t *testing.T) {
	tri := marchToJanuary(t).Coarsen(triangle.Annual, 3, true).Cumulative(triangle.MeasurePaid)
	want := []float64{100, 600, 600}
	for d, w := range want {
		if !approx(tri.Cells[0][d], w) {
			t.Errorf("cumulative dev %d = %v, want %v", d+1, tri.Cells[0][d], w)
		}
	}
	if tri.StartYear != 1998 {
		t.Errorf("StartYear = %d, want 1998", tri.StartYear)
	}
}

func TestCumulativeReportedCounts(t *testing.T) {
	tri := marchToJanuary(t).Coarsen(triangle.Annual, 3, true).Cumulative(triangle.MeasureReported)
	want := []float64{1, 1, 1}
	for d, w := range want {
		if !approx(tri.Cells[0][d], w) {
			t.Errorf("cumulative reported dev %d = %v, want %v", d+1, tri.Cells[0][d], w)
		}
	}
}

func TestAnnualTrianglesMatchTheExportedConstructors(t *testing.T) {
	claims, txs := fixtures()
	g, err := triangle.BuildMonthlyGrid(nil, claims, txs, jan1998, 24, triangle.AccidentMonth)
	if err != nil {
		t.Fatal(err)
	}
	set := g.AnnualTriangles(3)
	cases := []struct {
		name string
		got  triangle.Triangle
		want triangle.Triangle
	}{
		{"paid", set.Paid, triangle.PaidTriangle(claims, txs, 1998, 2, 3)},
		{"net paid", set.NetPaid, triangle.NetPaidTriangle(claims, txs, 1998, 2, 3)},
		{"incurred", set.Incurred, triangle.IncurredTriangle(claims, txs, 1998, 2, 3)},
	}
	for _, c := range cases {
		if len(c.got.Cells) != len(c.want.Cells) {
			t.Fatalf("%s: got %d origins, want %d", c.name, len(c.got.Cells), len(c.want.Cells))
		}
		for o := range c.want.Cells {
			for d := range c.want.Cells[o] {
				if !approx(c.got.Cells[o][d], c.want.Cells[o][d]) {
					t.Errorf("%s cell (%d, %d) = %v, want %v", c.name, o, d, c.got.Cells[o][d], c.want.Cells[o][d])
				}
			}
		}
	}
}

func TestCoarsenEmptyGrid(t *testing.T) {
	g, err := triangle.BuildMonthlyGrid(nil, nil, nil, jan1998, 12, triangle.AccidentMonth)
	if err != nil {
		t.Fatal(err)
	}
	set := g.Coarsen(triangle.Annual, 10, true)
	if len(set.Paid) != 1 {
		t.Fatalf("got %d origin years, want 1", len(set.Paid))
	}
	if len(set.Paid[0]) != 10 {
		t.Errorf("row width = %d, want 10", len(set.Paid[0]))
	}
}
```

Note: `fixtures()` is the existing helper in `triangle_test.go`; it covers a two-year window, so a 24-month grid matches `origins = 2`.

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/domain/triangle/ -run 'TestCoarsen|TestCumulative|TestAnnualTriangles' -v`
Expected: compile failure - `undefined: triangle.Annual`, `undefined: Coarsen`.

- [ ] **Step 3: Write the coarsening implementation**

Create `internal/domain/triangle/coarsen.go`:

```go
package triangle

import (
	"time"

	"github.com/le-marais/claimsgen/internal/domain/shared"
)

// PeriodKind is the grain of a triangle's origin and development axes.
type PeriodKind int

const (
	Monthly PeriodKind = iota
	Quarterly
	Annual
)

// epoch is an arbitrary fixed month, so a monthly period number is a stable
// integer whose differences are month counts.
var epoch = shared.NewMonth(0, time.January)

// index returns the absolute period number the month falls in, so that the
// difference of two indices is a whole number of periods.
func (k PeriodKind) index(m shared.Month) int {
	switch k {
	case Quarterly:
		return m.Year()*4 + m.Quarter() - 1
	case Annual:
		return m.Year()
	default:
		return shared.MonthsBetween(epoch, m)
	}
}

// IncrementalSet is a set of incremental triangles sharing one origin and
// development grain. Row p is origin period p counted from StartMonth's
// period, and slice index d holds development period d+1.
type IncrementalSet struct {
	Basis      OriginBasis
	Kind       PeriodKind
	StartMonth shared.Month
	Paid       [][]float64
	PaidNet    [][]float64
	Incurred   [][]float64
	Reported   [][]int
}

// Coarsen aggregates the incremental monthly grid onto a coarser grain. When
// devPeriods is positive every origin row is exactly devPeriods wide, and
// development beyond it is folded into the last period when foldTail is set
// and dropped otherwise. When devPeriods is zero rows take the grain's
// natural extent.
//
// Both axes are keyed on the calendar period the month falls in, not on whole
// multiples of the monthly development period. An accident in March 1998 paid
// in January 1999 is development year 2, because the payment falls in the next
// calendar year - which is what the annual triangles have always measured.
//
// Rows are zero-padded rather than left ragged when devPeriods is given. The
// annual triangles allocate a full rectangle and ATAFactors counts an origin
// at an age only when its row reaches that far, so a ragged row would drop
// short-tail origins out of the late-age factors and move them.
func (g MonthlyGrid) Coarsen(kind PeriodKind, devPeriods int, foldTail bool) IncrementalSet {
	set := IncrementalSet{Basis: g.Basis, Kind: kind, StartMonth: g.StartMonth}
	origins := g.Origins()
	if origins == 0 {
		return set
	}
	startIndex := kind.index(g.StartMonth)
	rows := kind.index(g.StartMonth.Add(origins-1)) - startIndex + 1

	// visit walks every monthly cell once, handing the coarse row and 1-based
	// coarse development period along with the monthly indices. Sizing and
	// filling share the walk so they cannot disagree.
	visit := func(f func(row, dev, origin, d int)) {
		for o := 0; o < origins; o++ {
			originMonth := g.StartMonth.Add(o)
			row := kind.index(originMonth) - startIndex
			originIndex := kind.index(originMonth)
			for d := 0; d < g.DevPeriods; d++ {
				dev := kind.index(originMonth.Add(d)) - originIndex + 1
				if devPeriods > 0 && dev > devPeriods {
					if !foldTail {
						continue
					}
					dev = devPeriods
				}
				f(row, dev, o, d)
			}
		}
	}

	widths := make([]int, rows)
	if devPeriods > 0 {
		for i := range widths {
			widths[i] = devPeriods
		}
	} else {
		visit(func(row, dev, _, _ int) {
			if dev > widths[row] {
				widths[row] = dev
			}
		})
	}
	set.Paid = raggedFloat(widths)
	set.PaidNet = raggedFloat(widths)
	set.Incurred = raggedFloat(widths)
	set.Reported = raggedInt(widths)
	visit(func(row, dev, o, d int) {
		set.Paid[row][dev-1] += g.Paid[o][d]
		set.PaidNet[row][dev-1] += g.PaidNet[o][d]
		set.Incurred[row][dev-1] += g.Incurred[o][d]
		set.Reported[row][dev-1] += g.Reported[o][d]
	})
	return set
}

// Cumulative returns the running-sum view of one measure as a Triangle. Its
// StartYear is the calendar year of StartMonth, which is what the annual
// grain the realism gate and the UI read needs.
func (s IncrementalSet) Cumulative(m Measure) Triangle {
	src := s.measure(m)
	cells := make([][]float64, len(src))
	for i, row := range src {
		out := make([]float64, len(row))
		running := 0.0
		for d, v := range row {
			running += v
			out[d] = running
		}
		cells[i] = out
	}
	return Triangle{StartYear: s.StartMonth.Year(), Cells: cells}
}

// measure returns one measure's incremental cells as float64, converting the
// reported counts.
func (s IncrementalSet) measure(m Measure) [][]float64 {
	switch m {
	case MeasurePaidNet:
		return s.PaidNet
	case MeasureIncurred:
		return s.Incurred
	case MeasureReported:
		out := make([][]float64, len(s.Reported))
		for i, row := range s.Reported {
			out[i] = make([]float64, len(row))
			for d, v := range row {
				out[i][d] = float64(v)
			}
		}
		return out
	default:
		return s.Paid
	}
}

// AnnualSet holds the cumulative annual triangles the realism gate and the UI
// read.
type AnnualSet struct {
	Paid     Triangle // gross of recoveries
	NetPaid  Triangle // net of salvage and subrogation
	Incurred Triangle // gross case plus net paid
}

// AnnualTriangles coarsens the grid to a yearly origin with devYears
// development years, folding later development into the last column, and
// returns the cumulative triangles. Reported counts are one
// Coarsen(...).Cumulative(MeasureReported) call away when something needs
// them.
func (g MonthlyGrid) AnnualTriangles(devYears int) AnnualSet {
	set := g.Coarsen(Annual, devYears, true)
	return AnnualSet{
		Paid:     set.Cumulative(MeasurePaid),
		NetPaid:  set.Cumulative(MeasurePaidNet),
		Incurred: set.Cumulative(MeasureIncurred),
	}
}

func raggedFloat(widths []int) [][]float64 {
	cells := make([][]float64, len(widths))
	for i, w := range widths {
		cells[i] = make([]float64, w)
	}
	return cells
}

func raggedInt(widths []int) [][]int {
	cells := make([][]int, len(widths))
	for i, w := range widths {
		cells[i] = make([]int, w)
	}
	return cells
}
```

- [ ] **Step 4: Re-implement the three annual constructors over the grid**

In `internal/domain/triangle/triangle.go`, replace the three constructors and delete `aggregate` entirely. Keep the `Triangle` type, `ATAFactors` and `latestDiagonal` as they are. The import block becomes:

```go
import (
	"math"
	"time"

	"github.com/le-marais/claimsgen/internal/domain/claim"
	"github.com/le-marais/claimsgen/internal/domain/shared"
	"github.com/le-marais/claimsgen/internal/domain/transaction"
)
```

and the three functions become:

```go
// PaidTriangle aggregates gross payments into a cumulative annual triangle by
// occurrence year. Development years beyond the last column are accumulated
// into it.
//
// Deprecated: a temporary shim over the monthly grid, removed once
// application.Aggregates owns the aggregation. Call BuildMonthlyGrid and
// AnnualTriangles instead.
func PaidTriangle(claims []claim.Claim, txs []transaction.Transaction, startYear, origins, devs int) Triangle {
	return annualShim(claims, txs, startYear, origins, devs).Paid
}

// NetPaidTriangle aggregates payments net of recoveries: salvage and
// subrogation rows subtract, so cumulative net paid can develop downward at
// late ages. Schedule P paid losses are net of salvage and subrogation, so
// this is the triangle the realism comparison uses.
//
// Deprecated: a temporary shim over the monthly grid, as PaidTriangle.
func NetPaidTriangle(claims []claim.Claim, txs []transaction.Transaction, startYear, origins, devs int) Triangle {
	return annualShim(claims, txs, startYear, origins, devs).NetPaid
}

// IncurredTriangle aggregates gross case plus net paid into a cumulative
// annual triangle by occurrence year: estimate movements and payments add,
// recoveries subtract.
//
// Deprecated: a temporary shim over the monthly grid, as PaidTriangle.
func IncurredTriangle(claims []claim.Claim, txs []transaction.Transaction, startYear, origins, devs int) Triangle {
	return annualShim(claims, txs, startYear, origins, devs).Incurred
}

// annualShim builds an accident-month grid over the same window and coarsens
// it back to years. The error cannot fire for these arguments - the basis is
// a constant and origins is at least one wherever the callers use it - so a
// failure yields empty triangles rather than a panic.
func annualShim(claims []claim.Claim, txs []transaction.Transaction, startYear, origins, devs int) AnnualSet {
	g, err := BuildMonthlyGrid(nil, claims, txs, shared.NewMonth(startYear, time.January), origins*12, AccidentMonth)
	if err != nil {
		return AnnualSet{}
	}
	return g.AnnualTriangles(devs)
}
```

- [ ] **Step 5: Run the whole suite - this is the behaviour-preservation gate**

Run: `go test ./... && go vet ./...`
Expected: **every** test passes with no test file edited except the new `coarsen_test.go`. In particular `TestPaidTriangleAggregatesCumulativePayments`, `TestIncurredTriangleIsPaidPlusOutstanding`, `TestNetPaidTriangleSubtractsRecoveries`, `TestIncurredTriangleSubtractsRecoveries` and `TestDefaultPresetIsRealistic` must pass untouched. If any of them fails, the coarsening is not faithful - fix the coarsening, do not adjust the test.

- [ ] **Step 6: Commit**

```bash
git add internal/domain/triangle/coarsen.go internal/domain/triangle/coarsen_test.go \
        internal/domain/triangle/triangle.go
git commit -m "Derive the annual triangles from the monthly grid"
```

---

### Task 5: One-based development periods

The UI and the report string already display 1-based periods computed from 0-based data. Move the convention into the data so every surface agrees.

**Files:**
- Modify: `internal/domain/triangle/compare.go` - `checkAges` and `Report.String()`
- Modify: `internal/infrastructure/web/static/app.js` - the band label
- Modify: `internal/domain/triangle/compare_test.go` - a test pinning the convention

**Interfaces:**
- Consumes: `triangle.AgeCheck` and `triangle.CompareToReference` as they are.
- Produces: `AgeCheck.Age` is the 1-based development period the factor develops **from**, so age 1 is the 1-to-2 factor. The JSON field `age` in `ageCheckJSON` carries the same 1-based value with no code change in `viewmodel.go`.

- [ ] **Step 1: Write the failing test**

Append to `internal/domain/triangle/compare_test.go` (create the file if it does not exist - the compare tests currently live in `triangle_test.go`, so append there instead and keep them together):

```go
func TestAgeChecksAreOneBased(t *testing.T) {
	refs := []triangle.ReferenceSet{
		{Name: "a", Paid: triangle.Triangle{Cells: [][]float64{{100, 150, 165}}},
			Incurred: triangle.Triangle{Cells: [][]float64{{140, 150, 165}}}, EarnedPremium: []float64{200}},
		{Name: "b", Paid: triangle.Triangle{Cells: [][]float64{{100, 160, 176}}},
			Incurred: triangle.Triangle{Cells: [][]float64{{150, 160, 176}}}, EarnedPremium: []float64{250}},
	}
	c := triangle.Comparison{
		Paid:          triangle.Triangle{Cells: [][]float64{{100, 155, 170}}},
		Incurred:      triangle.Triangle{Cells: [][]float64{{145, 155, 170}}},
		EarnedPremium: []float64{220},
	}
	report := triangle.CompareToReference(c, refs)
	if len(report.PaidATA) != 2 {
		t.Fatalf("got %d paid checks, want 2", len(report.PaidATA))
	}
	// The first factor develops development period 1 to 2, so its age is 1.
	if report.PaidATA[0].Age != 1 {
		t.Errorf("first paid check age = %d, want 1", report.PaidATA[0].Age)
	}
	if report.PaidATA[1].Age != 2 {
		t.Errorf("second paid check age = %d, want 2", report.PaidATA[1].Age)
	}
	if !strings.Contains(report.String(), "age 1-2") {
		t.Errorf("report should describe the first factor as age 1-2:\n%s", report.String())
	}
}
```

Add `"strings"` to that file's imports.

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/domain/triangle/ -run TestAgeChecksAreOneBased -v`
Expected: FAIL - `first paid check age = 0, want 1`.

- [ ] **Step 3: Make ages 1-based in the domain**

In `internal/domain/triangle/compare.go`, in `checkAges`, change the appended check to:

```go
		checks = append(checks, AgeCheck{
			Age: age + 1, Value: f, Band: bands[age], Within: bands[age].contains(f),
		})
```

and update the `AgeCheck` doc comment:

```go
// AgeCheck scores one development age against a band. Age is the 1-based
// development period the factor develops from, so age 1 is the factor from
// development period 1 to 2.
type AgeCheck struct {
```

In `Report.String()`, `writeChecks` stops adding one:

```go
	writeChecks := func(name string, checks []AgeCheck) {
		for _, c := range checks {
			fmt.Fprintf(&b, "%s ATA age %d-%d: %.4f in [%.4f, %.4f] = %v\n",
				name, c.Age, c.Age+1, c.Value, c.Band.Lo, c.Band.Hi, c.Within)
		}
	}
```

- [ ] **Step 4: Match the UI label**

In `internal/infrastructure/web/static/app.js`, in `bandCard`, change:

```js
    label.textContent = c.label ?? `${c.age + 1}→${c.age + 2}`;
```

to:

```js
    label.textContent = c.label ?? `${c.age}→${c.age + 1}`;
```

Leave the triangle table headers alone: they label matrix columns from a 0-indexed slice, so `Dev ${d + 1}` is already the 1-based period.

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./... && go vet ./...`
Expected: all PASS. `TestEvaluateRealismProducesChecksAtEveryAge` still expects 9 checks - the count is unchanged, only the labels shift.

- [ ] **Step 6: Commit**

```bash
git add internal/domain/triangle/compare.go internal/domain/triangle/triangle_test.go \
        internal/infrastructure/web/static/app.js
git commit -m "Number development ages from one"
```

---

### Task 6: `application.Aggregates` - one aggregation per run

Build the grid once, hand out the annual triangles, the monthly grid and the exposure. This is where the temporary shims die and where `RF-1` (the duplicated `developmentYears` constant with the same two triangles computed twice) is closed.

**Files:**
- Create: `internal/application/aggregate.go`
- Create: `internal/application/aggregate_test.go` - including the run-once oracle, which step 8 deletes again before the commit
- Modify: `internal/application/realism.go` - `EvaluateRealism` takes an `Aggregates`; the local `developmentYears` constant goes
- Modify: `internal/application/realism_test.go` - two call sites
- Modify: `internal/infrastructure/web/viewmodel.go` - `buildResponse` reads the aggregate; the local `developmentYears` constant goes
- Modify: `internal/infrastructure/web/server.go` - build the aggregate and pass it to `buildResponse`
- Modify: `internal/domain/triangle/triangle.go` - delete `PaidTriangle`, `NetPaidTriangle`, `IncurredTriangle` and `annualShim`
- Modify: `internal/domain/triangle/triangle_test.go` - the four tests calling those constructors move onto `AnnualTriangles`
- Modify: `internal/domain/triangle/coarsen_test.go` - `TestAnnualTrianglesMatchTheExportedConstructors` is deleted (its job passes to the oracle test)

**Interfaces:**
- Consumes: `triangle.BuildMonthlyGrid`, `(MonthlyGrid) AnnualTriangles`, `triangle.ExposureByMonth`, `triangle.EarnedPremiumByYear`, `triangle.OriginBasis`, `triangle.AccidentMonth`, `shared.NewMonth`.
- Produces: `application.Aggregates{Basis triangle.OriginBasis, StartYear, Years int, Grid triangle.MonthlyGrid, Exposure []triangle.MonthExposure, Annual triangle.AnnualSet, EarnedPremium []float64}`; `application.Aggregate(ds Dataset, startYear, years int, basis triangle.OriginBasis) (Aggregates, error)`; `application.EvaluateRealism(ag Aggregates, refs []triangle.ReferenceSet) triangle.Report`; the package-level constant `developmentYears = 10` now lives only in `aggregate.go`.

- [ ] **Step 1: Write the failing test**

Create `internal/application/aggregate_test.go`:

```go
package application_test

import (
	"testing"

	"github.com/le-marais/claimsgen/internal/application"
	"github.com/le-marais/claimsgen/internal/domain/claim"
	"github.com/le-marais/claimsgen/internal/domain/transaction"
	"github.com/le-marais/claimsgen/internal/domain/triangle"
	"github.com/le-marais/claimsgen/internal/infrastructure/random"
)

// legacyTriangle is a verbatim copy of the pre-refactor annual aggregation,
// used as a one-shot oracle: the grid-derived annual triangles must equal it
// cell for cell. It is the strongest available evidence that re-expressing the
// annual triangles as a coarsened monthly grid changed nothing, and so that
// the realism bands and the shipped preset's calibration still hold.
//
// It is deliberately temporary. Step 8 of this task deletes it once it has
// passed, so no duplicated aggregation logic lands on main - see "the oracle is
// run-once" below.
func legacyTriangle(claims []claim.Claim, txs []transaction.Transaction, startYear, origins, devs int, weight func(transaction.Transaction) float64) [][]float64 {
	occurrenceYear := make(map[int]int, len(claims))
	for _, c := range claims {
		occurrenceYear[c.ID] = c.OccurrenceDate.Year()
	}
	incremental := make([][]float64, origins)
	for i := range incremental {
		incremental[i] = make([]float64, devs)
	}
	for _, tx := range txs {
		w := weight(tx)
		if w == 0 {
			continue
		}
		occ := occurrenceYear[tx.ClaimID]
		origin := occ - startYear
		if origin < 0 || origin >= origins {
			continue
		}
		dev := tx.Date.Year() - occ
		if dev < 0 {
			dev = 0
		}
		if dev >= devs {
			dev = devs - 1
		}
		incremental[origin][dev] += w * tx.Amount.Dollars()
	}
	for _, row := range incremental {
		for d := 1; d < len(row); d++ {
			row[d] += row[d-1]
		}
	}
	return incremental
}

func legacyPaid(tx transaction.Transaction) float64 {
	if tx.Type == transaction.Payment {
		return 1
	}
	return 0
}

func legacyNetPaid(tx transaction.Transaction) float64 {
	switch {
	case tx.Type == transaction.Payment:
		return 1
	case tx.Type.IsRecovery():
		return -1
	}
	return 0
}

func legacyIncurred(tx transaction.Transaction) float64 {
	if tx.Type.IsRecovery() {
		return -1
	}
	return 1
}

func TestAggregateAnnualMatchesTheLegacyAggregation(t *testing.T) {
	req := request(t)
	ds, err := application.GenerateDataset(t.Context(), random.NewSource(3), req)
	if err != nil {
		t.Fatal(err)
	}
	ag, err := application.Aggregate(ds, req.StartYear, req.Years, triangle.AccidentMonth)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name   string
		got    triangle.Triangle
		weight func(transaction.Transaction) float64
	}{
		{"paid", ag.Annual.Paid, legacyPaid},
		{"net paid", ag.Annual.NetPaid, legacyNetPaid},
		{"incurred", ag.Annual.Incurred, legacyIncurred},
	}
	for _, c := range cases {
		want := legacyTriangle(ds.Claims, ds.Transactions, req.StartYear, req.Years, 10, c.weight)
		if len(c.got.Cells) != len(want) {
			t.Fatalf("%s: got %d origins, want %d", c.name, len(c.got.Cells), len(want))
		}
		for o := range want {
			if len(c.got.Cells[o]) != len(want[o]) {
				t.Fatalf("%s: origin %d is %d wide, want %d", c.name, o, len(c.got.Cells[o]), len(want[o]))
			}
			for d := range want[o] {
				if diff := c.got.Cells[o][d] - want[o][d]; diff > 1e-6 || diff < -1e-6 {
					t.Errorf("%s cell (%d, %d) = %v, want %v", c.name, o, d, c.got.Cells[o][d], want[o][d])
				}
			}
		}
	}
	if ag.Annual.Paid.StartYear != req.StartYear {
		t.Errorf("StartYear = %d, want %d", ag.Annual.Paid.StartYear, req.StartYear)
	}
}

func TestAggregateGridCountsEveryClaim(t *testing.T) {
	req := request(t)
	ds, err := application.GenerateDataset(t.Context(), random.NewSource(3), req)
	if err != nil {
		t.Fatal(err)
	}
	ag, err := application.Aggregate(ds, req.StartYear, req.Years, triangle.AccidentMonth)
	if err != nil {
		t.Fatal(err)
	}
	total := 0
	for _, row := range ag.Grid.Reported {
		for _, v := range row {
			total += v
		}
	}
	// Occurrences are constrained to the window and development runs to full
	// runoff, so no claim falls off an edge of the grid.
	if total != len(ds.Claims) {
		t.Errorf("reported count total = %d, want %d claims", total, len(ds.Claims))
	}
}

func TestAggregateGridReconcilesWithTheSummary(t *testing.T) {
	req := request(t)
	ds, err := application.GenerateDataset(t.Context(), random.NewSource(3), req)
	if err != nil {
		t.Fatal(err)
	}
	ag, err := application.Aggregate(ds, req.StartYear, req.Years, triangle.AccidentMonth)
	if err != nil {
		t.Fatal(err)
	}
	summary := application.Summarize(ds, req.StartYear, req.Years)

	sum := func(cells [][]float64) float64 {
		total := 0.0
		for _, row := range cells {
			for _, v := range row {
				total += v
			}
		}
		return total
	}
	if diff := sum(ag.Grid.Paid) - summary.Total.Paid; diff > 0.01 || diff < -0.01 {
		t.Errorf("grid paid total = %v, summary paid = %v", sum(ag.Grid.Paid), summary.Total.Paid)
	}
	wantNet := summary.Total.Paid - summary.Total.Recovered
	if diff := sum(ag.Grid.PaidNet) - wantNet; diff > 0.01 || diff < -0.01 {
		t.Errorf("grid net paid total = %v, want %v", sum(ag.Grid.PaidNet), wantNet)
	}
}

func TestAggregateExposureMatchesTheYearlyPremium(t *testing.T) {
	req := request(t)
	ds, err := application.GenerateDataset(t.Context(), random.NewSource(3), req)
	if err != nil {
		t.Fatal(err)
	}
	ag, err := application.Aggregate(ds, req.StartYear, req.Years, triangle.AccidentMonth)
	if err != nil {
		t.Fatal(err)
	}
	if len(ag.Exposure) != req.Years*12 {
		t.Fatalf("got %d exposure months, want %d", len(ag.Exposure), req.Years*12)
	}
	for y := 0; y < req.Years; y++ {
		sum := 0.0
		for i := y * 12; i < (y+1)*12; i++ {
			sum += ag.Exposure[i].Premium
		}
		if diff := sum - ag.EarnedPremium[y]; diff > 0.01 || diff < -0.01 {
			t.Errorf("year %d: monthly premium sum = %v, yearly = %v", req.StartYear+y, sum, ag.EarnedPremium[y])
		}
	}
}

func TestAggregateUnderwritingBasisLeavesTheAnnualViewAlone(t *testing.T) {
	req := request(t)
	ds, err := application.GenerateDataset(t.Context(), random.NewSource(3), req)
	if err != nil {
		t.Fatal(err)
	}
	accident, err := application.Aggregate(ds, req.StartYear, req.Years, triangle.AccidentMonth)
	if err != nil {
		t.Fatal(err)
	}
	underwriting, err := application.Aggregate(ds, req.StartYear, req.Years, triangle.UnderwritingMonth)
	if err != nil {
		t.Fatal(err)
	}
	// The realism gate and the UI must not change grain when the CSV basis
	// knob moves: Schedule P is an accident-year presentation.
	for o := range accident.Annual.Incurred.Cells {
		for d := range accident.Annual.Incurred.Cells[o] {
			a := accident.Annual.Incurred.Cells[o][d]
			u := underwriting.Annual.Incurred.Cells[o][d]
			if diff := a - u; diff > 1e-9 || diff < -1e-9 {
				t.Fatalf("annual incurred cell (%d, %d) moved with the basis: %v vs %v", o, d, a, u)
			}
		}
	}
	// The grid itself must differ, or the basis did nothing.
	if underwriting.Grid.Basis != triangle.UnderwritingMonth {
		t.Errorf("grid basis = %q, want underwriting", underwriting.Grid.Basis)
	}
}

func TestAggregateRejectsBadArguments(t *testing.T) {
	if _, err := application.Aggregate(application.Dataset{}, 1998, 0, triangle.AccidentMonth); err == nil {
		t.Error("want an error for zero years")
	}
	if _, err := application.Aggregate(application.Dataset{}, 1998, 3, triangle.OriginBasis("policy")); err == nil {
		t.Error("want an error for an unknown basis")
	}
}
```

**The oracle is run-once.** `legacyTriangle`, its three weight functions and `TestAggregateAnnualMatchesTheLegacyAggregation` prove the refactor and are then deleted in step 8, in the same commit, so main never carries a duplicate of the old aggregation logic. The other tests in `aggregate_test.go` stay. Do not skip running it - the whole point of Task 4's shims and this oracle is to catch a coarsening that is not faithful before anything is built on it.

The spec also lists a test that aggregating-then-cumulating commutes with cumulating-then-aggregating. `TestAggregateAnnualMatchesTheLegacyAggregation` subsumes it: the legacy oracle aggregates incremental cells and then cumulates, and the new path does the same through `Coarsen` and `Cumulative`, so equality across every cell of a generated dataset is the stronger statement. No separate test is needed.

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/application/ -run TestAggregate -v`
Expected: compile failure - `undefined: application.Aggregate`.

- [ ] **Step 3: Write the aggregate**

Create `internal/application/aggregate.go`:

```go
package application

import (
	"fmt"
	"time"

	"github.com/le-marais/claimsgen/internal/domain/shared"
	"github.com/le-marais/claimsgen/internal/domain/triangle"
)

// developmentYears is the depth of the annual triangles the realism gate and
// the UI read: Schedule P triangles have ten development years.
const developmentYears = 10

// Aggregates is everything derived from a dataset by pure aggregation: the
// monthly grid and exposure on the requested origin basis, plus the annual
// triangles and earned premium the realism gate and the UI read.
//
// Annual and EarnedPremium are always on the accident basis whatever Basis is.
// Schedule P is an accident-year presentation, so the realism comparison and
// the UI's triangle tab must not change grain when the origin-basis knob
// moves; the knob governs the monthly output.
type Aggregates struct {
	Basis         triangle.OriginBasis
	StartYear     int
	Years         int
	Grid          triangle.MonthlyGrid
	Exposure      []triangle.MonthExposure
	Annual        triangle.AnnualSet
	EarnedPremium []float64
}

// Aggregate aggregates a generated dataset. It draws no randomness and mutates
// nothing, so it cannot affect the reproducibility of a run.
func Aggregate(ds Dataset, startYear, years int, basis triangle.OriginBasis) (Aggregates, error) {
	if err := basis.Validate(); err != nil {
		return Aggregates{}, err
	}
	if years < 1 {
		return Aggregates{}, fmt.Errorf("years: must be at least 1, got %d", years)
	}
	start := shared.NewMonth(startYear, time.January)
	months := years * 12
	grid, err := triangle.BuildMonthlyGrid(ds.Policies, ds.Claims, ds.Transactions, start, months, basis)
	if err != nil {
		return Aggregates{}, err
	}
	accident := grid
	if basis != triangle.AccidentMonth {
		accident, err = triangle.BuildMonthlyGrid(ds.Policies, ds.Claims, ds.Transactions, start, months, triangle.AccidentMonth)
		if err != nil {
			return Aggregates{}, err
		}
	}
	return Aggregates{
		Basis:         basis,
		StartYear:     startYear,
		Years:         years,
		Grid:          grid,
		Exposure:      triangle.ExposureByMonth(ds.Policies, start, months, basis),
		Annual:        accident.AnnualTriangles(developmentYears),
		EarnedPremium: triangle.EarnedPremiumByYear(ds.Policies, startYear, years),
	}, nil
}
```

- [ ] **Step 4: Rewrite `realism.go` against the aggregate**

Replace the whole of `internal/application/realism.go` with:

```go
package application

import "github.com/le-marais/claimsgen/internal/domain/triangle"

// EvaluateRealism scores an aggregate's accident-year triangles and earned
// premium against the bands observed across the reference companies. Paid is
// net of salvage and subrogation to match Schedule P, which reports paid
// losses net of recoveries. Used as a test gate in the MVP; it also backs the
// UI's realism view.
func EvaluateRealism(ag Aggregates, refs []triangle.ReferenceSet) triangle.Report {
	return triangle.CompareToReference(triangle.Comparison{
		Paid:          ag.Annual.NetPaid,
		Incurred:      ag.Annual.Incurred,
		EarnedPremium: ag.EarnedPremium,
	}, refs)
}
```

- [ ] **Step 5: Update the realism test's two call sites**

In `internal/application/realism_test.go`, both tests replace

```go
			report := application.EvaluateRealism(ds, refs, req.StartYear, req.Years)
```

with

```go
			ag, err := application.Aggregate(ds, req.StartYear, req.Years, triangle.AccidentMonth)
			if err != nil {
				t.Fatal(err)
			}
			report := application.EvaluateRealism(ag, refs)
```

(dropping the extra indentation in the second test, which is not inside a subtest) and add `"github.com/le-marais/claimsgen/internal/domain/triangle"` to the imports.

- [ ] **Step 6: Update the web layer to the aggregate**

In `internal/infrastructure/web/viewmodel.go`: delete the `developmentYears` constant and its comment, and change `buildResponse` to take the aggregate rather than rebuild triangles:

```go
func buildResponse(req generateRequest, ds application.Dataset, ag application.Aggregates, refs []triangle.ReferenceSet) generateResponseJSON {
	return generateResponseJSON{
		Run: runInfoJSON{
			LOB:             req.Params.Name,
			Seed:            req.Seed,
			StartYear:       req.StartYear,
			Years:           req.Years,
			InitialBookSize: req.InitialBookSize,
			OutDir:          req.OutDir,
			Policies:        len(ds.Policies),
			Claims:          len(ds.Claims),
			Transactions:    len(ds.Transactions),
		},
		Summary: summaryView(application.Summarize(ds, req.StartYear, req.Years)),
		Triangles: trianglesJSON{
			Paid:     triangleView(ag.Annual.Paid),
			NetPaid:  triangleView(ag.Annual.NetPaid),
			Incurred: triangleView(ag.Annual.Incurred),
		},
		Distributions: distributionsView(application.ComputeDistributions(ds)),
		Realism:       realismView(application.EvaluateRealism(ag, refs)),
	}
}
```

In `internal/infrastructure/web/server.go`, in `handleGenerate`, build the aggregate after generation and pass it through. Immediately after the `csvout.WriteDataset` block, insert:

```go
	ag, err := application.Aggregate(ds, req.StartYear, req.Years, triangle.AccidentMonth)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
```

and change the final line to `writeJSON(w, http.StatusOK, buildResponse(req, ds, ag, s.refs))`. Add the `triangle` import if it is not already there. Task 9 replaces the hardcoded `triangle.AccidentMonth` with the request's basis and adds the `WriteAggregates` call.

- [ ] **Step 7: Delete the shims and move the domain tests onto `AnnualTriangles`**

In `internal/domain/triangle/triangle.go`, delete `PaidTriangle`, `NetPaidTriangle`, `IncurredTriangle` and `annualShim`. The file now holds only the `Triangle` type, `ATAFactors` and `latestDiagonal`, and its imports reduce to `"math"`.

In `internal/domain/triangle/coarsen_test.go`, delete `TestAnnualTrianglesMatchTheExportedConstructors` - the oracle test in the application layer now carries that job against a full generated dataset.

In `internal/domain/triangle/triangle_test.go`, add a helper and rewrite the four tests that called the deleted constructors:

```go
// annualFrom builds the annual triangles the way the application does: an
// accident-month grid over the window, coarsened back to years.
func annualFrom(t *testing.T, claims []claim.Claim, txs []transaction.Transaction, startYear, years, devs int) triangle.AnnualSet {
	t.Helper()
	g, err := triangle.BuildMonthlyGrid(nil, claims, txs, shared.NewMonth(startYear, time.January), years*12, triangle.AccidentMonth)
	if err != nil {
		t.Fatal(err)
	}
	return g.AnnualTriangles(devs)
}
```

Then in each test replace the constructor call, keeping every assertion exactly as it is:

- `TestPaidTriangleAggregatesCumulativePayments`: `tri := annualFrom(t, claims, txs, 1998, 2, 3).Paid`
- `TestIncurredTriangleIsPaidPlusOutstanding`: `tri := annualFrom(t, claims, txs, 1998, 2, 3).Incurred`
- `TestNetPaidTriangleSubtractsRecoveries`: `gross := annualFrom(t, claims, txs, 1998, 3, 3).Paid` and `net := annualFrom(t, claims, txs, 1998, 3, 3).NetPaid`
- `TestIncurredTriangleSubtractsRecoveries`: `incurred := annualFrom(t, claims, txs, 1998, 2, 2).Incurred`

- [ ] **Step 8: Run the whole suite, then retire the oracle**

Run: `go test ./... && go vet ./...`
Expected: all PASS. The oracle test proves the annual triangles are unchanged on a generated dataset; `TestDefaultPresetIsRealistic` still passes on all three seeds; the golden hash is untouched because no generated CSV changed.

Only once that run is green, delete the oracle from `internal/application/aggregate_test.go`: `legacyTriangle`, `legacyPaid`, `legacyNetPaid`, `legacyIncurred` and `TestAggregateAnnualMatchesTheLegacyAggregation`, plus the now-unused `claim` and `transaction` imports. Keep every other test in the file. Then run `go test ./... && go vet ./...` again and confirm it is still green.

Report both runs in the report file: the green run **with** the oracle (quoting its result) is the behaviour-preservation evidence, and the green run after removing it is the state that gets committed. If the oracle fails, do not delete it and do not commit - the coarsening is not faithful; fix that first.

- [ ] **Step 9: Commit**

```bash
git add internal/application/aggregate.go internal/application/aggregate_test.go \
        internal/application/realism.go internal/application/realism_test.go \
        internal/infrastructure/web/viewmodel.go internal/infrastructure/web/server.go \
        internal/domain/triangle/triangle.go internal/domain/triangle/triangle_test.go \
        internal/domain/triangle/coarsen_test.go
git commit -m "Aggregate a dataset once per run"
```

---

### Task 7: The two new CSV files

**Files:**
- Create: `internal/infrastructure/csv/monthly.go`
- Create: `internal/infrastructure/csv/monthly_test.go`
- Modify: `internal/infrastructure/csv/writer.go` - the package doc mentions five files
- Modify: `internal/application/golden_test.go` - a second hash for the two new files

**Interfaces:**
- Consumes: `application.Aggregates`, `triangle.MonthlyGrid` fields `StartMonth`, `DevPeriods`, `Paid`, `PaidNet`, `Incurred`, `Reported`, `(MonthlyGrid) Origins()`, `triangle.MonthExposure`, `(shared.Month) String()`, and the existing package-private `writeFile(dir, name, header string, rows int, row func(int) string) error`.
- Produces: `csv.WriteAggregates(dir string, ag application.Aggregates) error`, writing `triangles.csv` and `exposure.csv`.

- [ ] **Step 1: Write the failing test**

Create `internal/infrastructure/csv/monthly_test.go`:

```go
package csv_test

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/le-marais/claimsgen/internal/application"
	"github.com/le-marais/claimsgen/internal/domain/claim"
	"github.com/le-marais/claimsgen/internal/domain/policy"
	"github.com/le-marais/claimsgen/internal/domain/shared"
	"github.com/le-marais/claimsgen/internal/domain/transaction"
	"github.com/le-marais/claimsgen/internal/domain/triangle"
	csvout "github.com/le-marais/claimsgen/internal/infrastructure/csv"
)

// aggregateFixture is one policy incepting 1 January 1998 and one claim
// occurring in March 1998, paid $1,000 in April 1998 with $150 of salvage in
// June 1998.
func aggregateFixture(t *testing.T) application.Aggregates {
	t.Helper()
	ds := application.Dataset{
		Policies: []policy.Policy{{
			ID:         1,
			CoverStart: shared.NewDate(1998, time.January, 1),
			CoverEnd:   shared.NewDate(1998, time.December, 31),
			Premium:    shared.FromDollars(365),
		}},
		Claims: []claim.Claim{{
			ID: 1, PolicyID: 1,
			OccurrenceDate: shared.NewDate(1998, time.March, 1),
			ReportDate:     shared.NewDate(1998, time.March, 5),
		}},
		Transactions: []transaction.Transaction{
			{ID: 1, ClaimID: 1, Date: shared.NewDate(1998, time.March, 5), Type: transaction.Estimate, Amount: shared.FromDollars(1000)},
			{ID: 2, ClaimID: 1, Date: shared.NewDate(1998, time.April, 1), Type: transaction.Payment, Amount: shared.FromDollars(1000)},
			{ID: 3, ClaimID: 1, Date: shared.NewDate(1998, time.April, 1), Type: transaction.Estimate, Amount: shared.FromDollars(-1000)},
			{ID: 4, ClaimID: 1, Date: shared.NewDate(1998, time.June, 1), Type: transaction.Salvage, Amount: shared.FromDollars(150)},
		},
	}
	ag, err := application.Aggregate(ds, 1998, 1, triangle.AccidentMonth)
	if err != nil {
		t.Fatal(err)
	}
	return ag
}

func writeFixture(t *testing.T) (dir string, ag application.Aggregates) {
	t.Helper()
	dir = t.TempDir()
	ag = aggregateFixture(t)
	if err := csvout.WriteAggregates(dir, ag); err != nil {
		t.Fatal(err)
	}
	return dir, ag
}

func readLines(t *testing.T, path string) []string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSuffix(string(b), "\n"), "\n")
}

func TestWriteAggregatesTrianglesHeaderAndShape(t *testing.T) {
	dir, ag := writeFixture(t)
	lines := readLines(t, filepath.Join(dir, "triangles.csv"))
	if lines[0] != "origin_month,dev_month,paid,paid_net,incurred,reported_count" {
		t.Errorf("header = %q", lines[0])
	}
	wantRows := ag.Grid.Origins() * ag.Grid.DevPeriods
	if got := len(lines) - 1; got != wantRows {
		t.Errorf("got %d rows, want %d (%d origins x %d development months)",
			got, wantRows, ag.Grid.Origins(), ag.Grid.DevPeriods)
	}
	// Rows are ordered by origin month then development month, and the first
	// development month is 1.
	if !strings.HasPrefix(lines[1], "1998-01,1,") {
		t.Errorf("first row = %q, want it to start 1998-01,1,", lines[1])
	}
	if !strings.HasPrefix(lines[2], "1998-01,2,") {
		t.Errorf("second row = %q, want it to start 1998-01,2,", lines[2])
	}
}

func TestWriteAggregatesTriangleValues(t *testing.T) {
	dir, _ := writeFixture(t)
	lines := readLines(t, filepath.Join(dir, "triangles.csv"))
	find := func(origin string, dev int) string {
		t.Helper()
		prefix := origin + "," + strconv.Itoa(dev) + ","
		for _, l := range lines[1:] {
			if strings.HasPrefix(l, prefix) {
				return l
			}
		}
		t.Fatalf("no row for %s dev %d", origin, dev)
		return ""
	}
	// March origin, development 1: the case estimate is raised, nothing paid,
	// one claim reported.
	if got, want := find("1998-03", 1), "1998-03,1,0.00,0.00,1000.00,1"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	// Development 2 is April: pay 1,000 and release the case, so incurred is flat.
	if got, want := find("1998-03", 2), "1998-03,2,1000.00,1000.00,0.00,0"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	// Development 4 is June: salvage of 150 comes back.
	if got, want := find("1998-03", 4), "1998-03,4,0.00,-150.00,-150.00,0"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestWriteAggregatesExposure(t *testing.T) {
	dir, ag := writeFixture(t)
	lines := readLines(t, filepath.Join(dir, "exposure.csv"))
	if lines[0] != "origin_month,premium,exposure_units,policies" {
		t.Errorf("header = %q", lines[0])
	}
	if got := len(lines) - 1; got != len(ag.Exposure) {
		t.Errorf("got %d rows, want %d", got, len(ag.Exposure))
	}
	// A 365-day policy on $365 earns a dollar a day, so January earns 31.
	if got, want := lines[1], "1998-01,31.00,0.084873,1"; got != want {
		t.Errorf("January row = %q, want %q", got, want)
	}
}

func TestWriteAggregatesNeverWritesNegativeZero(t *testing.T) {
	dir, _ := writeFixture(t)
	for _, name := range []string{"triangles.csv", "exposure.csv"} {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(b), "-0.00") || strings.Contains(string(b), "-0.000000") {
			t.Errorf("%s contains a negative zero", name)
		}
	}
}

func TestWriteAggregatesIsByteStable(t *testing.T) {
	ag := aggregateFixture(t)
	dirA, dirB := t.TempDir(), t.TempDir()
	if err := csvout.WriteAggregates(dirA, ag); err != nil {
		t.Fatal(err)
	}
	if err := csvout.WriteAggregates(dirB, ag); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"triangles.csv", "exposure.csv"} {
		a, err := os.ReadFile(filepath.Join(dirA, name))
		if err != nil {
			t.Fatal(err)
		}
		b, err := os.ReadFile(filepath.Join(dirB, name))
		if err != nil {
			t.Fatal(err)
		}
		if string(a) != string(b) {
			t.Errorf("%s differs between two writes of the same aggregate", name)
		}
	}
}

func TestWriteAggregatesCreatesTheDirectory(t *testing.T) {
	ag := aggregateFixture(t)
	dir := filepath.Join(t.TempDir(), "nested", "out")
	if err := csvout.WriteAggregates(dir, ag); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "triangles.csv")); err != nil {
		t.Errorf("triangles.csv missing: %v", err)
	}
}
```

The expected exposure units in `TestWriteAggregatesExposure` are `31 / 365.25 = 0.0848733744...`, which is `0.084873` at six places. If the assertion fails on the last digit, verify the arithmetic before changing the expectation.

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/infrastructure/csv/ -run TestWriteAggregates -v`
Expected: compile failure - `undefined: csvout.WriteAggregates`.

- [ ] **Step 3: Write the writer**

Create `internal/infrastructure/csv/monthly.go`:

```go
package csv

import (
	"fmt"
	"math"
	"os"
	"strconv"

	"github.com/le-marais/claimsgen/internal/application"
)

// WriteAggregates writes triangles.csv and exposure.csv into dir, creating it
// if needed.
//
// triangles.csv holds the incremental monthly grid, one row per cell, ordered
// by origin month then development month. Development is 1-based and runs to
// full runoff, so a valuation-date view is a filter on
// origin_month + dev_month - 1. Every cell is written, zeros included, so the
// file states the grid's exact extent.
//
// exposure.csv holds one row per origin month on the same origin basis.
func WriteAggregates(dir string, ag application.Aggregates) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating output directory: %w", err)
	}
	g := ag.Grid
	rows := g.Origins() * g.DevPeriods
	if err := writeFile(dir, "triangles.csv",
		"origin_month,dev_month,paid,paid_net,incurred,reported_count",
		rows, func(i int) string {
			o, d := i/g.DevPeriods, i%g.DevPeriods
			return fmt.Sprintf("%s,%d,%s,%s,%s,%d",
				g.StartMonth.Add(o), d+1,
				formatAmount(g.Paid[o][d]),
				formatAmount(g.PaidNet[o][d]),
				formatAmount(g.Incurred[o][d]),
				g.Reported[o][d])
		}); err != nil {
		return err
	}
	return writeFile(dir, "exposure.csv",
		"origin_month,premium,exposure_units,policies",
		len(ag.Exposure), func(i int) string {
			e := ag.Exposure[i]
			return fmt.Sprintf("%s,%s,%s,%d",
				e.Month, formatAmount(e.Premium), formatUnits(e.ExposureUnits), e.Policies)
		})
}

// formatAmount renders a money amount at fixed precision. A value that rounds
// to zero is written as a positive zero, so a cell whose movements cancel
// reads "0.00" rather than "-0.00".
func formatAmount(v float64) string {
	if math.Round(v*100) == 0 {
		return "0.00"
	}
	return strconv.FormatFloat(v, 'f', 2, 64)
}

// formatUnits renders exposure units at fixed precision, with the same
// negative-zero guard.
func formatUnits(v float64) string {
	if math.Round(v*1e6) == 0 {
		return "0.000000"
	}
	return strconv.FormatFloat(v, 'f', 6, 64)
}
```

Then update the package doc at the top of `internal/infrastructure/csv/writer.go`:

```go
// Package csv writes the generated dataset and its aggregates as CSV files
// with stable formatting, so identical datasets produce byte-identical files:
// policies.csv, claims.csv and transactions.csv from WriteDataset, plus
// triangles.csv and exposure.csv from WriteAggregates.
package csv
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/infrastructure/csv/ -v` then `go vet ./internal/infrastructure/csv/`
Expected: all PASS, vet clean.

- [ ] **Step 5: Pin the new files in the golden test**

In `internal/application/golden_test.go`, add a second constant and a second test. Leave `wantHash` and `TestGoldenCSVBytes` untouched, so they keep proving that generation itself did not move:

```go
// wantAggregateHash pins the byte-stable aggregate CSV output for the same
// small deterministic dataset. Regenerate it the same way as wantHash: run the
// test once, it prints the actual value, paste it back in. Do not update it to
// hide an unintended change.
const wantAggregateHash = "PASTE_THE_PRINTED_HASH_HERE"

func TestGoldenAggregateCSVBytes(t *testing.T) {
	req := request(t)
	ds, err := application.GenerateDataset(t.Context(), random.NewSource(1), req)
	if err != nil {
		t.Fatal(err)
	}
	ag, err := application.Aggregate(ds, req.StartYear, req.Years, triangle.AccidentMonth)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := csvout.WriteAggregates(dir, ag); err != nil {
		t.Fatal(err)
	}
	h := sha256.New()
	for _, name := range []string{"triangles.csv", "exposure.csv"} {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		h.Write(b)
	}
	got := hex.EncodeToString(h.Sum(nil))
	if got != wantAggregateHash {
		t.Fatalf("golden aggregate CSV hash mismatch:\n got: %s\nwant: %s", got, wantAggregateHash)
	}
}
```

Add `"github.com/le-marais/claimsgen/internal/domain/triangle"` to that file's imports. Run the test once, take the `got:` value from the failure message, and paste it into `wantAggregateHash`.

- [ ] **Step 6: Run the full suite**

Run: `go test ./... && go vet ./...`
Expected: all PASS with the pasted hash in place.

- [ ] **Step 7: Commit**

```bash
git add internal/infrastructure/csv/monthly.go internal/infrastructure/csv/monthly_test.go \
        internal/infrastructure/csv/writer.go internal/application/golden_test.go
git commit -m "Write the monthly triangles and exposure as CSV"
```

---

### Task 8: CLI wiring

**Files:**
- Modify: `cmd/claimsgen/main.go` - usage text, `--origin-basis` flag, basis validation before the run, `WriteAggregates` call, extended success line
- Modify: `cmd/claimsgen/main_test.go` - coverage for all of the above

**Interfaces:**
- Consumes: `application.Aggregate`, `csvout.WriteAggregates`, `triangle.OriginBasis`, `(OriginBasis) Validate()`, `(MonthlyGrid) Origins()`.
- Produces: the `generate` subcommand writes five CSVs and accepts `--origin-basis`.

- [ ] **Step 1: Write the failing test**

Add to `cmd/claimsgen/main_test.go`:

```go
func TestGenerateWritesTrianglesAndExposure(t *testing.T) {
	out := filepath.Join(t.TempDir(), "output")
	var stdout, stderr bytes.Buffer
	code := run([]string{"generate", "--out", out, "--years", "2", "--initial-book-size", "100", "--seed", "7"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, stderr: %s", code, stderr.String())
	}
	for _, name := range []string{"policies.csv", "claims.csv", "transactions.csv", "triangles.csv", "exposure.csv"} {
		if _, err := os.Stat(filepath.Join(out, name)); err != nil {
			t.Errorf("missing %s: %v", name, err)
		}
	}
	if !strings.Contains(stdout.String(), "triangle rows") {
		t.Errorf("stdout %q should report the triangle row count", stdout.String())
	}
	if !strings.Contains(stdout.String(), "exposure rows") {
		t.Errorf("stdout %q should report the exposure row count", stdout.String())
	}
}

func TestGenerateAcceptsBothOriginBases(t *testing.T) {
	for _, basis := range []string{"accident", "underwriting"} {
		out := filepath.Join(t.TempDir(), "output")
		var stdout, stderr bytes.Buffer
		code := run([]string{"generate", "--out", out, "--years", "2", "--initial-book-size", "100",
			"--seed", "7", "--origin-basis", basis}, &stdout, &stderr)
		if code != 0 {
			t.Fatalf("basis %q: exit code = %d, stderr: %s", basis, code, stderr.String())
		}
		if _, err := os.Stat(filepath.Join(out, "triangles.csv")); err != nil {
			t.Errorf("basis %q: missing triangles.csv: %v", basis, err)
		}
	}
}

func TestGenerateOriginBasesDifferInTheOutput(t *testing.T) {
	read := func(t *testing.T, basis string) []byte {
		t.Helper()
		out := filepath.Join(t.TempDir(), "output")
		var buf bytes.Buffer
		if code := run([]string{"generate", "--out", out, "--years", "2", "--initial-book-size", "100",
			"--seed", "7", "--origin-basis", basis}, &buf, &buf); code != 0 {
			t.Fatalf("basis %q failed: %s", basis, buf.String())
		}
		b, err := os.ReadFile(filepath.Join(out, "triangles.csv"))
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	if bytes.Equal(read(t, "accident"), read(t, "underwriting")) {
		t.Error("the two origin bases produced identical triangles.csv; the flag is not wired through")
	}
}

func TestGenerateRejectsAnUnknownOriginBasis(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"generate", "--out", filepath.Join(t.TempDir(), "o"), "--origin-basis", "policy"}, &stdout, &stderr)
	if code == 0 {
		t.Fatal("expected nonzero exit for an unknown origin basis")
	}
	if !strings.Contains(stderr.String(), "origin basis") {
		t.Errorf("stderr %q should name the origin basis problem", stderr.String())
	}
}
```

Also extend the existing `TestGenerateSameSeedSameBytes` file list to cover the new files:

```go
	for _, name := range []string{"policies.csv", "claims.csv", "transactions.csv", "triangles.csv", "exposure.csv"} {
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./cmd/claimsgen/ -run 'TestGenerateWritesTriangles|TestGenerateAccepts|TestGenerateOriginBases|TestGenerateRejectsAnUnknown' -v`
Expected: FAIL - `missing triangles.csv`, and `flag provided but not defined: -origin-basis`.

- [ ] **Step 3: Wire the CLI**

In `cmd/claimsgen/main.go`, add to the `generate flags` block of the `usage` constant, after the `--initial-book-size` line:

```
  --origin-basis B         monthly triangle origin: accident or underwriting (default accident)
```

Add the flag alongside the others in `runGenerate`:

```go
	originBasis := fs.String("origin-basis", "accident", "monthly triangle origin basis")
```

Validate it immediately after `fs.Parse` succeeds, so a typo costs nothing:

```go
	basis := triangle.OriginBasis(*originBasis)
	if err := basis.Validate(); err != nil {
		fmt.Fprintf(stderr, "claimsgen: %v\n", err)
		return 1
	}
```

After the existing `csvout.WriteDataset` block, aggregate and write:

```go
	ag, err := application.Aggregate(ds, *startYear, *years, basis)
	if err != nil {
		fmt.Fprintf(stderr, "claimsgen: %v\n", err)
		return 1
	}
	if err := csvout.WriteAggregates(*out, ag); err != nil {
		fmt.Fprintf(stderr, "claimsgen: %v\n", err)
		return 1
	}
```

Extend the success line:

```go
	fmt.Fprintf(stdout, "%s: wrote %d policies, %d claims, %d transactions, %d triangle rows, %d exposure rows to %s (seed %d)\n",
		l.Name, len(ds.Policies), len(ds.Claims), len(ds.Transactions),
		ag.Grid.Origins()*ag.Grid.DevPeriods, len(ag.Exposure), *out, *seed)
```

Add `"github.com/le-marais/claimsgen/internal/domain/triangle"` to the imports.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./cmd/claimsgen/ -v` then `go vet ./cmd/...`
Expected: all PASS, vet clean.

- [ ] **Step 5: Sanity-check the real binary**

Run:

```bash
go build ./cmd/claimsgen
./claimsgen generate --out /tmp/claimsgen-check --years 3 --initial-book-size 500
head -3 /tmp/claimsgen-check/triangles.csv
head -3 /tmp/claimsgen-check/exposure.csv
wc -l /tmp/claimsgen-check/triangles.csv
rm -rf /tmp/claimsgen-check ./claimsgen
```

Expected: `triangles.csv` starts `origin_month,dev_month,paid,paid_net,incurred,reported_count` with a `1998-01,1,...` row; `exposure.csv` starts `origin_month,premium,exposure_units,policies`; the row count is `36 * DevPeriods + 1`.

- [ ] **Step 6: Commit**

```bash
git add cmd/claimsgen/main.go cmd/claimsgen/main_test.go
git commit -m "Write the monthly output from the generate command"
```

---

### Task 9: UI wiring

No new browser view - the tabs stay as they are. The UI gains one form control, sends it, writes the same five files, and shows the basis in the run line.

**Files:**
- Modify: `internal/infrastructure/web/server.go` - `origin_basis` on the request, validation, the `WriteAggregates` call, the basis passed to `Aggregate`
- Modify: `internal/infrastructure/web/viewmodel.go` - `origin_basis` on `runInfoJSON`
- Modify: `internal/infrastructure/web/static/index.html` - the select
- Modify: `internal/infrastructure/web/static/app.js` - send the field, show it in the run line
- Modify: `internal/infrastructure/web/server_test.go` - coverage

**Interfaces:**
- Consumes: everything from Tasks 6, 7 and 8.
- Produces: `POST /api/generate` accepts an optional `origin_basis` of `"accident"` or `"underwriting"`, defaulting to accident when absent or empty, rejecting anything else with 400, writing five CSVs, and echoing the basis back as `run.origin_basis`.

- [ ] **Step 1: Write the failing test**

Add to `internal/infrastructure/web/server_test.go`:

```go
func TestGenerateWritesTrianglesAndExposure(t *testing.T) {
	outDir := t.TempDir()
	rec := do(t, newTestServer(t), "POST", "/api/generate", generateBody(t, outDir))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	for _, name := range []string{"policies.csv", "claims.csv", "transactions.csv", "triangles.csv", "exposure.csv"} {
		if _, err := os.Stat(filepath.Join(outDir, name)); err != nil {
			t.Errorf("missing %s: %v", name, err)
		}
	}
}

func TestGenerateDefaultsToTheAccidentBasis(t *testing.T) {
	// generateBody carries no origin_basis, so the response must report the
	// accident default rather than an empty string.
	rec := do(t, newTestServer(t), "POST", "/api/generate", generateBody(t, t.TempDir()))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Run struct {
			OriginBasis string `json:"origin_basis"`
		} `json:"run"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Run.OriginBasis != "accident" {
		t.Errorf("origin_basis = %q, want \"accident\"", resp.Run.OriginBasis)
	}
}

func TestGenerateAcceptsTheUnderwritingBasis(t *testing.T) {
	body := generateBody(t, t.TempDir())
	body["origin_basis"] = "underwriting"
	rec := do(t, newTestServer(t), "POST", "/api/generate", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Run struct {
			OriginBasis string `json:"origin_basis"`
		} `json:"run"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Run.OriginBasis != "underwriting" {
		t.Errorf("origin_basis = %q, want \"underwriting\"", resp.Run.OriginBasis)
	}
}

func TestGenerateRejectsAnUnknownOriginBasis(t *testing.T) {
	body := generateBody(t, t.TempDir())
	body["origin_basis"] = "policy"
	rec := do(t, newTestServer(t), "POST", "/api/generate", body)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "origin basis") {
		t.Errorf("body %q should name the origin basis problem", rec.Body.String())
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/infrastructure/web/ -run 'TestGenerateWritesTriangles|TestGenerateDefaultsTo|TestGenerateAcceptsTheUnderwriting|TestGenerateRejectsAnUnknown' -v`
Expected: FAIL - `missing triangles.csv`, `origin_basis = ""`, and a 400 for the valid underwriting request because `DisallowUnknownFields` rejects the field.

- [ ] **Step 3: Wire the server**

In `internal/infrastructure/web/server.go`, add the field to `generateRequest`:

```go
type generateRequest struct {
	Seed            string           `json:"seed"`
	StartYear       int              `json:"start_year"`
	Years           int              `json:"years"`
	InitialBookSize int              `json:"initial_book_size"`
	OutDir          string           `json:"out_dir"`
	OriginBasis     string           `json:"origin_basis"`
	Params          config.LOBParams `json:"params"`
}
```

In `handleGenerate`, resolve and validate the basis before the run-size check, so a bad basis never occupies the run slot:

```go
	if req.OriginBasis == "" {
		req.OriginBasis = string(triangle.AccidentMonth)
	}
	basis := triangle.OriginBasis(req.OriginBasis)
	if err := basis.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
```

Replace the `application.Aggregate(...)` call added in Task 6 with one that uses the request's basis, and write the files:

```go
	ag, err := application.Aggregate(ds, req.StartYear, req.Years, basis)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := csvout.WriteAggregates(req.OutDir, ag); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
```

Keep it after the existing `csvout.WriteDataset` block so the three primary files land first.

In `internal/infrastructure/web/viewmodel.go`, add the field to `runInfoJSON` and populate it:

```go
type runInfoJSON struct {
	LOB             string `json:"lob"`
	Seed            string `json:"seed"`
	StartYear       int    `json:"start_year"`
	Years           int    `json:"years"`
	InitialBookSize int    `json:"initial_book_size"`
	OriginBasis     string `json:"origin_basis"`
	OutDir          string `json:"out_dir"`
	Policies        int    `json:"policies"`
	Claims          int    `json:"claims"`
	Transactions    int    `json:"transactions"`
}
```

and in `buildResponse`'s `runInfoJSON` literal add `OriginBasis: string(ag.Basis),`.

- [ ] **Step 4: Add the form control**

In `internal/infrastructure/web/static/index.html`, after the Output directory label (line 24):

```html
        <label>Origin basis <select id="origin-basis">
          <option value="accident" selected>Accident month</option>
          <option value="underwriting">Underwriting month</option>
        </select></label>
```

In `internal/infrastructure/web/static/app.js`, add the field to the request body next to `out_dir`:

```js
      origin_basis: $("#origin-basis").value,
```

and extend the run summary line so the basis is visible:

```js
    `${run.lob} · seed ${run.seed} · ${run.start_year}–${run.start_year + run.years - 1} · ` +
```

becomes

```js
    `${run.lob} · seed ${run.seed} · ${run.start_year}–${run.start_year + run.years - 1} · ` +
    `${run.origin_basis} origin · ` +
```

(insert the new line between the existing first and second template pieces; leave the rest of the concatenation as it is).

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./... && go vet ./...`
Expected: all PASS. The existing `TestGenerateRoundTrip` and friends still pass because `generateBody` omits `origin_basis` and an omitted field is not an unknown field.

- [ ] **Step 6: Check the UI by hand**

Run `go build ./cmd/claimsgen && ./claimsgen ui`, open `http://127.0.0.1:8080`, confirm the Origin basis select appears, generate with each value, and confirm the run line names the basis and that `triangles.csv` and `exposure.csv` appear in the output directory. Then stop the server and `rm ./claimsgen`.

- [ ] **Step 7: Commit**

```bash
git add internal/infrastructure/web/server.go internal/infrastructure/web/viewmodel.go \
        internal/infrastructure/web/server_test.go \
        internal/infrastructure/web/static/index.html internal/infrastructure/web/static/app.js
git commit -m "Offer the origin basis in the browser UI"
```

---

### Task 10: Documentation

**Files:**
- Modify: `README.md`
- Modify: `AGENTS.md`
- Modify: `docs/roadmap.md`
- Modify: `docs/todo.md`
- Modify: `docs/detailed-architecture.md`
- Modify: `docs/superpowers/specs/2026-08-18-monthly-triangles-design.md` - add the out-of-context banner now that the work has shipped

Writing style: sentence case headers, no em dashes (spaced hyphens ` - `), concise and factual, no embellishment beyond what the code does.

- [ ] **Step 1: Update `README.md`**

Line 5 and the list under it become five datasets:

```markdown
One run produces five linked CSV datasets for a class of business:

- **policies.csv** - the book of policies per calendar year: cover dates, sum insured, excess, risk factor, premium
- **claims.csv** - claim events with occurrence, report and close dates plus the initial case estimate
- **transactions.csv** - each claim's case estimate movements, payments, and recoveries (salvage and subrogation) over its lifetime
- **triangles.csv** - incremental monthly development triangles by origin month: paid, paid net of recoveries, incurred, and reported claim counts
- **exposure.csv** - exposure by origin month: premium, exposure units in policy-years, and policy count
```

In the `generate` example block around line 24, add the new flag:

```
  --origin-basis accident \   # monthly origin: accident or underwriting
```

Add a subsection after the output-notes section (near line 65) documenting the two files:

```markdown
### Monthly triangles and exposure

`triangles.csv` is one row per (origin month, development month) cell:

```
origin_month,dev_month,paid,paid_net,incurred,reported_count
```

Cells are **incremental**, not cumulative - the movement in that development
month - so they aggregate up by simple addition: sum cells into quarters or
years, or take a running sum along a row for the cumulative triangle. Development
months are numbered from 1, where 1 is the origin month itself, and run to full
runoff, so the file includes development after the run window ends. For a
valuation-date view, keep only the rows where `origin_month + dev_month - 1` is
at or before the valuation month. Every cell is written, zeros included, so the
grid's extent is explicit.

`paid` is gross of recoveries, `paid_net` subtracts salvage and subrogation, and
`incurred` is gross case plus net paid. `reported_count` counts claims in the
development month they were **reported**, not the month they occurred.

`exposure.csv` is one row per origin month on the same axis:

```
origin_month,premium,exposure_units,policies
```

`--origin-basis accident` (the default) keys a claim on the month it occurred and
reports exposure earned in each month, day pro-rata. `--origin-basis
underwriting` keys a claim on its policy's inception month and reports exposure
written in that month, so a policy's whole premium and whole term land at
inception. The basis governs these two files only: the annual triangles and the
realism check stay on the accident basis, because the Schedule P reference data
is an accident-year presentation.
```

In the UI paragraph (line 36), change "writes the same three CSVs on Generate" to "writes the same five CSVs on Generate", and add the origin basis to the list of run flags it offers.

- [ ] **Step 2: Update `AGENTS.md`**

In the "What this is" section, "One run produces three linked CSVs for a class of business" becomes "five linked CSVs", and add the two new bullets:

```markdown
- `triangles.csv` - incremental monthly development triangles by origin month (paid, paid net of recoveries, incurred, reported claim counts)
- `exposure.csv` - exposure by origin month (premium, exposure units in policy-years, policy count)
```

In the "Build, run, test" block, update the `generate` comment to "generate the five CSVs into ./output using the embedded motor preset".

In "Conventions and things to know", extend the golden-test bullet to mention both hashes:

```markdown
- **Golden tests.** `internal/application/golden_test.go` pins two SHA-256 digests: `wantHash` over the three dataset CSVs and `wantAggregateHash` over `triangles.csv` and `exposure.csv`. If you intentionally change the generated data or its encoding, the failing test prints the actual hash - paste it back into the constant. Do not update either to hide an unintended change; understand why the output moved first.
```

Add a bullet on the aggregation grain:

```markdown
- **One aggregation store.** `triangle.MonthlyGrid` is the canonical aggregate: incremental cells, origin months down, development months across, running to full runoff. Every coarser grain is `MonthlyGrid.Coarsen`, which keys both axes on the calendar period the month falls in - the annual triangles the realism gate and the UI read are `Coarsen(Annual, 10, foldTail)` cumulated. Add new aggregate views by coarsening the grid, never by re-scanning the transactions.
```

- [ ] **Step 3: Update `docs/roadmap.md`**

Add to the "Shipped" list, after the pricing entry:

```markdown
- **Monthly triangles and exposure** - `triangles.csv` carries incremental monthly development triangles by origin month (paid, paid net of recoveries, incurred, reported claim counts) and `exposure.csv` carries premium, exposure units and policy counts on the same axis, with an `--origin-basis` knob for accident or underwriting month. The monthly grid is the single aggregation store: the annual triangles the realism gate and the UI read are a coarsened view of it, and quarterly comes free from the same function.
```

In "Longer term", update the valuation-date extract entry to note that the monthly file already supports the cut:

```markdown
- **Valuation-date extract** - the mission deliberately generates every claim to closure for out-of-sample testing, but a chosen-date cut (open claims, outstanding case, no future knowledge) is trivial to derive and would let the tool feed a reserving demo with zero manual steps - the MVP's own success criterion. `triangles.csv` already supports the triangle side of this by filtering on `origin_month + dev_month - 1`; the remaining work is the claim and transaction extracts.
```

Remove the RF-1 mention from the "Known enablers and technical debt" list if one is present, since Task 6 closed it.

- [ ] **Step 4: Update `docs/todo.md`**

Move item `RF-1` ("`developmentYears` is defined twice") out of the open findings and into the file's "Resolved, for provenance" table, with the resolution:

```markdown
| RF-1 | `developmentYears` defined twice, with the same two triangles computed in both places | Resolved by the monthly triangles work: `application.Aggregates` builds one monthly grid per run and hands out the annual triangles, so the constant lives only in `internal/application/aggregate.go` and the triangles are computed once. `EvaluateRealism` now takes an `Aggregates`. |
```

Delete the RF-1 section from the open findings and renumber nothing else - the IDs are stable by convention.

- [ ] **Step 5: Update `docs/detailed-architecture.md`**

Line 11 and its list become five datasets, matching the README wording.

Rewrite section 10.1 to describe the new shape:

```markdown
### 10.1 `monthly.go` - the canonical aggregate

`MonthlyGrid{Basis, StartMonth, DevPeriods, Paid, PaidNet, Incurred [][]float64, Reported [][]int}` is the single aggregation store. Row `o` is origin month `StartMonth.Add(o)`; slice index `d` holds development period `d+1`, so index 0 is the origin month itself. Every row is `DevPeriods` wide.

Cells are **incremental**: a cell is the movement in that development month. Increments sum, so any coarser grain is a plain sum over cells and a cumulative view is a running sum along a row.

- `BuildMonthlyGrid(policies, claims, txs, startMonth, originMonths, basis)` - two passes over the input: one to size the rectangle to the widest development period any in-span claim reaches, one to place every movement. Weights match the annual triangles it replaced: paid counts `PAYMENT` only; net paid subtracts recoveries; incurred adds every case movement and payment and subtracts recoveries, so it is gross case plus net paid. Reported counts a claim in its **report** month. Development runs to full runoff, so the grid holds development after the run window ends.
- `OriginBasis` (`basis.go`) is the one configuration seam, consulted in exactly two places: a claim's origin month (occurrence month, or its policy's inception month) and a month's exposure (earned in the month, or written in it).
- `(g MonthlyGrid) Cell(measure, origin, dev)` reads a cell with a 1-based development period.

### 10.2 `coarsen.go` - every coarser grain

- `Coarsen(kind, devPeriods, foldTail)` maps both axes onto the calendar period the month falls in: `originPeriod = index(originMonth) - index(startMonth)` and `devPeriod = index(eventMonth) - index(originMonth) + 1`, for `Monthly`, `Quarterly` or `Annual`. Keying on the calendar period rather than dividing monthly development by twelve is what makes the annual result equal what the annual triangles have always measured: an accident in March 1998 paid in January 1999 is development year 2. Rows are zero-padded to `devPeriods` rather than left ragged, because `ATAFactors` counts an origin at an age only when its row reaches that far.
- `(s IncrementalSet) Cumulative(measure) Triangle` - the running-sum projection.
- `(g MonthlyGrid) AnnualTriangles(devYears) AnnualSet` - `Coarsen(Annual, devYears, true)` cumulated into the paid, net paid and incurred triangles the realism gate and the UI read.

### 10.3 `exposure.go` - exposure by month and year

- `ExposureByMonth(policies, startMonth, months, basis) []MonthExposure` - premium, exposure units in policy-years (`days / 365.25`) and policy count per origin month, earned day pro-rata on the accident basis and landed whole at inception on the underwriting basis.
- `EarnedPremiumByYear(policies, startYear, years) []float64` - the monthly premiums rolled up per calendar year, so the two views agree by construction.

### 10.4 `triangle.go` - the cumulative triangle and its factors
```

`Triangle{StartYear int, Cells [][]float64}` is a cumulative triangle indexed `[origin][dev]`; rows may be ragged. `ATAFactors` and `latestDiagonal` are unchanged. Keep the existing text for those two bullets and renumber the following sections (`10.2` compare, and so on) accordingly.

In section 10.2's `AgeCheck` description and wherever ages are mentioned, note that `Age` is the 1-based development period the factor develops from.

Update the `application.EvaluateRealism` line (around line 345) to:

```markdown
- `Aggregate(ds, startYear, years, basis) (Aggregates, error)` - one pure aggregation pass per run: the monthly grid and exposure on the requested basis, plus the accident-basis annual triangles and earned premium. `Annual` and `EarnedPremium` are always accident-basis, because Schedule P is an accident-year presentation.
- `EvaluateRealism(ag, refs) triangle.Report` - a thin adapter: builds a `Comparison` from the aggregate's net paid and incurred triangles and its earned premium, then returns `CompareToReference`. Used as a test gate (`TestDefaultPresetIsRealistic`) and by the UI.
```

Update section 12.3 to cover both writers and both header blocks:

```markdown
- `WriteDataset(dir, ds) error` - creates `dir` (0o755) and writes `policies.csv`, `claims.csv`, `transactions.csv`.
- `WriteAggregates(dir, ag) error` - writes `triangles.csv` (one row per grid cell, ordered by origin month then development month, zeros included) and `exposure.csv` (one row per origin month). Money is rendered at two decimal places and exposure units at six, with a guard so a value rounding to zero never prints as `-0.00`.
```

```
triangles.csv:    origin_month,dev_month,paid,paid_net,incurred,reported_count
exposure.csv:     origin_month,premium,exposure_units,policies
```

- [ ] **Step 6: Add the out-of-context banner to the spec**

The feature has shipped, so the spec becomes a historical record like every other file in `docs/superpowers/specs/`. Replace its two-line "Live spec for in-flight work" note at the top with the standard banner, matching the wording of the other files and dated today:

```markdown
> **OUT OF CONTEXT - do not read (2026-08-18):** historical design record, kept for provenance only. Agents must not load this file into context or treat it as a source of truth; it records decisions as of its own date and may not match current behaviour. For how the system works today see `README.md`, `AGENTS.md`, and `docs/detailed-architecture.md`.
```

- [ ] **Step 7: Verify the docs against the code**

Re-read each edited passage against the implementation. Specifically check: the CSV headers quoted in the docs match `monthly.go` exactly; the flag name and default match `main.go`; the five file names are right in all four documents; nothing claims a browser view for the monthly grid.

Run: `go test ./... && go vet ./...`
Expected: all PASS (documentation changes cannot break tests, but this is the last gate before the PR).

- [ ] **Step 8: Commit**

```bash
git add README.md AGENTS.md docs/roadmap.md docs/todo.md docs/detailed-architecture.md \
        docs/superpowers/specs/2026-08-18-monthly-triangles-design.md
git commit -m "Document the monthly triangles and exposure output"
```

---

## Finishing up

- [ ] **Run the full gate**

```bash
go test ./... && go vet ./...
```

Both must be clean. `TestDefaultPresetIsRealistic` is the one to watch: it runs three seeds of a 40,000-policy ten-year book against the Schedule P bands, and it must pass without any change to the preset or the bands. If it fails, the coarsening is not faithful to the old annual aggregation - debug that, do not recalibrate the preset.

- [ ] **Open the pull request**

Write the title and body as the commit message `main` will keep, since the merge squashes. Lead with what changed and why: monthly incremental triangles and monthly exposure as two new CSVs, an origin-basis knob, and the annual triangles re-expressed as a coarsened view of the same grid so they are provably unchanged. Say that `go test ./...` and `go vet ./...` both pass, and note that `RF-1` is closed.

```bash
git push -u origin feature/monthly-triangles
gh pr create --title "Add monthly triangles and monthly exposure" --body "$(cat <<'EOF'
`claimsgen generate` and the browser UI now write two more CSVs alongside the
three dataset files:

- `triangles.csv` - incremental monthly development triangles by origin month:
  paid, paid net of salvage and subrogation, incurred, and reported claim
  counts. Cells are incremental so they aggregate up by addition, development
  months are numbered from 1 and run to full runoff, and every cell is written
  so the grid's extent is explicit.
- `exposure.csv` - premium, exposure units in policy-years, and policy count on
  the same origin-month axis.

A new `--origin-basis accident|underwriting` knob (mirrored by a select in the
UI) keys claims on their occurrence month or on their policy's inception month,
with earned or written exposure to match. The annual triangles and the realism
check stay on the accident basis, because the Schedule P reference data is an
accident-year presentation.

The monthly grid is now the single aggregation store. The annual triangles the
realism gate and the UI read are a coarsened view of it - keyed on the calendar
period the event month falls in, so they are unchanged cell for cell, which an
oracle test against the previous implementation pins on a generated dataset.
Quarterly comes free from the same function. Development period numbering is
1-based throughout. `application.Aggregates` builds the grid once per run, which
closes RF-1 in `docs/todo.md`: the duplicated `developmentYears` constant and
the two triangles that were computed twice per run.

`go test ./...` and `go vet ./...` both pass, including
`TestDefaultPresetIsRealistic` across its three seeds with no change to the
preset or the reference bands.
EOF
)"
```

Squash merge it.
