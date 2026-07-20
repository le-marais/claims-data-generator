# Reference-data pruning follow-through and SL-1 resolution - design

Status: approved pending review

## Context

The Schedule P reference data was pruned in three commits (ahead of `origin/main`):
`07fff0b` (add gr-code list and pruning script), `fd2b971` (prune the 2025 schedule
p data), `27ea3ae` (remove the sep2011 reference data). The result:

- The embedded personal-motor reference pool is now a single vintage,
  `dec2025/ppauto_pos98-07`, hand-curated down to 96 companies (was 143 dec2025 +
  146 sep2011 = 289 across two vintages).
- Curation was done via a keep-list (`data/reference/gr-code-list.md`, one
  `<lob>: <grcode>` per line, all six Schedule P lines of business) applied by
  `tools/prune-dec2025.ps1`. The selection was mixed/judgemental: low-volume and
  degenerate companies removed by hand, no single mechanical rule.
- `refdata.go` (embed + `PersonalMotorDirs`) and the `motor-personal.yaml`
  calibration comment were updated; `reader_test.go`'s embedded-count assertion was
  changed 289 -> 96.

This work addresses code-review finding **SL-1** (`docs/code-review-2026-07-18.md`):
the realism bands were the raw min/max across every reference company, so a single
degenerate company set the bound and the gate was near-vacuous. Pruning removed the
degenerate companies; this design completes the fix by making the band method itself
robust, and cleans up everything the pruning left inconsistent.

The change left several gaps:

- **Broken test:** `internal/infrastructure/schedulep/reader_test.go:22`
  (`TestLoadDirReadsAllCompanies`) still asserts `>= 100` companies; there are 96, so
  it fails.
- **Stale docs:** `README.md:73` describes "289 ... from two vintages ... dec2025 and
  sep2011"; `docs/roadmap.md:10` says "~143 Schedule P ... reference datasets". Both
  are now wrong.
- **Production-dead code:** the multi-vintage loading path (variadic `LoadFS`
  dir-merge, vintage-qualified names) and its tests exercise a second vintage that no
  longer ships.
- **Undocumented rationale:** the realism story now rests on a curated subset, but the
  curation criterion is written down nowhere except implicitly in the keep-list.

## Goal

Make code, tests, and docs consistent with the pruned single-vintage reference data,
and fully resolve SL-1 by replacing near-vacuous min/max realism bands with percentile
bands plus a mechanical degeneracy backstop.

## Non-goals

- All other review findings (SL-2, SL-3, SL-4, MF-1, etc.) except the incidental SL-8,
  which is fixed only because it lives in the code being changed.
- Per-line-of-business reference calibration (roadmap item). Only `ppauto` is embedded
  and gated today; the other five LOB directories remain present but unused.

## Design

### 1. Realism bands (`internal/domain/triangle/compare.go`)

The substantive change. Today `ATABands` and the loss-ratio band in
`CompareToReference` accumulate raw min/max across all reference companies.

- **Scored band = P5-P95.** Compute the 5th and 95th percentiles per development age
  (paid and incurred ATA) and for the ultimate loss ratio, across the reference
  companies. `P5`/`P95` are named constants so the coverage can be widened during
  calibration without hunting through the code. Percentiles use linear interpolation
  between sorted order statistics; the helper is unit-tested against known inputs.
- **Backstop filter (mechanical, not the full manual judgement).** Exclude a company
  from band construction when it carries no valid signal:
  - total earned premium <= 0, or
  - its triangle yields no finite ATA factors / an all-zero latest diagonal.
  This protects future un-curated per-LOB reference data at a basic level. The
  hand-curation remains the authoritative quality mechanism; the filter is a backstop.
- **`Band` carries both bands.** The scored `[P5, P95]` drives the pass/fail decision;
  an outer `[Min, Max]` is retained for display context. Both are computed over the
  same filtered set, so the backstop applies to the outer band too. `contains` scores
  against the percentile band only.
- **Incidental SL-8 fix.** Propagate `lossRatio`'s `ok` flag: when the generated
  dataset has zero earned premium, mark the loss-ratio check failed instead of scoring
  a bogus `0` (which the old ~0 band min let pass).

`Report`/`Check`/`AgeCheck` grow to carry the outer band alongside the scored band so
the UI can render both.

### 2. UI surface (`internal/infrastructure/web/viewmodel.go`, `web/static/app.js`)

- The JSON realism report carries the scored band and the outer band per metric.
- `bandCard` draws a faint outer min/max rectangle behind the solid P5-P95 rectangle;
  the tooltip reports both ranges.
- Band card label wording: "P5-P95 of reference companies (min/max faint)".

### 3. Preset recalibration (`internal/infrastructure/config/motor-personal.yaml`)

Tighter bands will likely push one or more default-preset metrics outside P5-P95. This
is an empirical tune-and-check step, not a pre-computable set of values:

1. Implement the bands, run the realism gate.
2. For any metric outside its P5-P95 band, adjust the relevant preset knob to
   re-centre the generated value inside the band.
3. Re-verify across all tested seeds (see section 5).

Any changed values are documented; the calibration comment at the top of the YAML is
updated to describe the curated single-vintage pool and the percentile bands.

### 4. Simplify out the multi-vintage loader (`internal/infrastructure/schedulep/reader.go`, `data/reference/refdata.go`)

The variadic dir-merge and vintage-qualified naming exist only to blend two vintages,
which no longer happens.

- Collapse `LoadFS` to single-dir loading; drop vintage-qualified naming so company
  names are bare gr codes again.
- `PersonalMotorDirs` (a slice) becomes a single dir constant referenced by
  `refdata.go`'s embed directive and the reader.
- The other five dec2025 LOB directories stay on disk for future per-LOB calibration;
  no code references them today.

### 5. Tests

- **Fix the break:** `TestLoadDirReadsAllCompanies` asserts the curated count (96)
  rather than `>= 100`.
- **Simplify:** remove `TestLoadFSMergesDirsWithQualifiedNames`,
  `TestLoadFSErrorsWhenAnyDirIsEmpty`, and `TestLoadDirQualifiesNamesByVintage`;
  simplify `TestLoadFSEmbeddedMatchesDisk` to the single dir.
- **New unit tests (`compare_test.go`):** the percentile helper on known inputs; the
  backstop filter excludes a degenerate company from the band; `contains` scores
  against the percentile band; the SL-8 zero-premium path fails the loss-ratio check.
- **Harden the gate (D-3):** run `TestDefaultPresetIsRealistic` on 2-3 seeds, not just
  seed 42, so the tightened gate cannot pass by luck.

### 6. Documentation

- `README.md` Realism section (line ~73): rewrite to describe 96 hand-curated dec2025
  private passenger auto companies, curated via `data/reference/gr-code-list.md` and
  `tools/prune-dec2025.ps1` to remove low-volume and degenerate companies; scored
  against P5-P95 bands with a degeneracy backstop filter, with min/max shown for
  context. Remove all sep2011 / two-vintage language (also lines ~36, ~49 wording).
- `docs/roadmap.md:10`: "~143" -> "~96 curated"; refresh the realism-gate line.
- `data/reference/gr-code-list.md`: add a short header explaining it is a hand-curated
  keep-list covering all six Schedule P LOBs (only `ppauto` is embedded/gated today)
  and that `tools/prune-dec2025.ps1` applies it.
- Add a one-line "superseded" note to the two sep2011 docs
  (`docs/superpowers/specs/2026-07-17-sep2011-reference-pool-design.md`,
  `docs/superpowers/plans/2026-07-17-sep2011-reference-pool.md`) so they read as
  historical rather than current.

### 7. Verification

- `go test ./...` and `go vet ./...` green.
- The realism gate is green across every tested seed.

## Testing strategy

Unit tests cover the new percentile and filter logic in isolation (domain layer). The
realism gate remains the end-to-end guarantee, now multi-seed. The reader tests pin the
curated count and the single-vintage naming. `go vet` and the full suite gate the
change.
