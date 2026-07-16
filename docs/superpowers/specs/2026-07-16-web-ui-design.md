# Web UI design

Date: 2026-07-16
Status: approved pending review

## Summary

A simple local web UI for claimsgen, served by the existing Go binary via a new `claimsgen ui` subcommand. It lets the user configure a run (run flags plus overridable line of business parameters, prefilled from the preset), generate the dataset (CSVs written to disk exactly as the CLI does), and explore the result: summary stats, development triangles, distributions, and a realism check against the Schedule P reference bands. Motor personal is the only line of business for now, but the design is extendable: presets are listed from a registry and the UI is driven by whatever that registry returns.

## Decisions made during design

- Form factor: local web UI served on localhost by the Go binary, used in the browser. Not a TUI or desktop app.
- Configuration depth: run flags up front, plus a collapsed "advanced" section exposing every LoB parameter prefilled from the preset (the motor preset is only ~30 scalars plus the excess table, so full exposure stays simple). No hand-editing YAML needed from the UI workflow.
- Extensibility: the UI has a LoB dropdown backed by a preset registry. Adding a future LoB means a new YAML preset plus registration, no UI changes.
- Views: all four - summary stats, paid/incurred triangles with age-to-age factors, distributions (severity, report lag, close lag), and a realism view plotting the run against Schedule P bands.
- Run handling: latest run only. Each Generate replaces the previous results. No history, no side-by-side comparison.
- Synchronous generation: a default run takes under a second, so `POST /api/generate` blocks and returns everything in one response. No jobs, no progress machinery.
- Tech: single embedded static page (HTML/CSS/vanilla JS via `go:embed`), no node toolchain, no build step. Charts are hand-rolled SVG and HTML tables. All computation happens in Go; the frontend only renders JSON.

## Architecture

Follows the existing DDD layout. No changes to `internal/domain/`; `internal/application/` gains small view-model use cases; the web layer is a new infrastructure adapter.

```
cmd/claimsgen/                   gains the `ui` subcommand: wires config registry,
                                 random source, Schedule P references into the server
internal/application/            gains Summarize (per-year stats) and histogram
                                 helpers, so view models are testable without HTTP;
                                 GenerateDataset and EvaluateRealism reused as is
internal/infrastructure/web/     new adapter: HTTP handlers, JSON view-model
                                 assembly, embedded frontend
    static/index.html            single page
    static/app.js                fetch + render, vanilla JS
    static/style.css
```

`claimsgen ui` starts an HTTP server on `localhost:8080` (`--port` to override) and prints the URL.

## API

Three JSON endpoints:

- `GET /api/lobs` - available presets: `[{id: "motor-personal", name: ...}]`. Backed by a registry in the config package.
- `GET /api/lobs/{id}/preset` - the full default parameter set as JSON, mirroring the YAML structure (book / claims / runoff / excess table). The form prefills from this. Unknown id: 404.
- `POST /api/generate` - body: run flags (seed, start year, years, initial book size, output dir) plus the complete parameter set as edited in the form. The handler builds `lob.LineOfBusiness`, validates with the existing `Validate()`, runs `GenerateDataset`, writes the three CSVs with the existing writers, and returns in one response:
  - summary stats per accident year (policies, claims, premium, paid, incurred, loss ratio) plus totals
  - paid and incurred cumulative triangles with age-to-age factors
  - histograms for severity and report/close lags, binned server-side
  - realism report: the run's ATA factors and ultimate loss ratio alongside the Schedule P min/max bands, with in/out flags and an overall pass/fail

The frontend holds the latest response in memory and renders all views from it.

## UI layout

Single page, two zones.

Left, a fixed-width configuration panel:

- LoB dropdown (motor-personal for now)
- Run flags: seed, start year, years, initial book size, output directory
- Collapsible "Line of business parameters" section, collapsed by default, grouped as in the YAML: book, claims, runoff. Numeric inputs prefilled from the preset, labels/tooltips taken from the YAML comments. The excess table is a small editable rows-of-(value, weight) widget.
- "Reset to preset defaults" button
- Generate button with a busy state while the request runs

Right, the results area (empty state before the first run):

- A header line stating the run: seed, years, LoB, output path, row counts
- Four tabs:
  1. Summary - per-accident-year stats table plus totals row
  2. Triangles - paid and incurred as heatmap-shaded HTML tables with an ATA factor row underneath; paid/incurred toggle
  3. Distributions - SVG histograms: severity (log-x), report lag, close lag
  4. Realism - per development age, the run's paid and incurred ATA factors as dots inside the Schedule P min/max band drawn as a range bar, plus ultimate loss ratio vs its band; out-of-band values highlighted red, overall pass/fail banner

## Error handling

- Validation errors: 400 with the message from existing validation; shown in a red banner above the Generate button without clearing previous results. Basic client-side guards (required, numeric), server validation is the source of truth.
- Write failures (bad output dir, permissions): 500, same banner treatment. Generation is in-memory, so failure risk mirrors the CLI.
- Port in use: `claimsgen ui` fails fast suggesting `--port`.
- Unknown preset id: 404.
- All errors are plain JSON `{error: "..."}`.

## Testing

- View-model computation (summary stats, histograms, realism report assembly) unit-tested in `internal/application/` against a small generated dataset, matching existing test style.
- HTTP handlers tested with `httptest`: preset endpoint matches the embedded YAML; generate endpoint round-trips a small run (few hundred policies, 2 years) asserting response shape, CSV files written, and byte-identical CSVs versus the CLI path for the same seed.
- Frontend: no JS test framework. The JS stays thin (fetch, render); numeric correctness is covered server-side. One manual end-to-end pass in the browser during implementation.
