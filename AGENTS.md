# AGENTS.md

Guidance for AI coding agents working in this repository. Human contributors may find it useful too.

## What this is

`claimsgen` is a local Go CLI (with an optional browser UI) that generates realistic, fully synthetic insurance claims data for reserving demos and tests. One run produces five linked CSVs for a class of business:

- `policies.csv` - the book of policies per calendar year (cover dates, sum insured, excess, risk factor, premium)
- `claims.csv` - claim events (occurrence, report and close dates, initial case estimate)
- `transactions.csv` - each claim's case estimate movements, payments, and recoveries over its lifetime
- `triangles.csv` - incremental monthly development triangles by origin month (paid, paid net of recoveries, incurred, reported claim counts)
- `exposure.csv` - exposure by origin month (premium, exposure units in policy-years, policy count)

Nothing in the output is real, so there are no data governance concerns. See `docs/mission.md` for the full pitch and `docs/roadmap.md` for direction.

## Preferred workflows

- **Fetch at the start of every session.** Run `git fetch` before doing anything else, so you are working against the latest state and avoid diverging from `origin`.
- **Ship every chunk of work as a pull request, squash merged to `main`.** Never commit directly to `main`. Work on a branch named for the change (`feature/…`, `docs/…`, `fix/…`), commit as you go with whatever granularity helps review, then open a PR. `main` therefore holds one commit per feature or chunk of work, and the working history stays on the branch. Run `go test ./...` and `go vet ./...` before opening the PR, and say so in its body. Write the PR title and body as the commit message `main` will actually keep - the squash uses them - so lead with what changed and why, not with a list of commits.
- **The maintainer approves and merges every PR.** Open the PR and stop there. Do not merge it, approve it, or enable auto-merge yourself.

## Tech stack

- **Language:** Go 1.26.4 or later (see `go.mod`; module path `github.com/le-marais/claimsgen`)
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
- `docs/` - the five living docs (mission, roadmap, architecture, review, todo) plus historical records; see "Living docs".
- `tools/` - dev-only helpers, not part of the binary: `prune-dec2025.ps1` (reference-data curation) and `screenshots/` (a Node script that regenerates the README screenshots). The Node dependency there does not contradict the no-build-step UI.

## Build, run, test

```bash
go build ./cmd/claimsgen        # build the binary
./claimsgen generate            # generate the five CSVs into ./output using the embedded motor preset
./claimsgen ui                  # serve the browser UI on http://127.0.0.1:8080 (--port to change)

go test ./...                   # run all tests
go vet ./...                    # vet
golangci-lint run ./...         # lint (config in .golangci.yml)
```

Run `go test` and `go vet` before claiming work is done. CI (`.github/workflows/ci.yml`) runs gofmt, `go vet`, `go test` on Go 1.26.x and stable, `govulncheck` and `golangci-lint` on every pull request and on `main`; a PR is not ready to merge until it is green.

## Conventions and things to know

- **Reproducibility is a hard invariant.** The same seed plus the same config must produce byte-identical output. Randomness flows from a single seeded source split into labelled sub-streams so stages stay independent and repeatable. Never introduce nondeterminism (wall-clock time, map iteration order in output, unseeded randomness) into the generation path.
- **Golden tests.** `internal/application/golden_test.go` pins three SHA-256 digests: `wantHash` over the three dataset CSVs, `wantAggregateHash` over `triangles.csv` and `exposure.csv`, and `wantAnnualHash` over the annual triangles, including the `SectionComparison` triangles and earned premium that the realism gate scores. `wantAnnualHash` matters most: the realism bands are wide enough to absorb a real shift in the annual cells, so it is the only guard on the cells the realism gate and the preset's calibration depend on. If you intentionally change the generated data or its encoding, the failing test prints the actual hash - paste it back into the constant. Do not update any of them to hide an unintended change; understand why the output moved first.
- **Realism gate.** `TestDefaultPresetIsRealistic` scores the shipped preset against the embedded Schedule P bands across several seeds. The reference is private passenger auto *liability*, so the preset marks its two third-party sections (`third_party_property`, `third_party_injury`) `scored: true` and the gate scores those sections together (`application.SectionComparison`: their claims against their share of premium); own damage is not scored. Changes to the model must keep the default preset inside its P5-P95 bands.
- **One aggregation store.** `triangle.MonthlyGrid` is the canonical aggregate: incremental cells, origin months down, development months across, running to full runoff. Every coarser grain is `MonthlyGrid.Coarsen`, which keys both axes on the calendar period the month falls in - the annual triangles the realism gate and the UI read are `Coarsen(Annual, 10, true)` cumulated, the `true` folding development past age 10 into the last column. Add new aggregate views by coarsening the grid, never by re-scanning the transactions.
- **Adding a line-of-business parameter** touches these places, in order:
  1. The domain struct and its `validate` in `internal/domain/lob/lob.go`. A parameter that differs by section of cover belongs on `SectionParams` (and, if pricing assumes it, `PricingSectionParams`). A zero value must mean "off" or the old behaviour, so existing YAMLs keep working, and a switched-off block's other fields should not be required (see `RecoveryTypeParams.validate`).
  2. The mirrored config struct and `ToDomain` in `internal/infrastructure/config/config.go`, with the same Go field name as the domain. `TestToDomainMapsEveryField` fails if a field is not carried across.
  3. The preset YAML (`motor-personal.yaml`), with a comment saying what the parameter does and why the preset uses its value.
  4. The form registry `formFields` in `internal/infrastructure/web/fields.go`: label, tip and group. `TestFormFieldsCoverEveryParameter` fails if a parameter has no form field.
  5. Docs: the README if a user would notice the behaviour, including its model diagrams, which name parameters by their YAML key.
  6. If the preset sets a non-default value, refresh the golden hashes and re-check the realism gate.
- **Adding a line of business** is a YAML file for the CLI (`generate --config my-lob.yaml`, no code change); surfacing it in the UI means embedding the YAML in `internal/infrastructure/config/config.go` with a `//go:embed` line and registering it in both `presetInfos` and `presetYAML` there. See `internal/infrastructure/config/motor-personal.yaml` for the annotated preset.
- **Testing style.** Tests live beside the code as `_test.go`. Table-driven tests and external test packages (`package foo_test`) are the norm; internal tests use the `_internal_test.go` suffix.

## Architectural preferences

The maintainer prefers **domain-driven design** and **event sourcing where appropriate**. Let these shape new work:

- **Domain-driven design.** The layering above is deliberate: keep the domain pure and free of infrastructure concerns, model the business in ubiquitous language (policy, claim, transaction, line of business), and push YAML, CSV, HTTP, and randomness adapters to the edges. New behavior belongs in `internal/domain/`; `application/` orchestrates, `infrastructure/` adapts.
- **Event sourcing where appropriate.** The transaction ledger already fits this grain: a claim's state is derived by replaying its ordered `ESTIMATE`, `PAYMENT`, and recovery rows (outstanding case is the running sum of estimate movements; gross paid is the sum of payment rows). Prefer modelling a claim's lifetime as a stream of immutable events that state is folded from, rather than mutating snapshots in place. Apply it where it earns its keep - not every part of the model needs it.

## Living docs

`docs/` keeps five living docs. They record the current state and open work only, never history: no shipped lists, resolved or dropped lists, dated re-rankings, or "has since shipped" notes. Git history is where old content lives.

| File | Holds |
| --- | --- |
| `docs/mission.md` | The core purpose of the app: problem, audience, what it does, what success looks like. |
| `docs/roadmap.md` | High-level direction: what comes next and in what order. Nothing already shipped. |
| `docs/architecture.md` | An overview of the code as it is on `main`. |
| `docs/review.md` | Open review findings, recorded as reviews happen. |
| `docs/todo.md` | Smaller or administrative items still to be done. |

Keep them current in the same PR as the work:

- **When work ships,** delete it from the roadmap, `review.md` and `todo.md`, and update `architecture.md` to match the code. Name the closed IDs in the commit message, so the trail is in the history rather than in the files.
- **`review.md` and `todo.md` items** carry a position, a stable ID, a severity (**high** undermines the mission, **medium** worth addressing soon, **low** fix when touching the area), where the item lives, the evidence and the action. When an item closes, delete it and renumber the positions so the list stays dense; the IDs never change, so older references still resolve against git history. Review findings use **MR** IDs for the model review. A finding that supersedes a `todo.md` item replaces it: delete the `todo.md` item and say so in the finding.

Treat the code, `README.md`, this file and `docs/architecture.md` as the sources of truth.

Everything else in `docs/` is historical and intentionally not updated. `docs/background-context.md` and `docs/raw user inputs/` are the original brief and the transcripts behind it: read them for the original intent, not for current behaviour. `docs/superpowers/specs/` is **out of context**: every file there carries an "OUT OF CONTEXT - do not read" banner. Do not read them, load them into context, or cite them as current behaviour. Implementation plans (`docs/superpowers/plans/`) are deleted once their work ships. Any file added under `docs/superpowers/` must carry the same banner.

## Writing style (docs and comments)

- Sentence case for headers, not title case.
- No em dashes. Use spaced hyphens ` - ` instead.
- Be concise and factual; do not embellish beyond what the code or specs state.
