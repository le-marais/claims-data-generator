# Property calibration and file contract

The example uses judgement assumptions for commercial building and storm
losses. Four equal-share coverholder plans per year give both early and late
binder renewals. An initial plan of 1,200 policies provides about 25 policies
per binder-month, with enough observations to see shared batch movements.
The first seed checked, 71, is the shipped seed. The next two integers provide
replications. All three use the same configuration. The timing assumptions
were fixed before examining method errors. No accuracy tolerance was fitted.

## Rolling cuts

Run `python3 tools/property-calibration/calibrate.py` from the repository.
Each test uses ten annual accounts from 2017 through 2026. The comparator
fits volume-weighted age-to-age ratios on positive-denominator observed pairs
and extends the final observed age at unity. The mature column counts accounts
at least four calendar years old which exactly equal contractual premium.
The last three columns show estimates or observations for the valuation year's
account relative to its full contractual premium. At midyear the underwriter
sum includes only binders already incepted. At year end it includes all four.
The chain ladder projects the full year's cohort at both cuts.

| Seed | Valuation | Mature accounts | Declared / truth | Premium CL / truth | Available estimate / truth |
| --- | --- | ---: | ---: | ---: | ---: |
| 71 | 2025-12-31 | 5 | 55.4% | 104.6% | 97.3% |
| 71 | 2026-06-30 | 6 | 14.6% | 104.8% | 48.5% |
| 71 | 2026-09-30 | 6 | 28.8% | 93.3% | 77.3% |
| 71 | 2026-12-31 | 6 | 53.9% | 101.1% | 102.3% |
| 72 | 2025-12-31 | 5 | 51.2% | 98.2% | 102.4% |
| 72 | 2026-06-30 | 6 | 14.3% | 110.3% | 50.5% |
| 72 | 2026-09-30 | 6 | 29.2% | 99.3% | 80.6% |
| 72 | 2026-12-31 | 6 | 47.2% | 90.7% | 110.2% |
| 73 | 2025-12-31 | 5 | 53.6% | 105.0% | 110.2% |
| 73 | 2026-06-30 | 6 | 14.6% | 103.9% | 51.9% |
| 73 | 2026-09-30 | 6 | 31.4% | 104.8% | 76.9% |
| 73 | 2026-12-31 | 6 | 53.4% | 104.0% | 103.3% |

The separate backlog stress holds batch `20261-2026-11` (26 declarations)
from 10 December 2026 to 9 January 2027. This changes only the processing
date. No affected correction precedes the held initial. At December close,
observed 2026 premium falls from 801,114,569 cents to 767,205,211 cents and
CL/truth from 101.09% to 96.81%. At 31 March 2027 both scenarios observe
1,111,039,927 cents and project 102.16% of ultimate. Risks, claims, booked
amounts, and full-runoff premium are identical.

## CSV interface

The full audit export includes past and future records. Calendar dates use
ISO dates, monthly periods use YYYY-MM. Monetary columns ending in
`_cents` are signed integer cents. Legacy money columns use decimal dollars.

| File | Ordered columns |
| --- | --- |
| binders.csv | binder_id, coverholder_id, inception, expiry, year_of_account, estimate_cents |
| policy_binders.csv | policy_id, binder_id |
| premium_batches.csv | batch_id, binder_id, reporting_month, processed_date |
| premium_declarations.csv | declaration_id, original_declaration_id, policy_id, binder_id, policy_inception, effective_date, batch_id, processed_date, type, amount_cents |
| triangles.csv (binder basis) | origin_month, dev_month, paid, paid_net, incurred, reported_count, declared_premium_cents |
| claim_episodes.csv | claim_id, episode, open_date, close_date |

Policy and claim identifiers join the ordinary policy, claim and transaction
exports. The warm-up 2016 binders are excluded from the retained origins.
Policies attaching to the 2026 account continue into the next year. The
binder file includes the plan available on each inception. At any earlier
valuation omit that binder's estimate. Premium movements are admitted by
processed date; effective date and policy inception do not release them.
A declaration type is initial, additional_correction or return_correction.
An initial row's original_declaration_id is zero; every correction references
its own initial row. All policies reconcile exactly in cents at full runoff.

The monthly grid contains incremental values, including zeros. The full
rectangle includes future development. Apply the calendar valuation mask
before obtaining observed values. In exposure.csv the binder basis holds
full contractual premium and full policy-year exposure for validation.
Observed exposure can be rebuilt from the eligible policies' coverage days.
Episode opens and closes support as-of settled counts, including reopens.
