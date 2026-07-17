# Mission

## Pitch

A local app that generates realistic, fully synthetic insurance claims data as dummy input to reserving processes.

## The problem

Reserving teams often need anonymized individual claims data for demos and testing. Real data is sensitive and hard to share, public datasets lack transaction-level detail, and ad-hoc scripts aren't reusable. This app produces realistic synthetic data on demand, with no data governance concerns since nothing is real.

## Who it's for

Reserving actuaries and analysts on our team first. Later, the wider actuarial community as a general-purpose tool for anyone who needs realistic synthetic claims data.

## What it does

The app simulates three linked datasets for a class of business:

1. **Policy data** - the book of policies per calendar year, used for estimating exposure and simulating claim events in every year. Includes per-policy details such as sum insured, excess, and a risk factor. Varied by calendar year to reflect economic and other business trends.
2. **Claims data** - claim events arising from the policy book, with realistic occurrence, report, and close dates and an estimated initial claim
 size which is influenced by the policy's details (sum insured, excess, risk factor).
3. **Transactions** - the case estimate movements and payments over each claim's lifetime.

The simulated behavior is realistic: claim events are driven by exposure, report lags and close delays reflect the class of business, larger claims take longer to close, and every claim's case estimate converges to zero at closure, with payments derived from case estimate movements.

There is no set valuation date - all claims develop fully and run to closure. This is deliberate: for testing, the fully developed data supports out-of-sample analysis.

Generation is reproducible - the same seed and parameters produce the same dataset, so tests can rely on repeatable data.

The engine is parameterized per line of business and focused on short tail classes - book size, volatility, delays, severities - starting with personal motor insurance.

## Differentiators

- **Transaction-level realism** - not just claim triangles, but full policy, claim, and transaction detail that resembles a real claims system extract.
- **One parameterizable engine** - adjustable parameters mean any short tail class of business can be simulated with the same code.
- **Local, fast, zero-setup** - runs entirely on a laptop, no deployment needed. The choice of interface is a design decision.

## MVP success

A team member can generate a realistic motor personal dataset (policies, claims, transactions) on their laptop and feed it into a reserving demo without manual fixes.

To assess realism, the simulated data is compared to Schedule P datasets of a similar class of business.

## Beyond the MVP

Extend the engine to more short tail lines of business (e.g. commercial property), then open the tool up to the wider actuarial community. Later, assess whether the simulation can be extended to long tail classes such as liability.

Add further features of real claims data that are excluded from the MVP:

- Nil claims (closed without payment) - done
- Reopened claims
- Recoveries (salvage and subrogation) - done
- Claims inflation across calendar years - done

See `docs/roadmap.md` for current status and sequencing.
