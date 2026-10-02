# Roadmap

Where claimsgen is heading, in recommended order. It lists outstanding direction only: when an item ships, delete it. `docs/architecture.md` describes what exists today.

## Near term

The next step is the second line of business below, which folds in the per-section excess switch from MR-8 (excess on liability claims).

## Mid term - second line of business

Prove the "one parameterizable engine" differentiator by adding **commercial auto** to personal motor. Shipped presets are standard classes that match the CAS Schedule P lines, so each calibrates against its own reference, and commercial auto liability (Schedule P Part 1C) is the only other line with a reference pool large enough for P5-P95 bands. The plumbing is ready - sections of cover, the preset registry, the line-of-business dropdown, and the preset-driven UI form were built so a new class is a YAML file plus a registration line. The real work is:

- **Per-line-of-business reference data and calibration.** The realism gate is motor-only today: `claimsgen ui` loads the private passenger auto file (`refdata.PersonalMotorFile`) and scores against the companies `application.PersonalMotorCriteria` selects. Reference data and its selection criteria need keying per line of business. `data/reference/schedule p/comauto_pos_98-07.csv` is in the repo but not embedded. Commercial auto companies are smaller than personal auto ones: of those that pass Meyers' limits for the line (net premium CV under 0.399, net-to-direct CV under 0.125) and are not reinsurers, 54 in all, 42 write at least $1m a year and 25 at least $5m, so the line needs a lower size floor than personal auto's.
- **Class-specific behaviour** commercial auto needs that personal motor does not: a fleet book, liability sections that take no excess (MR-8), and slower settlement. Part 1C pays about 30% of its age-10 incurred within the first year, against about 45% for personal auto.

## Longer term

- **Valuation-date extract** - every claim runs to closure for out-of-sample testing. A cut at a chosen valuation date (open claims, outstanding case, nothing known after the date) would hand users a reserving data set they can use without manual fixes, the mission's success criterion. `triangles.csv` already supports the triangle side by filtering on `origin_month + dev_month - 1`; the claim and transaction extracts remain.
- **Payment-date (calendar-year) inflation** - the shipped inflation is by occurrence date, which keeps the ultimate-first invariant. Payment-date inflation creates the calendar-year development distortions reserving methods struggle with, but it makes the ultimate emergent and interacts with case adequacy, so it needs its own design.
- **Long-tail classes** - assess whether the engine can extend to the long-tail Schedule P lines. Under the same rules, workers compensation (25 companies at $1m a year) and other liability (31, a mixed bucket that cedes heavily) have reference pools; products liability (8) and medical malpractice (2, and claims-made) are too thin for P5-P95 bands. The gate already compares both sides at age 10, so a line still developing after age 10 is scored like for like. Flagged in the mission as a later question, not a commitment.
