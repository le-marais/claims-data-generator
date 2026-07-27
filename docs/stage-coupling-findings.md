# Stage isolation findings: policy / claims / transactions

Goal: the three generated datasets - **policy** (book), **claims**, and **transactions** (runoff + recoveries) - should share as little parameter and code context as possible. Each stage should own its own knobs, and cross-stage dependencies should be either removed or made explicit through a narrow contract rather than implicit reach-through.

This document lists every place where context bleeds across those boundaries today, with a suggested alteration for each. It is a review list, not a change - action items are yours to accept or reject.

## How to read this

Each finding is tagged:

- **Type** - `config coupling` (a stage reads another stage's parameters), `logic duplication` (the same model exists in two stages and must be kept in sync), `data smuggling` (a value is carried on a struct across a boundary that does not need it), or `namespacing` (a parameter lives under the wrong stage).
- **Nature** - `removable` (the coupling is incidental and can be cut), or `inherent` (the domain genuinely links the stages, so the best you can do is make the link explicit and single-sourced).

A quick summary table is at the end.

## What is already well isolated

Worth stating so the review stays focused:

- **Randomness.** Every stage draws from its own labelled sub-stream (`book`, `inflation`, `claims`, `reopening`, `runoff`, `recovery`) and, below that, per-entity streams keyed on IDs. Toggling one stage's knobs does not reshuffle another's draws. This part of the design does not need changing.
- **Per-stage sub-configs.** `LineOfBusiness` splits into `Book`, `Claims`, and `Runoff`, and most simulators take only their own slice. The bleeds below are the specific exceptions.

---

## F1 - Policy pricing embeds the entire claims model

**Type:** config coupling + logic duplication. **Nature:** inherent link, but currently implicit and duplicated.

**Where:**
- `internal/domain/policy/book.go:28,33` - `BookSimulator` stores `claims lob.ClaimParams`.
- `internal/domain/policy/book.go:78` - `premium := s.claims.ExpectedPolicyLoss(...) / s.book.TargetLossRatio`.
- `internal/domain/lob/expectedloss.go` - `ExpectedPolicyLoss` reads `Severity`, `BaseFrequency`, and `Reopening`, and reimplements the severity mixture (Pareto + limited lognormal) that also lives in `internal/domain/claim/claim.go` (`drawGroundUpLoss`, the own-damage cap, the inflation trend).

**What bleeds:** the policy stage cannot run without the full claims parameter set, and the expected-loss math is a second, deterministic copy of the stochastic severity model in the claim package. The two models must stay numerically consistent by hand or premiums drift away from the target loss ratio.

**Why it matters:** this is the single largest cross-stage dependency. Any change to the claims severity model (a new component, a different cap rule, a distribution swap) silently requires a matching change in `expectedloss.go`, and there is no compiler or test link forcing that.

**Suggested alteration:** introduce a pricing port that the book stage depends on instead of `ClaimParams`, e.g.

```go
type LossPricer interface {
    ExpectedLoss(sumInsured, excess, riskFactor float64, year int) float64
}
```

The application layer constructs the pricer from the claims model and injects it into `NewBookSimulator`, so `policy` no longer imports claim severity knobs. Better still, source the pricer and the simulator from one severity model type so there is a single definition of the mixture, removing the duplication rather than only hiding it. If you prefer minimal churn, at least move `ExpectedPolicyLoss` next to the claim severity code so the two live side by side.

**Status (2026-07-27):** Addressed. Premium is now priced from a policy-owned `PricingParams` block (see `docs/superpowers/specs/2026-07-27-independent-pricing-basis-design.md`); the `policy` package no longer imports `ClaimParams`, and the duplicated inflation-mean path (the F2 aspect) is now the intentional pricing-vs-truth separation.

---

## F2 - Claims inflation mean is duplicated across pricing and the claims stage

**Type:** logic duplication. **Nature:** removable. **Partly reduced by F1 (2026-07-27).**

**Where:**
- `internal/domain/policy/book.go:57` - `inflation := math.Pow(s.pricing.InflationMean, float64(y))` (a deterministic mean path used for pricing).
- `internal/domain/claim/inflation.go` - `NewInflationIndex` builds the *stochastic* path from `claims.Inflation`, and the claims stage trends losses by it.

**What bleeds:** two different code paths derive an inflation factor and each hard-code the compounding rule, so a change to how inflation compounds must be made in both.

**Why it matters:** it is a subtle consistency trap - pricing uses `Mean^y`; the claims stage uses a compounded noisy index whose expectation is `Mean^y` only approximately. Divergence here shows up as a systematic loss-ratio bias that is hard to trace.

**Status (2026-07-27):** Partly reduced. The independent pricing basis (F1) split the *shared parameter*: pricing now reads its own assumed `pricing.InflationMean`, and the claims stage reads the true `claims.Inflation` - they are meant to differ, so this is no longer a coupling. What remains is the *logic* duplication: `book.go:57` and `inflation.go` still each hard-code the `Mean^y` compounding independently.

**Suggested alteration:** give the inflation model one type that can yield both an expected factor and a sampled factor for a given year, and have both stages consume it, rather than each raising `Mean` to a power independently. This folds naturally into the F1 pricing port (the pricer takes the expected inflation index).

---

## F3 - Claims stage reaches into a book parameter (`SumInsuredInflation`)

**Type:** config coupling. **Nature:** inherent, but can be made explicit.

**Where:**
- `internal/application/generate.go:55` - `.WithBaseYear(req.LOB.Book.SumInsuredInflation, req.StartYear)`.
- `internal/domain/claim/claim.go:77-91` - `WithBaseYear` / `baseSumInsured` deflate the policy's drifted sum insured by `sum_insured_inflation` to recover base-year terms.

**What bleeds:** the claims stage needs a *book* knob (`SumInsuredInflation`) to interpret the sum insured it reads off each policy. So the claims severity model is coupled to how the book drifts sums insured.

**Why it matters:** the sum-insured drift rate is defined once in `book`, but its value is now load-bearing in two stages. If book drift changes, claims severity scaling changes too, invisibly.

**Suggested alteration:** move the base-year concept into the policy record. Have the book stage store a `BaseYearSumInsured` (or an underwriting-year offset) on each `Policy`, computed where the drift rate already lives. The claims stage then reads a self-describing field off the policy and never needs the book's drift rate. This removes the config reach-through and keeps the coupling as ordinary policy→claim data flow (which is inherent and fine).

---

## F4 - Recovery parameters are namespaced under claims but drive the transaction stage

**Type:** namespacing. **Nature:** removable.

**Where:**
- `internal/application/generate.go:62` - `transaction.NewRecoverySimulator(req.LOB.Claims.Recoveries)`.
- `internal/domain/lob/lob.go` - `RecoveryParams` sits inside `ClaimParams`; the YAML has `claims.recoveries`.

**What bleeds:** recoveries (salvage and subrogation) are produced by the transaction stage, but their parameters live under `claims`. The transaction stage therefore reaches into the claims config block.

**Why it matters:** it blurs which stage owns recoveries and makes the claims parameter surface look larger than the claims stage actually uses.

**Suggested alteration:** move `Recoveries` out of `ClaimParams`. Either into `RunoffParams` (they are part of the transaction lifecycle) or into a new sibling group, and mirror the move in `motor-personal.yaml` (`recoveries:` at the top level or under `runoff:`). Update the config DTO in `internal/infrastructure/config` accordingly.

---

## F5 - Nil and reopening are claims knobs but drive the transaction lifecycle

**Type:** config coupling + data smuggling. **Nature:** partly inherent; the ownership can be clarified.

**Where:**
- `NilProbability` and `Reopening` live under `ClaimParams`.
- The claims stage sets `Claim.Nil` (`claim.go`) and the reopen fields (`reopen.go`), but the transaction stage consumes them: `runoff.go:83,85,90,91` branch on `c.Nil`, `c.FirstCloseDate`, `c.ReopenDate`, `c.ReopenEstimate`.

**What bleeds:** the nil and reopen decisions are made in the claims stage but their entire effect is a transaction-stage lifecycle (extra episodes, zero-payment closes). These knobs span the claims and transaction stages.

**Why it matters:** it is hard to reason about "what does the transaction stage depend on" when its behaviour is driven by flags set two stages upstream.

**Status (2026-07-27):** The pricing sub-point is resolved. Pricing no longer reads `claims.Reopening` for the reopen uplift; the independent pricing basis (F1) gave it its own assumed `pricing.ReopenProbability` and `pricing.ReopenEstimateFactor` (`internal/domain/lob/expectedloss.go`). The claims-vs-transaction lifecycle ownership below is unchanged.

**Suggested alteration:** decide the canonical owner of the claim lifecycle. The cleaner split is: the claims stage owns occurrence, report, and severity only; the transaction/runoff stage owns the *lifecycle* (nil, close timing, reopen), drawing those decisions itself. That would move `NilProbability` and `Reopening` into the transaction config and remove the nil/reopen fields from the `Claim` struct. If reproducibility layering makes that too invasive, the lighter step is to regroup these as explicitly lifecycle knobs.

---

## F6 - `Claim.RiskFactor` is carried as policy state but only used inside the claims stage

**Type:** data smuggling + stale comment. **Nature:** removable.

**Where:**
- `internal/domain/claim/claim.go:26-27` - field `RiskFactor float64`, commented "carried from the policy for downstream stages".
- Set at `claim.go:201` from `pol.RiskFactor`.
- Read only at `claim.go:185` (close-lag draw) and `reopen.go:46` (reopen close-lag draw). No transaction-stage file reads it (confirmed by search - `runoff.go` and `recovery.go` never reference `RiskFactor`).

**What bleeds:** a policy attribute is copied onto the persisted `Claim` struct with a comment implying a downstream (transaction) dependency that does not exist. It suggests a policy→transaction link that is not real.

**Why it matters:** it overstates the coupling and enlarges the `Claim` type with a field that is neither written to CSV nor read outside the claims stage.

**Suggested alteration:** drop `RiskFactor` from the exported `Claim` struct. The close lag is computed inside the claims stage where the originating `policy.Policy` (and its `RiskFactor`) is already in hand, so pass it directly to `drawCloseLag` at draw time. For the reopen pass, either compute the close lag during the first claims pass or thread the value through an internal (unexported) structure. At minimum, correct the comment to say it is claims-internal.

---

## F7 - `Claim.OwnDamage` crosses claim → transaction for recovery eligibility

**Type:** data coupling. **Nature:** inherent.

**Where:**
- Set in the claims stage (severity mixture in `claim.go`).
- Read at `recovery.go:87` - `if !c.OwnDamage || paid <= 0 { return nil }`.

**What bleeds:** a claims-stage severity decision ("this claim is own damage vs third party") is consumed by the transaction stage to decide whether recoveries apply.

**Why it matters:** this is a genuine hand-off - only the severity draw knows the claim type, and recovery eligibility genuinely depends on it. It cannot be fully removed without re-deriving the classification, which only the claims stage can do.

**Suggested alteration:** accept it as an inherent, deliberate contract, but make it explicit. Consider a small, named claim-classification value (e.g. a `ClaimKind` enum) rather than a bare bool whose meaning ("severity mixture picked own damage") is only clear from a comment. Document it in the architecture doc as the one intended claim→transaction data dependency, alongside the initial estimate and dates.

---

## F8 - Stage constructors receive whole parameter blocks, so boundaries are not enforced

**Type:** structural. **Nature:** removable.

**Where:**
- `NewBookSimulator(book, pricing)` takes all of `PricingParams`; other stage constructors likewise take a whole domain sub-struct by value.
- Nothing structurally stops a stage from reading fields outside its concern - the separation is by convention.

**What bleeds:** there is no compile-time boundary preventing a stage from reaching into another stage's knobs; the current separation is by convention only, which is how F1-F5 crept in.

**Why it matters:** without an enforced boundary, future changes will re-introduce reach-through even after F1-F5 are fixed.

**Status (2026-07-27):** Partly demonstrated. The independent pricing basis (F1) is an instance of this pattern applied to one stage: the book simulator now takes a purpose-built `PricingParams` instead of the whole `ClaimParams`, so it structurally cannot read claims knobs. The remaining stages still take whole sub-structs.

**Suggested alteration:** give each stage a narrow, purpose-built input - either a small per-stage config struct (as `PricingParams` now is) or an interface exposing only what that stage needs. Keep `LineOfBusiness` as the aggregate the config layer produces, and map it to per-stage inputs at the application boundary in `generate.go`. The compiler then enforces isolation.

---

## Out of scope: read-side joins in analytics

The analytics (`internal/domain/triangle`, `internal/application/summary.go`, `histogram.go`) legitimately join across all three datasets - triangles need claim occurrence years, transaction amounts, and policy premium together. This is read-only aggregation over already-generated data, not generation-time coupling, so it is not a bleed and needs no change. Noted here only so it is not mistaken for one.

## Summary table

| ID | Bleed | Type | Nature | Core suggestion |
|----|-------|------|--------|-----------------|
| F1 | Book stage embeds and duplicates the claims severity model to price premium | config coupling + logic duplication | inherent (make explicit) | **Done (2026-07-27).** Book stage now prices from its own `PricingParams`, independent of `ClaimParams` |
| F2 | Inflation mean derived twice, for pricing and for claims | logic duplication | removable | Partly reduced (2026-07-27): shared param split by F1; `Mean^y` logic still duplicated. One inflation model yielding expected and sampled factors |
| F3 | Claims stage reads `Book.SumInsuredInflation` for base-year deflation | config coupling | inherent (make explicit) | Store base-year sum insured on `Policy` |
| F4 | Recovery params namespaced under `claims` but used by transactions | namespacing | removable | Move `Recoveries` under `runoff`/its own group + YAML |
| F5 | Nil and reopening are claims knobs but drive the transaction lifecycle | config coupling + data smuggling | partly inherent | Pricing sub-point resolved by F1 (2026-07-27). Move lifecycle decisions to the transaction stage, or clearly regroup |
| F6 | `Claim.RiskFactor` carried as policy state but only used inside claims | data smuggling + stale comment | removable | Remove from `Claim`; pass at draw time; fix comment |
| F7 | `Claim.OwnDamage` crosses claim → transaction for recovery eligibility | data coupling | inherent | Keep, but make it an explicit `ClaimKind` contract |
| F8 | Stages receive whole parameter blocks; boundaries are convention-only | structural | removable | Partly demonstrated (2026-07-27): F1 gave the book stage a narrow `PricingParams`. Per-stage narrow inputs / interfaces for the rest |
