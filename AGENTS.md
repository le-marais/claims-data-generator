# AGENTS.md

Guidance for AI coding agents working in this repository. Human contributors may find it useful too.

## What this is

`claimsgen` is a local Go CLI (with an optional browser UI) that generates realistic, fully synthetic insurance claims data for reserving demos and tests. One run produces three linked CSVs for a class of business:

- `policies.csv` - the book of policies per calendar year (cover dates, sum insured, excess, risk factor, premium)
- `claims.csv` - claim events (occurrence, report and close dates, initial case estimate)
- `transactions.csv` - each claim's case estimate movements, payments, and recoveries over its lifetime

Nothing in the output is real, so there are no data governance concerns. See `docs/mission.md` for the full pitch and `docs/roadmap.md` for status and sequencing.

## Tech stack

- **Language:** Go 1.26 (see `go.mod`; module path `github.com/le-marais/claimsgen`)
- **Dependencies:** kept deliberately small - `gonum.org/v1/gonum` (distributions and randomness) and `gopkg.in/yaml.v3` (config). Prefer the standard library; do not add dependencies without a clear reason.
- **UI:** a self-contained web server (`net/http`) serving embedded static assets (plain HTML/CSS/JS, no framework or build step)
- **Data:** Schedule P reference data and the motor preset are embedded in the binary via `go:embed`, so the built binary is fully self-contained.

## Architecture

The layout is domain-driven. Respect the dependency direction: `domain` depends on nothing outside itself, `application` orchestrates the domain, `infrastructure` adapts the outside world.

- `cmd/claimsgen/` - entry point. Two subcommands: `generate` and `ui`.
- `internal/domain/` - the simulation model, no outside dependencies:
  - `claim/` - claim events, inflation, close lag, reopening
  - `policy/` - the policy book
  - `transaction/` - case estimate runoff and recoveries
  - `lob/` - the `LineOfBusiness` parameter object that drives all behavior
  - `triangle/` - development triangles and comparison
  - `shared/` - dates, money, distributions, randomness helpers
- `internal/application/` - use cases: `GenerateDataset`, summary stats, histograms, and the realism check.
- `internal/infrastructure/` - adapters: `config` (YAML plus the embedded motor preset), `csv` (writer), `schedulep` (reference-data reader), `random` (gonum-backed source), `web` (server, view models, static assets).
- `data/reference/` - embedded Schedule P reference companies and the curation list.
- `docs/` - mission, roadmap, and design specs/plans under `docs/superpowers/`.

## Build, run, test

```bash
go build ./cmd/claimsgen        # build the binary
./claimsgen generate            # generate the three CSVs into ./output using the embedded motor preset
./claimsgen ui                  # serve the browser UI on http://127.0.0.1:8080 (--port to change)

go test ./...                   # run all tests
go vet ./...                    # vet
```

Run both `go test ./...` and `go vet ./...` before claiming work is done.

## Conventions and things to know

- **Reproducibility is a hard invariant.** The same seed plus the same config must produce byte-identical output. Randomness flows from a single seeded source split into labelled sub-streams so stages stay independent and repeatable. Never introduce nondeterminism (wall-clock time, map iteration order in output, unseeded randomness) into the generation path.
- **Golden test.** `internal/application/golden_test.go` pins a SHA-256 of the CSV output. If you intentionally change the generated data or its encoding, the test prints the actual hash - paste it back into the `wantHash` constant. Do not update it to hide an unintended change; understand why the output moved first.
- **Realism gate.** `TestDefaultPresetIsRealistic` scores the shipped preset against the embedded Schedule P bands across several seeds. Changes to the model must keep the default preset inside its P5-P95 bands.
- **Adding a line of business** is a YAML file for the CLI (`generate --config my-lob.yaml`, no code change); surfacing it in the UI also needs one registration line in the preset registry. See `internal/infrastructure/config/motor-personal.yaml` for the annotated preset.
- **Testing style.** Tests live beside the code as `_test.go`. Table-driven tests and external test packages (`package foo_test`) are the norm; internal tests use the `_internal_test.go` suffix.

## Architectural preferences

The maintainer prefers **domain-driven design** and **event sourcing where appropriate**. Let these shape new work:

- **Domain-driven design.** The layering above is deliberate: keep the domain pure and free of infrastructure concerns, model the business in ubiquitous language (policy, claim, transaction, line of business), and push YAML, CSV, HTTP, and randomness adapters to the edges. New behavior belongs in `internal/domain/`; `application/` orchestrates, `infrastructure/` adapts.
- **Event sourcing where appropriate.** The transaction ledger already fits this grain: a claim's state is derived by replaying its ordered `ESTIMATE`, `PAYMENT`, and recovery rows (outstanding case is the running sum of estimate movements; gross paid is the sum of payment rows). Prefer modelling a claim's lifetime as a stream of immutable events that state is folded from, rather than mutating snapshots in place. Apply it where it earns its keep - not every part of the model needs it.

## Design and process docs

Substantial features are specced before implementation. Specs live in `docs/superpowers/specs/` and plans in `docs/superpowers/plans/`, dated and named per feature. Read the relevant spec before changing an area it covers, and keep `docs/roadmap.md` current when shipping or planning work.

## Writing style (docs and comments)

- Sentence case for headers, not title case.
- No em dashes. Use spaced hyphens ` - ` instead.
- Be concise and factual; do not embellish beyond what the code or specs state.
