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

Nothing is queued here. The open model findings are in `docs/review.md`, and
the exposure milestone below is conditional.

The sequence across this file and `docs/review.md` lives in `docs/roadmap.md`.

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
