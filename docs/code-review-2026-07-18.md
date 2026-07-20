# Code review - 2026-07-18

Full-solution review of claimsgen at commit `2889b09` (branch `main`) against the mission in `docs/mission.md`: generate realistic, fully synthetic insurance claims data as dummy input to reserving processes. Scope: all Go source, the embedded preset, the web UI and static front end, the CSV and Schedule P infrastructure, the screenshot tooling, the test suite, and the docs. This is a review only - no source changes were made.

Note: an earlier review of commit `71b5f5f` (before the two-vintage reference pool landed) was found untracked at this path. Its unique findings were re-verified against the current code and merged into this document (appended at the end of each section, marked "merged from the earlier review"), and the old file was then removed. This document supersedes it.

Baseline: `go vet ./...` is clean and `go test ./...` passes on every package. Findings were verified against the actual code; the realism band and loss ratio figures below were measured empirically on the embedded reference data and the default run (seed 1, 1998, 10 years, 20,000 policies).

**Update 2026-07-18:** the simple-to-resolve findings were addressed in a single cleanup pass (commit `ce5c013`; design in `docs/superpowers/specs/2026-07-18-review-cleanup-design.md`) and removed from this document. Findings that were only partially addressed have been trimmed to the remaining work, with a note on what was done. What remains below is the substantive backlog: the two high-severity findings, the simulation-model changes, the larger refactors, the heavier server guardrails, and CI.

Severity scale: **high** - materially undermines the mission, fix before building on top; **medium** - worth addressing soon; **low** - fix when touching the area.

## Summary

The codebase is in very good shape. The transaction accounting invariants the README claims (outstanding case is the running sum of ESTIMATE rows, exactly zero at close, total paid equals ultimate to the cent, recoveries strictly after final close and strictly below gross paid, nil claims pay nothing in their first episode) all hold by construction in the emitter design and are enforced by a strong end-to-end invariant test. The labelled sub-stream RNG architecture genuinely delivers the reproducibility contract. Layering is clean: the domain imports nothing outward, and the CLI and web UI share the same use cases with byte-identical CSV output (tested).

Two high-severity findings, both about the gap between what the realism story promises and what it delivers:

1. **The realism gate is near-vacuous** (SL-1). Bands are the raw min/max across all 289 reference companies with no volume or quality filter, so degenerate companies set the bounds. Measured: the loss ratio band is [0.00, 1.48] and the paid age 1-2 band is [0.71, 37.5]. A run with a 5% or 140% loss ratio still passes.
2. **The default preset's loss ratio drifts from 69% to 101% across the ten years** (MF-1), because premiums grow at 3%/yr (sum insured inflation) while own-damage losses grow at roughly 7%/yr (sum insured inflation compounded with claims inflation) and the premium rate factor never responds. The pooled-book realism check cannot see this; any actuary computing accident-year loss ratios in a demo will notice it immediately.

The remaining mediums cluster into three themes: the severity and inflation model has internal inconsistencies (uncapped own damage, double-counted own-damage trend, the nil draw breaking knob isolation), the triangle comparison differs from Schedule P in two definitional ways (post-age-10 development folded into the last column, immature reference diagonals treated as ultimates), and the web server lacks the heavier guardrails (no concurrency control, no input bounds or cancellation).

Findings are labelled SL (simulation logic), MF (mission fit), R (robustness outside the simulation), RF (refactoring), and D (documentation and tests), ranked by severity within each section; items merged from the earlier review are appended at the end of their section regardless of severity and marked as such. Suggested priorities are at the end.

## Simulation logic

### SL-1 (high) - min/max realism bands over unfiltered companies make several checks near-vacuous

- Where: `internal/domain/triangle/compare.go:35-54` (ATA bands), `internal/domain/triangle/compare.go:119-131` (loss ratio band).
- Bands are the raw min and max across every reference company, with no volume or quality filter, so a single tiny or degenerate company sets the bound. Measured on the embedded data: loss ratio band [0.0000, 1.4762] (the min set by dec2025/11460, a company with near-zero incurred); paid ATA age 1-2 band [0.714, 37.5] (the max set by sep2011/18309); incurred ATA ages 1-2, 3-4, and 6-7 have band min 0.0000. Only the late ages genuinely constrain (paid age 8-9 is [0.960, 1.083]). The sep2011 vintage also contains all-zero rows (e.g. sep2011/10007, years 1988-1993).
- Impact: "realistic" per the gate means "not wilder than the wildest of 289 companies". Users may over-trust the green banner, and `TestDefaultPresetIsRealistic` constrains much less than it appears to.
- Action: filter reference companies below a minimum earned premium or non-zero-triangle threshold, and replace min/max with percentile bands (for example 5th-95th), optionally keeping min/max as a secondary hard band. At minimum, label the UI wording "min/max observed".

### SL-2 (medium) - reference and generated "ultimate loss ratio" are definitionally different quantities

- Where: `internal/domain/triangle/compare.go:150-164` (`lossRatio` = latest diagonal over total earned premium, used for both sides), doc wording in `internal/application/realism.go`.
- The generated incurred triangle is fully developed (every claim runs to closure, so the latest diagonal is true ultimate), while reference triangles are ragged: their latest diagonal for recent origins is immature case-incurred excluding IBNR. The reference band is therefore biased low relative to true ultimates, and the metric is not the "ultimate loss ratio" the docs claim.
- Action: restrict the reference loss ratio to mature origins (those with all ten development years), or chain-ladder-complete the reference diagonals first, and rename or document the metric accordingly.

### SL-3 (medium) - own-damage severity is uncapped at the sum insured

- Where: `internal/domain/claim/claim.go:131-138` (`drawGroundUpLoss`).
- The own-damage loss is sum insured times an uncapped lognormal fraction. With the preset's `own_damage_median_fraction: 0.12` and `own_damage_sigma: 1.0`, about 1.7% of own-damage claims draw a fraction above 1, and claims inflation pushes that higher in later years - producing own-damage claims paying several times the vehicle's insured value, then earning salvage and subrogation on top. Real own-damage claims cap at sum insured less excess (a total loss), which is exactly the salvage-eligible case.
- Action: cap the own-damage ground-up loss at the (inflated) sum insured, or add a per-LOB cap parameter; consider flagging capped claims as total losses for the salvage model.

### SL-4 (medium) - claims inflation double-counts price drift for the own-damage component

- Where: `internal/domain/claim/claim.go:105-106` and `internal/domain/policy/book.go:52`.
- Own-damage losses are proportional to a sum insured that already drifts by `sum_insured_inflation` per underwriting year, and are then multiplied again by the occurrence-year claims inflation index, while third-party losses (fixed Pareto scale) get only the claims index. With the preset (1.03 and 1.04) the own-damage severity trend is about 7.1%/yr but the third-party trend is 4%/yr - anyone calibrating "claims inflation" from the YAML will misread the own-damage trend in the triangles. This is also the driver of MF-1.
- Progress (2026-07-18): the doc part is done - the README and a code comment now state the own-damage trend is the product of the two knobs. The model rebase remains.
- Action: rebase the own-damage component (for example apply claims inflation only to third party, or express own damage in start-year sum insured terms) so the trend is not the silent product of two knobs.

### SL-5 (medium) - the nil-claim draw breaks the knob-isolation property the other stages deliberately maintain

- Where: `internal/domain/claim/claim.go:114` (conditional Bernoulli), `internal/domain/claim/claim.go:73-79` (claims share one per-policy stream); same pattern at `internal/domain/transaction/recovery.go:96-98` (salvage short-circuit).
- All claims on a policy draw sequentially from one per-policy stream, and the nil Bernoulli is only drawn when `NilProbability > 0`, so toggling nil claims on consumes an extra uniform per claim and reshuffles the dates and severities of every subsequent claim on the same policy. This contradicts the design contract that reopening (`internal/domain/claim/reopen.go:12-14`) and recoveries (`internal/domain/transaction/recovery.go:38-40`) uphold via per-claim labelled splits. Similarly, disabling salvage alone changes the same claim's subrogation outcome; `TestRecoveriesDoNotShiftOtherStages` only tests both recovery types off together.
- Action: draw each claim from its own labelled sub-stream, or move the nil draw to a labelled post-pass like the reopen simulator; always consume the salvage draw. Document which knobs are shift-free.

### SL-6 (medium) - development beyond age 10 is folded into the last column, unlike Schedule P

- Where: `internal/domain/triangle/triangle.go:87-89` (`dev >= devs` clamps to `devs-1`).
- Generated payments and especially post-close recoveries (subrogation has a long median lag) landing in development year 11+ are added to column 10, whereas Schedule P triangles never observe post-age-10 development. This biases the generated age 9-10 factor inside the realism comparison itself.
- Progress (2026-07-18): the UI tooltip mislabel is fixed - the last display column now reads "dev 10+". The triangle-censoring model change remains.
- Action: drop transactions with `dev >= devs` when building the realism triangles (matching Schedule P censoring), keeping the fold-in only where a lifetime-total view is wanted.

### SL-7 (medium) - case estimates re-centre on the true ultimate at the first revision, so incurred development carries almost no systematic signal

- Where: `internal/domain/transaction/runoff.go:136-142`; `case_adequacy_mean: 1.0` in `internal/infrastructure/config/motor-personal.yaml:75`.
- Every revision targets `(ultimate - paid)` times mean-one lognormal noise whose sigma decays with age. Even with a case adequacy mean away from 1, the first revision (Poisson, about 4/yr) snaps the case to an unbiased view of the truth. Real incurred triangles show persistent case strengthening or weakening that IBNER methods are built to detect; here incurred is unbiased at every age, so incurred-based methods will look trivially perfect in demos.
- Progress (2026-07-18): documented (README note + a code comment). The model change remains.
- Action: let the adequacy bias decay gradually over the claim's life instead of vanishing at the first revision.

### SL-13 (low) - reopen probability is uniform across claims, and a reopened nil claim always converts to a paying claim (merged from the earlier review)

- Where: `internal/domain/claim/reopen.go:34` (pure Bernoulli, no dependence on claim type or size), `internal/domain/transaction/runoff.go:85` (the second episode is never nil).
- In reality reopen propensity correlates with claim type and size (third-party injury reopens more than own damage). And because the reopen episode always pays, every reopened nil claim ends up paying while still counting in the summary's "Nil claims" column, whose UI label does not carry the "at first close" nuance the `YearSummary` doc comment states.
- Action: consider claim-type-dependent reopen probability when a second line of business arrives, and a UI tooltip clarifying the nil count.

### SL-14 (low) - recovery lags are anchored to the final close (merged from the earlier review)

- Where: `internal/domain/transaction/recovery.go:114` (`c.CloseDate.AddDays(lag)`, the final close).
- For a reopened claim, salvage realistically follows the first close (the wreck is sold once the vehicle claim settles), possibly before the reopen. Minor realism point; the "recoveries strictly after close" invariant would need rethinking if changed.

### SL-16 (low) - the degenerate interim-payment fallback discards the whole payment plan (merged from the earlier review)

- Where: `internal/domain/transaction/runoff.go:189-192`.
- If rounding pushes the interim payments' sum to at least the ultimate, `drawInterimPayments` returns nil and everything settles at close; trimming the last payment would preserve the payment pattern. In practice this can only fire for cent-scale ultimates (the ultimate is floored at one cent, `internal/domain/transaction/runoff.go:151-158`), so it is cosmetic.

## Mission fit

### MF-1 (high) - premium adequacy: the loss ratio deteriorates about 3%/yr with no rate response, and the realism gate cannot see it

- Where: `internal/domain/policy/book.go:79` (premium = sum insured x constant rate x risk factor), `internal/infrastructure/config/motor-personal.yaml:11` (`sum_insured_inflation: 1.03`), `:18` (`premium_rate_factor: 0.035`, constant), `:39` (`inflation.mean: 1.04`), `internal/domain/triangle/compare.go:119-133` (single whole-book loss ratio check).
- Measured on the default run: per-year loss ratios climb monotonically from 69.3% (1998) to 101.0% (2007). Premiums grow with sum insured at 3%/yr while own-damage losses compound both inflation knobs (see SL-4). The gate checks only the pooled ten-year loss ratio against [0, 1.48], so it passes.
- Impact: the mission promises data a team member can "feed into a reserving demo without manual fixes"; a book drifting from profit to 100%+ with no rate action reads as a data artifact.
- Action: apply the same drift to `premium_rate_factor`, or price off inflated expected loss (a target loss ratio knob would do both - see MF-7); add per-year loss ratio to the realism gate; at minimum document the drift in the README.

### MF-2 (medium) - claims.csv contains a partial extra accident year that the summary, triangles, and realism check silently drop

- Where: `internal/domain/claim/claim.go:100` (occurrence uniform over the full 12-month cover, spilling past the run window), `internal/application/summary.go:58` and `:69-71`, `internal/domain/triangle/triangle.go:79-81` (occurrence years outside the window are skipped).
- On the default run the UI header reports 27,823 claims but the summary totals 26,150; the missing 1,673 claims (6%) occur in 2008, outside the ten-year window, yet are written to claims.csv. A user triangulating the CSVs themselves gets an eleventh, partial-exposure accident year the in-app views never showed, and the header count contradicts the summary on screen.
- Action: either stop simulating occurrences past the window (pro-rate the final year's frequency to exposure inside the window, as the design spec's "exposed fraction of the term" wording implied - `docs/superpowers/specs/2026-07-15-claims-generator-design.md:88` vs `internal/domain/claim/claim.go:74`), or document the trailing partial year prominently and include it in the summary.

### MF-3 (medium) - relax validation of sub-blocks whose probability or weight is 0

- Where: `internal/domain/lob/lob.go:264-280` and `:302-318` (recovery and severity sub-blocks must validate even when their probability or weight is 0).
- Progress (2026-07-18): the README overstatement is fixed - the "new short-tail classes need no code changes" sentence now matches the roadmap (CLI is pure YAML, a UI preset needs one registration line). The validation friction remains.
- Action: relax validation of sub-blocks whose probability or weight is 0, so a new-class YAML author is not forced to supply parameters for features they turned off.

### MF-7 (low) - no target loss ratio parameter despite the original idea suggesting one

- Where: `docs/raw user inputs/rough-idea.md:9` ("Consider using pricing loss ratio as a parameter or target") vs `internal/domain/lob/lob.go:31` (only `PremiumRateFactor` exists; the loss ratio is emergent).
- Action: a target loss ratio knob would directly fix MF-1 and match the original intent.

### Verified as delivered (no gap)

- Exposure handling is real: claims occur strictly within cover dates (`internal/application/invariants_test.go:47-49`) and premium is genuinely pro-rated day by day into earned premium (`internal/domain/triangle/triangle.go:102-133`).
- "No valuation date, all claims run to closure" holds everywhere, including reopens (final close) and post-close recoveries.
- Report lags naturally produce roughly 12% same-day reports via rounded lognormal draws - realistic for motor, no artificial zero-inflation.
- Nil claims, recoveries, and reopens behave exactly as the README documents.
- Frequency is Poisson mixed over a gamma risk factor, giving realistic negative-binomial overdispersion.

## Robustness outside the simulation

### R-1 (medium) - concurrent generate requests can silently corrupt CSV output

- Where: `internal/infrastructure/web/server.go:106-132`, `internal/infrastructure/csv/writer.go:54-72`.
- `handleGenerate` has no synchronization and `writeFile` truncates then buffer-writes, so two simultaneous POSTs to `/api/generate` with the same `out_dir` (two tabs, or a script) interleave writes to the same three files while both get 200 responses.
- Action: serialize generation with a mutex in `Server`, or write to a temp dir and rename, or reject overlapping runs with 409.

### R-2 (medium) - no bounds on years or book size and no cancellation lets one request hang or exhaust the server

- Where: `internal/infrastructure/web/server.go:117-122`, `internal/application/generate.go:30-38`, `internal/infrastructure/web/static/index.html:22-23`.
- Validation only enforces lower bounds, and book size compounds by the growth factor per year, so years 100 with growth 1.5 explodes to billions of policies. The handler ignores `r.Context()`, so the run cannot be cancelled; the UI shows "Generating…" forever.
- Action: cap years and book size server-side (mirrored as `max=` on the inputs) and plumb the request context into generation.

### R-4 (medium) - no progress or cancel for long runs; failed runs leave stale results rendered

- Where: `internal/infrastructure/web/static/app.js:176-204`.
- Feedback during generation is only the disabled button label; there is no elapsed-time hint, no cancel, no fetch timeout, and after an error the previous run's results remain fully rendered beneath the banner next to the new parameters.
- Action: add an AbortController-backed cancel, an elapsed-time indicator, and dim or mark the results as stale while generating or after an error.

### R-13 (low) - no CI configuration (merged from the earlier review)

- Where: repo root (no `.github/` or other CI config, verified absent).
- `go test ./...` and `go vet ./...` run only when someone remembers. Before opening the tool to the wider community (roadmap), add a minimal CI workflow and consider `golangci-lint`.

### R-14 (low) - server hardening gaps acceptable today, worth revisiting before wider distribution (merged from the earlier review)

- Where: `cmd/claimsgen/main.go:127` (`http.Serve` with the default server - no read/write timeouts), `internal/infrastructure/web/server.go:113-130` (`handleGenerate` writes CSVs to any absolute path the browser sends).
- Both are acceptable for a loopback-only local app - the Host/Origin guards mean only a local page can trigger generation. If the app is ever distributed more widely or binds beyond 127.0.0.1, use an `http.Server` with timeouts and constrain `out_dir` to a base directory.

## Refactoring

### RF-1 (medium) - triangles are computed twice per generate request, and the web viewmodel bypasses the application layer to do it

- Where: `internal/infrastructure/web/viewmodel.go:106-127` builds the display triangles by calling the domain triangle package directly, then `internal/application/realism.go:13-20` recomputes the same two triangles over all transactions; `developmentYears = 10` is duplicated at `internal/infrastructure/web/viewmodel.go:12` and `internal/application/realism.go:6`.
- Half the aggregation work per request is wasted on large runs, the two constants can drift silently, and triangle business rules end up split between layers (summary, distributions, and realism all correctly route through application use cases; triangles are the exception).
- Action: extract an application use case that returns triangles plus the realism report once, and delete the duplicate constant. Longer term, the development-year depth should come from the reference sets themselves rather than a constant, so a longer-tailed vintage is not silently truncated.

### RF-4 (low) - `runEpisode` carries two boolean mode flags and duplicates the revision-target logic

- Where: `internal/domain/transaction/runoff.go:96-149`.
- The nil branch repeats the sigma-decay and target computation of the main loop with a different remaining-source and a different floor rule (`floorRevisions` vs always-floor). A `remaining()` closure plus one unconditional keep-open floor removes the duplication and one boolean parameter, and eases adding episode types for long-tail lines.
- Progress (2026-07-18): the `event.kind` magic numbers 0/1 are now named constants. The `runEpisode` de-duplication remains.

### RF-5 (low) - hardcoded term and calendar constants that should be parameters

- Where: `internal/domain/policy/book.go:75` (364-day term), `internal/domain/transaction/runoff.go:99` (365 divisor).
- The 12-month term is baked in; commercial property or long-tail liability per the roadmap may need different terms.
- Progress (2026-07-18): the repeated one-cent floor literal is now a single named constant. Promoting term length to `BookParams` remains.
- Action: promote term length to `BookParams`.

### RF-7 (low) - run defaults duplicated in four places

- Where: `cmd/claimsgen/main.go:23-39` (usage text), `:65-69` (flag defaults), `internal/infrastructure/web/static/index.html:20-24` (input values).
- 1998 / 10 / 20000 / seed 1 are hand-maintained in the usage string, the flag definitions, and the HTML. Define defaults once and have the UI fetch them (extend `/api/lobs` or add `/api/defaults`).

### RF-9 (low) - app.js is a 500-line script with top-level side effects and no tests

- Where: `internal/infrastructure/web/static/app.js:495-498`.
- Form wiring, state, rendering, and SVG drawing live in one file that executes on load and exports nothing. The spec accepted untested front-end JS (numeric logic is server-side) and that is fine at the current size, but `renderTriangles`, `histogramCard`, and `bandCard` are already pure enough to move into an ES module imported by a thin bootstrap, enabling DOM tests as it grows.

### RF-10 (low) - small duplications worth consolidating

- Occurrence-year map building: `internal/application/summary.go:55-57` vs `internal/domain/triangle/triangle.go:65-68`.
- Min/max band accumulation: `internal/domain/triangle/compare.go:42-51` vs `:119-131` (an `expand(v)` helper on `Band` serves both).
- First-close derivation (`c.CloseDate` unless reopened): `internal/domain/transaction/runoff.go:75-78` and `internal/application/invariants_test.go:73-76` - deserves a `Claim.EffectiveFirstClose()` method.
- Duplicate CLI tests: `cmd/claimsgen/main_test.go:77-85` and `:101-109` assert the same unknown-command behavior; merge them.
- Paid-by-claim maps are built in `internal/domain/transaction/recovery.go:42-47` and `internal/application/histogram.go:84-89`; a `transaction.PaidByClaim(txs)` helper would remove both hand-rolled loops (merged from the earlier review).
- The "lognormal lag with median m, rounded, floored at 1 day" pattern appears three times (`internal/domain/claim/claim.go:102-103` without the floor, `internal/domain/claim/reopen.go:37-40`, `internal/domain/transaction/recovery.go:102-105`); a shared `drawLagDays(src, median, sigma, min)` would name the concept (merged from the earlier review).
- `config.MotorPersonal()` (`internal/infrastructure/config/config.go:183-186`) loads the YAML independently of `config.Preset("motor-personal")` (`:165`); keep the convenience but implement it as a one-line delegate so there is a single load path (merged from the earlier review).

### RF-11 (low) - `RecoveryParams` hardcodes exactly two named types

- Where: `internal/domain/transaction/recovery.go:87-93`, `internal/domain/lob/lob.go:80-83`.
- A named list of recovery types would generalize better to other lines of business.

### RF-13 (medium) - adding one line-of-business parameter touches five places (merged from the earlier review)

- Where: the domain struct plus validation (`internal/domain/lob/lob.go`), the config DTO plus `ToDomain` (`internal/infrastructure/config/config.go:188` onward), the preset YAML (`internal/infrastructure/config/motor-personal.yaml`), and the UI form metadata (`internal/infrastructure/web/static/app.js:7-62`, where labels and tips restate the YAML comments by hand). The recoveries and reopening features each show this full fan-out in their diffs.
- This is the main friction for the roadmap's second line of business. Suggestions, in increasing order of ambition: (1) document the checklist in a short "adding a parameter" doc; (2) serve the form metadata from the server - a small registry of label/tip/group per field would let app.js build the form generically and eliminate the JS-side duplication and its drift risk against the YAML comments; (3) revisit whether the DTO layer pays its way - the mirrored structs keep the domain tag-free, a legitimate choice, but if the `ToDomain` mapping keeps growing, consider code generation or accepting yaml/json tags on the lob package.

### RF-14 (medium) - the Claim struct is accumulating pipeline-carry fields (merged from the earlier review)

- Where: `internal/domain/claim/claim.go:25-41` - `RiskFactor`, `Nil`, `OwnDamage`, `FirstCloseDate`, `ReopenDate`, and `ReopenEstimate`, each annotated "carried to the runoff/recovery stage but never written to CSV".
- Every future feature (a valuation-date extract, per-claim-type behavior for new lines) will want more such fields. Splitting the persisted claim record from a development-context struct passed between stages would make the CSV surface explicit in the type system instead of a comment convention.

## Documentation and tests

### D-3 (low) - remaining test gaps worth closing

- No test pins the MF-2 behavior (out-of-window occurrences present in the CSVs but absent from summaries and triangles); whichever way that finding is resolved, a test should document the choice (merged from the earlier review).
- No test covers the statistical shape of the close-lag size loading (the step at `SizeThreshold`, `internal/domain/claim/claim.go:143-150`), which calibration for a second class will likely touch (merged from the earlier review).
- Done (2026-07-18): the realism gate now runs on multiple seeds; a sep2011 known-value reader test was added (`StartYear == 1988`); and the two loss ratios are labelled ("gross" in the summary, "net vs Schedule P" in the realism tab).

## Strengths

1. **The RNG architecture is genuinely excellent.** SHA-256-keyed labelled sub-streams (`internal/infrastructure/random/source.go:40-47`) make every draw depend only on the seed and label path, splits are proven independent of parent draw count (`internal/infrastructure/random/source_test.go:32-45`), and the recoveries and reopening no-shift tests prove the knob-isolation property draw for draw rather than asserting it.
2. **Transaction invariants hold by construction, not by patching.** The emitter (`internal/domain/transaction/runoff.go:215-256`) makes outstanding-case bookkeeping and paid-equals-ultimate exact in integer cents, and `internal/application/invariants_test.go` validates the full ledger as a state machine - referential integrity, date ordering, case never negative, zero at close, nil and reopen sequencing, recovery bounds - so it would catch regressions in any of the four simulators.
3. **Distribution parameterizations are consistently correct.** Mean-one lognormal via the -sigma^2/2 adjustment, mean-one gamma via shape 1/sigma^2, the case adequacy mu adjustment, Beta mean/concentration form, and Pareto alpha > 1 enforced for a finite mean.
4. **Security posture is unusually careful for a localhost tool.** Loopback-only bind, Host and Origin checks against DNS rebinding and CSRF (`internal/infrastructure/web/server.go:49-76`, tested), `MaxBytesReader`, and the front end builds DOM exclusively via `textContent`/`createElementNS` - no XSS surface.
5. **Config handling is strict and well tested end to end** (`KnownFields(true)` YAML decoding, field-named validation errors asserted from the CLI, a preset round-trip test), and CLI/web parity is pinned byte-identically (`internal/infrastructure/web/server_test.go:179-207`).
6. **Documentation discipline is rare at this quality.** Specs record rejected alternatives, the roadmap honestly flags its own debt, and the README matched the code on nearly every checkable claim.

## Suggested priorities

1. SL-1 plus MF-1 together: filter and tighten the realism bands, then fix the premium drift (a target loss ratio knob covers MF-7 too). These two make the realism story real.
2. SL-5: close the remaining gap in the reproducibility contract (nil-claim knob isolation) before more features stack on top. (The full-dataset determinism test, RF-2, is done.)
3. SL-3 and SL-4: cap own damage at sum insured and rebase the own-damage inflation component - both distort severity levels and trend calibration. (SL-4's documentation is done; the model rebase remains.)
4. R-1 and R-2: the heavier server guardrails (mutex or 409 on concurrent runs, input caps plus context cancellation) before sharing the UI with the team.
5. MF-2: decide the trailing-accident-year policy so the CSVs and the in-app views agree.
6. RF-13 and RF-14 before starting the second line of business: both the parameter fan-out and the Claim struct's pipeline-carry fields get more expensive with every added parameter and feature.
