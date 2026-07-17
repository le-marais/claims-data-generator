# Roadmap

A living view of where claimsgen is and what comes next. Grounded in `mission.md` (see its "Beyond the MVP" section) and decisions recorded in `docs/superpowers/specs/`. Order is a recommendation, not a commitment.

## Shipped

- **Generation engine** - policy book, claim events, case estimate runoff, and transactions for a class of business, reproducible from a seed and a line-of-business YAML.
- **CLI** - `claimsgen generate` writes the three linked CSVs (policies, claims, transactions).
- **Browser UI** - `claimsgen ui`: configure a run (flags plus every line-of-business parameter), generate, and explore the result across summary, development triangles, distributions, and a realism check. Self-contained single binary, embedded reference data.
- **Realism gate** - generated motor data is scored against the ~143 Schedule P private passenger auto reference datasets; the shipped preset must land inside the observed bands (`TestDefaultPresetIsRealistic`).
- **Claims inflation** - stochastic occurrence-year inflation index, one user-facing mean knob per line of business, applied to every claim's ground-up loss.
- **Nil claims** - a share of reported claims close without payment, with a dedicated no-payment runoff path and a `nil_probability` off switch.
- **Recoveries (salvage and subrogation)** - money coming back on own-damage claims after close, as SALVAGE and SUBROGATION transaction types; triangles and the realism gate go net of recoveries, and the triangle tab gains a gross/net toggle.
- **Reopened claims** - a closed claim can reopen once and develop a second episode; claims.csv shows the final close date and the reopen appears in transactions as a case re-raised after a release to zero, with a reopen_probability off switch.

Only motor personal exists as a line of business today.

## Near term

The real-claims-data backlog from the mission is complete (claims inflation, nil claims, recoveries, reopened claims). The next candidate is the mid-term second line of business below.

## Mid term - second line of business

Prove the "one parameterizable engine" differentiator by adding a second short-tail class, most likely **commercial property**. The plumbing is ready - the preset registry, the line-of-business dropdown, and the preset-driven UI form were built so a new class is a YAML file plus a registration line. The real work is:

- **Per-line-of-business reference data and calibration.** The realism gate is motor-only today, and the `ui` command hardcodes the private-passenger-auto reference directory. Reference data needs to be keyed per line of business so each class calibrates against an appropriate Schedule P family (commercial auto, commercial multi-peril).
- **Any class-specific behavior** commercial property needs that motor does not (for example severity capped harder at sum insured, no third-party tail).

Then open the tool to the wider actuarial community once a second class demonstrates reusability.

## Longer term

- **Valuation-date extract** - the mission deliberately generates every claim to closure for out-of-sample testing, but a chosen-date cut (open claims, outstanding case, no future knowledge) is trivial to derive and would let the tool feed a reserving demo with zero manual steps - the MVP's own success criterion. Could ride along with any earlier work as an output option.
- **Payment-date (calendar-year) inflation** - the shipped inflation is occurrence-year, which keeps the ultimate-first invariant. Payment-date inflation creates the calendar-year development distortions reserving methods actually struggle with, but it makes the ultimate emergent and interacts with case adequacy, so it deserves its own design. Deferred in the claims-inflation spec.
- **Long-tail classes** - assess whether the engine can extend to long-tail lines such as liability. Flagged in the mission as a later question, not a commitment.

## Known enablers and technical debt

Small items that make the above cheaper or are worth cleaning up when touched:

- The `ui` command's reference-data directory is hardcoded to private passenger auto; generalising it is a prerequisite for a second line of business's realism view.
- Reference-data loading should be keyed per line of business (currently a single embedded set).
- The nil-claim runoff floors its case release at one cent to guarantee a close-date transaction; if very small initial estimates ever become common, revisit the runoff's sub-cent behavior more broadly.
