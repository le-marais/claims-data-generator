> **OUT OF CONTEXT - do not read (2026-10-02):** implementation plan, deleted once its work ships. Agents must not load this file into context or treat it as a source of truth. For how the system works today see `README.md`, `AGENTS.md`, and `docs/architecture.md`.

# MR-19 drawn settlement share implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Each paying episode with interim payments draws its settlement share from a Beta with mean `settlement_share`, so the final payment no longer sits at exactly `settlement_share` of every claim's cost.

**Architecture:** One new runoff parameter, `SettlementConcentration`, on `lob.RunoffParams`; `drawInterimPayments` in `internal/domain/transaction/runoff.go` draws the share from the claim's runoff stream when it is above 0. Config, presets, form registry and docs follow the AGENTS.md "adding a line-of-business parameter" order.

**Tech Stack:** Go 1.26, gonum (Beta draws via `shared.RandomSource.Beta`), yaml.v3.

## Global constraints

- Same seed plus same config gives byte-identical output; draw only from the claim's existing `runoff-claim-<id>` stream.
- A zero `settlement_concentration` keeps the old fixed share, so existing YAMLs keep working.
- The YAML and `lob` hold simulation parameters only.
- Docs and comments: sentence case headers, no em dashes (use ` - `), concise.
- Run `go test ./...` and `go vet ./...` before the PR; the realism gate must stay green.

---

### Task 1: the parameter and its validation

**Files:**
- Modify: `internal/domain/lob/lob.go` (`RunoffParams`, `RunoffParams.validate`)
- Test: `internal/domain/lob/lob_test.go` (`TestValidateNamesOffendingField` table)

**Interfaces:**
- Produces: `lob.RunoffParams.SettlementConcentration float64`.

- [ ] **Step 1: Write the failing tests.** Add to the field table, after the `runoff.concentration` row:

```go
		{"runoff.settlement_concentration", func(l *LineOfBusiness) { l.Runoff.SettlementConcentration = -1 }},
		{"runoff.settlement_share", func(l *LineOfBusiness) { l.Runoff.SettlementConcentration = 4; l.Runoff.SettlementShare = 1 }},
```

- [ ] **Step 2: Run** `go test ./internal/domain/lob/` - expect a compile failure (no field).

- [ ] **Step 3: Implement.** In `RunoffParams`, after `SettlementShare`:

```go
	// SettlementConcentration is the Beta concentration of each paying
	// episode's settlement share, drawn around SettlementShare when the
	// episode has interim payments; higher keeps the share closer to it.
	// 0 fixes the share at SettlementShare.
	SettlementConcentration float64
```

Update the `SettlementShare` comment to "the mean fraction of ultimate reserved for the final settlement payment at close". In `validate`, add `namedFloat{"runoff.settlement_concentration", r.SettlementConcentration}` to `checkFinite`, and after the `settlement_share` range check:

```go
	if r.SettlementConcentration < 0 {
		return fmt.Errorf("runoff.settlement_concentration: must not be negative, got %v", r.SettlementConcentration)
	}
	if r.SettlementConcentration > 0 && r.SettlementShare == 1 {
		return fmt.Errorf("runoff.settlement_share: must be below 1 when settlement_concentration is above 0, got %v", r.SettlementShare)
	}
```

- [ ] **Step 4: Run** `go test ./internal/domain/lob/` - PASS.

- [ ] **Step 5: Commit** "Add a settlement concentration runoff parameter".

### Task 2: the runoff draws the share

**Files:**
- Modify: `internal/domain/transaction/runoff.go` (`drawInterimPayments`)
- Test: `internal/domain/transaction/runoff_test.go`

**Interfaces:**
- Consumes: `lob.RunoffParams.SettlementConcentration`.

- [ ] **Step 1: Write the failing tests.** Add a helper and two tests:

```go
// finalShares returns, for each claim with an interim payment, its final
// payment as a share of its ultimate. Every test claim has one episode.
func finalShares(claims []claim.Claim, txs []transaction.Transaction) []float64 {
	grouped := byClaim(txs)
	var shares []float64
	for _, c := range claims {
		var payments []shared.Money
		for _, tx := range grouped[c.ID] {
			if tx.Type == transaction.Payment {
				payments = append(payments, tx.Amount)
			}
		}
		if len(payments) < 2 {
			continue
		}
		shares = append(shares, payments[len(payments)-1].Dollars()/c.Episodes[0].Ultimate.Dollars())
	}
	return shares
}

func TestSettlementShareIsFixedWithoutConcentration(t *testing.T) {
	claims := testClaims(500)
	txs := transaction.NewRunoffSimulator(params()).Simulate(random.NewSource(4), claims)
	shares := finalShares(claims, txs)
	if len(shares) < 50 {
		t.Fatalf("only %d claims have an interim payment, want at least 50", len(shares))
	}
	for i, s := range shares {
		// Interim payments round to the cent each, so allow a few cents.
		if math.Abs(s-0.4) > 0.001 {
			t.Fatalf("claim %d: final payment share = %v, want 0.4", i, s)
		}
	}
}

func TestSettlementShareVariesAroundItsMean(t *testing.T) {
	claims := testClaims(2000)
	p := params()
	p.SettlementConcentration = 4
	txs := transaction.NewRunoffSimulator(p).Simulate(random.NewSource(5), claims)
	shares := finalShares(claims, txs)
	if len(shares) < 200 {
		t.Fatalf("only %d claims have an interim payment, want at least 200", len(shares))
	}
	mean, sq := 0.0, 0.0
	for _, s := range shares {
		mean += s
		sq += s * s
	}
	mean /= float64(len(shares))
	sd := math.Sqrt(sq/float64(len(shares)) - mean*mean)
	if math.Abs(mean-0.4) > 0.03 {
		t.Errorf("mean final payment share = %.3f, want about 0.4", mean)
	}
	// Beta(1.6, 2.4) has a standard deviation of about 0.22.
	if sd < 0.15 || sd > 0.3 {
		t.Errorf("final payment share standard deviation = %.3f, want about 0.22", sd)
	}
}
```

- [ ] **Step 2: Run** `go test ./internal/domain/transaction/` - the fixed-share test passes, the varying test fails with a standard deviation near 0.

- [ ] **Step 3: Implement.** In `drawInterimPayments`, replace the `pool` line:

```go
	pool := ultimate.MulFloat(1 - s.settlementShare(src)).Dollars()
```

and add, after `drawInterimPayments`:

```go
// settlementShare is the share of the ultimate an episode with interim
// payments leaves for its final settlement: SettlementShare, or a Beta draw
// with that mean when SettlementConcentration is above 0.
func (s *RunoffSimulator) settlementShare(src shared.RandomSource) float64 {
	m, k := s.params.SettlementShare, s.params.SettlementConcentration
	if k == 0 {
		return m
	}
	return src.Beta(m*k, (1-m)*k)
}
```

Update the `drawInterimPayments` comment: "splits (1 - settlement share) of the ultimate, the share drawn per episode when settlement_concentration is above 0, across ...".

- [ ] **Step 4: Run** `go test ./internal/domain/transaction/` - PASS, including the existing invariant tests.

- [ ] **Step 5: Commit** "Draw each episode's settlement share from a Beta (MR-19)".

### Task 3: config, presets, form field and golden hashes

**Files:**
- Modify: `internal/infrastructure/config/config.go` (`RunoffParams`, `ToDomain`)
- Modify: `internal/infrastructure/config/motor-personal.yaml`, `motor-commercial.yaml` (`runoff` block)
- Modify: `internal/infrastructure/web/fields.go` (Runoff group)
- Modify: `internal/application/golden_test.go` (four hashes)

- [ ] **Step 1: Config.** Add `SettlementConcentration float64 \`yaml:"settlement_concentration" json:"settlement_concentration"\`` after `SettlementShare`, and `SettlementConcentration: d.Runoff.SettlementConcentration,` in `ToDomain`. Run `go test ./internal/infrastructure/config/` - `TestToDomainMapsEveryField` passes.

- [ ] **Step 2: Form field.** After the `settlement_share` field:

```go
			{Path: []string{"runoff", "settlement_concentration"}, Label: "Settlement concentration", Tip: "Beta concentration of each claim's settlement share around the settlement share; 0 fixes it."},
```

and change the `settlement_share` tip to "Mean fraction of ultimate paid at close, on claims with interim payments." Run `go test ./internal/infrastructure/web/` - PASS.

- [ ] **Step 3: Presets.** In both `runoff` blocks add `settlement_concentration: 4` after `settlement_share`, and extend the block comment. Personal:

```yaml
# A claim with interim payments leaves a share of its cost for the final
# settlement, drawn per episode from a Beta with mean settlement_share and
# concentration settlement_concentration: 4 gives Beta(1.6, 2.4), a
# standard deviation of about 0.22, so the final payment varies by claim
# without crowding toward zero. A judgement value.
```

Commercial: the same sentence with "Beta(2, 2)".

- [ ] **Step 4: Golden hashes.** Run `go test ./internal/application/ -run Golden`; paste each printed actual hash into its constant; rerun - PASS. The output moved because the presets now draw the share, which also shifts later draws in each claim's runoff stream.

- [ ] **Step 5: Realism gate and loss ratio.** Run `go test ./internal/application/` - PASS. If the gate fails, stop and investigate; the expected paid pattern should not move.

- [ ] **Step 6: Commit** "Set a settlement concentration of 4 on both presets".

### Task 4: docs, evidence and PR

**Files:**
- Modify: `README.md` (runoff diagram, worked example, its prose)
- Modify: `docs/review.md` (delete MR-19, renumber)
- Delete: this plan

- [ ] **Step 1: README diagram.** The `events` node's last line becomes `sharing (1 - share) × ultimate by Dirichlet(concentration) weights,<br/>share ~ Beta, mean settlement_share, settlement_concentration`.

- [ ] **Step 2: README worked example.** Intro: "under an illustrative `case_adequacy_mean` of 0.90 and a drawn settlement share of 0.35". Rows from the interim payment on:

| Date | Type | Amount | Outstanding case | Gross paid | What happened |
| --- | --- | ---: | ---: | ---: | --- |
| 2001-04-28 | PAYMENT | 2600.00 | 4162.33 | 2600.00 | interim payment: the whole 65% pool in one payment |
| 2001-04-28 | ESTIMATE | -2600.00 | 1562.33 | 2600.00 | the payment releases its own case |
| 2001-05-23 | ESTIMATE | -115.17 | 1447.16 | 2600.00 | revision at u = 0.78: aims at 1400 × 0.90 ^ (-0.22), noise 1.01 |
| 2001-06-12 | PAYMENT | 1400.00 | 1447.16 | 4000.00 | close: settles the remaining ultimate |
| 2001-06-12 | ESTIMATE | -1400.00 | 47.16 | 4000.00 | the payment releases its own case |
| 2001-06-12 | ESTIMATE | -47.16 | 0.00 | 4000.00 | the rest of the case is released to zero |

Incurred line: "4800.00, 4162.33, 4162.33, 4047.16 and 4000.00". The prose above it, "downward in the preset, whose cases open about 11% redundant", becomes "upward in both presets, whose cases open deficient", and the example's own drift stays "drifting down, because the case opened redundant".

- [ ] **Step 3: review.md.** Delete MR-19 and renumber: MR-20 becomes 1, MR-12 becomes 2.

- [ ] **Step 4: Evidence.** Re-measure on the personal preset (seed 1, 1998-2000): of the single-episode paying claims with two or more payments, how many have a final payment within $1.50 of 40% of their cost, and the spread of the final share. Record the figures in the PR body.

- [ ] **Step 5: Checks.** `gofmt -l .`, `go vet ./...`, `go test ./...`, `golangci-lint run ./...`.

- [ ] **Step 6: Commit** "Document the drawn settlement share and close MR-19", deleting this plan in the same commit; push and open the PR. Do not merge.
