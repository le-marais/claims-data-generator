> **OUT OF CONTEXT - do not read (2026-10-13):** implementation plan, deleted once its work ships. Agents must not load this file into context or treat it as a source of truth. For how the system works today see `README.md`, `AGENTS.md`, and `docs/architecture.md`.

# Weather peril implementation plan

**Goal:** Add an event-clustered `weather` section of cover (hail and storm damage to the insured vehicle) to both motor presets: the generator's first catastrophe or event clustering.

**Architecture:** A new optional `events` block on `lob.SectionParams` (and an `events` assumption on `lob.PricingSectionParams`) switches a section from per-policy Poisson claims to a book-level event set. `claim.DrawEvents` draws each events section's set from its own root stream (`<section>-events`, beside `inflation`); the claim simulator gives each policy in force during an event a Bernoulli hit from the policy's own section stream and draws the occurrence inside the event. Everything after the occurrence date is the existing claim simulation. A per-section `no_subrogation` switch keeps salvage on and subrogation off.

**Tech stack:** Go 1.26, standard library, gonum via the existing `random.Source`.

## Global constraints

- Same seed plus same config gives byte-identical output.
- Zero values keep today's behaviour: `events.per_year` 0 is off, `no_subrogation` false keeps subrogation.
- The new section is appended after the existing three, so their stream keys, draws, premiums and claims do not move.
- Golden numbers are refreshed deliberately, with before/after per-section evidence.
- Docs style: sentence case, no em dashes, spaced hyphens.

## Tasks

1. **Domain parameters** (`internal/domain/lob/lob.go`): `EventParams` (`PerYear`, `HitShare`, `DurationDays`, `SeasonStartMonth`, `SeasonMonths`) on `SectionParams.Events`; `PricingEventParams` (`PerYear`, `HitShare`) on `PricingSectionParams.Events`; `SectionParams.NoSubrogation`. Validation: off block not required; an events section must leave `base_frequency` at 0; a section is active with either. Tests in `lob_test.go`.
2. **Pricing** (`expectedloss.go`): a section's assumed frequency is `base_frequency`, or `events.per_year x events.hit_share` on an events section. Test.
3. **Event set and hits** (`internal/domain/claim/events.go`, `claim.go`): `DrawEvents`; `ClaimSimulator.WithEvents`; per policy, each event overlapping the occurrence span is hit with probability `min(1, hit_share x risk factor x overlap / duration)`, occurrence uniform over the overlap. Tests: off leaves output unchanged, hits cluster in event windows, frequency matches pricing.
4. **Orchestration** (`internal/application/generate.go`): draw the event set from `src.Split(name + "-events")`.
5. **Recoveries** (`internal/domain/transaction/recovery.go`): skip subrogation on a `no_subrogation` section. Test.
6. **Config, presets, form**: mirror structs and `ToDomain`; `weather` section in both presets with comments; `formFields` entries.
7. **Goldens and gates**: refresh the four hashes, run the realism gate and loss-ratio tests, compare per-section aggregates before and after.
8. **Docs**: README (outputs, diagrams, simplifications), `docs/architecture.md`, roadmap, review, todo. Delete this plan before the PR.
