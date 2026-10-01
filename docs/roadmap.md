# Roadmap

A living view of where claimsgen is and what comes next. Grounded in `mission.md` (see its "Beyond the MVP" section) and in the shipped code. Order is a recommendation, not a commitment.

## Shipped

- **Generation engine** - policy book, claim events, case estimate runoff, and transactions for a class of business, reproducible from a seed and a line-of-business YAML.
- **CLI** - `claimsgen generate` writes the three dataset CSVs (policies, claims, transactions) plus the monthly aggregate CSVs below.
- **Browser UI** - `claimsgen ui`: configure a run (flags plus every line-of-business parameter), generate, and explore the result across summary, development triangles, distributions, and a realism check. Self-contained single binary, embedded reference data.
- **Realism gate** - the third-party (liability) section of generated motor data is scored against the 96 hand-curated Schedule P private passenger auto liability reference companies, on its share of premium; the shipped preset must land inside the observed P5-P95 bands (`TestDefaultPresetIsRealistic`). Own damage has no 10-year Schedule P reference and is not scored.
- **Claims inflation** - stochastic inflation index, simulated per year and interpolated by occurrence date, one user-facing mean knob per line of business, applied to every claim's ground-up loss.
- **Third-party report lag and IBNR** - third-party injury claims report later than own damage (median 20 days, about 15% of them after their accident year ends), so reported counts and incurred carry pure IBNR to estimate; their settlement time grows smoothly with size; and the realism gate scores incurred with pure IBNR at its true value, like Schedule P total incurred.
- **Persistent case adequacy** - the opening case's adequacy bias decays over each claim's life instead of vanishing at the first revision, so incurred develops in a consistent direction for IBNER methods to find; the preset opens cases about 11% redundant.
- **Nil claims** - a share of reported claims close without payment, with a dedicated no-payment runoff path and a `nil_probability` off switch.
- **Recoveries (salvage and subrogation)** - money coming back on own-damage claims after close, as SALVAGE and SUBROGATION transaction types, with salvage only on total losses; triangles and the realism gate go net of recoveries, and the triangle tab gains a gross/net toggle.
- **Reopened claims** - a closed claim can reopen once and develop a second episode; claims.csv shows the final close date and the reopen appears in transactions as a case re-raised after a release to zero, with a reopen_probability off switch.
- **Premium pricing to a target loss ratio** - premium is priced from an independent pricing basis (the insurer's assumed loss cost) divided by a `target_loss_ratio`, and trended with the assumed inflation, so the accident-year loss ratio does not drift just because severities inflate. The pricing basis lives in its own `pricing` block, decoupled from the claims model, and the loss ratio is emergent: the preset starts its assumptions from the claims values, so it lands around the target, and deviating them models underpricing or adverse experience. `adequacy_volatility` adds random per-underwriting-year mispricing around the target, like an underwriting cycle. The realism gate scores loss-ratio drift against the reference companies' spread, and a noise-free test guards against systematic drift.
- **CI** - every pull request and push to `main` runs gofmt, `go vet`, `go test` on the minimum and current Go releases, `govulncheck` and `golangci-lint`.
- **Monthly triangles and exposure** - `triangles.csv` carries incremental monthly development triangles by origin month (paid, paid net of recoveries, incurred, reported claim counts) and `exposure.csv` carries premium, exposure units and policy counts on the same axis, with an `--origin-basis` knob for accident or underwriting month. The monthly grid is the single aggregation store: the annual triangles the realism gate and the UI read are a coarsened view of it, and quarterly comes free from the same function.

Only motor personal exists as a line of business today.

## Near term

The real-claims-data backlog from the mission is complete (claims inflation, nil claims, recoveries, reopened claims), and so is the model-realism pass agreed on 2026-10-01 (MR-13, MR-9, SL-7, MR-7) and CI (CI-1). The next step is the second line of business below, which folds in MR-12 (separate sum-insured and risk spreads) and the per-class switches from MR-8 (liability limit, excess on liability claims). Adding a parameter now follows the checklist in `AGENTS.md`, with tests that catch a field missing from the config mapping or the UI form.

## Mid term - second line of business

Prove the "one parameterizable engine" differentiator by adding a second short-tail class, most likely **commercial property**. The plumbing is ready - the preset registry, the line-of-business dropdown, and the preset-driven UI form were built so a new class is a YAML file plus a registration line. The real work is:

- **Per-line-of-business reference data and calibration.** The realism gate is motor-only today, and the `ui` command hardcodes the private-passenger-auto reference directory. Reference data needs to be keyed per line of business so each class calibrates against an appropriate Schedule P family. The curation work is already partly done: `data/reference/schedule p/` holds hand-curated companies for all six Schedule P lines - private passenger auto (embedded, 96 companies), commercial auto (92), other liability (92), workers compensation (61), products liability (13) and medical malpractice (8) - kept against `data/reference/gr-code-list.md`. Only the private-passenger-auto set is embedded and scored today. Note that Schedule P carries liability lines only, so a commercial *property* class has no direct reference family here; commercial auto is the closest short-tail fit.
- **Any class-specific behavior** commercial property needs that motor does not (for example severity capped harder at sum insured, no third-party tail).

Then open the tool to the wider actuarial community once a second class demonstrates reusability.

## Longer term

- **Valuation-date extract** - the mission deliberately generates every claim to closure for out-of-sample testing, but a chosen-date cut (open claims, outstanding case, no future knowledge) is trivial to derive and would let the tool feed a reserving demo with zero manual steps - the MVP's own success criterion. `triangles.csv` already supports the triangle side of this by filtering on `origin_month + dev_month - 1`; the remaining work is the claim and transaction extracts.
- **Payment-date (calendar-year) inflation** - the shipped inflation is by occurrence date, which keeps the ultimate-first invariant. Payment-date inflation creates the calendar-year development distortions reserving methods actually struggle with, but it makes the ultimate emergent and interacts with case adequacy, so it deserves its own design. Deferred in the claims-inflation spec.
- **Long-tail classes** - assess whether the engine can extend to long-tail lines such as liability. Flagged in the mission as a later question, not a commitment.

## Known enablers and technical debt

Small items that make the above cheaper or are worth cleaning up when touched:

- The `ui` command's reference-data directory is hardcoded to private passenger auto; generalising it is a prerequisite for a second line of business's realism view.
- Reference-data loading should be keyed per line of business (currently a single embedded set, though the other five curated families are already in the repo - see above).
- The nil-claim runoff floors its case release at one cent to guarantee a close-date transaction; if very small initial estimates ever become common, revisit the runoff's sub-cent behavior more broadly.
- `docs/todo.md` is the live backlog and the best source for "what to clean up next": the open code-review, security-review and stage-isolation findings in one place. Its "Exposure milestone" section lists what must change before the UI is shared beyond one laptop.
