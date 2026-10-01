# Mission

## Pitch

A local app that generates realistic, fully synthetic insurance claims data as input to reserving demos and tests.

## The problem

Reserving teams need individual claims data for demos and testing. Real data is sensitive and hard to share, public datasets lack transaction-level detail, and ad-hoc scripts are not reusable. claimsgen produces realistic data on demand, and because nothing in it is real, there are no data governance concerns.

## Who it's for

Reserving actuaries and analysts on our team first, then the wider actuarial community.

## What it does

One run simulates a class of business and writes five linked CSVs:

- `policies.csv` - the book of policies per calendar year, with sum insured, excess, risk factor and premium
- `claims.csv` - claim events with occurrence, report and close dates and an initial case estimate
- `transactions.csv` - each claim's case estimate movements, payments and recoveries over its lifetime
- `triangles.csv` - monthly development triangles: paid, paid net of recoveries, incurred and reported claim counts
- `exposure.csv` - premium, exposure and policy count by origin month

Claim events are driven by exposure and policy details, report and settlement lags reflect the class of business, and the data carries the features of a real claims extract: nil claims, reopened claims, salvage and subrogation, and claims inflation across calendar years.

Every claim runs to closure - there is no valuation date - so the fully developed data supports out-of-sample testing of reserving methods. The same seed and parameters always produce byte-identical output.

The engine is parameterized per line of business. Personal motor ships as the embedded preset; a new short-tail class is a YAML file.

It runs as a CLI (`claimsgen generate`) or as a local browser UI (`claimsgen ui`) that also shows summary stats, triangles, distributions and a realism check.

## Differentiators

- **Transaction-level realism** - full policy, claim and transaction detail resembling a claims system extract, not just triangles.
- **One parameterizable engine** - any short-tail class can be simulated with the same code by changing parameters.
- **Local and self-contained** - a single binary with the reference data embedded, no deployment or setup.

## Success

A team member can generate a realistic personal motor dataset on a laptop and feed it into a reserving demo without manual fixes.

Realism is measured against Schedule P: the third-party (liability) section of the shipped preset must sit inside the P5-P95 bands of the private passenger auto liability reference companies.

## Next

More short-tail lines of business, starting with commercial property, then opening the tool to the wider actuarial community. Whether the engine extends to long-tail classes is a later question. See `docs/roadmap.md` for status and sequencing.
