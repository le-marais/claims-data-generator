# Roadmap

A living view of where claimsgen is and what comes next. Grounded in `mission.md` (see its "Next" section) and in the shipped code. Order is a recommendation, not a commitment.

## Shipped

- **Generation engine** - policy book, claim events, case estimate runoff, and transactions for a class of business, reproducible from a seed and a line-of-business YAML.
- **CLI** - `claimsgen generate` writes the five CSVs: policies, claims, transactions, triangles and exposure.
- **Browser UI** - `claimsgen ui` configures a run with every flag and line-of-business parameter, and shows summary stats, development triangles, distributions and a realism check.
- **Realism gate** - the third-party (liability) section of the preset is scored on its share of premium against the 96 curated Schedule P private passenger auto liability companies, and must land inside their P5-P95 bands (`TestDefaultPresetIsRealistic`). Own damage has no 10-year Schedule P reference and is not scored.
- **Claims inflation** - a stochastic index by occurrence date, with one mean knob per line of business, applied to every claim's ground-up loss.
- **Third-party report lag and IBNR** - third-party claims report later than own damage, so reported counts and incurred carry pure IBNR to estimate, and their settlement time grows with size.
- **Persistent case adequacy** - the opening case bias decays over each claim's life, so incurred develops in a consistent direction for IBNER methods to find.
- **Nil claims** - a share of reported claims close without payment.
- **Recoveries** - salvage on own-damage total losses and subrogation on own-damage claims, received after close; triangles and the realism gate go net of recoveries.
- **Reopened claims** - a closed claim can reopen once and develop a second episode.
- **Premium pricing to a target loss ratio** - premium comes from a separate pricing basis divided by `target_loss_ratio`, so the loss ratio is emergent rather than forced, and `adequacy_volatility` adds an underwriting cycle.
- **Monthly triangles and exposure** - `triangles.csv` and `exposure.csv` by accident or underwriting month (`--origin-basis`). The monthly grid is the single aggregation store; the annual triangles are a coarsened view of it.
- **CI** - every pull request and push to `main` runs gofmt, `go vet`, `go test` on the minimum and current Go releases, `govulncheck` and `golangci-lint`.

Only motor personal exists as a line of business today.

## Near term

The next step is the second line of business below, which folds in MR-12 (separate sum-insured and risk spreads) and the per-class switches from MR-8 (liability limit, excess on liability claims).

## Mid term - second line of business

Prove the "one parameterizable engine" differentiator by adding a second short-tail class, most likely **commercial property**. The plumbing is ready - the preset registry, the line-of-business dropdown, and the preset-driven UI form were built so a new class is a YAML file plus a registration line. The real work is:

- **Per-line-of-business reference data and calibration.** The realism gate is motor-only today: `claimsgen ui` loads the private passenger auto reference directory (`refdata.PersonalMotorDir`), and only that set is embedded. Reference data needs to be keyed per line of business so each class calibrates against an appropriate Schedule P family. `data/reference/schedule p/` already holds hand-curated companies for all six Schedule P lines - private passenger auto (96 companies), commercial auto (92), other liability (92), workers compensation (61), products liability (13) and medical malpractice (8) - kept against `data/reference/gr-code-list.md`. Schedule P carries liability lines only, so a commercial *property* class has no direct reference family; commercial auto is the closest short-tail fit.
- **Any class-specific behavior** commercial property needs that motor does not (for example severity capped harder at sum insured, no third-party tail).

## Longer term

- **Valuation-date extract** - every claim runs to closure for out-of-sample testing. A cut at a chosen valuation date (open claims, outstanding case, nothing known after the date) would hand users a reserving data set they can use without manual fixes, the mission's success criterion. `triangles.csv` already supports the triangle side by filtering on `origin_month + dev_month - 1`; the claim and transaction extracts remain.
- **Payment-date (calendar-year) inflation** - the shipped inflation is by occurrence date, which keeps the ultimate-first invariant. Payment-date inflation creates the calendar-year development distortions reserving methods struggle with, but it makes the ultimate emergent and interacts with case adequacy, so it needs its own design.
- **Long-tail classes** - assess whether the engine can extend to long-tail lines such as liability. Flagged in the mission as a later question, not a commitment.

## Technical debt

- The nil-claim runoff floors its case release at one cent to guarantee a close-date transaction; if very small initial estimates ever become common, revisit the runoff's sub-cent behavior more broadly.
- `docs/todo.md` is the live backlog of open code-review, security-review and stage-isolation findings. Its "Exposure milestone" section lists what must change before the UI is served beyond one machine.
