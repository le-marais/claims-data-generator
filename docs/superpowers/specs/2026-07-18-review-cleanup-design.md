# Review cleanup design

Date: 2026-07-18
Status: shipped

## Summary

A single focused cleanup pass addressing every *simple-to-resolve* finding from `docs/code-review-2026-07-18.md` in one coordinated change: documentation-accuracy fixes, known-simplification notes, inert defensive guards, small web-server hardening, naming and refactor cleanups, a couple of UI labels, and test hardening. These are the findings that are cheap, low-risk, and largely output-neutral - the kind worth clearing in a batch so the review's remaining substantive work stands out.

Explicitly NOT in scope, because none of these is simple:

- The two highs: SL-1 (filter and tighten the realism bands) and MF-1 (premium adequacy / loss-ratio drift).
- The simulation-model changes SL-3 (cap own damage at sum insured), SL-5 (nil-draw knob isolation), MF-2 (trailing partial accident year), and MF-7 (target loss ratio knob).
- The large refactors RF-1 (compute triangles once via an application use case), RF-7 (single source of run defaults), RF-9 (app.js into an ES module), RF-10 (the broader consolidation set), RF-13 (parameter fan-out), and RF-14 (split the Claim struct).
- The heavier server guardrails R-1 (concurrency control), R-2 (input bounds and cancellation), R-4 (progress / cancel / stale-results UX), and R-14 (server timeouts and out_dir base directory).
- CI (R-13).

The remaining review items map onto the grouped change sections below.

## Decisions made during design

- **Only SL-9 changes output.** Building the inflation index one year longer nudges the severity of tail-year occurrences up (by up to one year's drift), which moves the default run and therefore the realism band. Everything else in this bundle is output-neutral by construction. So the plan is: land SL-9 first, re-verify the realism gate, and treat `TestDefaultPresetIsRealistic` as the acceptance test for that one change.
- **Two pairs need coordinated updates.** RF-6 (adding the `claims.` prefix to `severity.*` and `close_lag.*` validation errors) changes strings that validation tests assert on, so the tests move in the same commit. R-6 (JSON `DisallowUnknownFields`) plus RF-8 (dropping the dead `lob_id` field) touch the same request path - RF-8 must land with or before R-6, or a client still sending `lob_id` would start getting rejected. They are done together.
- **The bundle is otherwise output-neutral.** The defensive guards (SL-8, SL-10, R-3, R-5) only fire on inputs the current preset and UI never produce, the naming and doc changes touch no numbers, and the UI-label changes are cosmetic. So apart from the SL-9 re-verification, existing determinism and golden tests should stay green unchanged.

## A. Documentation and known-simplification notes

Pure documentation - no behavior change.

- **D-1** - stale and broken doc references. `docs/roadmap.md` "~143 Schedule P reference datasets" corrected to 289 (143 dec2025 + 146 sep2011). `docs/background-context.md` pointer `docs/transcripts/` corrected to `docs/raw user inputs/transcripts/`. All six existing specs under `docs/superpowers/specs/` moved from "approved / approved pending review" to "Status: shipped".
- **D-2** - godoc on the exported constructors (`NewBookSimulator`, `NewClaimSimulator`, `NewRunoffSimulator`, `NewRecoverySimulator`, `NewReopenSimulator`) and the config DTO types.
- **SL-11** - `BaseFrequency` doc comment reworded from "reported claims per policy-year" to "ground-up occurrence frequency", since sub-excess occurrences are discarded after the Poisson draw.
- **SL-4** (doc part) - README note that own-damage severity trend is the product of `sum_insured_inflation` and claims inflation, while third-party carries only the claims index. (The model rebase is the out-of-scope part.)
- **SL-7** (doc part) - README note that case estimates re-centre on the true ultimate at the first revision, so incurred development carries little systematic IBNER signal. (Letting the bias decay is the out-of-scope part.)
- **SL-12** - README note that nil claims draw severity and probability independently of claim size, a known simplification.
- **MF-3** (README) - soften the "no code changes" sentence: the CLI path is pure YAML, but a UI preset needs one registration line. Point at the roadmap.
- **MF-4** - README note: no seasonality, catastrophe, or event clustering; claims independent across policies.
- **MF-6** - README note: each year's book is an independent cohort, no policy renews.
- **MF-8** - README note: the insurer prices risk perfectly (premium uses the exact frequency-driving risk factor, no pricing error).
- **R-11** - comment on the CSV writer pinning the "fields never contain delimiters" invariant it relies on.
- **RF-12** (CSV order note) - README note that transactions are emitted in claim-registration order, not date order, so recovery rows may post-date a claim's close and a later claim's rows can carry earlier dates.

## B. Defensive guards and small correctness

Inert on today's inputs; they turn undefined or silently-wrong behavior into a clear failure.

- **SL-8** - propagate the `ok` flag from `lossRatio`: when the generated dataset has no premium, mark the loss-ratio check failed instead of scoring the 0 against the band.
- **SL-9** - build the inflation index with `years+1` at the composition root so late-final-year occurrences in `startYear+years` get their own factor; keep the `For` clamp as a defensive path only. This is the one item that moves default output; the realism gate is re-verified after.
- **SL-10** - guard `total <= 0` in the Dirichlet split (fall back to settling at close) and reject NaN/Inf in `FromDollars` / `MulFloat`, so a degenerate `runoff.concentration` cannot produce NaN money.
- **R-3** - add a NaN/Inf sweep over the float fields in the `lob` validators, so `.nan` in YAML (or via the JSON API) fails validation instead of producing garbage draws.
- **R-5** - handle the `writeJSON` encode error and sanitize +/-Inf in `realismView` the way `triangleView` already sanitizes NaN, so a `Band{Min: +Inf}` cannot yield a truncated 200 with an empty body.

## C. Web server hardening and UX

- **R-6 + RF-8** - set `DisallowUnknownFields` on the JSON request decoder to match the strict YAML config layer, and in the same change drop the dead `lob_id` field the handler never reads (done together so a misspelled or stale key is a clear error, not a silent default).
- **R-9** - `ui --port 0` prints `ln.Addr()` (the bound address) instead of the requested port, so an OS-assigned port shows a usable URL.
- **R-10** - resolve `out_dir` to an absolute path server-side and echo the resolved path in `run.out_dir`, so users launched from a shortcut can find the CSVs.
- **R-7** - send the seed as a string (or document a 2^53 limit with `max=` on the input) so a 64-bit seed pasted from a CLI run is not silently rounded by the UI's `Number(...)`.
- **R-8** - guard the front-end paths that assume the preset loaded: Generate and "Reset to preset defaults" both surface a clear banner instead of a raw TypeError when `loadPreset` failed.
- **R-12** - the screenshot script also watches for a visible error banner and rejects immediately with its text (instead of hanging the full 120 s timeout), and derives the Chrome path per platform.

## D. Refactor and naming cleanups

Output-neutral internal tidy-ups.

- **RF-3** - delete the private mean-one lognormal copy in `book.go` (which guards `== 0`) and call the shared `shared.MeanOneLogNormal` (which guards `<= 0`).
- **RF-6** - prefix the `severity.*` and `close_lag.*` validation errors with `claims.` to match every sibling field's YAML path; validation tests updated in the same change.
- **RF-4** - replace the `event.kind` magic numbers 0/1 with named constants (the fuller `runEpisode` de-duplication is left as the out-of-scope part of RF-4).
- **RF-5** - name the one-cent floor as a single constant rather than repeating the literal across reopen, runoff, and recovery (promoting term length to `BookParams` is deferred).
- **RF-12** - the small style nits: rename the local `close` that shadows the builtin to `closeDate`, and the CSV-order documentation note (see group A).

## E. UI labels

- **SL-6** - the triangle tab tooltip on the last development column relabelled to "dev 10+", since it folds in all post-age-10 development rather than being "cumulative to dev year 10" (the triangle-censoring model change is out of scope).
- **SL-15** - a "paying claims only" caption on the severity histogram, since nil claims (paid 0) are dropped from it with no indication.

## F. Tests

- **RF-2** - strengthen the determinism test from slice-length / element comparison to a full `reflect.DeepEqual` over the entire dataset, plus a golden-hash test on the CSV bytes, so a nondeterminism bug that kept counts stable can no longer pass.
- **D-3** - close the flagged test gaps: run the realism gate on multiple seeds (not just seed 42) so a recalibration cannot pass on one lucky seed; add a sep2011 known-value reader test (including `StartYear == 1988`); and label the two loss ratios so the summary tab's gross LR and the realism tab's net LR vs Schedule P are not silently confused.

## Testing

- `go vet ./...` and `go test ./...` stay green across every package.
- `TestDefaultPresetIsRealistic` is re-verified after SL-9, which is the only item that moves default output; it is the acceptance test for that change.
- The CLI/web byte-identical parity test and the determinism tests (now the strengthened RF-2 versions) pass.
- A UI smoke check of the visible changes: the "dev 10+" tooltip, the "paying claims only" severity caption, the resolved absolute `out_dir` in the run header, and the preset-failure and JSON-unknown-field error banners.
