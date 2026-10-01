# Todo

Smaller or administrative items still to be done. This file lists open items
only: when an item is done, delete it and renumber the positions so the list
stays dense. The finding ID after each position is stable, so older references
still resolve against git history. Model and code review findings live in
`docs/review.md`; direction and sequencing live in `docs/roadmap.md`.

ID prefixes: **SL** simulation logic, **MF** mission fit, **R** robustness,
**RF** refactoring, **D** documentation, **L** low-severity security, **UX**
and **CI** merged items. Severity: **high** undermines the mission, **medium**
worth addressing soon, **low** fix when touching the area.

## 1. RF-15 (low) - sub-cent case releases on nil claims

- Where: `internal/domain/transaction/runoff.go`.
- The nil-claim runoff floors its case release at one cent to guarantee a
  close-date transaction.
- Action: if very small initial estimates ever become common, revisit the
  runoff's sub-cent behavior more broadly.

## Exposure milestone (conditional)

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
exposure. The UI's run-size caps and one-run-at-a-time slot are sized for a mistyped form, not for an attacker: revisit both numbers
here rather than assuming they carry over.
