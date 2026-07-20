> **Superseded (2026-07-20):** the sep2011 vintage was removed and the reference pool is now single-vintage (dec2025, hand-curated). See `docs/superpowers/specs/2026-07-20-reference-pruning-realism-bands-design.md`. Kept for historical context.

# Merge the sep2011 personal motor reference data into the app's reference pool

Date: 2026-07-17
Status: approved

## Goal

The app currently evaluates realism (test gate and web UI) against a single Schedule P
personal motor dataset: `data/reference/schedule p/dec2025/ppauto_pos98-07` (143
companies, accident years 1998-2007). A second vintage now exists at
`data/reference/schedule p/sep2011/auto_personal` (146 companies, accident years
1988-1997, same JSON schema plus extra `FuturePaid`/`FutureIncurred` fields the parser
ignores). Both vintages must feed one combined reference pool wherever reference data is
used: the realism test gate and the web UI comparison.

## Design

### Reference manifest (`data/reference/refdata.go`)

- Add a second `//go:embed` pattern so `Files` also embeds
  `"schedule p/sep2011/auto_personal/*.json"`.
- Add `var PersonalMotorDirs = []string{"schedule p/dec2025/ppauto_pos98-07", "schedule p/sep2011/auto_personal"}`.
  This is the single source of truth for which embedded directories make up the personal
  motor reference pool. Adding a future vintage means appending one entry and one embed
  pattern.

### Loader (`internal/infrastructure/schedulep`)

- `LoadFS(fsys fs.FS, dirs ...string)` becomes variadic. It loads every `*.json` under
  each dir in the order given, files sorted by name within each dir (deterministic).
  Zero total files across all dirs is an error naming the dirs.
- `ReferenceSet.Name` becomes vintage-qualified: `<parent-dir>/<file-stem>`, e.g.
  `dec2025/10007` and `sep2011/10007`. Both vintages contain overlapping company IDs;
  qualification keeps names unique. Names are not user-facing today (debugging only).
- `LoadDir` keeps its signature (one disk directory) and its current unqualified names
  are acceptable to change to match; the embedded-vs-disk parity test defines equality.

### Call sites

- `cmd/claimsgen/main.go` `runUI`: `schedulep.LoadFS(refdata.Files, refdata.PersonalMotorDirs...)`.
- `internal/infrastructure/web/server_test.go`: same merged pool.
- `internal/application/realism_test.go`: load the merged pool via
  `schedulep.LoadFS(refdata.Files, refdata.PersonalMotorDirs...)` so the test gate
  scores against exactly what the app ships.
- `internal/infrastructure/schedulep/reader_test.go`: expected embedded count becomes
  289 (143 dec2025 + 146 sep2011); parity test compares embedded vs the two disk dirs.

### Behavior

- Realism bands are the min/max across both vintages, so bands can only widen relative
  to today; `TestDefaultPresetIsRealistic` keeps passing.
- sep2011 files with zero-padded early rows are already safe: zero denominators yield
  NaN age-to-age factors, which `ATABands` skips; zero earned premium companies are
  skipped by the loss-ratio band.

### Docs

- README: describe the two-vintage pool (~289 reference datasets, accident years
  1988-1997 and 1998-2007).

## Out of scope

- The non-motor dec2025 datasets (comauto, medmal, othliab, prodliab, wkcomp) and
  non-motor sep2011 datasets stay unused and unembedded.
- No dataset selection UI or CLI flag; the pool is always merged.
