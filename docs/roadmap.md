# Roadmap

Where claimsgen is heading, in recommended order. It lists outstanding direction only: when an item ships, delete it. `docs/architecture.md` describes what exists today.

## Near term

The next step is the second line of business below, which folds in MR-12 (separate sum-insured and risk spreads) and the per-class switches from MR-8 (liability limit, excess on liability claims).

## Mid term - second line of business

Prove the "one parameterizable engine" differentiator by adding a second short-tail class to personal motor, most likely **commercial property**. The plumbing is ready - the preset registry, the line-of-business dropdown, and the preset-driven UI form were built so a new class is a YAML file plus a registration line. The real work is:

- **Per-line-of-business reference data and calibration.** The realism gate is motor-only today: `claimsgen ui` loads the private passenger auto reference directory (`refdata.PersonalMotorDir`), and only that set is embedded. Reference data needs to be keyed per line of business so each class calibrates against an appropriate Schedule P family. `data/reference/schedule p/` already holds hand-curated companies for all six Schedule P lines - private passenger auto (96 companies), commercial auto (92), other liability (92), workers compensation (61), products liability (13) and medical malpractice (8) - kept against `data/reference/gr-code-list.md`. Schedule P carries liability lines only, so a commercial *property* class has no direct reference family; commercial auto is the closest short-tail fit.
- **Any class-specific behavior** commercial property needs that motor does not (for example severity capped harder at sum insured, no third-party tail).

## Longer term

- **Valuation-date extract** - every claim runs to closure for out-of-sample testing. A cut at a chosen valuation date (open claims, outstanding case, nothing known after the date) would hand users a reserving data set they can use without manual fixes, the mission's success criterion. `triangles.csv` already supports the triangle side by filtering on `origin_month + dev_month - 1`; the claim and transaction extracts remain.
- **Payment-date (calendar-year) inflation** - the shipped inflation is by occurrence date, which keeps the ultimate-first invariant. Payment-date inflation creates the calendar-year development distortions reserving methods struggle with, but it makes the ultimate emergent and interacts with case adequacy, so it needs its own design.
- **Long-tail classes** - assess whether the engine can extend to long-tail lines such as liability. Flagged in the mission as a later question, not a commitment.
