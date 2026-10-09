> **OUT OF CONTEXT - do not read (2026-10-09):** historical design record, kept for provenance only. Agents must not load this file into context or treat it as a source of truth; it records decisions as of its own date and may not match current behaviour. For how the system works today see `README.md`, `AGENTS.md`, and `docs/architecture.md`.

# Business-day processing calendar - design

Date: 2026-10-09

## Goal

Claims are processed on business days. Every transaction and processing event - case estimates, payments, closes, reopens and recoveries - falls on a business day of the line's market, never on a weekend or public holiday. Report dates stay on any day by default, as they do for personal lines, where policyholders report at weekends; an option rolls them to business days too, as is more usual for some commercial lines. Occurrence dates never move.

## Parameters

A new top-level block on `LineOfBusiness`, `BusinessDays BusinessDayParams`:

```yaml
business_days:
  calendar: us        # none (default) | weekends | us | uk | za
  roll_reports: false # true: report dates roll to business days too
```

- `Calendar` is a string enum. `""` and `none` are off; any other unknown value fails validation, naming `business_days.calendar`.
- `RollReports` is a bool. It is read only when a calendar is on.
- The zero value is off, so existing YAMLs keep working. Both fields are YAML-only, like `seasonal_holiday.hemisphere`: the form registry covers numeric parameters.

## Calendars

A new domain package, `internal/domain/calendar`, depending on `shared` only.

```go
type Calendar struct{ /* name, holiday rule, per-year cache */ }
func Lookup(name string) (Calendar, error) // "", "none", "weekends", "us", "uk", "za"
func (c Calendar) Enabled() bool
func (c Calendar) IsBusinessDay(d shared.Date) bool
func (c Calendar) Following(d shared.Date) shared.Date // d, or the next business day after it
func (c Calendar) Preceding(d shared.Date) shared.Date // d, or the last business day before it
```

`Following` and `Preceding` are the identity when the calendar is off. Each year's holiday set is computed once from rules and cached; the cache is filled under a mutex or built eagerly per lookup, so a `Calendar` is safe to share across stages. Every calendar treats Saturday and Sunday as non-business days.

- **weekends**: no public holidays.
- **us** (federal holidays): New Year's Day (1 Jan), Martin Luther King Jr. Day (third Monday of January, from 1986), Washington's Birthday (third Monday of February), Memorial Day (last Monday of May), Juneteenth (19 June, from 2021), Independence Day (4 July), Labor Day (first Monday of September), Columbus Day (second Monday of October), Veterans Day (11 November), Thanksgiving (fourth Thursday of November), Christmas (25 December). A fixed-date holiday on a Saturday is observed the Friday before (New Year's Day on a Saturday is observed on 31 December of the previous year), on a Sunday the Monday after.
- **uk** (England and Wales bank holidays): New Year's Day, Good Friday, Easter Monday, early May (first Monday of May), spring (last Monday of May), summer (last Monday of August), Christmas Day and Boxing Day. New Year's Day on a weekend is substituted by the following Monday. Christmas on a Saturday gives Monday 27 and Tuesday 28 December; on a Sunday, Boxing Day Monday 26 and a substitute Tuesday 27; Boxing Day on a Saturday gives Monday 28.
- **za** (South African public holidays, from 1995): New Year's Day (1 Jan), Human Rights Day (21 Mar), Good Friday, Family Day (Easter Monday), Freedom Day (27 Apr), Workers' Day (1 May), Youth Day (16 Jun), National Women's Day (9 Aug), Heritage Day (24 Sep), Day of Reconciliation (16 Dec), Christmas Day (25 Dec), Day of Goodwill (26 Dec). A holiday on a Sunday makes the following Monday a holiday; no substitute for Saturday.

Easter Sunday is computed with the anonymous Gregorian algorithm. One-off holidays (royal events, jubilees, election days) and bank holidays moved by proclamation in particular years are not modelled; the README says so.

## Where dates roll

Dates roll where they are drawn, so the ledger stays consistent by construction. Rolling takes no random draws. Where several rules apply, the order is: draw, then seasonal holiday deferral, then the roll.

1. **Claim stage** (`claim.ClaimSimulator`). With `RollReports`, the report rolls `Following`; the close lag and payment delay then run from the rolled report. Every close, nil or paying, rolls `Following` after any holiday deferral.
2. **Reopen stage** (`claim.ReopenSimulator`). The reopen date rolls `Following` (after its holiday deferral), the second close runs from it, and the second close rolls `Following` (after its deferral). The reopen stays strictly after the first close because rolling only moves dates later.
3. **Runoff** (`transaction.RunoffSimulator`), offsets relative to the episode's open date, which is already a business day:
   - Interim payment offsets roll `Following` after any holiday deferral and before sorting, so the existing hold-over rules (minimum payment, spacing of at least the payment delay, the last interim day `duration - edge`, and the minimum final settlement) apply to the rolled days.
   - Revision offsets roll `Following`. A revision rolled onto or past the close day is dropped, as revisions are strictly inside the episode.
   - A payment's bill is at `Preceding(payment - delay)`, clamped at the open. Rolling it back keeps it at least the delay before its payment, and since every payment is a business day no later than the unrolled bill day, the bill never falls before the previous payment. The rule that revisions after a bill's day cannot raise the case reads the rolled bill day.
4. **Recoveries** (`transaction.RecoverySimulator`). Each recovery date, the final close plus its lag, rolls `Following`; it stays strictly after the close.

Each simulator takes the calendar through a `WithCalendar(calendar.Calendar)` option (the claim simulator also takes `RollReports`, through `WithBusinessDays(calendar.Calendar, rollReports bool)` - one option per simulator, matching `WithSeasonalHoliday`). `GenerateDataset` builds the calendar once from `req.LOB.BusinessDays` and passes it to the claim, reopen, runoff and recovery stages.

## Payment delay

`payment_delay_days` stays in calendar days. Raises (the opening case at a rolled report or reopen, revisions, bills) and payments are placed so every payment is still at least the delay after the last raise: payments and closes only move later, bills only move earlier, and the spacing rule runs on rolled days.

## Determinism

Rolling takes no draws, so with the calendar off the output is byte-identical to today, and with it on only dates change. No new random stream is needed.

## Presets

Both presets use `calendar: us`, as US Schedule P lines. Motor personal keeps `roll_reports: false` - policyholders report at weekends. Motor commercial sets `roll_reports: true` - fleet operators report through the office or a broker. The YAML comments say so. Events from the last days of December can roll into January, so the annual triangles move slightly: refresh the four golden hashes after confirming that with the calendar off they are unchanged, and re-run the realism gate and drift test.

## Docs

- README: a "Business days" section beside "Summer holiday", the parameters in the model diagrams that name report, close, reopen, runoff and recovery dates, the top-level blocks list, and the simplifications (rule-based holidays only).
- `docs/architecture.md`: the `calendar` package, the stage descriptions, and the options.
- `docs/roadmap.md`: delete the item.

## Testing

- `calendar`: holiday tables for known years - US 2021 (Juneteenth observed Friday 18 June, New Year's Day 2022 observed Friday 31 December 2021, Independence Day observed Monday 5 July), US 1998 (no Juneteenth); UK 2021 (Christmas Saturday: 27 and 28 December), UK 2022 (Christmas Sunday: 26 and 27 December), Easter dates for several years; ZA 2021 (Freedom Day Tuesday 27 April, Christmas Saturday no substitute), ZA 2016 (Day of Reconciliation on a Friday, Christmas on a Sunday with 26 December already a holiday); `Following` and `Preceding` across a long weekend; `Lookup` rejects an unknown name; off is the identity.
- Stage tests: with a calendar, every report (with `RollReports`), close and reopen is a business day; without `RollReports` reports keep their drawn day; every runoff and recovery row is on a business day; the payment delay and ledger invariants hold; holiday deferral composes with rolling.
- Off equals no block: a dataset with `calendar: none` is identical to one without the block.
- `TestToDomainMapsEveryField`, `TestFormFieldsCoverEveryParameter`, the invariants test, the realism gate and the golden tests.
