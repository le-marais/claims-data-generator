# Own-damage severity rework (SL-3 + SL-4) - design (2026-07-21)

Closes two findings from `docs/code-review-2026-07-18.md`:

- **SL-3** - own-damage severity is uncapped at the sum insured
  (`internal/domain/claim/claim.go:147-148`): the own-damage ground-up loss is
  `SumInsured x lognormal(fraction)` with no cap, so ~1.7% of own-damage claims
  draw a fraction above 1 and pay more than the vehicle's insured value.
- **SL-4** - own-damage severity double-counts price drift
  (`internal/domain/claim/claim.go:105-111`, `internal/domain/policy/book.go:55`):
  the loss scales with a `SumInsured` that already drifts at
  `sum_insured_inflation` (3%), then is multiplied again by the occurrence-year
  claims index (4%), giving an own-damage trend of ~7.1%/yr while third-party
  carries only the claims index (4%). The trend is the silent product of two
  knobs, so anyone calibrating "claims inflation" from the YAML misreads it.

Both live in the own-damage severity path and must be mirrored in
`ExpectedPolicyLoss` (pricing), so they are fixed together.

## Decisions

- **SL-4 rebase**: express own-damage severity in **base-year sum insured
  terms** and trend it at the **claims index only** (4%), matching third party.
  (Chosen over "sum-insured-inflation only". The trade-off accepted: own-damage
  severity is expressed relative to the base-year sum insured rather than the
  drifted one, and the cap trends differently from severity - see below.)
- **SL-3 cap**: cap the own-damage ground-up loss at the policy's drifted
  `SumInsured` (a total loss). **Defer** any salvage / total-loss coupling to a
  separate future finding - salvage stays probability-based on all own-damage
  claims for now.

## Target model

```
baseSI        = SumInsured with the sum_insured_inflation drift removed
                = SumInsured / sum_insured_inflation ^ (underwriting-year offset)

OD ground-up  = min( fraction x baseSI x claims_inflation(occ),  SumInsured )
TP ground-up  = Pareto(scale, alpha) x claims_inflation(occ)      # unchanged
payout basis  = ground-up - excess                                # unchanged
```

Net effect:

- Own-damage severity trends at the claims index (4%), the same as third party.
  One knob, one trend, legible from the YAML.
- The cap is the drifted `SumInsured` (the vehicle's insured value at the time
  of loss), which trends at `sum_insured_inflation` (3%). Because severity (4%)
  outpaces the cap (3%), the share of own-damage claims that hit the cap (total
  losses) rises slowly over the run window. This is acceptable and, unlike the
  current silent product, is now explainable.

## Change 1 - severity draw and cap (`internal/domain/claim/claim.go`)

`drawGroundUpLoss` returns the own-damage component in **base-year** terms:

```go
fraction := src.LogNormal(math.Log(sev.OwnDamageMedianFraction), sev.OwnDamageSigma)
return baseSI * fraction, true   // was pol.SumInsured.Dollars() * fraction
```

`simulateClaim` keeps the existing `loss *= s.inflation.For(occurrence.Year())`
line (correct for both components now: TP gets the claims index; OD gets the
claims index applied to a base-year scale). Immediately after, cap the
own-damage component at the drifted sum insured:

```go
loss *= s.inflation.For(occurrence.Year())
if ownDamage {
    if cap := pol.SumInsured.Dollars(); loss > cap {
        loss = cap
    }
}
estimate := loss - pol.Excess.Dollars()
```

No new random draws are introduced (the cap and the base-year rescale are
deterministic transforms of existing draws), so the **SL-5 shift-free contract
is preserved**: toggling inflation or the cap never reshuffles later claims on
the same policy.

## Change 2 - plumbing baseSI

The severity draw needs `sum_insured_inflation` and the policy's
underwriting-year offset. **Recommended approach** (the plan will confirm):
pass `sum_insured_inflation` and the run `startYear` into `ClaimSimulator`
(mirroring the existing `WithInflation` builder) and compute

```
offset = pol.CoverStart.Year() - startYear
baseSI = pol.SumInsured.Dollars() / math.Pow(sumInsuredInflation, float64(offset))
```

De-drift is by the policy's **underwriting** year (that is the year the
`SumInsured` median was drifted to in `book.go`), while the claims index is
applied by **occurrence** year, as today. The cross-year effect for claims that
occur the year after underwriting is negligible.

Alternative considered: store `baseSI` (or the drift factor) on the `Policy`
struct so the claim simulator needs no book parameters. The plan picks one; the
recommended approach keeps `Policy` free of derived fields.

## Change 3 - pricing mirror (`internal/domain/lob/expectedloss.go`)

`ExpectedPolicyLoss` must mirror both changes so premium stays priced to the
target loss ratio (otherwise the per-year loss-ratio drift guard breaks):

- Own-damage median uses **baseSI**: `odMedian = claims_inflation x baseSI x
  OwnDamageMedianFraction` (drop the current use of the drifted `sumInsured` for
  this term).
- Switch the own-damage stop-loss to a **limited** lognormal:
  `E[(min(X, cap) - excess)+] = E[(X - excess)+] - E[(X - cap)+]` with
  `cap = SumInsured` (the drifted value). Both terms use the existing
  `stopLossLognormal` closed form; since `cap > excess` always, no extra
  branching is needed.
- Third party is unchanged.

`book.go` already computes the drifted `sumInsured` and the claims
`inflationFactor` per policy; it will additionally pass `baseSI` (or the drift
factor) so the closed form can rebuild the base-year median. This keeps pricing
draw-free.

## Recalibration

Own-damage trend drops from ~7% to 4% and the severity tail is capped, so the
development triangles shift. `internal/infrastructure/config/motor-personal.yaml`
must be re-tuned to keep `TestDefaultPresetIsRealistic` green - this is part of
the work, not a follow-up. Watch **paid ATA age 2-3**, the tightest-margin
metric in the realism gate (per the config comment), after the change.

## Golden fixture

`internal/application/golden_test.go` pins a single SHA-256 over the three CSVs.
Both changes alter the default run, so the fixture changes. Update procedure:
implement the model change, confirm the realism gate passes on the re-tuned
preset, then run `TestGoldenCSVBytes`, take the printed `got` hash, and paste it
into `wantHash`.

## Testing

- **Cap**: no own-damage claim's ground-up loss exceeds its policy's
  `SumInsured`. Construct a preset with a high `OwnDamageMedianFraction` /
  `OwnDamageSigma` so the cap bites, and assert the bound holds.
- **Trend**: own-damage severity trends at the claims index, not the product of
  the two knobs. With `claims_inflation = 1` and `sum_insured_inflation > 1`,
  mean own-damage ground-up (before the cap) is flat across occurrence years
  (up to sampling noise); with `claims_inflation > 1` it trends at that rate.
- **Pricing consistency**: the per-year loss ratio stays flat across the window
  on the re-tuned preset (the existing drift guard), confirming
  `ExpectedPolicyLoss` mirrors the new severity.
- **Shift-free**: toggling the cap on/off (via a preset with/without a binding
  cap) does not move any claim's dates or the third-party severities - only the
  own-damage amounts that were above the cap change. Update
  `internal/domain/lob/expectedloss_test.go` for the base-year OD term.

## Out of scope

- Salvage / total-loss coupling (flagging capped claims as total losses and
  making salvage eligibility depend on it) - deferred to a separate finding.
- SL-2 (reference immaturity), SL-7 (case re-centring), and all other review
  findings.
- MF-2 (trailing partial accident year) is a sibling change with its own design
  doc, `docs/superpowers/specs/2026-07-21-trailing-accident-year-design.md`.
