# Roadmap

Where claimsgen is heading, in recommended order. It lists outstanding direction only: when an item ships, delete it. `docs/architecture.md` describes what exists today.

## Longer term

- **Valuation-date extract** - every claim runs to closure for out-of-sample testing. A cut at a chosen valuation date (open claims, outstanding case, nothing known after the date) would hand users a reserving data set they can use without manual fixes, the mission's success criterion. `triangles.csv` already supports the triangle side by filtering on `origin_month + dev_month - 1`; the claim and transaction extracts remain.
- **Payment-date (calendar-year) inflation** - the shipped inflation is by occurrence date, which keeps the ultimate-first invariant. Payment-date inflation creates the calendar-year development distortions reserving methods struggle with, but it makes the ultimate emergent and interacts with case adequacy, so it needs its own design.
- **Long-tail classes** - assess whether the engine can extend to the long-tail Schedule P lines. Under the same rules, workers compensation (25 companies at $1m a year) and other liability (31, a mixed bucket that cedes heavily) have reference pools; products liability (8) and medical malpractice (2, and claims-made) are too thin for P5-P95 bands. The gate already compares both sides at age 10, so a line still developing after age 10 is scored like for like. Flagged in the mission as a later question, not a commitment.
