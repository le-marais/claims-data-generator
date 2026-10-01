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
failed that test and were dropped; twelve more collapsed into four. This file
carries only outstanding work, so neither those nor anything since resolved is
listed here - recover them from git history if a future review needs to know
what was already considered. `go build`, `go test ./...` and `go vet ./...` are
clean.

Items are in priority order, weighing mission value against cost. The leading
number is a position and will change; the finding ID after it is stable and is
preserved from the source reviews, so older references still resolve: **SL**
simulation logic, **MF** mission fit, **R** robustness, **RF** refactoring,
**D** documentation, **L** low-severity security, **UX** and **CI** merged
items. Findings from the model review of the simulation logic live in
`docs/review.md` under **MR** IDs. Severity: **high** undermines the mission,
**medium** worth addressing soon, **low** fix when touching the area. Nothing
high-severity is open.

## Order

1. **SL-7** - the only open item that changes what a reserving actuary sees in
   the data.
2. **RF-13** - gates the second line of business.
3. **RF-14** - gates the same work, and compounds with every feature added.
4. **CI-1** - gated on the roadmap's "open to the wider community" step.

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
- The opening case's adequacy bias is set by `transaction.CaseEstimator`
  (`internal/domain/transaction/estimate.go`), which draws the case around the
  claim's true cost; the runoff then removes that bias at the first revision.
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

## 2. RF-13 (medium) - adding one line-of-business parameter touches five places

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

## 3. RF-14 (medium) - the claim record and the pipeline-carry context are the same struct

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

## 4. CI-1 (low) - no CI and no dependency scanning

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
