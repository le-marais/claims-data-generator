# Todo

The open-work backlog for claimsgen. It consolidates the three review documents
that used to live here - the code review (2026-07-18), the security and
vulnerability review (2026-07-22), and the stage-isolation findings
(2026-07-27). Their full text is recoverable from git history
(`git log --diff-filter=D -- docs/code-review-2026-07-18.md`).

Every item was re-verified against the code on 2026-08-09 and then scored
against `docs/mission.md`: does it make the generated data more useful for a
reserving demo, make a second line of business cheaper, or protect the people
who actually run the tool (local, single-user, on a laptop)? Seventeen findings
failed that test and were dropped; twelve more collapsed into four. They are
listed at the end so they are not rediscovered from scratch by the next review.
`go build`, `go test ./...` and `go vet ./...` are clean.

Items are in priority order, weighing mission value against cost. The leading
number is a position and will change; the finding ID after it is stable and is
preserved from the source reviews, so older references still resolve: **SL**
simulation logic, **MF** mission fit, **R** robustness, **RF** refactoring,
**D** documentation, **L** low-severity security, **UX** and **CI** merged
items. Severity: **high** undermines the mission, **medium** worth addressing
soon, **low** fix when touching the area. Nothing high-severity is open.

## Order

1. **SL-7** - the only open item that changes what a reserving actuary sees in
   the data.
2. **D-1** - the README currently advertises defects that were fixed months ago.
3. **MF-3** - a small change that removes the main friction from the "a new
   class is just a YAML file" promise.
4. **RF-13** - gates the second line of business.
5. **RF-14** - gates the same work, and compounds with every feature added.
6. **SL-2** - wording, so the realism gate stops claiming more than it measures.
8. **L2** - two lines in `.gitignore`.
9. **CI-1** - gated on the roadmap's "open to the wider community" step.

Items 6 and 8 are each under an hour; batch them into any commit that touches
the area rather than scheduling them.

## 1. SL-7 (medium) - case estimates re-centre on the true ultimate at the first revision

- Where: `internal/domain/transaction/runoff.go`, `runEpisode`;
  `case_adequacy_mean: 1.0` in `internal/infrastructure/config/motor-personal.yaml`.
- Every revision targets `(ultimate - paid)` times mean-one lognormal noise
  whose sigma decays with age. Even with a case adequacy mean away from 1, the
  first revision (Poisson, about 4/yr) snaps the case to an unbiased view of the
  truth. Real incurred triangles show persistent case strengthening or weakening
  that IBNER methods are built to detect; here incurred is unbiased at every
  age, so incurred-based methods look trivially perfect - the wrong impression
  for a tool whose job is feeding reserving demos.
- Already documented in the README and in a code comment. The model change
  remains.
- Action: let the adequacy bias decay gradually over the claim's life instead of
  vanishing at the first revision. While in this function, fold in the old RF-4:
  the nil branch duplicates the sigma-decay and target computation with a
  different remaining-source and floor rule, and a `remaining()` closure plus one
  unconditional keep-open floor removes both the duplication and a boolean
  parameter.
- Cost note: this is the most expensive item here. It changes generated output,
  so it needs a golden-hash refresh and a realism-gate re-check, and the preset
  may need recalibrating.

## 2. D-1 (low) - the UI screenshots predate the pricing and windowing work

- Where: `docs/screenshots/`, embedded in `README.md`.
- The images were last regenerated before target-loss-ratio pricing and claim
  windowing, so they still show both fixed defects: a header claim count that
  disagrees with the summary total (27,823 versus 26,150) and per-year loss
  ratios climbing from 0.693 to 1.010. A current run of the same defaults gives
  26,040 claims in both places and loss ratios from 0.662 to 0.752. The sidebar
  also predates the Pricing parameter group, and the realism tab predates the
  loss-ratio-drift card. The README is the shopfront and it currently
  advertises fixed bugs.
- Action, all in one pass: regenerate with `tools/screenshots` (start the UI on
  port 8093, `npm install`, `node screenshots.js`; needs a local Chrome,
  `CHROME_PATH` to override the location); add the missing Pricing group to the
  README's Browser UI paragraph (old D-2); and while in `tools/screenshots`,
  commit `package-lock.json` and pin `puppeteer-core` to an exact version
  instead of `^24.0.0` (old L3), which is the repo's one supply-chain weakness.

## 3. MF-3 (medium) - sub-blocks must validate even when switched off

- Where: `internal/domain/lob/lob.go`, `RecoveryTypeParams.validate` and
  `SeverityParams.validate`.
- A recovery type with `probability: 0` still has to supply a `mean_share` in
  the open interval (0, 1), a positive concentration and a positive lag median.
  A new-class YAML author is forced to invent parameters for features they
  turned off, which cuts against the "a new class is a YAML file" promise.
- Action: skip validation of a sub-block whose probability or weight is 0.

## 4. RF-13 (medium) - adding one line-of-business parameter touches five places

- Where: the domain struct plus validation (`internal/domain/lob/lob.go`), the
  config DTO plus `ToDomain` (`internal/infrastructure/config/config.go`), the
  preset YAML (`internal/infrastructure/config/motor-personal.yaml`), and the UI
  form metadata (`internal/infrastructure/web/static/app.js`, where labels and
  tips restate the YAML comments by hand).
- This is the main friction for the roadmap's second line of business. The
  2026-07-27 independent-pricing-basis feature added the `pricing` block through
  exactly this fan-out - a current instance, not a historical one.
- Action, in increasing order of ambition: (1) document the checklist in a short
  "adding a parameter" note; (2) serve the form metadata from the server - a
  small registry of label, tip and group per field would let `app.js` build the
  form generically and remove the JS-side duplication and its drift risk against
  the YAML comments; (3) revisit whether the DTO layer pays its way - the
  mirrored structs keep the domain tag-free, a legitimate choice, but if
  `ToDomain` keeps growing, consider code generation or accepting yaml/json tags
  on the `lob` package.
- Principle to hold to (the old F8): give each stage a narrow, purpose-built
  input rather than a whole parameter block, so the compiler enforces isolation.
  `NewBookSimulator(book, pricing)` already shows it working - it takes a
  purpose-built `PricingParams` and structurally cannot read claims knobs.

## 5. RF-14 (medium) - the claim record and the pipeline-carry context are the same struct

Merges the old F3 and F6, which describe the same problem from the parameter
side.

- Where: `internal/domain/claim/claim.go` - `RiskFactor`, `Nil`, `OwnDamage`,
  `FirstCloseDate`, `ReopenDate` and `ReopenEstimate`, each annotated "carried
  to the runoff/recovery stage but never written to CSV". Every future feature
  (a valuation-date extract, per-claim-type behavior for new lines) will want
  more such fields.
- `RiskFactor` is the clearest case: its comment says "carried from the policy
  for downstream stages", but it is read only by the close-lag draw in
  `claim.go` and the reopen close-lag draw in `reopen.go`. No transaction-stage
  file references it (confirmed 2026-08-09). The comment asserts a
  policy-to-transaction link that does not exist.
- The related reach-through: `internal/application/generate.go` passes
  `req.LOB.Book.SumInsuredInflation` into `ClaimSimulator.WithBaseYear`, so the
  claims stage needs a *book* knob to interpret the sum insured it reads off
  each policy.
- Action: split the persisted claim record from a development-context struct
  passed between stages, so the CSV surface is explicit in the type system
  instead of a comment convention. Drop `RiskFactor` from the exported struct
  and pass it at draw time (or fix the comment as a stopgap). Have the book
  stage store a base-year sum insured on `Policy`, computed where the drift rate
  already lives, so the claims stage reads a self-describing field.
- One data dependency stays and is deliberate: `Claim.OwnDamage` crosses from
  the claims stage to `recovery.go`, because only the severity draw knows the
  claim type and recovery eligibility genuinely depends on it.

## 6. SL-2 (low) - the realism gate's "ultimate loss ratio" is not one

- Where: `internal/domain/triangle/compare.go` (`lossRatio` = latest diagonal
  over total earned premium, used for both sides); wording in
  `internal/application/realism.go` and the UI.
- The generated incurred triangle is fully developed, so its latest diagonal is
  a true ultimate. Reference triangles are ragged: their latest diagonal for
  recent origins is immature case-incurred excluding IBNR. The reference band is
  therefore biased low relative to true ultimates.
- Action: rename and document the metric so the gate does not claim more than it
  measures. Chain-ladder-completing the reference diagonals would shift every
  band and force a preset recalibration - not worth it to remove a known,
  directional bias that the wording can state instead.

## 8. L2 (low) - agent-artifact ignore rules are not in the tracked gitignore

- Where: `.gitignore`, which covers only `/output/`, `/claimsgen`, `*.exe`,
  `tools/screenshots/node_modules/` and `tools/screenshots/package-lock.json`.
- It does not cover `.claude/` or `.superpowers/`, so a contributor running
  Claude Code in a fresh clone would generate session artifacts (prompts,
  scheduled-task metadata, agent memory) that are not ignored. The 2026-07-22
  review recorded local `.git/info/exclude` rules covering these; this working
  copy has none, so the tracked `.gitignore` is the only protection there is. No
  such artifact is currently tracked - this is preventive.
- Action: add the two patterns. Two lines.

## 9. CI-1 (low) - no CI and no dependency scanning

Merges the old R-13 and I2.

- Where: repo root. There is no `.github/workflows` and no evidence of
  `govulncheck`. `AGENTS.md` documents `go test` and `go vet` as the gate, which
  runs only when someone remembers and does not catch known-vulnerable
  dependencies.
- Action: add a minimal workflow running `go test ./...`, `go vet ./...` and
  `govulncheck ./...` before the roadmap's "open to the wider community" step.
  Consider `golangci-lint` in the same pass.

## Exposure milestone (conditional, not work today)

The security review's low rating depends on the tool staying loopback-only and
single-user. Today `handleGenerate` takes `out_dir` verbatim and writes the
five CSVs wherever it points, which is the feature, not a vulnerability: the
user is writing their own files to their own disk with their own privileges. The
same applies to raw error strings echoing local paths, and to serving without
read or write timeouts.

If the UI is ever bound beyond 127.0.0.1 or shared, all of that inverts at once.
Before any non-loopback deployment: add authentication; confine `out_dir` to a
server-configured base directory and reject absolute paths and `..` escapes
after `filepath.Clean`; stop treating an absent `Origin` header as trusted for
state-changing requests; use an `http.Server` with timeouts; and return generic
error messages while logging detail server-side. Design these in when the
second-line-of-business plumbing is touched, rather than retrofitting after
exposure. The run-size caps and the one-run-at-a-time slot that UX-1 and R-1
added are sized for a mistyped form, not for an attacker: revisit both numbers
here rather than assuming they carry over.

## Invariants not to regress

Verified sound by both reviews and worth protecting in any of the work above.

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
  adjustment, Beta mean/concentration form, and Pareto alpha > 1 for a finite
  mean.
- **Loopback-only security posture.** 127.0.0.1 bind, Host and Origin checks
  against DNS rebinding and CSRF, `MaxBytesReader`, `DisallowUnknownFields` on
  JSON and `KnownFields(true)` on YAML, an embedded `fs.Sub` static tree with no
  path traversal, and a front end that builds DOM exclusively via `textContent`
  and `createElementNS` - no XSS sink.
- **The CSV output has no free-text column.** Every field is numeric, an
  ISO-8601 date or a fixed enum, which is why plain `fmt.Sprintf` is safe. If a
  claim description or class name is ever added, switch to `encoding/csv` with
  formula-lead-character escaping in the same change.
- **No secrets, no PII.** The reference data is public NAIC Schedule P aggregate
  triangles keyed by company code; the generated output is fully synthetic. Two
  direct dependencies, both current, with `go.sum` pinning hashes.

## Considered and dropped (2026-08-09)

Scored against the mission and judged not worth carrying. Full text of each is
in the removed review documents in git history.

| ID | Was | Why dropped |
| --- | --- | --- |
| SL-6 | Post-age-10 development folded into the last triangle column | Measured, not assumed: payments beyond dev 10 are 0.10% of net paid, and the volume-weighted 9-10 factor moves 1.00179 (fold-in) to 1.00064 (censored) against a gate band of [0.9994, 1.0056]. It cannot flip a pass or fail. The finding's stated driver was also wrong - subrogation contributes $906 of $15.7M beyond age 10. Revisit only if a genuinely long-tail class lands, where it would matter. |
| SL-14 | Recovery lags anchored to the final close, not the first | Affects only reopened own-damage claims, and fixing it means weakening the "recoveries strictly after close" invariant to correct a detail no view surfaces. |
| SL-16 | Degenerate interim-payment fallback discards the payment plan | Self-described cosmetic; can only fire for cent-scale ultimates. |
| F2 | Inflation compounding written twice | The shared parameter is gone by design. What is left is a deterministic pricing assumption (`Mean^y`) and a stochastic path - different things, nothing to single-source. |
| F4 | Move `recoveries` out of the `claims` config block | A YAML key rename that invalidates every existing config file and the shipped preset, for a tidier namespace. |
| F5 | Move nil and reopening ownership to the transaction stage | Would move draws between sub-streams, breaking the golden hash and every no-shift test, with zero change to the output. |
| F7 | Replace the `OwnDamage` bool with a `ClaimKind` enum | One producer, one consumer, already documented. |
| I1 | API error responses leak absolute filesystem paths | The paths leaked are the ones the user typed into the form. |
| I3 | CSV formula-injection invariant | Not an action item; it is an invariant, and now lives in that section above. |
| M1 | Unbounded `out_dir` and empty-Origin trust | Writing CSVs where the user points is the feature; any local process that can POST can already write files directly. Kept only as a precondition in the exposure milestone. |
| R-14 | No read or write timeouts on the server | Explicitly "acceptable today"; the conditional is the exposure milestone. |
| RF-5 | 364-day policy term hardcoded | Annual terms are near-universal in every short-tail class on the roadmap. |
| RF-7 | Run defaults duplicated in three places | Four numbers that change roughly never. |
| RF-9 | Modularize the 500-line `app.js` | It works, holds no numeric logic, and the refactor enables tests nobody has asked for. |
| RF-10 | Five small code duplications | The definition of "fix when touching the area"; tracking them costs more attention than the duplication does. |
| RF-11 | Generalize `RecoveryParams` to a named list of types | Speculative until a class needs a third type. |
| SL-13 (part) | Claim-type-dependent reopen propensity | Speculative until a second line of business exists; only the nil-count tooltip was kept. |

## Resolved, for provenance

Full text and the measurements behind each are in git history.

| ID | Was | Resolution |
| --- | --- | --- |
| RF-1 | `developmentYears` defined twice, with the same two triangles computed in both places | Resolved by the monthly triangles work: `application.Aggregates` builds one monthly grid per run and hands out the annual triangles, so the constant lives only in `internal/application/aggregate.go` and the triangles are computed once. `EvaluateRealism` now takes an `Aggregates`. |

- **2026-08-10 web-run reliability pass**: **R-1** (generation runs one at a
  time behind a one-deep slot on `Server`, so two tabs pointed at one `out_dir`
  can no longer interleave writes; the slot is a channel rather than a mutex so
  a queued request can answer its own cancel), **UX-1** (run-size caps on years,
  initial book size and the compounded projection - served from `GET
  /api/limits` so the form mirrors them rather than restating them; the request
  context plumbed through `GenerateDataset` and checked between stages; an
  AbortController cancel button and an elapsed-time indicator; previous results
  dimmed and labelled while a run is in flight or after one fails), and
  **SL-13** (the "Nil claims" tooltip). Cancellation is checked between stages,
  not inside them, to keep the domain free of `context` - so an abandoned run
  ends within one stage rather than immediately, which is why a queued retry
  waits instead of being rejected.
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
