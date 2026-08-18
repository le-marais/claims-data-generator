> **Live spec for in-flight work.** Add the standard "OUT OF CONTEXT - do not read"
> banner that every other file in this directory carries once the feature ships.

# Monthly triangles and monthly exposure

Date: 2026-08-18

## Problem

`claimsgen` aggregates a generated dataset into development triangles today,
but only annually, only for internal use, and only for two measures:

- `triangle.PaidTriangle`, `NetPaidTriangle` and `IncurredTriangle` build
  cumulative triangles on an accident-*year* origin with ten development years.
- They exist to feed the realism gate and the UI's triangle tab. Nothing is
  written to CSV, so no downstream tool can consume a triangle.
- There is no claim-count triangle, so frequency work is impossible.
- Exposure exists only as `EarnedPremiumByYear`: one number per calendar year,
  no count denominator.
- Each of the three constructors re-scans the transactions, and `realism.go` and
  `viewmodel.go` each build the same triangles independently off a duplicated
  `developmentYears` constant (`RF-1` in `docs/todo.md`).

Reserving demos and tests want a finer grain: monthly origin periods, a reported
claim-count triangle alongside paid and incurred, and exposure on the same
monthly axis so loss ratios, burning cost and frequency can all be computed from
the output.

## What we build

1. A monthly incremental triangle grid as the single canonical aggregation
   store, covering paid (gross), paid net of recoveries, incurred, and reported
   claim counts.
2. Monthly exposure on the same origin axis: premium, exposure units and policy
   counts.
3. A configurable origin basis - accident month or underwriting month - with
   both implemented.
4. Two new CSV outputs: `triangles.csv` and `exposure.csv`.
5. The existing annual triangles re-expressed as a coarsened view of the monthly
   grid, so every grain is built by one code path. Quarterly falls out of the
   same function.
6. Development period numbering made 1-based everywhere.

Non-goals: no new browser view (the UI keeps rendering only the annual
triangles), no change to the generation model, no change to the realism gate's
bands or the shipped preset's calibration.

## Design

### 1. `shared.Month`

A comparable calendar-month value type, alongside `shared.Date`:

```go
// Month is a calendar month.
type Month struct {
    year  int
    month time.Month
}

func NewMonth(year int, m time.Month) Month
func (d Date) Month() Month
func (m Month) Year() int
func (m Month) Quarter() int   // 1..4
func (m Month) Add(n int) Month
func (m Month) Start() Date
func (m Month) End() Date
func (m Month) String() string // "1998-01"

// MonthsBetween returns the number of whole months from a to b, negative if
// b is earlier.
func MonthsBetween(a, b Month) int
```

Internally a month is the absolute index `year*12 + int(month) - 1`, so
`MonthsBetween` and `Add` are integer arithmetic with no date construction.

`triangle.overlapDays(start, end Date, year int)` becomes
`overlapDays(start, end, rangeStart, rangeEnd Date)`. The yearly premium
function passes year bounds and the monthly exposure passes month bounds, so
there is one pro-rata rule rather than two copies of it.

### 2. Origin basis

```go
// OriginBasis selects which period a claim's origin is keyed on.
type OriginBasis string

const (
    // AccidentMonth keys a claim on the month it occurred, and exposure on
    // the exposure earned in each month.
    AccidentMonth OriginBasis = "accident"
    // UnderwritingMonth keys a claim on the inception month of its policy,
    // and exposure on the exposure written in each month.
    UnderwritingMonth OriginBasis = "underwriting"
)

func (b OriginBasis) Validate() error
```

The basis is consulted in exactly two places:

- **A claim's origin month.** Accident: `c.OccurrenceDate.Month()`.
  Underwriting: the cover-start month of the claim's policy, from a
  policy-id-to-month map built once per grid. A claim whose policy is absent is
  an error, not a silent drop.
- **A month's exposure.** Accident: premium and exposure earned in that
  calendar month, day pro-rata. Underwriting: premium and exposure written in
  it, so a policy's whole premium and whole term land in its inception month.

Everything downstream - development indexing, coarsening, CSV shape - is
basis-agnostic.

### 3. `triangle.MonthlyGrid` - the canonical store

New file `internal/domain/triangle/monthly.go`.

```go
// MonthlyGrid holds incremental monthly triangles: row o is origin month
// StartMonth.Add(o) and slice index d holds development period d+1, so index
// 0 is development period 1 - the origin month itself. Every row has
// DevPeriods columns.
//
// Cells are incremental, not cumulative: a cell is the movement in that
// development month. Increments sum, so any coarser origin or development
// grain is a plain sum over cells (see Coarsen), and a cumulative view is a
// running sum along a row.
type MonthlyGrid struct {
    Basis      OriginBasis
    StartMonth shared.Month
    DevPeriods int
    Paid       [][]float64 // gross of recoveries
    PaidNet    [][]float64 // net of salvage and subrogation
    Incurred   [][]float64 // gross case plus net paid
    Reported   [][]int     // claim counts, by report month
}

// Cell reads one cell with a 1-based development period. Reported counts
// convert to float64.
func (g MonthlyGrid) Cell(m Measure, origin, dev int) float64

func BuildMonthlyGrid(
    policies []policy.Policy,
    claims []claim.Claim,
    txs []transaction.Transaction,
    startMonth shared.Month,
    originMonths int,
    basis OriginBasis,
) (MonthlyGrid, error)
```

`Measure` is an enum: `Paid`, `PaidNet`, `Incurred`, `Reported`.

Event placement, one pass over the transactions plus one over the claims:

| Measure | Event month | Weight |
|---|---|---|
| `Paid` | transaction month | `+1` for `PAYMENT` |
| `PaidNet` | transaction month | `+1` for `PAYMENT`, `-1` for recoveries |
| `Incurred` | transaction month | `+1` for every type, `-1` for recoveries |
| `Reported` | the claim's **report** month | `+1` per claim |

These match today's `PaidTriangle`, `NetPaidTriangle` and `IncurredTriangle`
weights exactly, so the derived annual triangles are unchanged.

Extent. `originMonths` is `years * 12`, counted from January of the run's start
year, matching the origin axis of the annual triangles and of the monthly
exposure. Development runs to **full runoff**:

```
DevPeriods = MonthsBetween(StartMonth, lastEventMonth) + 1
```

where `lastEventMonth` is the latest transaction or report month among events
whose origin falls inside the grid, so an event skipped for an out-of-range
origin cannot stretch the rectangle.
The grid is a rectangle: every origin gets `DevPeriods` columns. Origin 1 fills
that width; later origins carry trailing zeros for development months that lie
past the data. `DevPeriods` is at least 1 even for an empty dataset.

**Why full runoff and not a cut at the window's last month.** Claims run to
closure and only *occurrences* are windowed, so today's annual triangle
deliberately includes development after the window ends: that is what makes its
latest diagonal a true ultimate, which the realism gate's late-age factors and
ultimate loss ratio depend on, and which the mission's out-of-sample story rests
on. A grid cut at the window's end could not reproduce it, and deriving the
annual triangles from a truncated grid would force a recalibration of the
shipped preset against the Schedule P bands. A full-runoff grid is a superset:
a valuation-date view is one filter away, `origin_month + dev_month - 1 <=`
the window's last month.

Guards, all covered by tests: an origin index outside `[0, originMonths)` is
skipped (as today); a development period below 1 is clamped to 1. The clamp
cannot fire in generated data - a policy's cover start precedes its claims'
occurrences, which precede their report dates, which precede their transactions
- but the grid is a public domain function and should not index out of range on
hand-built input.

### 4. Coarsening: annual and quarterly from one path

```go
// PeriodKind is the grain of a triangle's origin and development axes.
type PeriodKind int

const (
    Monthly PeriodKind = iota
    Quarterly
    Annual
)

// Coarsen aggregates the incremental monthly grid onto a coarser grain. When
// devPeriods is positive, every origin row is exactly devPeriods wide and
// development beyond it is folded into the last period if foldTail is set and
// dropped otherwise. When devPeriods is zero, rows take the grid's natural
// extent.
func (g MonthlyGrid) Coarsen(kind PeriodKind, devPeriods int, foldTail bool) IncrementalSet
```

Coarsening maps **both** axes onto the coarser calendar period. For cell
`(o, d)`, the origin month is `StartMonth.Add(o)` and the event month is
`originMonth.Add(d-1)`. With `index(kind, month)` returning the absolute period
number (month index, `year*4 + quarter - 1`, or `year`):

```
originPeriod = index(kind, originMonth) - index(kind, StartMonth)
devPeriod    = index(kind, eventMonth)  - index(kind, originMonth) + 1
```

**Rows must be zero-padded to `devPeriods`, not left ragged.** Today's annual
triangle allocates a full `origins x devs` rectangle and places every
transaction in it, so each origin has all ten cumulative columns, repeating its
last value once development stops. `ATAFactors` counts an origin at an age only
when its row reaches that far, and `latestDiagonal` reads each row's last
column. A ragged derived triangle would therefore drop late origins out of the
late-age factors and change them: a short-tail motor origin whose payments stop
at development year 6 contributes a 1.0 factor at ages 7 to 10 today, and would
contribute nothing if its row ended at 6. Padding keeps the shape, and with it
the factors, identical.

Keying development on the *calendar* period of the event month, rather than on
`floor((dev-1)/12)`, is what makes this equal today's annual triangle. An
accident in March 1998 paid in January 1999 is development year 2 on the
calendar rule (`1999 - 1998 + 1`) and development year 1 on the division rule.
Today's annual triangle uses `txYear - occYear`, so the calendar rule is the
faithful one. Quarterly follows the same rule for free.

```go
// IncrementalSet is a set of incremental triangles sharing one grain.
type IncrementalSet struct {
    Basis      OriginBasis
    Kind       PeriodKind
    StartMonth shared.Month
    Paid, PaidNet, Incurred [][]float64
    Reported                [][]int
}

// Cumulative returns the running-sum view of one measure as a Triangle.
func (s IncrementalSet) Cumulative(m Measure) Triangle
```

`Triangle` keeps its current shape (`StartYear`, cumulative `Cells`) and its
`ATAFactors` and `latestDiagonal` methods: age-to-age factors need cumulative
input, and the UI's display is cumulative. Incremental is the store; cumulative
is a projection of it. `Cumulative` is used for the annual grain, where
`StartYear` is meaningful.

The three annual constructors are replaced by one accessor:

```go
// AnnualTriangles builds the cumulative annual triangles the realism gate and
// the UI use: accident- or underwriting-year origin, devYears development
// years with later development folded into the last column.
func (g MonthlyGrid) AnnualTriangles(devYears int) AnnualSet

type AnnualSet struct {
    Paid, NetPaid, Incurred Triangle
}
```

`AnnualSet` carries the three measures its consumers need today; reported counts
are one `Cumulative(Reported)` call away when something needs them.

### 5. One-based development periods

The UI and `Report.String()` already *display* 1-based periods computed from
0-based data (`Dev ${d + 1}` in `app.js`, `age %d-%d` with `c.Age+1` in
`compare.go`). The convention moves into the data:

- `triangle.AgeCheck.Age` becomes the 1-based development period the factor
  develops **from**, so age 1 is the 1-to-2 factor. `checkAges` sets
  `Age: age + 1`; `Report.String()` prints `c.Age, c.Age+1`; `app.js` drops its
  `+1`s in the band labels and the triangle headers.
- `dev_month` in `triangles.csv` and every development number in the JSON API
  are 1-based at birth.
- Raw `[][]float64` fields stay 0-indexed Go slices, with the doc comment on
  `MonthlyGrid` fixing the convention: index `d` holds period `d+1`. Domain code
  that reads individual cells uses the 1-based `Cell` accessor.

Reference-set slice indices are never surfaced to a user and are compared
index-to-index on both sides, so the bands are unaffected by the shift.

### 6. Monthly exposure

`EarnedPremiumByYear` and the new monthly function move to
`internal/domain/triangle/exposure.go`, leaving `triangle.go` holding only
triangles.

```go
// MonthExposure is one origin month's exposure. On the accident basis the
// figures are earned in the month; on the underwriting basis they are written
// in it.
type MonthExposure struct {
    Month         shared.Month
    Premium       float64
    ExposureUnits float64 // policy-years
    Policies      int
}

func ExposureByMonth(policies []policy.Policy, startMonth shared.Month, months int, basis OriginBasis) []MonthExposure
```

- Accident basis, per policy and month: `Premium += perDay * overlapDays`,
  `ExposureUnits += overlapDays / 365.25`, `Policies++` when the policy has at
  least one day of cover in the month.
- Underwriting basis, per policy, into its cover-start month only:
  `Premium += p.Premium`, `ExposureUnits += termDays / 365.25`, `Policies++`.

Exposure is only counted inside the run window, so the final months are thin -
the same truncation `EarnedPremiumByYear` already has.

`EarnedPremiumByYear(policies, startYear, years)` is reimplemented as the sum of
`ExposureByMonth(policies, January of startYear, years*12, AccidentMonth)`
premiums per calendar year. The two views are then consistent by construction
rather than by a test. Day pro-rata is additive, so the value is unchanged up to
floating-point summation order; earned premium reaches only the summary table
and the realism loss ratio, never a CSV, so the golden hash cannot move and a
relative change of order 1e-13 cannot shift a band.

### 7. `application.Aggregates` - one aggregation per run

New file `internal/application/aggregate.go`.

```go
// Aggregates is everything derived from a dataset by pure aggregation: the
// monthly grid and exposure on the requested origin basis, plus the annual
// triangles the realism gate and the UI read.
type Aggregates struct {
    Basis    triangle.OriginBasis
    Grid     triangle.MonthlyGrid
    Exposure []triangle.MonthExposure
    Annual   triangle.AnnualSet
    // EarnedPremium is per calendar year, for the realism comparison.
    EarnedPremium []float64
}

func Aggregate(ds Dataset, startYear, years int, basis triangle.OriginBasis) (Aggregates, error)
```

`Annual` and `EarnedPremium` are always built on the **accident** basis,
whatever `Basis` is. Schedule P is an accident-year presentation, so the realism
gate and the UI's triangle tab must not change grain when the CSV basis knob
moves; the knob governs the new monthly output only. On the default accident
basis one grid serves both. On the underwriting basis a second accident-basis
grid is built for the annual view - a cost paid only on the non-default path.

`EvaluateRealism` loses its dataset argument and takes the aggregate:

```go
func EvaluateRealism(ag Aggregates, refs []triangle.ReferenceSet) triangle.Report
```

The duplicated `developmentYears` constant collapses into one, in the
application layer, and the two triangles are computed once per run instead of
twice. This closes `RF-1` in `docs/todo.md`, which moves to that file's
"Resolved, for provenance" list.

### 8. CSV output

New file `internal/infrastructure/csv/monthly.go`, reusing the existing
`writeFile` helper and its byte-stability discipline.

`triangles.csv`, one row per grid cell, ordered by origin month then
development month:

```
origin_month,dev_month,paid,paid_net,incurred,reported_count
1998-01,1,0.00,0.00,0.00,0
1998-01,2,1250.00,1250.00,4300.00,3
```

`exposure.csv`, one row per origin month, in order:

```
origin_month,premium,exposure_units,policies
1998-01,56671.94,80.962354,1908
```

Formatting:

- Money at fixed 2 decimal places. Transaction amounts are integer cents, so a
  sum of them is exact at 2dp and the rendering is lossless.
- `exposure_units` at fixed 6 decimal places, matching `FormatRiskFactor`'s
  precision discipline.
- Negative zero is normalised to zero before formatting, so a cell whose
  movements cancel renders `0.00` rather than `-0.00`.
- Counts as plain integers.

Every cell is emitted, zeros included, so the file states the grid's exact
extent and every consumer reads the same rectangle. Cost: a default ten-year run
gives 120 origin months by roughly 215 development months, so about 26,000 rows
and a little over a megabyte. Row count scales with the square of the run
length - at the UI's `maxYears` of 100 the file would reach order 1.5 million
rows and tens of megabytes. That is a
consequence of the run size the user asked for, the same way `transactions.csv`
is; `checkRunSize` is not extended here, but the generate output line reports
the row count so the size is never a surprise.

```go
func WriteAggregates(dir string, ag application.Aggregates) error
```

### 9. Wiring

**CLI.** `generate` gains `--origin-basis accident|underwriting` (default
`accident`), validated through `OriginBasis.Validate` with a clear error and a
non-zero exit. After `WriteDataset`, `main` builds the aggregate and calls
`WriteAggregates`. The success line becomes:

```
motor personal: wrote 213481 policies, 41792 claims, 315044 transactions,
25800 triangle rows, 120 exposure rows to output (seed 1)
```

**UI.** `generateRequest` gains `origin_basis` (the decoder rejects unknown
fields, so the field has to exist before the form can send it); an empty value
means accident, so an older client keeps working. `handleGenerate` writes all
five files. `buildResponse` takes the aggregate rather than rebuilding
triangles, and `viewmodel.go`'s `developmentYears` constant goes away. The
config form gains one `origin_basis` select. No new tab or view: the browser
still renders the annual triangles only.

## Invariants and behaviour preservation

- **Reproducibility is untouched.** Everything added is pure aggregation over an
  existing dataset. No new draws, no new randomness, no change to the generation
  path.
- **The annual triangles are unchanged, cell for cell.** Guarded by an oracle
  test (below), so the realism bands, `TestDefaultPresetIsRealistic` and the
  shipped preset's calibration all stay as they are.
- **The existing golden hash is unchanged.** `policies.csv`, `claims.csv` and
  `transactions.csv` are not touched, so `wantHash` keeps proving that
  generation did not move. The new files get their own pin.

## Testing

Domain:

- `shared/month_test.go` - `MonthsBetween` and `Add` across year boundaries and
  backwards, `Quarter`, `Start`/`End`, `String` zero padding.
- `triangle/monthly_test.go` - hand-built claims and transactions:
  development-period-1 placement, incremental cells not cumulative, rectangular
  row widths, `DevPeriods` from the last event month, reported counts keyed on
  report month rather than occurrence month, gross versus net paid, incurred
  including outstanding case, underwriting-basis origin mapping, an error for a
  claim with no matching policy, the sub-1 development clamp, and an empty
  dataset.
- `triangle/coarsen_test.go` - the March 1998 / January 1999 case pinning the
  calendar-period rule, annual and quarterly grains, `foldTail` on and off, and
  a `Cumulative` round trip.
- `triangle/exposure_test.go` - a twelve-month policy straddling a year end
  splits by days across months; monthly premium summed over a year equals
  `EarnedPremiumByYear`; exposure units and policy counts on both bases.
- `triangle/compare_test.go` - `AgeCheck.Age` is 1-based.
- `triangle/triangle_test.go` - the five existing tests that call
  `PaidTriangle`, `NetPaidTriangle` and `IncurredTriangle` directly are rewritten
  to build a grid and call `AnnualTriangles`, keeping their assertions as they
  are.

Application:

- `application/aggregate_test.go` **oracle test**: today's `aggregate` weights
  are copied into the test file as `legacyPaidTriangle`, `legacyNetPaidTriangle`
  and `legacyIncurredTriangle`, and the derived `AnnualSet` is asserted equal
  cell for cell on a generated dataset. This is the test that proves the
  refactor is behaviour-preserving; it stays as a regression guard.
- Total `reported_count` across the grid equals `len(ds.Claims)` - full runoff
  means no claim is lost off the edge of the grid.
- Grid paid totals reconcile to `SummaryReport.Paid`, and grid `PaidNet` totals
  to `Paid - Recovered`.
- Aggregating then cumulating equals cumulating then aggregating on the latest
  diagonal.
- Underwriting-basis aggregate leaves `Annual` and `EarnedPremium` identical to
  the accident-basis one.

Infrastructure:

- `csv/monthly_test.go` - header, ordering, 1-based `dev_month`, fixed
  precision, no `-0.00`, row counts equal to `originMonths * DevPeriods` and
  `originMonths`.
- `golden_test.go` - a second constant, `wantAggregateHash`, over
  `triangles.csv` and `exposure.csv` for the same small deterministic dataset.
  `wantHash` is left alone.
- `cmd/claimsgen/main_test.go` - `--origin-basis` accepted for both values,
  rejected with a clear message otherwise, and the five files written.
- `web/server_test.go` - `origin_basis` absent defaults to accident; an invalid
  value is a 400; the response still carries annual triangles.

Then `go test ./...` and `go vet ./...` before the PR, as `AGENTS.md` requires.

## Docs to update

- `README.md` - the output is five CSVs, not three; the new `--origin-basis`
  flag; a short description of the two new files and their columns.
- `AGENTS.md` - "three linked CSVs" becomes five, in both places.
- `docs/roadmap.md` - a shipped entry for monthly triangles and exposure.
- `docs/todo.md` - `RF-1` moves to "Resolved, for provenance".
- `docs/detailed-architecture.md` - the triangle package's new shape: monthly
  incremental grid as the store, annual and quarterly as coarsened views.

## Decisions and rejected alternatives

| Decision | Rejected alternative and why |
|---|---|
| Incremental cells as the canonical store | Cumulative. Coarsening cumulative cells is not a sum, so quarterly and annual grains would each need their own logic. Increments make every grain one sum, and a cumulative view is a running sum away. |
| Reported = claim counts by report month | Reported amounts, which would duplicate `IncurredTriangle` under a second name. Counts give the frequency denominator that exposure pairs with. |
| `paid`, `paid_net` and `incurred` columns | Net-only or gross-only. The dataset has recoveries and the UI already offers a gross/net toggle; one extra column keeps both bases recoverable from the file. |
| Full-runoff extent | A ragged cut at the window's last month. It cannot reproduce the fully-developed annual triangle the realism gate needs, and a valuation-date view is one filter over the full-runoff file. |
| Every cell emitted, zeros included | Skipping all-zero rows. Sparse output is roughly a third smaller but leaves the grid's extent implicit; a stated rectangle is worth the zeros. |
| Development keyed on the calendar period of the event month | `floor((dev-1)/12)`. It disagrees with today's `txYear - occYear` whenever the origin month is not January, so it would silently redefine the annual triangle. |
| Annual view stays on the accident basis | Following the origin-basis knob. Schedule P is accident-year, so the realism gate would need recalibrating and the UI's grain would move for a knob that is meant to govern CSV output. |
| Both origin bases implemented now | Accident only, with underwriting rejected. The underwriting basis is one origin lookup and one exposure rule, so shipping half a knob is not worth the saving. |

## Follow-ups, deliberately out of scope

- A quarterly CSV output. `Coarsen(Quarterly, ...)` exists and is tested, but
  nothing writes it until someone asks.
- Surfacing the monthly grid in the browser. A 120-by-215 grid needs its own
  rendering treatment.
- Letting the UI's annual triangle tab follow the origin-basis knob, which needs
  per-basis reference data before it means anything.
- Extending `checkRunSize` to bound the triangle file's size for long runs.
