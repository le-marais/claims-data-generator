# Trailing partial accident year (MF-2) - design (2026-07-21)

Closes finding **MF-2** from `docs/code-review-2026-07-18.md`: occurrence dates
are uniform over the full 12-month cover term
(`internal/domain/claim/claim.go:100-101`), so policies written late in the last
underwriting year spill claims into `startYear+years`. The summary
(`internal/application/summary.go:58`) and triangles
(`internal/domain/triangle/triangle.go:79-81`) drop occurrences outside the
window, but those claims are still written to `claims.csv` and counted in the UI
header (`internal/infrastructure/web/viewmodel.go:124`). On the default run the
header reports 27,823 claims but the summary totals 26,150 - the missing 1,673
(6%) form an eleventh, partial-exposure accident year the in-app views never
show.

## Decision

**Don't generate out-of-window claims.** Window the exposure at the simulation
source: constrain occurrences to the run window and pro-rate the tail's
frequency to its in-window exposure (matching the design spec's "exposed
fraction of the term" wording, `docs/superpowers/specs/2026-07-15-claims-generator-design.md:88`).

Chosen over "surface the partial year everywhere". A reserving demo should not
have to know to discard an under-developed trailing year; removing it at the
source makes `claims.csv`, the summary, the triangles, and the header agree with
no downstream special-casing.

## Target model

```
windowEnd        = Jan 1 of (startYear + years)          # exclusive bound
inWindowDays     = days from CoverStart to min(CoverEnd, windowEnd)
termDays         = days from CoverStart to CoverEnd       # ~364
exposedFraction  = inWindowDays / termDays                # 1 for fully-in-window policies

frequency        ~ Poisson(BaseFrequency x RiskFactor x exposedFraction)
occurrence       ~ uniform over [CoverStart, min(CoverEnd, windowEnd)]
```

Only policies written in the last underwriting year, whose 12-month cover spills
past `windowEnd`, get `exposedFraction < 1`. Every other policy is unchanged.
Because the policy book only writes underwriting years `[startYear,
startYear+years-1]`, no policy starts before the window, so no left-truncation is
needed.

## Change 1 - window the claim simulator (`internal/domain/claim/claim.go`)

`ClaimSimulator` gains the run window (`startYear`, `years`) via a `WithWindow`
builder, mirroring the existing `WithInflation` builder. `Simulate` uses it to
pro-rate the Poisson mean per policy:

```go
exposed := exposedFraction(pol, windowEnd)   // in-window days / term days
n := stream.Poisson(s.params.BaseFrequency * pol.RiskFactor * exposed)
```

`simulateClaim` constrains the occurrence draw to the in-window portion of the
cover:

```go
effectiveEnd := pol.CoverEnd
if windowEnd.Before(effectiveEnd) {
    effectiveEnd = windowEnd
}
span := shared.DaysBetween(pol.CoverStart, effectiveEnd)
occurrence := pol.CoverStart.AddDays(int(src.Uniform() * float64(span+1)))
```

(The exact inclusive/exclusive day arithmetic - the current `term+1` - is
preserved against `effectiveEnd` instead of `CoverEnd`; the plan pins the
boundary day.)

The per-policy split streams (`"claims-policy-%d"`) are unchanged, so pro-rating
one policy's frequency never affects another policy - determinism holds; only
the tail policies' claim counts and occurrence ranges shift.

## Change 2 - wire the window (`internal/application/generate.go`)

`GenerateDataset` already has `req.StartYear` and `req.Years`; it passes them to
`ClaimSimulator` via the new `WithWindow` builder.

The inflation index is currently built with a `Years+1` span
(`generate.go`) specifically because occurrences could spill into
`startYear+years`. With MF-2, occurrences can no longer spill past the window, so
this simplifies to `Years`; the `InflationIndex.For` clamp stays as a defensive
fallback.

## Consequences

- Every claim occurs inside the window, so `claims.csv`, the summary, the
  triangles, and the header count (`len(ds.Claims)`) all agree automatically -
  **no viewmodel change is required**; the header is simply consistent now.
- Earned premium is already pro-rated to the window day-by-day
  (`internal/domain/triangle/triangle.go:102-133`), so windowing claims the same
  way keeps the per-year loss ratio consistent: a tail policy's ~1 month of
  in-window earned premium matches its ~1 month of in-window claim exposure.

## Golden fixture

`internal/application/golden_test.go` pins a single SHA-256 over the three CSVs.
Dropping the trailing partial year changes the default run, so the fixture
changes. Update procedure: implement the change, confirm the realism gate still
passes, run `TestGoldenCSVBytes`, take the printed `got` hash, and paste it into
`wantHash`.

## Testing

- **No spill**: no claim has an occurrence date on or after Jan 1 of
  `startYear+years`.
- **Counts agree**: `len(ds.Claims)` equals the summary's total claim count
  equals the number of rows in `claims.csv`.
- **Pro-rated frequency**: on a run whose last underwriting year has cover
  spilling past the window, the final accident year's claim count is consistent
  with full in-window exposure (no visible drop-off from truncation), and
  earlier years are unchanged versus the current model for fully-in-window
  policies.
- **Determinism**: two runs with the same seed and request produce identical
  datasets; per-policy streams are untouched for fully-in-window policies.

## Out of scope

- Recalibration of `motor-personal.yaml` is not expected from this change alone
  (removing a partial-exposure year does not change the mature development
  factors), but re-run `TestDefaultPresetIsRealistic` to confirm.
- The own-damage severity rework (SL-3 + SL-4) is a sibling change with its own
  design doc,
  `docs/superpowers/specs/2026-07-21-own-damage-severity-rework-design.md`.
