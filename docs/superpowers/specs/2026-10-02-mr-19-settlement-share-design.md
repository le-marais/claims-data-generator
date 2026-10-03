> **OUT OF CONTEXT - do not read (2026-10-02):** historical design record, kept for provenance only. Agents must not load this file into context or treat it as a source of truth; it records decisions as of its own date and may not match current behaviour. For how the system works today see `README.md`, `AGENTS.md`, and `docs/architecture.md`.

# MR-19: a per-section settlement - design

Date: 2026-10-02

## Goal

Close MR-19. When an episode draws interim payments, they share exactly
`1 - settlement_share` of its cost, so the final payment is always
`settlement_share` of the cost: 40% in the personal preset, 50% in the
commercial preset. Anyone reading `transactions.csv` sees the pattern.

A claim-level read of the first fix (a Beta share around the one runoff
`settlement_share`) found two more payment tells: interim payments of a few
dollars ($1.64, $8.73), and one settlement shape for every section, where an
injury file usually ends on a large settlement and an own-damage file pays
once or ends on a small balance.

## Options tested

Each was prototyped and run on the personal preset (seed 1, 20,000
policies, 1998-2000) and through the realism gate on both presets at seeds 1,
42 and 7.

| Option | Final shares in one 1% bin | Interim under $25 | Injury final largest | Gate |
| --- | ---: | ---: | ---: | --- |
| Fixed share (main) | 100% | 3.0% | 66% | pass |
| One Beta share | 2.1% | 3.8% | 57% | pass |
| Each payment a Beta share of the remaining cost | 5.5% | 10.1% | 13% | 6-8 fails a seed |
| Paid to date follows cost emergence, capped | 18-20% | 0.3% | 24-41% | pass or 1 fail |
| Cost emergence plus a settlement share | 2.1% | 0.6% | 93% | pass |
| Beta share per section, $50 minimum payment | 2.5% | 0.0% | 88% | pass |

The per-section Beta share with a minimum payment is the only option that
gives each section its own shape without new failure modes. A 5%-of-cost
minimum merged too much (injury payments fell from 5.6 to 3.9 a claim); a
$50 minimum barely moved the count (5.3).

On top of it, a lump-sum probability per section makes one payment the norm
on damage claims: own damage paid once on 77% of claims and property damage
on 65%, against judgement targets of about 90% and 75% for personal motor.
A lower payment rate per section reached the same targets, but its
single-payment share falls out of the close lag, so every close-lag
recalibration would move it.

## Design

### Section settlement

`lob.SectionParams.Settlement` (YAML `claims.sections[].settlement`):

- `LumpSumProbability` (`lump_sum_probability`): the chance a paying episode
  pays its whole cost in one settlement at close, with no interim payments.
  An episode that draws no interim payments pays in one settlement anyway,
  so more claims than this pay once. 0 switches lump sums off.
- `Share` (`share`): the mean fraction of its cost an episode with interim
  payments leaves for the final settlement.
- `Concentration` (`concentration`): the Beta concentration of that share
  around `Share`; 0 fixes the share at `Share`.

Validation, on active sections only: finite values; `lump_sum_probability` in
[0, 1]; at 1 the share is never read and is not required; otherwise `share`
in (0, 1], `concentration` not negative, and `share` below 1 when
`concentration` is above 0.

They replace `runoff.settlement_share`, which moves onto the sections. A YAML
that still sets it fails to load with an unknown-field error.

### Minimum payment

`lob.RunoffParams.MinPayment` (`runoff.min_payment`): the smallest interim
payment in nominal dollars. A smaller one is held over and paid with the
next, or with the final settlement. 0 switches it off.

### Runoff

`NewRunoffSimulator(runoff, sections)` keeps each section's settlement and
develops each claim's episodes with its section's. For a paying episode open
two days or more, `drawInterimPayments`:

1. draws the lump sum when `lump_sum_probability` is above 0: a lump-sum
   episode has no interim payments;
2. draws the Poisson count; none means no interim payments;
3. draws the Dirichlet weights and the settlement share (Beta when the
   concentration is above 0), so the interim pool is `ultimate * (1 - share)`;
4. draws the payment days and sorts them, gives the weights out in date
   order, and holds a payment below `min_payment` over to the next.

The final settlement pays the rest, as before, and the existing rounding
guard stays. Every draw comes from the claim's `runoff-claim-<id>` stream.

### Presets

Both presets, judgement values:

| Section | `lump_sum_probability` | `share` | `concentration` |
| --- | ---: | ---: | ---: |
| own damage | 0.57 | 0.25 | 4 |
| third-party property | 0.28 | 0.3 | 4 |
| third-party injury | 0 | 0.6 | 4 |

`runoff.concentration` rises from 1 to 4, so the interim pool splits less
unevenly, and `runoff.min_payment` is 50.

### Config, UI and docs

- The mirrored config structs and `ToDomain`.
- Form fields: the three settlement fields in the per-section Claims group;
  `min_payment` in Runoff, which loses `settlement_share`.
- README: the runoff diagram, the sections paragraph, the worked example.
- `docs/architecture.md` where it describes the runoff; MR-19 is deleted
  from `docs/review.md`.

## Testing

- Validation names each settlement field and `runoff.min_payment`.
- A fixed share (concentration 0) pays exactly `share` at close; a drawn one
  varies around it.
- `lump_sum_probability` 1 pays every claim once; 0.5 lifts the
  single-payment share by about half the gap to 1.
- No interim payment falls below `min_payment`, and total paid is still the
  ultimate.
- Claims use their own section's settlement.
- Golden hashes refreshed; the realism gate and the loss-ratio test pass;
  the single-payment shares by section re-measured on both presets.
