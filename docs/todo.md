# Todo

The single open-work backlog for claimsgen. It consolidates the three review
documents that used to live here - the full code review (2026-07-18), the
security and vulnerability review (2026-07-22), and the stage-isolation findings
(2026-07-27). Those files were removed; their full text, including every
resolved finding and the measurements behind it, is recoverable from git history
(`git log --diff-filter=D -- docs/code-review-2026-07-18.md`).

Every item below was re-verified against the code on 2026-08-09. `go build`,
`go test ./...` and `go vet ./...` are clean.

## How to read this

Finding IDs are preserved from the source reviews so older references still
resolve:

- **SL** simulation logic, **MF** mission fit, **R** robustness outside the
  simulation, **RF** refactoring, **D** documentation and tests - from the code
  review.
- **M**, **L**, **I** - security findings, prefixed by their severity (medium,
  low, informational) rather than category.
- **F** - stage-isolation findings: places where the policy, claims and
  transaction stages share parameters or code they should not.

Severity: **high** materially undermines the mission, fix before building on
top; **medium** worth addressing soon; **low** fix when touching the area.

Nothing high-severity is open. The two high-severity code-review findings
(SL-1, MF-1) were resolved on 2026-07-27.

## Priorities

1. **Server guardrails before the UI is shared with the team**: R-1, R-2, M1,
   L1. Concurrency, input bounds, output-path confinement.
2. **Before the second line of business**: RF-13 and RF-14 (parameter fan-out
   and the `Claim` struct's pipeline-carry fields), plus the stage-isolation
   items F3, F4, F5 and F8 that make the same work harder.
3. **Realism depth**: SL-2, SL-6, SL-7 - the three places the generated data is
   easier than real data.
4. **Cheap and visible**: D-1 (stale screenshots), L2, L3, I2.

## Simulation model

### SL-2 (medium) - reference and generated "ultimate loss ratio" are definitionally different quantities

- Where: `internal/domain/triangle/compare.go` (`lossRatio` = latest diagonal
  over total earned premium, used for both sides); wording in
  `internal/application/realism.go`.
- The generated incurred triangle is fully developed - every claim runs to
  closure, so its latest diagonal is a true ultimate. Reference triangles are
  ragged: their latest diagonal for recent origins is immature case-incurred
  excluding IBNR. The reference band is therefore biased low relative to true
  ultimates, and the metric is not the "ultimate loss ratio" the docs call it.
- Action: restrict the reference loss ratio to mature origins (those with all
  ten development years), or chain-ladder-complete the reference diagonals
  first, and rename or document the metric accordingly.

### SL-6 (medium) - development beyond age 10 is folded into the last column, unlike Schedule P

- Where: `internal/domain/triangle/triangle.go`, `aggregate` (`dev >= devs`
  clamps to `devs-1`).
- Generated payments and especially post-close recoveries (subrogation has a
  long median lag) landing in development year 11+ are added to column 10,
  whereas Schedule P triangles never observe post-age-10 development. This
  biases the generated age 9-10 factor inside the realism comparison itself.
- Done already: the UI tooltip now reads "dev 10+". The model change remains.
- Action: drop transactions with `dev >= devs` when building the realism
  triangles (matching Schedule P censoring), keeping the fold-in only where a
  lifetime-total view is wanted.

### SL-7 (medium) - case estimates re-centre on the true ultimate at the first revision

- Where: `internal/domain/transaction/runoff.go`, `runEpisode`;
  `case_adequacy_mean: 1.0` in `internal/infrastructure/config/motor-personal.yaml`.
- Every revision targets `(ultimate - paid)` times mean-one lognormal noise
  whose sigma decays with age. Even with a case adequacy mean away from 1, the
  first revision (Poisson, about 4/yr) snaps the case to an unbiased view of the
  truth. Real incurred triangles show persistent case strengthening or
  weakening that IBNER methods are built to detect; here incurred is unbiased at
  every age, so incurred-based methods look trivially perfect in demos.
- Done already: documented in the README and in a code comment. The model change
  remains.
- Action: let the adequacy bias decay gradually over the claim's life instead of
  vanishing at the first revision.

### MF-3 (medium) - sub-blocks must validate even when switched off

- Where: `internal/domain/lob/lob.go`, `RecoveryTypeParams.validate` and
  `SeverityParams.validate`.
- A recovery type with `probability: 0` still has to supply a `mean_share` in
  the open interval (0, 1), a positive concentration and a positive lag median.
  A new-class YAML author is forced to invent parameters for features they
  turned off.
- Action: skip validation of a sub-block whose probability or weight is 0.

### SL-13 (low) - reopen probability is uniform, and a reopened nil claim always converts to a paying claim

- Where: `internal/domain/claim/reopen.go` (pure Bernoulli, no dependence on
  claim type or size), `internal/domain/transaction/runoff.go` (the second
  episode is never nil).
- In reality reopen propensity correlates with claim type and size (third-party
  injury reopens more than own damage). And because the reopen episode always
  pays, every reopened nil claim ends up paying while still counting in the
  summary's "Nil claims" column, whose UI label does not carry the "at first
  close" nuance the `YearSummary` doc comment states.
- Action: consider claim-type-dependent reopen probability when a second line of
  business arrives, plus a UI tooltip clarifying the nil count.

### SL-14 (low) - recovery lags are anchored to the final close

- Where: `internal/domain/transaction/recovery.go` (`c.CloseDate.AddDays(lag)`,
  the final close).
- For a reopened claim, salvage realistically follows the *first* close - the
  wreck is sold once the vehicle claim settles, possibly before the reopen.
  Minor realism point; the "recoveries strictly after close" invariant would
  need rethinking if changed.

### SL-16 (low) - the degenerate interim-payment fallback discards the whole payment plan

- Where: `internal/domain/transaction/runoff.go`, `drawInterimPayments`.
- If rounding pushes the interim payments' sum to at least the ultimate, the
  function returns nil and everything settles at close; trimming the last
  payment would preserve the payment pattern. In practice this can only fire for
  cent-scale ultimates (the ultimate is floored at one cent), so it is cosmetic.

## Stage isolation

Goal: the three generated datasets - policy (book), claims, and transactions
(runoff plus recoveries) - should share as little parameter and code context as
possible. Each stage should own its own knobs, and cross-stage dependencies
should be removed or made explicit through a narrow contract rather than
implicit reach-through.

Each finding is tagged by **type** - `config coupling` (a stage reads another
stage's parameters), `logic duplication` (the same model exists twice and must
be kept in sync), `data smuggling` (a value rides on a struct across a boundary
that does not need it), `namespacing` (a parameter lives under the wrong stage)
- and by **nature**: `removable` (incidental, can be cut) or `inherent` (the
domain genuinely links the stages, so the best you can do is make the link
explicit and single-sourced).

Already well isolated, and not to be regressed: randomness (every stage draws
from its own labelled sub-stream, and below that per-entity streams keyed on
IDs) and the per-stage sub-configs of `LineOfBusiness`. The read-side joins in
analytics (`internal/domain/triangle`, `internal/application/summary.go`,
`histogram.go`) legitimately span all three datasets - that is read-only
aggregation over generated data, not generation-time coupling, and needs no
change.

### F2 (logic duplication, removable) - the inflation compounding rule is written twice

- Where: `internal/domain/policy/book.go` (`inflation := math.Pow(s.pricing.InflationMean, float64(y))`)
  and `internal/domain/claim/inflation.go` (`NewInflationIndex` builds the
  stochastic path).
- The *shared parameter* problem is gone: since the independent pricing basis
  landed, pricing reads its own assumed `pricing.inflation_mean` and the claims
  stage reads the true `claims.inflation`, and they are *meant* to differ. What
  remains is that both files hard-code `Mean^y` compounding independently, so a
  change to how inflation compounds must be made in both.
- Action: give the inflation model one type that yields both an expected factor
  and a sampled factor for a given year, and have both stages consume it.

### F3 (config coupling, inherent) - the claims stage reaches into a book parameter

- Where: `internal/application/generate.go`
  (`.WithBaseYear(req.LOB.Book.SumInsuredInflation, req.StartYear)`) and
  `internal/domain/claim/claim.go` (`WithBaseYear`, `baseSumInsured`).
- The claims stage needs the *book* knob `sum_insured_inflation` to interpret
  the sum insured it reads off each policy, so claims severity is coupled to how
  the book drifts sums insured. The drift rate is defined once but is
  load-bearing in two stages.
- Action: move the base-year concept into the policy record - have the book
  stage store a `BaseYearSumInsured` (or an underwriting-year offset) on each
  `Policy`, computed where the drift rate already lives. The claims stage then
  reads a self-describing field and never needs the book's drift rate.

### F4 (namespacing, removable) - recovery parameters sit under claims but drive the transaction stage

- Where: `internal/application/generate.go`
  (`transaction.NewRecoverySimulator(req.LOB.Claims.Recoveries)`);
  `RecoveryParams` inside `ClaimParams` in `internal/domain/lob/lob.go`; YAML
  key `claims.recoveries`.
- Recoveries are produced by the transaction stage, so the transaction stage
  reaches into the claims config block. It blurs ownership and makes the claims
  parameter surface look larger than the claims stage actually uses.
- Action: move `Recoveries` out of `ClaimParams` - into `RunoffParams` or a new
  sibling group - and mirror the move in `motor-personal.yaml` and the config
  DTO.

### F5 (config coupling plus data smuggling, partly inherent) - nil and reopening are claims knobs but drive the transaction lifecycle

- Where: `NilProbability` and `Reopening` live under `ClaimParams`; the claims
  stage sets `Claim.Nil` and the reopen fields, and
  `internal/domain/transaction/runoff.go` branches on `c.Nil`,
  `c.FirstCloseDate`, `c.ReopenDate` and `c.ReopenEstimate`.
- The decisions are made in the claims stage but their entire effect is a
  transaction-stage lifecycle (extra episodes, zero-payment closes), which makes
  "what does the transaction stage depend on" hard to answer.
- The pricing sub-point is already resolved: pricing no longer reads
  `claims.reopening` for its uplift, it has its own assumed
  `pricing.reopen_probability` and `pricing.reopen_estimate_factor`.
- Action: decide the canonical owner of the claim lifecycle. The cleaner split
  is claims owns occurrence, report and severity; the runoff stage owns the
  lifecycle (nil, close timing, reopen) and draws those decisions itself, which
  would move both knobs into the transaction config and drop the fields from
  `Claim`. If that is too invasive for the reproducibility layering, the lighter
  step is to regroup them as explicitly lifecycle knobs.

### F6 (data smuggling plus stale comment, removable) - `Claim.RiskFactor` is claims-internal

- Where: `internal/domain/claim/claim.go:26-27` - the field is commented
  "carried from the policy for downstream stages". It is set from
  `pol.RiskFactor` and read only by the close-lag draw in `claim.go` and the
  reopen close-lag draw in `reopen.go`. No transaction-stage file reads it
  (`runoff.go` and `recovery.go` never reference it - confirmed again
  2026-08-09).
- The comment implies a policy-to-transaction link that does not exist, and the
  field is neither written to CSV nor read outside the claims stage.
- Action: drop `RiskFactor` from the exported `Claim` struct and pass the value
  to `drawCloseLag` at draw time (threading it through an unexported structure
  for the reopen pass). At minimum, correct the comment.

### F7 (data coupling, inherent) - `Claim.OwnDamage` crosses claim to transaction

- Where: set by the severity mixture in `internal/domain/claim/claim.go`, read
  by `internal/domain/transaction/recovery.go` (`if !c.OwnDamage || paid <= 0`).
- Only the severity draw knows the claim type and recovery eligibility genuinely
  depends on it, so this is a real hand-off that cannot be removed without
  re-deriving the classification.
- Action: keep it, but make it explicit - a named `ClaimKind` value rather than
  a bare bool, documented in `docs/detailed-architecture.md` as the one intended
  claim-to-transaction data dependency alongside the initial estimate and dates.

### F8 (structural, removable) - stage constructors receive whole parameter blocks

- Where: every stage constructor takes a whole domain sub-struct by value.
  Nothing structurally stops a stage from reading fields outside its concern.
- Without an enforced boundary, reach-through will creep back in even after
  F2-F5 are fixed. The independent pricing basis shows the fix working on one
  stage: `NewBookSimulator(book, pricing)` takes a purpose-built `PricingParams`
  instead of the whole `ClaimParams`, so it structurally cannot read claims
  knobs.
- Action: give each remaining stage a narrow, purpose-built input - a small
  per-stage config struct or an interface exposing only what it needs. Keep
  `LineOfBusiness` as the aggregate the config layer produces, and map it to
  per-stage inputs at the application boundary in `generate.go`.

## Security and hardening

Overall risk rating from the 2026-07-22 review: **low**, and that rating depends
on the tool staying loopback-only and single-user. If the UI is ever exposed
beyond localhost, M1 and L1 escalate to high or critical and I1 becomes a real
information leak. Treat the roadmap's "open the tool to the wider community"
step as a security milestone: authentication, path confinement, parameter and
resource limits before any non-loopback deployment.

### M1 (low; high if exposed) - unbounded output path in the generate endpoint

- Where: `internal/infrastructure/web/server.go`, `handleGenerate` and
  `ServeHTTP`; `internal/infrastructure/csv/writer.go`, `WriteDataset`.
- `out_dir` is taken verbatim from the request body, resolved with
  `filepath.Abs`, and passed to a writer that calls `os.MkdirAll` and
  `os.Create`. There is no base-directory confinement and no rejection of
  absolute paths or `..` escapes, so the endpoint can create directories and
  overwrite those three fixed filenames anywhere the process user can write.
- The CSRF guard also trusts an absent Origin
  (`origin != "" && !localOrigin(origin)`), so any local non-browser process can
  drive file writes.
- Action: confine `out_dir` to a server-configured base directory, reject
  absolute paths and paths that escape the base after `filepath.Clean`, and stop
  treating an empty Origin as trusted for state-changing requests. Keep the
  loopback bind.

### L1 (low) - no bounds on numeric run parameters

- Where: `internal/infrastructure/web/server.go`, `handleGenerate`.
- `StartYear`, `Years` and `InitialBookSize` go straight into
  `application.GenerateDataset`, which validates only lower bounds. Book size
  compounds by the growth factor per year, so a large `years` or
  `initial_book_size` drives unbounded memory, CPU and disk - a local denial of
  service. The 1 MB body cap does not help, since a small body can request a
  huge run.
- Action: validate sane upper bounds in the handler before generation, mirrored
  as `max=` on the UI inputs.

### L2 (low) - agent-artifact ignore rules are not in the tracked gitignore

- Where: `.gitignore`.
- The tracked `.gitignore` covers only `/output/`, `/claimsgen`, `*.exe`,
  `tools/screenshots/node_modules/` and `tools/screenshots/package-lock.json`.
  It does not cover `.claude/` or `.superpowers/`, so a contributor running
  Claude Code in a fresh clone would generate session artifacts (prompts,
  scheduled-task metadata, agent memory) that are not ignored. No such artifact
  is currently tracked - this is preventive.
- Note (2026-08-09): the 2026-07-22 review recorded that local
  `.git/info/exclude` rules covered these paths on that machine. The working
  copy today has no local exclude rules at all and no `.claude/` or
  `.superpowers/` directory, so the tracked `.gitignore` is the only protection
  there is.
- Action: add `.claude/` and `.superpowers/` (or the specific artifact patterns)
  to the committed `.gitignore`.

### L3 (low) - unpinned screenshot tool dependency, no committed lockfile

- Where: `tools/screenshots/package.json` (`puppeteer-core: ^24.0.0`),
  `.gitignore` (excludes `tools/screenshots/package-lock.json`).
- With no committed lockfile, `npm install` resolves a floating version with no
  integrity pinning, so the dev tool is non-reproducible and exposed to a
  malicious minor or patch release of puppeteer-core or its transitive tree.
  This is the one supply-chain weakness in the repo; blast radius is a developer
  machine, since the tool is manually run and launches a local Chrome.
- Action: commit `tools/screenshots/package-lock.json` (remove it from
  `.gitignore`) and pin `puppeteer-core` to an exact version.

### I1 (info) - error responses leak local filesystem paths

- Where: `internal/infrastructure/web/server.go`, `writeError` and its
  filesystem-error callers.
- `err.Error()` is returned directly to the client for config-parse and
  filesystem errors, which can include absolute paths from `os.MkdirAll` and
  `os.Create`. Low impact on a local single-user tool, but unnecessary detail in
  an API response.
- Action: return generic messages for internal errors and log the detail
  server-side.

### I2 (info) - no automated vulnerability scanning or CI

- Where: repo-wide. There is no `.github/workflows` and no evidence of
  `govulncheck` or dependency scanning. `AGENTS.md` documents `go test` and
  `go vet` as the gate, which does not catch known-vulnerable dependencies.
- Action: add `govulncheck ./...` to the local workflow, and to CI when CI is
  introduced (see R-13).

### I3 (info) - CSV formula-injection invariant, safe today

- Where: `internal/infrastructure/csv/writer.go`.
- The writer documents that every emitted field is numeric, an ISO-8601 date, or
  a fixed enum, so no field can contain a comma, newline or spreadsheet formula
  lead character, and plain `fmt.Sprintf` is safe. Correct today; the risk is
  future drift.
- Action: keep the note, and switch to `encoding/csv` with formula-lead-character
  escaping the moment any free-text column (a claim description, a class name)
  is introduced.

## Robustness and tooling

### R-1 (medium) - concurrent generate requests can silently corrupt CSV output

- Where: `internal/infrastructure/web/server.go`, `handleGenerate` (no
  synchronization); `internal/infrastructure/csv/writer.go`, `writeFile`
  (truncate then buffer-write).
- Two simultaneous POSTs to `/api/generate` with the same `out_dir` - two tabs,
  or a script - interleave writes to the same three files while both get 200
  responses.
- Action: serialize generation with a mutex on `Server`, write to a temp dir and
  rename, or reject overlapping runs with 409.

### R-2 (medium) - no cancellation for long runs

- Where: `internal/infrastructure/web/server.go`, `handleGenerate`;
  `internal/application/generate.go`.
- The handler ignores `r.Context()`, so a run cannot be cancelled and the UI
  shows "Generating…" forever. Pairs with L1 (missing input caps).
- Action: plumb the request context into generation, and cap the inputs per L1.

### R-4 (medium) - no progress or cancel in the UI; failed runs leave stale results rendered

- Where: `internal/infrastructure/web/static/app.js`.
- Feedback during generation is only the disabled button label: no elapsed-time
  hint, no cancel, no fetch timeout. After an error, the previous run's results
  remain fully rendered beneath the banner, next to the new parameters.
- Action: add an AbortController-backed cancel, an elapsed-time indicator, and
  dim or mark results as stale while generating or after an error.

### R-13 (low) - no CI configuration

- Where: repo root - no `.github/` or other CI config.
- `go test ./...` and `go vet ./...` run only when someone remembers.
- Action: add a minimal CI workflow before opening the tool to the wider
  community, and consider `golangci-lint` and `govulncheck` (I2) in the same
  pass.

### R-14 (low) - server hardening gaps acceptable today

- Where: `cmd/claimsgen/main.go` (`http.Serve` with the default server, so no
  read or write timeouts); `internal/infrastructure/web/server.go`
  (`handleGenerate` writes CSVs to any absolute path the browser sends - see
  M1).
- Both are acceptable for a loopback-only local app: the Host and Origin guards
  mean only a local page can trigger generation.
- Action: if the app is ever distributed more widely or binds beyond 127.0.0.1,
  use an `http.Server` with timeouts and constrain `out_dir` to a base
  directory.

## Refactors

### RF-13 (medium) - adding one line-of-business parameter touches five places

- Where: the domain struct plus validation (`internal/domain/lob/lob.go`), the
  config DTO plus `ToDomain` (`internal/infrastructure/config/config.go`), the
  preset YAML (`internal/infrastructure/config/motor-personal.yaml`), and the UI
  form metadata (`internal/infrastructure/web/static/app.js`, where labels and
  tips restate the YAML comments by hand).
- This is the main friction for the roadmap's second line of business. The
  2026-07-27 independent-pricing-basis feature added the `pricing` block through
  exactly this fan-out - a fresh instance, not a historical one. See also F8,
  which it partly demonstrates the fix for.
- Action, in increasing order of ambition: (1) document the checklist in a short
  "adding a parameter" note; (2) serve the form metadata from the server - a
  small registry of label, tip and group per field would let `app.js` build the
  form generically and remove the JS-side duplication and its drift risk against
  the YAML comments; (3) revisit whether the DTO layer pays its way - the
  mirrored structs keep the domain tag-free, a legitimate choice, but if
  `ToDomain` keeps growing, consider code generation or accepting yaml/json tags
  on the `lob` package.

### RF-14 (medium) - the `Claim` struct is accumulating pipeline-carry fields

- Where: `internal/domain/claim/claim.go` - `RiskFactor`, `Nil`, `OwnDamage`,
  `FirstCloseDate`, `ReopenDate` and `ReopenEstimate`, each annotated "carried
  to the runoff/recovery stage but never written to CSV".
- Every future feature (a valuation-date extract, per-claim-type behavior for
  new lines) will want more such fields.
- Action: split the persisted claim record from a development-context struct
  passed between stages, so the CSV surface is explicit in the type system
  instead of a comment convention. Overlaps F5, F6 and F7.

### RF-1 (medium) - triangles are computed twice per generate request

- Where: `internal/infrastructure/web/viewmodel.go` builds the display triangles
  by calling the domain triangle package directly, then
  `internal/application/realism.go` recomputes the same two triangles over all
  transactions. `developmentYears = 10` is duplicated in both files.
- Half the aggregation work per request is wasted on large runs, the two
  constants can drift silently, and triangle business rules end up split between
  layers - summary, distributions and realism all correctly route through
  application use cases; triangles are the exception.
- Action: extract an application use case that returns triangles plus the
  realism report once, and delete the duplicate constant. Longer term, the
  development-year depth should come from the reference sets themselves rather
  than a constant, so a longer-tailed vintage is not silently truncated.

### RF-4 (low) - `runEpisode` carries two boolean mode flags and duplicates the revision-target logic

- Where: `internal/domain/transaction/runoff.go`.
- The nil branch repeats the sigma-decay and target computation of the main loop
  with a different remaining-source and a different floor rule (`floorRevisions`
  versus always-floor). A `remaining()` closure plus one unconditional
  keep-open floor removes the duplication and one boolean parameter, and eases
  adding episode types for long-tail lines.

### RF-5 (low) - hardcoded term and calendar constants that should be parameters

- Where: `internal/domain/policy/book.go` (364-day term),
  `internal/domain/transaction/runoff.go` (365 divisor).
- The 12-month term is baked in; commercial property or long-tail liability may
  need different terms.
- Action: promote term length to `BookParams`.

### RF-7 (low) - run defaults duplicated in three places

- Where: `cmd/claimsgen/main.go` (the usage text and the flag defaults) and
  `internal/infrastructure/web/static/index.html` (input values).
- 1998 / 10 / 20000 / seed 1 are hand-maintained in all three.
- Action: define the defaults once and have the UI fetch them (extend
  `/api/lobs` or add `/api/defaults`).

### RF-9 (low) - app.js is a 500-line script with top-level side effects and no tests

- Where: `internal/infrastructure/web/static/app.js`.
- Form wiring, state, rendering and SVG drawing live in one file that executes
  on load and exports nothing. Untested front-end JS is acceptable at this size
  (the numeric logic is server-side), but `renderTriangles`, `histogramCard` and
  `bandCard` are already pure enough to move into an ES module imported by a
  thin bootstrap, enabling DOM tests as it grows.

### RF-10 (low) - small duplications worth consolidating

The band min/max accumulation and the `MotorPersonal`/`Preset` load path were
consolidated on 2026-07-27. Still outstanding:

- Occurrence-year map building: `internal/application/summary.go` versus
  `internal/domain/triangle/triangle.go`.
- First-close derivation (`c.CloseDate` unless reopened):
  `internal/domain/transaction/runoff.go` and
  `internal/application/invariants_test.go` - deserves a
  `Claim.EffectiveFirstClose()` method.
- Duplicate CLI tests: two cases in `cmd/claimsgen/main_test.go` assert the same
  unknown-command behavior; merge them.
- Paid-by-claim maps are hand-rolled in
  `internal/domain/transaction/recovery.go` and
  `internal/application/histogram.go`; a `transaction.PaidByClaim(txs)` helper
  would remove both loops.
- The "lognormal lag with median m, rounded, floored at 1 day" pattern appears
  three times (`claim.go` without the floor, `reopen.go`, `recovery.go`); a
  shared `drawLagDays(src, median, sigma, min)` would name the concept.

### RF-11 (low) - `RecoveryParams` hardcodes exactly two named types

- Where: `internal/domain/transaction/recovery.go`,
  `internal/domain/lob/lob.go`.
- A named list of recovery types would generalize better to other lines of
  business. Pairs with F4 (where the parameters should live).

## Documentation

### D-1 (low) - the UI screenshots predate the pricing and windowing work

- Where: `docs/screenshots/`, embedded in `README.md`.
- The images were last regenerated before target-loss-ratio pricing (MF-1) and
  claim windowing (MF-2), so they still show both fixed defects: a header claim
  count that disagrees with the summary total (27,823 versus 26,150) and
  per-year loss ratios climbing from 0.693 to 1.010. A current run of the same
  defaults gives 26,040 claims in both places and loss ratios from 0.662 to
  0.752. The sidebar also predates the Pricing parameter group, and the realism
  tab predates the loss-ratio-drift card.
- Action: regenerate with `tools/screenshots` (start the UI on port 8093,
  `npm install`, `node screenshots.js`; needs a local Chrome, `CHROME_PATH` to
  override the location).

### D-2 (low) - the README's UI description omits the Pricing parameter group

- Where: `README.md`, the "Browser UI" section.
- It lists the Recoveries group and the reopening knobs but not the Pricing
  group added with the independent pricing basis.
- Action: one sentence, best done alongside D-1.

## Invariants not to regress

Verified sound by both reviews and worth protecting in any of the work above:

- **Reproducibility.** SHA-256-keyed labelled sub-streams make every draw depend
  only on the seed and the label path; splits are provably independent of parent
  draw count, and draw counts are held constant across knob toggles. The
  recoveries and reopening no-shift tests prove this draw for draw.
- **Transaction accounting by construction.** The emitter makes outstanding-case
  bookkeeping and paid-equals-ultimate exact in integer cents;
  `internal/application/invariants_test.go` validates the full ledger as a state
  machine (referential integrity, date ordering, case never negative, zero at
  close, nil and reopen sequencing, recovery bounds).
- **Distribution parameterizations.** Mean-one lognormal via the -sigma^2/2
  adjustment, mean-one gamma via shape 1/sigma^2, the case adequacy mu
  adjustment, Beta mean/concentration form, and Pareto alpha > 1 enforced for a
  finite mean.
- **Loopback-only security posture.** 127.0.0.1 bind, Host and Origin checks
  against DNS rebinding and CSRF, `MaxBytesReader`, `DisallowUnknownFields` on
  JSON and `KnownFields(true)` on YAML, an embedded `fs.Sub` static tree with no
  path traversal, and a front end that builds DOM exclusively via `textContent`
  and `createElementNS` - no XSS sink.
- **No secrets, no PII.** The reference data is public NAIC Schedule P aggregate
  triangles keyed by company code; the generated output is fully synthetic.
- **Two direct dependencies**, both current: `gonum.org/v1/gonum` and
  `gopkg.in/yaml.v3`, with `go.sum` pinning hashes.

## Resolved, for provenance

Closed before this file was created; full text and the measurements behind each
are in git history.

- **2026-07-18 cleanup pass** (commit `ce5c013`): the batch of simple
  documentation-accuracy, defensive-guard, naming and test-hardening findings.
- **2026-07-27**: **SL-1** (P5-P95 percentile bands plus the `usableRefs`
  filter), **MF-1** (premium priced to a target loss ratio, plus the
  loss-ratio-drift gate), **MF-7** (`target_loss_ratio` knob), **SL-3** (own
  damage capped at sum insured), **SL-4** (own-damage trend rebased to base-year
  sum insured), **SL-5** (nil Bernoulli always drawn; recovery types split into
  their own sub-streams), **MF-2** (claim occurrences windowed to the run
  period), **D-3** (window and close-lag tests), **F1** (premium priced from an
  independent `PricingParams` basis), and parts of **RF-10**.
