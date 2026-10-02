> **OUT OF CONTEXT - do not read (2026-10-02):** implementation plan, deleted once its work ships. Agents must not load this file into context or treat it as a source of truth. For how the system works today see `README.md`, `AGENTS.md`, and `docs/architecture.md`.

# MR-19 per-section settlement implementation plan

**Goal:** replace the runoff-level Beta settlement share with a per-section settlement (lump-sum probability, Beta share, concentration) and a runoff minimum payment, per `docs/superpowers/specs/2026-10-02-mr-19-settlement-share-design.md`.

**Global constraints:** byte-identical output for a seed and config; draws only from `runoff-claim-<id>`; a lump sum is drawn only when its probability is above 0; YAML holds simulation parameters only; sentence case, no em dashes.

### Task 1: domain parameters and validation

- `internal/domain/lob/lob.go`: add `SettlementParams` and `SectionParams.Settlement`, validated under `claims.sections[i].settlement` on active sections; remove `RunoffParams.SettlementShare` and `SettlementConcentration`; add `RunoffParams.MinPayment` (not negative).
- `internal/domain/lob/lob_test.go`: `validMotor` sections carry `Settlement{Share: 0.4}`; field-naming cases for `lump_sum_probability` (-0.1, 1.5), `share` (0, 1.5), `concentration` (-1), `share` 1 with concentration 4, `runoff.min_payment` (-1); a lump-sum probability of 1 needs no share.
- Run `go test ./internal/domain/lob/`; commit.

### Task 2: runoff by section

- `internal/domain/transaction/runoff.go`: `NewRunoffSimulator(p lob.RunoffParams, sections []lob.SectionParams)`; `runEpisode` and `drawInterimPayments` take the claim's `lob.SettlementParams`; the draw order of the spec; sorted payment days; held-over payments below `MinPayment`; `settlementShare(src, st)`.
- `internal/application/generate.go`: pass `req.LOB.Claims.Sections`.
- Tests (`runoff_test.go`, `recovery_test.go`, `estimate_test.go`): update call sites to a one-section settlement; fixed and drawn share tests on the section; `TestLumpSumPaysInOneSettlement` (probability 1: every paying claim pays once; 0.5: the single-payment share rises by about half the gap); `TestMinPaymentHoldsSmallPaymentsOver` (no interim payment below the minimum, total paid is the ultimate); `TestClaimsSettleBySection` (section 1's share applies to section-1 claims).
- Run `go test ./internal/domain/...`; commit.

### Task 3: config, form, presets, golden hashes

- `internal/infrastructure/config/config.go`: mirrored `SettlementParams`, `SectionParams.Settlement`, `RunoffParams.MinPayment`, `ToDomain`; the YAML fixture in `config_test.go`.
- `internal/infrastructure/web/fields.go`: three settlement fields in the Claims section group; `min_payment` replaces `settlement_share` and `settlement_concentration` in Runoff.
- Both preset YAMLs: settlement blocks (0.57/0.25/4, 0.28/0.3/4, 0/0.6/4) with comments; `runoff.concentration: 4`, `runoff.min_payment: 50`.
- Refresh the four golden hashes; run `go test ./...`; re-measure single-payment shares by section on both presets; commit.

### Task 4: docs and PR

- README runoff diagram, sections paragraph and worked example; `docs/architecture.md` runoff description; MR-19 already removed from `docs/review.md`.
- `gofmt -l .`, `go vet ./...`, `go test ./...`, `golangci-lint run ./...`; regenerate screenshots.
- Delete this plan; commit; push; update the PR title and body. Do not merge.
