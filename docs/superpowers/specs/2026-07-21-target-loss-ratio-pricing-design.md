# Target loss ratio pricing and per-year drift guard (MF-1 / MF-7)

Date: 2026-07-21
Addresses: MF-1 (high), MF-7 (low) from `docs/code-review-2026-07-18.md`.

## Problem

Premium per policy grows at `sum_insured_inflation` (~3%/yr) because
`Premium = SumInsured x PremiumRateFactor x RiskFactor` and the rate factor is
constant (`internal/domain/policy/book.go:80`, `internal/infrastructure/config/motor-personal.yaml:19`).
Losses grow faster: own damage scales with the (already inflating) sum insured
*and* the claims-inflation index (~7.1%/yr), third party carries the claims
index only (~4%/yr), for a blended loss trend near 6%/yr. The accident-year
loss ratio therefore climbs monotonically from 69.3% (1998) to 101.0% (2007).

The realism gate checks only the pooled ten-year loss ratio
(`internal/domain/triangle/compare.go:195-203`), so it cannot see the drift.

## Goal

Premium tracks the loss trend so the accident-year loss ratio stays flat, and
the realism gate guards against the drift returning. Fix the pricing model
(MF-1) via a target-loss-ratio knob (MF-7) and add a per-year drift check to
the gate.

## Design

### 1. Pricing model

Replace the constant `premium_rate_factor` with a `target_loss_ratio` knob.
Price each policy on its expected ultimate gross incurred loss divided by the
target:

```
Premium_i = ExpectedLoss(policy_i, coverYear) / target_loss_ratio
```

`ExpectedLoss` is deterministic (no RNG draws, so the labelled-sub-stream
reproducibility contract is untouched):

```
ExpectedLoss = baseFrequency * riskFactor_i
             * [ tp_weight       * E_TP[(I_y * X    - excess_i)+]
               + (1 - tp_weight) * E_OD[(I_y * SI_i * frac - excess_i)+] ]
             * (1 + reopen_prob * estimate_factor)
```

- The `(. - excess)+` term folds the sub-excess discard and the
  net-of-excess amount into one closed form - exactly the quantity the
  simulator emits (`internal/domain/claim/claim.go:112-115`). A separate
  P(keep) factor is not needed; the `+` handles the discard.
- Own damage: `I_y * SI_i * frac` is lognormal with median
  `I_y * SI_i * own_damage_median_fraction` and sigma `own_damage_sigma`.
  Use the standard stop-loss form
  `E[(X - e)+] = E[X] * Phi(d1) - e * Phi(d2)`,
  where `E[X] = median * exp(sigma^2 / 2)`,
  `d1 = (ln(E[X]/e) + sigma^2/2) / sigma`, `d2 = d1 - sigma`.
  When `e <= 0`, `E[(X - e)+] = E[X] - e`.
- Third party: `I_y * Pareto(scale, alpha)` is Pareto with scale `I_y * scale`
  and the same alpha. With `xm = I_y * scale`:
  if `e <= xm`, `E[(X - e)+] = E[X] - e` with `E[X] = xm * alpha/(alpha-1)`;
  if `e > xm`, `E[(X - e)+] = (xm / (alpha - 1)) * (xm / e)^(alpha - 1)`.
- `I_y` is the claims-inflation index at the policy's cover-start year
  (`InflationParams`, compounded from 1.0 at the start year). A policy spans two
  occurrence years; using the start-year index leaves a negligible half-year
  residual and introduces no systematic drift.
- Reopening adds a small constant uplift `(1 + reopen_prob * estimate_factor)`
  because the reopen episode's extra payment lands in the fully developed
  incurred triangle.
- Recoveries are excluded: the target is on the gross-incurred basis the
  summary already reports. The net-of-recovery loss ratio therefore sits
  slightly below target; this is documented, not corrected here.

Risk differentiation is preserved automatically because expected loss is
proportional to the risk factor. Sum-insured and risk-factor heterogeneity
still vary premiums policy to policy.

### 2. Placement and layering

- The formula lives as an `ExpectedLoss` method on the claim-parameter types
  (`ClaimParams` / `SeverityParams` in `internal/domain/lob/lob.go`), next to
  the severity model it mirrors, so the two cannot drift silently.
- `NewBookSimulator` (`internal/domain/policy/book.go:31`) takes claim params in
  addition to book params. Update the one caller
  (`internal/application/generate.go:47`).

### 3. Config and knob changes

- `internal/infrastructure/config/motor-personal.yaml`: drop
  `premium_rate_factor`, add `target_loss_ratio` with default **0.72**,
  recalibrated empirically after implementation (full-fidelity pricing should
  land the achieved gross LR close to the target).
- Parameter fan-out per RF-13, following the existing pattern (no refactor of
  the fan-out here): domain `BookParams`, config DTO + `ToDomain`
  (`internal/infrastructure/config/config.go`), and the UI form metadata
  (`internal/infrastructure/web/static/app.js`).

### 4. Realism gate: per-year drift check

Add a self-consistency check to `internal/domain/triangle/compare.go`: split the
generated accident years into a first and second half, aggregate incurred and
earned premium within each half, and check the ratio of the second-half loss
ratio to the first-half loss ratio stays within a tolerance
(`[1/tol, tol]`, proposed **tol = 1.10**). A value near 1 means a flat
loss-ratio trend. Aggregating each half (rather than a per-year max/min) is
robust to a single outlier year - important while own-damage severity is still
uncapped (SL-3). This needs no reference data, so it sidesteps the SL-2
reference-immaturity problem. The incurred triangle is already net of
recoveries, so the drift check runs on net incurred; the trend is basis-neutral.
Surface the result in `Report` / `Report.Pass()` and the realism UI tab.

### 5. Testing

- Unit-test `ExpectedLoss` closed forms against a Monte-Carlo estimate of the
  actual simulator draws, tying the formula to the model.
- Test that per-year loss ratios on the default run are flat within tolerance
  (pins MF-1 fixed).
- Test the new gate check fails on a deliberately drifting config.
- Update premium-value assertions affected by dropping `premium_rate_factor`.

### 6. Docs

README: replace the `premium_rate_factor` description with
`target_loss_ratio`; remove or replace the MF-1 drift note once the fix lands.

## Out of scope and interactions

- SL-3 (own-damage cap) and SL-4 (inflation rebase): pricing is on expected
  loss, so it auto-adapts if those land later - only the target default would
  need recalibration, not the mechanism. Not fixed here.
- SL-2 (reference loss-ratio immaturity): untouched; the self-consistency gate
  avoids depending on it.
- Recovery netting in the target basis: excluded, documented.
