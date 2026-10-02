> **OUT OF CONTEXT - do not read (2026-10-02):** historical design record, kept for provenance only. Agents must not load this file into context or treat it as a source of truth; it records decisions as of its own date and may not match current behaviour. For how the system works today see `README.md`, `AGENTS.md`, and `docs/architecture.md`.

# MR-19: a drawn settlement share - design

Date: 2026-10-02

## Goal

Close MR-19. When an episode draws interim payments, they share exactly
`1 - settlement_share` of its cost, so the final payment is always
`settlement_share` of the cost: 40% in the personal preset, 50% in the
commercial preset. Anyone reading `transactions.csv` sees the pattern.

## Approach

Draw each paying episode's settlement share from a Beta with mean
`settlement_share`, the way recoveries draw their shares (`mean_share` and
`concentration`). The mean is unchanged and the share is independent of the
payment dates, so the expected paid pattern does not move and the realism
gate should barely notice.

Rejected:

- Making the final payment one more Dirichlet weight: no new parameter, but
  the final payment's mean share would depend on the number of interim
  payments, so `settlement_share` would stop meaning what it says.
- Paying each interim payment as a share of the remaining cost: changes the
  paid pattern, needs both presets recalibrated, and overlaps MR-20.

## Design

### Parameter

`lob.RunoffParams.SettlementConcentration` (YAML `runoff.settlement_concentration`):
the Beta concentration of each episode's settlement share. Higher keeps the
share closer to `SettlementShare`; 0 keeps the share fixed at
`SettlementShare`, the old behaviour, so existing YAMLs keep working.

Validation: finite and not negative. When above 0, `settlement_share` must be
below 1, since a Beta with mean 1 has no second shape parameter.

### Runoff

In `drawInterimPayments`, after the Poisson count and the Dirichlet weights,
an episode with a concentration above 0 draws

    share ~ Beta(m * k, (1 - m) * k),  m = settlement_share, k = settlement_concentration

from the claim's `runoff-claim-<id>` stream, and the interim pool is
`ultimate * (1 - share)`. The final settlement pays the rest, as before.

- An episode with no interim payment draws no share and pays 100% at close.
- A nil episode draws no payments and no share.
- A reopen episode draws its own share.
- The existing guard stays: if rounding leaves nothing for the final
  payment, the episode settles everything at close.

The draw is inline on the claim's stream, like the other runoff draws, so
later draws in a claim shift; the golden hashes are refreshed regardless.

### Presets

Both presets set `settlement_concentration: 4`, a judgement value: Beta(1.6,
2.4) for personal motor (mean 0.4, standard deviation about 0.22) and Beta(2,
2) for commercial motor (mean 0.5, standard deviation about 0.22). Both shape
parameters are above 1, so neither the final payment nor the interim pool
crowds toward zero.

### Config, UI and docs

- The mirrored config field and `ToDomain` in
  `internal/infrastructure/config/config.go`.
- A "Settlement concentration" form field in the Runoff group of
  `internal/infrastructure/web/fields.go`.
- README: the runoff diagram names the drawn share, and the worked example
  shows a drawn share. The example's "preset's `case_adequacy_mean` of 0.90"
  is stale (the preset is 1.05); it becomes an illustrative value.
- `docs/architecture.md` if it describes the share; MR-19 is deleted from
  `docs/review.md` and the positions renumbered.

## Testing

- With a concentration above 0, the final payment's share of paying
  episodes with interim payments varies across claims and averages near
  `settlement_share`.
- With a concentration of 0, every such episode pays exactly
  `settlement_share` at close (to the cent).
- `validate` rejects a negative concentration, and a concentration above 0
  with `settlement_share: 1`.
- The existing runoff invariants hold.
- Refresh the four golden hashes; the realism gate and the loss-ratio test
  stay green; re-measure the MR-19 evidence on the personal preset.
