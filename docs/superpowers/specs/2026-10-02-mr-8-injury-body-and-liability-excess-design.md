> **OUT OF CONTEXT - do not read (2026-10-02):** historical design record, kept for provenance only. Agents must not load this file into context or treat it as a source of truth; it records decisions as of its own date and may not match current behaviour. For how the system works today see `README.md`, `AGENTS.md`, and `docs/architecture.md`.

# MR-8: injury severity body and personal liability excess - design

Date: 2026-10-02

## Goal

Close MR-8. Its two halves:

1. The personal motor preset applies the insured's excess to its liability
   claims, so third parties are underpaid and the reported property damage
   frequency depends on the own-damage excess.
2. Injury severity is a bare Pareto, so no injury claim is below the Pareto
   minimum and the mode sits at that floor ($5,000-$6,000 after excess in the
   personal preset, $9,000 in the commercial one).

## 1. Severity kind `lognormal_pareto`

`lob.LognormalPareto`, YAML `kind: lognormal_pareto`, with the existing keys
`median`, `sigma` (the lognormal body), `scale` (the threshold where the tail
starts, its minimum) and `alpha` (the tail index). All dollar amounts are in
start-year dollars, as for the other dollar kinds.

- Density: below `scale`, the body lognormal truncated at `scale`, weight
  `1 - w`; above it, a Pareto with minimum `scale` and index `alpha`, weight
  `w`.
- Continuity at `scale` sets `w`. With `z = ln(scale / median) / sigma`,
  `r = phi(z) / (sigma * alpha * Phi(z))` and `w = r / (1 + r)`. Scaling the
  distribution by a factor leaves `z`, and so `w`, unchanged, so the claims
  inflation index and pricing's trend scale `median` and `scale` alike.
- Draw: one uniform `U`. If `U < 1 - w`, the loss is
  `exp(ln median + sigma * PhiInv(U / (1 - w) * Phi(z)))`, with `PhiInv` from
  `math.Erfinv`; otherwise `scale * ((1 - U) / w)^(-1 / alpha)`. One draw per
  claim, as for every other kind.
- Pricing: `E[(X - t)+]` in closed form. For `t >= scale`,
  `w * scale^alpha * t^(1 - alpha) / (alpha - 1)`. For `t < scale`,
  `(scale - t) - (1 - w) / Phi(z) * I + w * scale / (alpha - 1)`, where
  `I = scale F(scale) - t F(t) - (PM(scale) - PM(t))`, `F` the body lognormal
  CDF and `PM(a) = E[X; X <= a] = e^(mu + sigma^2/2) Phi((ln a - mu - sigma^2) / sigma)`.
  A limit `L` prices as `E[(X - d)+] - E[(X - d - L)+]`, as for the other
  dollar kinds.
- Validation: `median`, `sigma` and `scale` positive, `alpha` above 1.
- Form: four severity fields for the kind.

## 2. Personal motor liability takes no excess

`no_excess: true` on `third_party_property` and `third_party_injury`, in the
claims and pricing blocks of `motor-personal.yaml`.

## 3. Calibration

Injury moves to `lognormal_pareto` in both presets.

- Personal: about median $5,000, sigma 1.0, scale $30,000, alpha 1.8, under
  the $100,000 limit, aiming at an average injury claim of about $9,000-$10,000,
  injury carrying about half the liability cost and about three property
  damage claims per injury claim.
- Commercial: a larger body and a heavier tail under the $1m limit, keeping
  the average injury claim near today's ~$20,000.
- Each preset's injury close lag (and frequency where needed) is re-tuned
  until seeds 1-30 pass the realism gate with margin.
- All four golden hashes move by design.

## 4. Tests

- The stop-loss agrees with Monte Carlo on the kind's own draws, with and
  without a limit, at excesses below and above `scale`.
- The empirical tail share matches `w`, and the density has no step at
  `scale` (counts in equal-width bins either side agree).
- Validation names each bad field; the form registry covers the kind.
- The realism gate, drift guard and loss-ratio test pass for both presets.

## 5. Docs

README (the kind in the claim events diagram and text, personal liability
taking no excess), `docs/architecture.md`, both preset YAMLs' comments, and
`docs/review.md` (MR-8 deleted, positions renumbered).
