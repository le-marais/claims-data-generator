# Background context: claims data simulator

Consolidated from the transcripts in `docs/transcripts/`.

## Purpose

The team frequently needs individual insurance claims data for use in demo models and reserving processes. The app's mission is to simulate individual claims for loss reserving purposes, where the reserving exercise may be done either on an individual-claims basis or on an aggregated (triangulated) basis.

## The three datasets to simulate

**1. Claims dataset (claim events).** One record per claim, keyed by claim ID, containing:

- Date of occurrence of the event
- Report date (a report timestamp also exists in reality, but is not needed in the simulation)
- Close date, if the claim is closed - claims may still be open

**2. Transactions dataset.** Linked to the claims dataset via claim ID (a primary key / foreign key relationship). Each transaction relates to one claim and is one of two types:

- A case estimate movement (an update to the estimated outstanding claim size)
- A paid transaction (a payment made to the customer)

A key consistency requirement: the case estimate represents the amount outstanding, so when a claim is closed the cumulative case estimate must converge to zero - total paid and total case estimate must reconcile at finalization.

**3. Exposure (policy) dataset.** A simulation of all policies for the line of business - every customer who holds a policy. This defines the exposure to risk used in reserving models. For motor insurance, a policy carries things like:

- Sum insured / maximum possible loss (e.g. the vehicle being written off)
- Monthly premium paid by the customer
- Other class-specific details

What insurers actually capture varies, so only a basic form of this dataset is needed - just enough to measure exposure.

## How the app should work

**Deployment and tech stack.** The app runs entirely locally on a laptop - no deployment needed. It should be relatively fast and small to ship, suggesting Go or Rust. The front end should preferably be a web interface with buttons and interactivity (a desktop front end is acceptable if easier), but all simulation runs locally. The tech stack choice should weigh the availability of simulation packages: compound distributions (e.g. compound Poisson) and lognormal-type distributions for loss sizes, with parameterization support. Heavy parallelization isn't really needed - it could help when simulating multiple classes at once, but that's simple to dispatch.

(Speaker's caveat: these comments are rough and will be refined in the design phase.)

**Scope.** Start with personal motor insurance only, for simplicity. Other lines (e.g. commercial property) can be added later, so everything should be parameterized per line of business to make the code reusable.

## Simulation design

### Step 1: simulate the policy book (exposure)

- Simplifying assumption: one vehicle per customer; two vehicles means two policies.
- Simulate the sum insured of each vehicle (in dollars) and a risk factor per vehicle, used as a loading for how likely that policy is to claim.
- App parameters: the size of the book (number of policies) and a spread parameter controlling heterogeneity - large means a volatile book, small means a homogeneous book where sum insured and riskiness are similar across policies. These two parameters are what make the code reusable for other lines of business.
- Each policy also gets an excess. Simplify to a discrete set of choices: $0, $100, $300, $500, $1,000. The set of possible excesses is itself a parameter per line of business.

### Step 2: simulate claim events

- Each policy can claim more than once, independently.
- Simulate the date of occurrence, then the report date. The occurrence-to-report lag is a parameter: for personal motor, most claims are reported within a day or two, with the upper end around 30 days (soft bound - outliers allowed).
- At report, simulate an initial estimated loss size. It must be related to the sum insured, and a claim cannot be smaller than the excess (otherwise it isn't reportable). A distribution recommendation is requested - lognormal or Pareto were suggested - with parameters configurable per line of business.
- Personal motor can have third party insurance claims, so the loss size can be heavy tailed and is not capped at the write-off value (sum insured).
- Simulate the close date via a report-to-close lag. Suggested approach: an exponential distribution, like a queuing/wait process with a single event (explicitly not compound Poisson). A global parameter per class of business is adjusted by a formula using policy details - sum insured and policyholder riskiness - and correlated with the initial claim size. Smaller claims should on average close quicker; larger claims tend to take longer. (The speaker initially considered making close time independent of size, then explicitly reversed that.) A recommendation on the right distribution is requested, as the speaker isn't certain.

### Step 3: simulate the case estimate runoff

- Simulate the case estimate path first, before payments.
- Starting from the initial case estimate, the estimate can jump up or down over time based on some parameters - possibly a survival- or decay-type process - but it must converge to zero at the close date.
- This needs randomness combined with some bespoke logic; the math may be tricky.

### Step 4: derive payment transactions from the case estimate path

- Each payment transaction has a date and an amount. Amounts must be realistic and must not exceed the cumulative case estimate at that point.
- Base rule: when the case estimate drops, assume a payment of exactly the size of the gap (e.g. an estimate starting at $10,000 that drops shortly after implies an initial payment of the difference).
- But not every decrease should be a payment, so some random component is needed to decide which decreases are payments versus pure estimate revisions. This isn't fully designed yet - a recommendation is requested.
