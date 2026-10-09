> **OUT OF CONTEXT - do not read (2026-10-09):** historical design record, kept for provenance only. Agents must not load this file into context or treat it as a source of truth; it records decisions as of its own date and may not match current behaviour. For how the system works today see `README.md`, `AGENTS.md`, and `docs/architecture.md`.

# Seasonal holiday deferral - design

Date: 2026-10-09

## Goal

Add one aspect of seasonality: claims handling slows over the summer holiday. A share of the reports and payments the simulation dates inside a holiday window is moved to the same day of the next month, on top of what that month already holds. Other seasonal aspects (occurrence seasonality, catastrophes) are out of scope; the name `seasonal_holiday` leaves room for them.

## Parameters

A new top-level block on `LineOfBusiness`, `SeasonalHoliday SeasonalHolidayParams`:

```yaml
seasonal_holiday:
  hemisphere: northern   # none (default) | northern | southern
  report_share: 0.10     # share of reports dated in the window moved to the next month
  payment_share: 0.15    # share of payments (interim and final settlement) dated in the window moved
```

- `Hemisphere` is a string enum: `""` or `none` (off), `northern`, `southern`. Any other value fails validation, naming `seasonal_holiday.hemisphere`.
- `ReportShare` and `PaymentShare` are each in [0, 1] and finite. When the hemisphere is off they are not checked, so a switched-off block needs no other fields.
- The zero value is off, so existing YAMLs keep working unchanged.
- The block holds simulation parameters only.

## Holiday window

Inclusive day ranges:

- `northern`: 15 July to 14 August.
- `southern`: 15 December to 14 January, spanning the year end.

`SeasonalHolidayParams.InWindow(d shared.Date) bool` reports whether a date falls in the window, and `Defers(d shared.Date, u float64, share float64) bool` is `InWindow(d) && u < share`.

## Deferral rule

A deferred date moves to the same day of the next month, clamped to that month's last day: a new `shared.Date.AddMonths(n int)` with end-of-month clamping (31 January plus one month is 28 or 29 February). A window date is the 14th or 15th onwards, so the landing date (15 August to 14 September, or 15 January to 14 February) is always outside the window: an event is deferred at most once by construction.

## Where it applies

Deferral happens where each date is drawn, so the ledger is right by construction and every invariant holds: the final settlement lands on the close date, the case ends at exactly zero, the payment delay is respected, and gross paid equals the claim's cost. Costs never change.

1. **Claim stage** (`claim.ClaimSimulator.simulateClaim`). After the report date is drawn, defer it at `ReportShare`. The close lag is drawn and the payment delay added as today, measured from the (possibly deferred) report, so a deferred report moves the claim's whole timeline back. Then, if the claim pays (not nil), defer the close date at `PaymentShare`: the close carries the final settlement. A nil close is not a payment and never moves.
2. **Reopen stage** (`claim.ReopenSimulator.Apply`). A reopen episode always pays, so its close is deferred at `PaymentShare`. Its open date is not a report and is not deferred. The reopen is drawn after the first close, which the claim stage has already deferred, so ordering holds.
3. **Runoff stage** (`transaction.RunoffSimulator.drawInterimPayments`). After the interim payment offsets are drawn, each whose date is in the window is deferred at `PaymentShare`, then the offsets are sorted and the existing minimum-payment, spacing and hold-over rules applied. A payment deferred past the last allowed interim day (`duration - edge`) is held over into the final settlement through the existing hold-over path. Bills for the payment delay are placed from the deferred offsets, as today.

The claim, reopen and runoff simulators each take the params through a `WithSeasonalHoliday(lob.SeasonalHolidayParams)` option, matching `WithPaymentDelay`; `GenerateDataset` passes `req.LOB.SeasonalHoliday` to all three. The runoff dates an interim offset as the episode's open date plus the offset to test it against the window.

## Determinism

Every deferral draws one uniform per candidate date from a new sub-stream labelled `seasonal-holiday`, split off the entity's existing stream: `claims-policy-<id>/<section>` (one split per section, a report draw and a close draw per claim, reportable or not), `reopen-claim-<id>`, and `runoff-claim-<id>`. `Split` is keyed by label and takes no draws from its parent, so:

- with the block off, no draws are taken and the output is byte-identical to today;
- with it on, no existing draw moves; only dates change.

The draw is taken for every candidate whether or not its date is in the window, so a parameter change to the share or hemisphere never shifts later deferral draws.

## Presets

Both presets (`motor-personal.yaml`, `motor-commercial.yaml`) switch on `northern` with `report_share: 0.10` and `payment_share: 0.15`. Both are US Schedule P lines, so the northern summer fits. The shares are illustrative: Schedule P is annual and says nothing about monthly seasonality. The YAML comment says so.

Northern shifts land in the same calendar year, so the annual triangles move only where a deferred report pushes a close across a year end. The four golden hashes are refreshed after checking the movement comes only from this feature (with the block off the hashes are unchanged), and the realism gate and drift test are re-run.

## UI and docs

- Form registry: the two shares in a new "Seasonal holiday" group. The hemisphere is left to the YAML, like severity kind.
- `config`: mirrored struct with the same Go field names, mapped in `ToDomain`.
- README: document the block where users see timing behaviour, add it to the model diagrams by YAML key, and remove "No seasonality" from the simplifications (keep catastrophe and event clustering).
- `docs/architecture.md`: the claim, reopen and runoff stage descriptions, the `lob` parameter tree, and the stream labels.

## Testing

- `shared.Date.AddMonths`: ordinary days, end-of-month clamping, leap years, December to January.
- `SeasonalHolidayParams`: window boundaries for both hemispheres (14 July out, 15 July in, 14 August in, 15 August out; 14 December out, 15 December in, 14 January in, 15 January out), validation of the enum and shares, shares ignored when off.
- Claim stage with `report_share: 1`: every window report moves exactly one month, every other report is unchanged; with `payment_share: 1` every paying window close moves, nil closes do not.
- Runoff with `payment_share: 1`: window interim payments move a month, one pushed past the last interim day is folded into the final settlement, and the ledger invariants hold.
- Off block equals no block: a dataset generated with `hemisphere: none` and any shares is identical to one without the block.
- `TestToDomainMapsEveryField`, `TestFormFieldsCoverEveryParameter`, the invariants test, the realism gate and the golden tests.
