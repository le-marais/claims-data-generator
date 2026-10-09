> **OUT OF CONTEXT - do not read (2026-10-09):** implementation plan, deleted once its work ships. Agents must not load this file into context or treat it as a source of truth. For how the system works today see `README.md`, `AGENTS.md`, and `docs/architecture.md`.

# Seasonal holiday deferral implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Defer a share of reports and payments dated in a summer holiday window to the same day of the next month.

**Architecture:** A `lob.SeasonalHolidayParams` value object decides the window and the deferral; the claim, reopen and runoff simulators apply it where each date is drawn, through a `WithSeasonalHoliday` option, drawing from a `seasonal-holiday` sub-stream split off each entity's stream.

**Tech Stack:** Go 1.26, standard library, gonum via the existing `random.Source`.

## Global Constraints

- Same seed plus same config gives byte-identical output; with the block off, output is byte-identical to `main`.
- Zero value is off: `hemisphere` `""` or `none`; shares not validated when off.
- Windows inclusive: northern 15 July - 14 August, southern 15 December - 14 January.
- Deferral moves a date to the same day next month, clamped to month end.
- Nil closes never move; costs never change.
- Docs style: sentence case, no em dashes, spaced hyphens.
- Run `go test ./...` and `go vet ./...` before the PR.

---

### Task 1: `shared.Date.AddMonths`

**Files:** Modify `internal/domain/shared/date.go`; test `internal/domain/shared/date_test.go`.

**Produces:** `func (d Date) AddMonths(n int) Date` - same day n months on, clamped to the target month's last day.

- [ ] Write table test: 2001-07-15 +1 = 2001-08-15; 2001-01-31 +1 = 2001-02-28; 2000-01-31 +1 = 2000-02-29; 2001-12-20 +1 = 2002-01-20; 2001-08-31 +1 = 2001-09-30.
- [ ] Run, expect compile failure.
- [ ] Implement:

```go
func (d Date) AddMonths(n int) Date {
	target := d.Month().Add(n)
	day := min(d.t.Day(), target.End().t.Day())
	return NewDate(target.Year(), target.Month(), day)
}
```

- [ ] Run, expect pass. Commit.

### Task 2: `lob.SeasonalHolidayParams`

**Files:** Create `internal/domain/lob/seasonalholiday.go`, test `internal/domain/lob/seasonalholiday_test.go`; modify `internal/domain/lob/lob.go` (field on `LineOfBusiness`, call validate).

**Produces:**

```go
type Hemisphere string
const (
	NoHoliday Hemisphere = "none"
	Northern  Hemisphere = "northern"
	Southern  Hemisphere = "southern"
)
type SeasonalHolidayParams struct {
	Hemisphere   Hemisphere
	ReportShare  float64
	PaymentShare float64
}
func (s SeasonalHolidayParams) Enabled() bool
func (s SeasonalHolidayParams) InWindow(d shared.Date) bool
func (s SeasonalHolidayParams) Defer(d shared.Date, u, share float64) shared.Date // d.AddMonths(1) when InWindow(d) && u < share, else d
func (s SeasonalHolidayParams) validate() error
```

`LineOfBusiness.SeasonalHoliday SeasonalHolidayParams`, validated last in `Validate`.

- [ ] Tests: window boundaries for both hemispheres (14 Jul out, 15 Jul in, 14 Aug in, 15 Aug out; 14 Dec out, 15 Dec in, 31 Dec in, 1 Jan in, 14 Jan in, 15 Jan out); off never in window; `Defer` moves at u < share only; validation rejects unknown hemisphere and shares outside [0, 1] or NaN, naming `seasonal_holiday.<field>`; off block with share 5 validates.
- [ ] Run, fail; implement; pass; commit.

### Task 3: claim and reopen stages

**Files:** Modify `internal/domain/claim/claim.go`, `internal/domain/claim/reopen.go`; test `internal/domain/claim/seasonalholiday_test.go`.

**Consumes:** `lob.SeasonalHolidayParams.Defer`, `Enabled`.

**Produces:** `(*ClaimSimulator).WithSeasonalHoliday(lob.SeasonalHolidayParams) *ClaimSimulator`, `(*ReopenSimulator).WithSeasonalHoliday(lob.SeasonalHolidayParams) *ReopenSimulator`.

- `Simulate`: when enabled, `holiday := sectionStream.Split("seasonal-holiday")` per section; `simulateClaim` takes it (nil when off) and, when non-nil, draws `uReport, uClose` first thing, so every claim takes two draws, reportable or not. Report: `report = s.holiday.Defer(report, uReport, ReportShare)`. Close: when not nil, `closeDate = Defer(closeDate, uClose, PaymentShare)`.
- `ReopenSimulator.Apply`: when enabled, `u := stream.Split("seasonal-holiday").Uniform()`; second close `= Defer(close, u, PaymentShare)`.
- [ ] Tests: report share 1 - every report differs from the share-0 run only when the share-0 report is in the window, and then it is `AddMonths(1)` of it; payment share 1 - paying window closes move, nil closes do not; off block gives `reflect.DeepEqual` claims to no block; reopen share 1 moves window second closes.
- [ ] Run, fail; implement; pass; commit.

### Task 4: runoff stage

**Files:** Modify `internal/domain/transaction/runoff.go`; test `internal/domain/transaction/seasonalholiday_test.go`.

**Produces:** `(*RunoffSimulator).WithSeasonalHoliday(lob.SeasonalHolidayParams) *RunoffSimulator`.

- `simulateClaim`: when enabled, `holiday := src.Split("seasonal-holiday")`, passed to `runEpisode` and `drawInterimPayments` with the episode's open date.
- `drawInterimPayments`: after drawing offsets and before sorting, each offset whose date `open.AddDays(off)` is deferred becomes `DaysBetween(open, deferred)`; one draw per offset. In the payout loop, an offset past `duration - edge` is held over (paid with the final settlement).
- [ ] Tests: share 1 with a claim whose interim payments fall in July: every interim payment date is outside the window or was moved there by one month; a claim closing 20 August with a July interim payment folds it into the final settlement; total paid equals ultimate; off equals no block.
- [ ] Run, fail; implement; pass; commit.

### Task 5: wiring, config, form, presets

**Files:** `internal/application/generate.go`; `internal/infrastructure/config/config.go`; `internal/infrastructure/web/fields.go`; both preset YAMLs; `internal/application/golden_test.go`.

- `GenerateDataset` passes `req.LOB.SeasonalHoliday` to claim, reopen and runoff simulators.
- Config: `SeasonalHoliday SeasonalHolidayParams \`yaml:"seasonal_holiday" json:"seasonal_holiday"\``, mirrored struct with `Hemisphere string`, `ReportShare`, `PaymentShare`; `ToDomain` maps it.
- Form: "Seasonal holiday" group with `report_share` and `payment_share`.
- Presets: `seasonal_holiday: {hemisphere: northern, report_share: 0.10, payment_share: 0.15}` with a comment.
- [ ] Before editing presets, run the golden tests with the wiring only: they must pass unchanged (off equals main).
- [ ] Turn on in presets, run goldens, paste the new hashes, run the realism gate and drift test.
- [ ] Commit.

### Task 6: docs

- README: parameter, model diagram, simplifications. `docs/architecture.md`: lob tree, claim, reopen and runoff stages, stream labels. Delete this plan.
- [ ] `go test ./...`, `go vet ./...`, `gofmt -l .`, `golangci-lint run ./...`. Commit, push, open PR.
