# SL-5 knob isolation - design (2026-07-21)

Closes finding **SL-5** from `docs/code-review-2026-07-18.md`: two RNG draws are
skipped when their knob is off, shifting every later draw that shares the same
sub-stream and so breaking the reproducibility / knob-isolation contract that
`ReopenSimulator` and the recovery per-claim split already uphold.

Approach chosen: **hybrid** (always-consume for nil, per-type sub-streams for
recoveries) - the smallest change that fully isolates both knobs.

## Background: the two defects

**Defect A - nil claim draw.** All claims on a policy draw sequentially from one
per-policy stream (`internal/domain/claim/claim.go:74`). The nil Bernoulli
(`claim.go:121`) is only drawn when `NilProbability > 0`:

```go
isNil := s.params.NilProbability > 0 && src.Bernoulli(s.params.NilProbability)
```

Toggling nil on therefore consumes one extra uniform per claim, reshuffling the
occurrence, report, close and severity draws of every *later* claim on the same
policy.

**Defect B - salvage short-circuit.** Salvage and subrogation share one per-claim
recovery stream, drawn in order (`internal/domain/transaction/recovery.go:97-98`):

```go
if k.p.Probability <= 0 || !src.Bernoulli(k.p.Probability) {
    continue
}
```

When salvage `Probability <= 0` its Bernoulli is skipped, shifting subrogation;
and even when salvage *fires* it consumes Beta + lag draws that shift
subrogation. So toggling salvage alone changes the same claim's subrogation
outcome.

## Key asymmetry

Nil is the **last** draw in each claim's sequence, so always consuming it makes
the per-claim draw count constant regardless of the nil knob - full isolation
with no structural change. Salvage is **not** last (subrogation follows it on
the same stream), so always-consuming its Bernoulli only fixes the `prob == 0`
case; the fire/no-fire shift into subrogation remains. Salvage therefore needs
its own labelled sub-stream, which the nil fix does not.

## Change A - nil isolation

`internal/domain/claim/claim.go:121` becomes:

```go
isNil := src.Bernoulli(s.params.NilProbability)
```

`Bernoulli(0)` still calls `Float64()` and returns false
(`internal/infrastructure/random/source.go:53-55`), so removing the
`NilProbability > 0` guard makes the draw unconditional. Behaviour when
`NilProbability == 0` is unchanged (no nil claims) but a draw is now always
consumed, so toggling the nil knob no longer shifts any later claim.

The comment at `claim.go:119-120` is updated to note the draw is always consumed
so the knob is shift-free.

**Golden impact: none.** The preset has `NilProbability > 0`, so the Bernoulli is
already drawn on the default run; removing the guard does not change the call
when the probability is positive.

## Change B - recovery-type isolation

In `internal/domain/transaction/recovery.go`, `simulateClaim` gives each recovery
type its own labelled sub-stream derived from the per-claim recovery stream.
Inside the `kinds` loop:

```go
ksrc := src.Split(string(k.t)) // "recovery-claim-{id}/SALVAGE", ".../SUBROGATION"
```

Every draw for that type (the Bernoulli, the Beta share, the lag lognormal) is
taken from `ksrc` instead of the shared `src`. The two types no longer share a
draw sequence, so toggling salvage's probability - or whether it fires - never
touches subrogation's stream.

The `Probability <= 0` guard may stay: skipping a type's *own* draw is now
harmless to the other type. The sequential "total recovered strictly below gross
paid" cap (salvage accumulates into `recovered` before subrogation) is unchanged;
it is accounting, not RNG, and preserves the existing invariant.

The `RecoverySimulator.Apply` doc comment (`recovery.go:38-41`) is updated to
state each recovery type draws from its own sub-stream.

**Golden impact: yes.** Salvage and subrogation draws now come from split
sub-streams rather than the sequential parent stream, so the default golden
fixture changes.

## Testing

Two new tests in `internal/application/recoveries_test.go` (nil test may live in
the claim or generate test file, following existing placement), mirroring the
style of `TestRecoveriesDoNotShiftOtherStages`:

1. **Nil no-shift.** Generate the same request twice - once with
   `NilProbability = 0`, once with the preset value. Assert every claim's
   `OccurrenceDate`, `ReportDate`, `CloseDate`, `InitialEstimate` and `OwnDamage`
   are identical across the two runs; only the `Nil` flag (and downstream
   payments) may differ. Fails before Change A, passes after.

2. **Salvage no-shift.** Generate twice - once with `Salvage.Probability = 0`,
   once with it on. Assert every `SUBROGATION` transaction (claim id, amount,
   date) is identical across both runs. Fails before Change B, passes after.

Also tighten the existing `TestRecoveriesDoNotShiftOtherStages`
(`recoveries_test.go:55`), which currently only tests both recovery types off
*together*, per the review note.

## Golden fixture

`internal/application/golden_test.go` pins a single SHA-256 over the three CSVs;
there is no per-row fixture. Update procedure:

1. During implementation, write the CSVs before and after Change B and diff them
   manually to confirm the **only** differences are in `SALVAGE`/`SUBROGATION`
   rows - nothing upstream of recoveries should move. (The nil change should
   produce no diff at all on the default run.)
2. Once confirmed, run `TestGoldenCSVBytes`, take the printed `got` hash, and
   paste it into `wantHash` (`golden_test.go:19`).

## Docs

- `claim.go:119-120` comment: nil draw always consumed, knob is shift-free.
- `recovery.go:38-41` doc comment: each recovery type has its own sub-stream.
- A short "shift-free knobs" note where the reproducibility contract is
  documented (README and/or the relevant spec) listing nil, reopening, salvage
  and subrogation as independently toggleable.

## Out of scope

- RF-11 (generalizing `RecoveryParams` to a named list of recovery types) -
  explicitly deferred.
- Any change to the nil severity model (SL-5's "nil draws independent of claim
  size" note is a separate simplification, not the isolation defect).
