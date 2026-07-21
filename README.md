# claimsgen

A local CLI app that generates realistic, fully synthetic insurance claims data as dummy input to reserving processes. Nothing in the output is real, so there are no data governance concerns.

One run produces three linked CSV datasets for a class of business:

- **policies.csv** - the book of policies per calendar year: cover dates, sum insured, excess, risk factor, premium
- **claims.csv** - claim events with occurrence, report and close dates plus the initial case estimate
- **transactions.csv** - each claim's case estimate movements, payments, and recoveries (salvage and subrogation) over its lifetime

## Quickstart

```
go build ./cmd/claimsgen
./claimsgen generate
```

That generates a personal motor book (10 calendar years from 1998, 20,000 policies in year one) into `./output/` using the embedded preset. Options:

```
claimsgen generate \
  --config my-lob.yaml \      # line of business parameters (default: embedded motor-personal preset)
  --seed 42 \                 # master random seed (same seed + config = byte-identical output)
  --out ./output \            # output directory
  --start-year 1998 \         # first calendar year of the book
  --years 10 \                # number of calendar years
  --initial-book-size 20000   # policies written in the first year
```

## Browser UI

```
./claimsgen ui
```

Serves a local web UI on `http://127.0.0.1:8080` (`--port` to change). It offers the same run flags as the CLI plus every line of business parameter (prefilled from the preset, editable, including a Recoveries group for the salvage and subrogation probabilities, mean shares, and lags, and a reopen probability and reopen estimate factor for reopened claims), writes the same three CSVs on Generate, and shows the result: per-year summary stats (including a Recovered column and a Reopened column), paid and incurred development triangles with age-to-age factors and a Paid (gross) / Paid (net) / Incurred toggle, severity and lag distributions, and the run's position inside the Schedule P realism bands. The Schedule P reference data is embedded in the binary.

Configure a run in the sidebar and hit Generate - the summary tab shows per-year stats for the book:

![Run configuration and per-year summary](docs/screenshots/ui-summary.png)

| Development triangles | Distributions |
| --- | --- |
| Cumulative triangles as heatmaps - paid gross, paid net of recoveries, or incurred - with volume-weighted age-to-age factors underneath. | Claim severity (log-spaced bins) plus report and close lag histograms. |
| ![Paid development triangle heatmap with age-to-age factors](docs/screenshots/ui-triangles.png) | ![Severity and lag distribution histograms](docs/screenshots/ui-distributions.png) |

| Realism check | Realism check - failing run |
| --- | --- |
| Every metric of the default preset falls inside the bands observed across the Schedule P reference companies. | Cranking base frequency to 0.5 pushes the ultimate loss ratio outside its band. |
| ![Realism tab passing, every metric inside its reference band](docs/screenshots/ui-realism-pass.png) | ![Realism tab failing, ultimate loss ratio outside its reference band](docs/screenshots/ui-realism-fail.png) |

## How the simulation works

1. **Policy book** - each year's book size is the previous year's size times a growth factor times random noise, so the book trends upward but can shrink in individual years. Per policy: sum insured (lognormal with calendar-year inflation), a mean-1 risk factor loading claim frequency, an excess from a discrete choice set. Premium is priced to a target loss ratio - each policy's premium is its expected ultimate loss divided by `target_loss_ratio` - so the accident-year loss ratio stays flat as severities inflate.
2. **Claim events** - Poisson claim counts per policy scaled by the risk factor; short lognormal report lags; ground-up losses mixing own damage (lognormal, scaled by sum insured) and third party liability (Pareto, not capped at sum insured), then scaled by a claims-inflation index at the claim's occurrence year; losses below the excess are not reportable. Close delays are gamma distributed: own-damage claims settle fast (stretched for large claims and risky policyholders), while third-party (bodily-injury) claims draw from a slower long-tail regime, so paid losses keep developing at later ages. A share of reported claims are nil - they close without any payment at their first close.
3. **Case estimate runoff** - each claim's true ultimate cost is drawn around the initial estimate, payments split it over the claim's life, and the case estimate is a noisy view of the remaining cost that settles as the claim ages. A nil claim instead carries its case estimate through revisions and releases it to zero at close, paying nothing.
4. **Transactions** - the first row of every claim is its initial case estimate on the report date, so the outstanding case at any time is the running sum of `ESTIMATE` amounts. Every payment carries a matching case reduction. At close the outstanding case is exactly zero and total paid equals the ultimate (zero for a nil claim). Recovery rows are the only transactions dated after a claim's final close date.
5. **Recoveries** - own-damage claims can yield salvage (the wreck is sold) and subrogation (the payout is recovered from an at-fault third party). Each is a Beta-distributed share of the claim's gross paid, received a lognormal lag after the close date - subrogation typically much later than salvage. Recoveries are pure cash events: the case estimate stays gross, and a claim's total recovered is always below its gross paid. Setting a recovery type's probability to 0 switches it off.
6. **Reopened claims** - a closed claim can reopen once: the case is re-raised a lognormal lag after the first close, a second episode develops and pays an additional amount, and the claim closes for good. The reopen estimate is a configurable factor of the original initial estimate, and a nil claim that reopens pays in its second episode. claims.csv shows the final close date; the reopen is visible in transactions as the case re-raised after a release to zero. Setting the reopen probability to 0 switches reopening off.

Gross paid is the sum of a claim's `PAYMENT` rows; net paid subtracts its `SALVAGE` and `SUBROGATION` rows.

transactions.csv is emitted in claim-registration order, not date order: all of a claim's rows are written together, and because recovery rows can post-date a claim's close date, a later claim's rows can carry earlier dates. Sort by date yourself if your reserving tool expects a date-ordered ledger.

Claims inflation is a stochastic path: each calendar year's factor is a mean level (a per-line-of-business knob) times lognormal noise, compounding from the start year and drawn from its own labelled sub-stream so it stays reproducible and independent of the other stages.

Every independent decision is drawn from its own labelled sub-stream keyed by the seed and a label path, so toggling a knob is invisible to unrelated draws: turning nil claims, reopening, salvage, or subrogation on or off never reshuffles the dates or severities of any other claim or stage. (Salvage and subrogation amounts remain linked through the rule that a claim's total recovered stays below its gross paid, which is an accounting constraint, not a random draw.)

There is no valuation date: every claim runs to closure, which supports out-of-sample testing of reserving methods.

## Parameters per line of business

All behavior is driven by a YAML file mapped to the `LineOfBusiness` domain object - see `internal/infrastructure/config/motor-personal.yaml` for the annotated motor preset. A new short-tail class is a YAML file for the CLI (`generate --config my-lob.yaml` is pure YAML, no code changes); surfacing it as a UI preset also needs one registration line in the preset registry. See `docs/roadmap.md` for the second-line-of-business plan.

## Assumptions and known simplifications

The model deliberately trades some realism for a clean, reproducible engine. The main simplifications a reviewer should know about:

- **Own-damage severity trends at the claims index only and is capped at the sum insured.** Own-damage losses are sized off a fixed base-year sum insured, trended by the occurrence-year claims-inflation index alone, and capped at the policy's sum insured - so own damage and third party share the single claims-inflation trend and own damage can never exceed the cover. Third-party losses carry the same claims-inflation index and are not capped at the sum insured.
- **Case estimates re-centre on the true ultimate at the first revision**, so incurred development carries little systematic IBNER signal - incurred is close to unbiased at every age, and incurred-based methods will look flattering on this data.
- **Nil claims draw severity and probability independently of claim size**; real withdrawn or nil claims skew small.
- **No seasonality, catastrophe, or event clustering.** Occurrences are uniform within each cover period and claims are independent across policies (the only cross-policy link is the shared inflation path).
- **Each year's book is an independent cohort** - no policy renews, so per-policy claim histories never correlate across years.
- **The insurer prices risk perfectly** - premium uses the exact risk factor that drives claim frequency, with no pricing error, so loss ratios are more stable across cohorts than a real book's.

## Realism

Generated data is checked against 96 hand-curated Schedule P private passenger
auto reference companies (`data/reference/schedule p/ppauto_pos98-07/`,
accident years 1998-2007). The companies were curated from the full Schedule P
extract via `data/reference/gr-code-list.md` and `tools/prune-dec2025.ps1` to
remove low-volume and degenerate companies. Paid and incurred age-to-age
development factors and the ultimate loss ratio must fall inside the P5-P95
bands observed across those companies, with a backstop filter that drops any
company carrying no scorable signal; the full min/max range is shown for
context. The paid comparison is net of recoveries, matching how Schedule P
reports paid losses. This runs as a test gate (`TestDefaultPresetIsRealistic`,
across several seeds).

## Development

```
go test ./...
go vet ./...
```

Screenshots are regenerated with `tools/screenshots` (start the UI on port 8093, `npm install`, `node screenshots.js`).

The layout is domain-driven: `internal/domain/` holds the simulation model (policy, claim, transaction, lob, triangle) with no outside dependencies, `internal/application/` the use cases, and `internal/infrastructure/` the adapters (config, CSV, Schedule P reader, gonum-backed randomness). Design docs live in `docs/`.
