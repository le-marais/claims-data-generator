# Reopened claims design

Date: 2026-07-17
Status: shipped

## Summary

A closed claim can reopen once: after a lag the case estimate is re-raised and a second, smaller runoff episode develops and pays an additional amount, then the claim closes for good. claims.csv keeps its schema - `close_date` becomes the final close - and the reopen is visible in transactions.csv the way a real extract shows it: an `ESTIMATE` row re-raising the case after it had been released to zero. Nil claims can reopen and pay; the nil flag then describes their first episode only. Reopening is decided in its own post-pass with per-claim sub-streams, so switching it on never reshuffles existing stages. This is the last item from the mission's "further features of real claims data" backlog.

## Decisions made during design

- claims.csv shows the latest close date only. The schema is unchanged; consumers keep working, and anyone reconstructing episode history can do it from transactions, exactly like a real claims extract. Adding reopen columns and one-row-per-episode were both rejected as contract changes for knowledge a real extract would not state.
- Cost model: an additional ultimate per episode. The original episode runs exactly as today; if the claim reopens, a reopen case estimate is drawn and a second, shorter runoff episode develops it with the existing case-adequacy machinery. Ultimate-first holds within each episode, and non-reopened claims are completely untouched. Splitting one pre-drawn total ultimate across episodes was rejected: it couples the episodes and changes existing behavior for every reopened claim.
- At most one reopen per claim. Covers the dominant real-world case and keeps the invariants simple; the episode design leaves the door open to chains later. A geometric reopen chain was rejected as rare-tail complexity in short-tail business.
- Nil claims can reopen, and their second episode pays. A closed-without-payment claim reopening and then paying is one of the most characteristic real reopen patterns. The nil invariant becomes: a claim that is nil and never reopens pays nothing.
- Reopening is decided in a post-pass after claim IDs are assigned (like recoveries), not inline in the claim-events stage, so per-claim labelled sub-streams keep every other stage byte-identical when the feature is toggled.

## Parameters and validation

New YAML block under `claims`, mirrored in the exported config DTOs and the `lob` domain object:

```yaml
claims:
  reopening:
    probability: 0.04       # chance a closed claim reopens once; 0 switches reopening off
    estimate_factor: 0.45   # mean of the reopen case estimate as a factor of the claim's original initial estimate (user-facing knob)
    estimate_sigma: 0.5     # sigma of the lognormal noise on the reopen estimate (not in the UI form)
    lag_median_days: 90     # median days from first close to reopen (not in the UI form)
    lag_sigma: 0.7          # sigma of the lognormal close-to-reopen lag (not in the UI form)
```

Validation in `lob`:

- `probability` must be in `[0, 1)`, with the YAML comment and UI tooltip both stating that 0 disables reopening.
- `estimate_factor` must be positive (it may exceed 1: reopens can be bigger than the original claim).
- `estimate_sigma` must not be negative.
- `lag_median_days` must be positive; `lag_sigma` must not be negative.

Consistent with the repo's strict-decoding config philosophy, old YAML files lacking the new block fail validation, and the embedded motor preset is updated in the same change. Motor targets: probability around 0.03-0.06, estimate factor around 0.3-0.6, lag median around 60-120 days; the calibration determines the exact numbers and the realism gate is the acceptance test.

## Claim entity and the reopen post-pass

`Claim` gains internal fields, carried to the runoff stage but never written to CSV (the `Nil` / `OwnDamage` pattern):

- `FirstCloseDate` - the close of the first episode (what `CloseDate` is today).
- `ReopenDate` - set only for reopened claims; strictly after `FirstCloseDate`.
- `ReopenEstimate` - the case estimate re-raised at the reopen date.

`CloseDate` keeps its name and its CSV column but becomes the final close: equal to `FirstCloseDate` for the ~96% of claims that never reopen, and the second episode's close for reopened ones.

A new `ReopenSimulator` post-pass runs after the claim simulator has sorted claims and assigned IDs, seeded like the recovery stage: `src.Split("reopening")` off the master source, then `reopen-claim-<id>` per claim. Per claim:

- Bernoulli on `probability`; probability 0 makes no draw at all (true no-op).
- If it fires: reopen date = first close + a lognormal lag (median `lag_median_days`, sigma `lag_sigma`, floored at 1 day so the reopen is strictly after the first close); reopen estimate = original initial estimate x `estimate_factor` x mean-one lognormal noise of sigma `estimate_sigma`, floored at one cent; second close date drawn with the existing close-lag machinery applied to the reopen estimate (so bigger reopens stay open longer), strictly after the reopen date.
- `CloseDate` is set to the second close; `FirstCloseDate`, `ReopenDate`, `ReopenEstimate` are recorded.

Because the pass runs after sorting and ID assignment, and reads only the claim's own labelled sub-stream, enabling reopening leaves the policy book, claim events, inflation path, and every non-reopened claim's runoff byte-identical.

## Runoff episodes

For a claim without a reopen, the runoff is exactly today's code against `FirstCloseDate` (nil path included).

For a reopened claim:

- Episode 1 runs as today against `FirstCloseDate` - initial estimate on the report date, interim payments and revisions (or the nil no-payment path), outstanding released to exactly zero at the first close.
- On the reopen date, one `ESTIMATE` row re-raises the case to `ReopenEstimate`.
- Episode 2 runs the existing episode machinery between reopen date and final close: episode ultimate = `ReopenEstimate` x the existing case-adequacy noise, interim payments and revisions at the existing intensities over the episode duration, final settlement and release to exactly zero at the final close. Episode 2 reuses the nil runoff's one-cent revision floor so the terminal release always lands on the final close date, even for tiny reopen estimates.
- A reopened nil claim's first episode is the existing nil runoff (no payments); its second episode is a normal paying episode.

Total gross paid = sum of the episode ultimates (episode 1's ultimate is zero for a nil first episode). Transactions stay chronological per claim; IDs are assigned once as today.

## Recoveries interaction

Unchanged code path, naturally correct: the recovery stage keys off the final `CloseDate` and total gross paid, so recoveries remain the only transactions after the final close and the recovered-strictly-below-gross-paid bound holds over the full claim. A reopened nil own-damage claim becomes recovery-eligible because it now has paid > 0, which is the right behavior.

## Invariants

Updated and enforced by construction and in the end-to-end sweep:

- Outstanding case is exactly zero at the first close and at the final close, and never negative anywhere.
- For a reopened claim, the reopen date is strictly after the first close, and the reopen row is an `ESTIMATE` with a strictly positive amount. (No claim is made that outstanding stays positive throughout episode 2: as with episode 1 today, a revision can legitimately touch zero for tiny estimates.)
- Non-recovery transactions live in [report date, final close]; the last case activity is on the final close date.
- A nil claim that never reopens pays nothing; a reopened nil claim pays only in its second episode.
- Total paid equals the sum of episode ultimates; recoveries stay strictly below total gross paid.

## UI changes

- Two new visible fields in the Claims group: "Reopen probability" (tooltip: chance a closed claim reopens once; 0 switches reopening off) and "Reopen estimate factor" (tooltip: mean reopen case estimate as a factor of the original initial estimate). The sigma and lag parameters ride on preset defaults with no form field.
- The summary tab gains a "Reopened" column: count of reopened claims per occurrence year, next to the existing Nil claims column.
- The "Nil claims" column keeps counting first-episode nils; the tooltip/README wording becomes "closed without payment at first close".
- Triangles need no change - reopens appear as the late paid and incurred development they create.

## Realism and calibration

Reopens push paid and incurred development up at late ages and raise the ultimate loss ratio slightly. The motor preset is recalibrated only if `TestDefaultPresetIsRealistic` fails with the new defaults; the gate stays the acceptance test.

## Testing

- Reopen post-pass unit tests: probability 0 is a no-op (claims unchanged, no draw); reopen dates strictly after first close; second close strictly after reopen; estimate floors respected; determinism per seed.
- Runoff episode unit tests: outstanding zero at both closes; re-raise row on the reopen date; reopened nil pays only in episode 2; per-claim rows chronological.
- Off-switch test at the output level: with `reopening.probability: 0`, no claim's transactions show case activity after a release to zero (byte-identical to today's output for the same seed).
- Sub-stream independence: enabling reopening leaves the book, claim events (dates, estimates before the post-pass), inflation, and non-reopened claims' transactions byte-identical.
- The end-to-end invariant sweep is extended with the invariants above; recoveries tests keep passing unchanged.
- `TestDefaultPresetIsRealistic` gates the (re)calibrated preset; existing determinism tests continue to pass.
