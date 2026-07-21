# SL-5 knob isolation implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Close finding SL-5 - make the nil-claim and salvage knobs shift-free so toggling either never reshuffles the RNG draws of any other claim or stage.

**Architecture:** Two independent RNG-isolation fixes. (A) The nil Bernoulli is drawn unconditionally so the per-claim draw count is constant regardless of the nil knob. (B) Each recovery type (salvage, subrogation) draws from its own labelled sub-stream so toggling one never touches the other's stream. Spec: `docs/superpowers/specs/2026-07-21-sl5-knob-isolation-design.md`.

**Tech Stack:** Go. RNG via `internal/infrastructure/random.Source` (SHA-256-keyed labelled sub-streams: `src.Split(label)`). Tests use `go test ./...`.

## Global Constraints

- Writing style: sentence case headers; no em dashes, use ` - `; no invented content.
- `Bernoulli(p)` always calls `Float64()` (`internal/infrastructure/random/source.go:53-55`), so `Bernoulli(0)` consumes one draw and returns false.
- Git: commit and push as separate commands. End commit messages with `Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>`.
- Baseline must stay green: `go vet ./...` clean and `go test ./...` passing.

---

### Task 1: Nil knob isolation (Change A)

**Files:**
- Test: `internal/application/nil_test.go` (create)
- Modify: `internal/domain/claim/claim.go:119-121`

**Interfaces:**
- Consumes: `application.GenerateDataset(src, req)`, `request(t)` helper (`internal/application/generate_test.go:13`), `lob.ClaimParams.NilProbability` (set via `req.LOB.Claims.NilProbability`).
- Produces: no new exported symbols; behaviour change only.

- [ ] **Step 1: Write the failing test**

Create `internal/application/nil_test.go`:

```go
package application_test

import (
	"testing"

	"github.com/le-marais/claimsgen/internal/application"
	"github.com/le-marais/claimsgen/internal/infrastructure/random"
)

// TestNilClaimsDoNotShiftOtherStages proves the nil knob is shift-free: the
// nil Bernoulli is always drawn, so toggling NilProbability never reshuffles
// the dates or severities of any claim. Only the Nil flag itself may change.
func TestNilClaimsDoNotShiftOtherStages(t *testing.T) {
	off := request(t)
	off.LOB.Claims.NilProbability = 0
	dsOff, err := application.GenerateDataset(random.NewSource(13), off)
	if err != nil {
		t.Fatal(err)
	}
	dsOn, err := application.GenerateDataset(random.NewSource(13), request(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(dsOn.Claims) != len(dsOff.Claims) {
		t.Fatalf("claim count changed: %d vs %d", len(dsOn.Claims), len(dsOff.Claims))
	}
	sawNil := false
	for i := range dsOn.Claims {
		a, b := dsOn.Claims[i], dsOff.Claims[i]
		if a.Nil {
			sawNil = true
		}
		a.Nil, b.Nil = false, false
		if a != b {
			t.Fatalf("claim %d shifted when nil toggled:\n on:  %+v\n off: %+v",
				dsOn.Claims[i].ID, dsOn.Claims[i], dsOff.Claims[i])
		}
	}
	if !sawNil {
		t.Fatal("expected at least one nil claim on the default run")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/application/ -run TestNilClaimsDoNotShiftOtherStages -v`
Expected: FAIL - claims shift because the nil-off run skips the Bernoulli, moving later claims on each policy.

- [ ] **Step 3: Write minimal implementation**

In `internal/domain/claim/claim.go`, replace the nil draw and its comment (`:119-121`):

```go
	// Nil claims draw their severity and probability independently of claim
	// size; real withdrawn claims skew small, so this is a known simplification.
	// The Bernoulli is always drawn - Bernoulli(0) still consumes one uniform
	// and returns false - so toggling the nil knob never reshuffles the draws of
	// later claims on the same policy. This is the shift-free contract the reopen
	// and recovery post-passes also uphold.
	isNil := src.Bernoulli(s.params.NilProbability)
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/application/ -run TestNilClaimsDoNotShiftOtherStages -v`
Expected: PASS.

- [ ] **Step 5: Confirm no golden regression**

Run: `go test ./internal/application/ -run TestGoldenCSVBytes -v`
Expected: PASS unchanged - the preset has `NilProbability > 0`, so the default run already drew this Bernoulli; removing the guard does not change the default output.

- [ ] **Step 6: Commit**

```bash
git add internal/application/nil_test.go internal/domain/claim/claim.go
git commit -m "$(cat <<'EOF'
Make the nil knob shift-free (SL-5, part A)

Draw the nil Bernoulli unconditionally so toggling NilProbability no longer
consumes a conditional draw that reshuffles later claims on the same policy.

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
EOF
)"
```

---

### Task 2: Recovery-type sub-stream isolation (Change B) + golden regen

**Files:**
- Test: `internal/application/recoveries_test.go` (modify - append one test)
- Modify: `internal/domain/transaction/recovery.go:38-41` (doc comment), `:84-123` (`simulateClaim`)
- Modify: `internal/application/golden_test.go:19` (regenerated hash)

**Interfaces:**
- Consumes: `application.GenerateDataset`, `request(t)`, `transaction.Subrogation`, `transaction.Salvage`, `shared.Date`.
- Produces: no new exported symbols; recovery draws now come from per-type sub-streams `recovery-claim-{id}/SALVAGE` and `recovery-claim-{id}/SUBROGATION`.

- [ ] **Step 1: Write the failing test**

Append to `internal/application/recoveries_test.go` (add `"reflect"` and the `shared` import `"github.com/le-marais/claimsgen/internal/domain/shared"` to the import block):

```go
// TestSalvageDoesNotShiftSubrogation proves per-recovery-type stream
// independence: toggling salvage must not move whether subrogation fires or
// when. It compares subrogation rows by (ClaimID, Date) - the pure sub-stream
// signal. It does not compare amounts: the "total recovered below gross paid"
// cap accumulates salvage before subrogation, so a capped subrogation amount
// can legitimately differ when salvage fires (an economic coupling, not an RNG
// shift).
func TestSalvageDoesNotShiftSubrogation(t *testing.T) {
	off := request(t)
	off.LOB.Claims.Recoveries.Salvage.Probability = 0

	dsOn, err := application.GenerateDataset(random.NewSource(13), request(t))
	if err != nil {
		t.Fatal(err)
	}
	dsOff, err := application.GenerateDataset(random.NewSource(13), off)
	if err != nil {
		t.Fatal(err)
	}

	type subKey struct {
		claimID int
		date    shared.Date
	}
	collect := func(ds application.Dataset) map[subKey]int {
		m := map[subKey]int{}
		for _, tx := range ds.Transactions {
			if tx.Type == transaction.Subrogation {
				m[subKey{tx.ClaimID, tx.Date}]++
			}
		}
		return m
	}
	onSubs, offSubs := collect(dsOn), collect(dsOff)
	if len(onSubs) == 0 {
		t.Fatal("expected subrogation rows on the default run")
	}
	if !reflect.DeepEqual(onSubs, offSubs) {
		t.Fatalf("subrogation firing/timing shifted when salvage toggled: %d keys with salvage on, %d off",
			len(onSubs), len(offSubs))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/application/ -run TestSalvageDoesNotShiftSubrogation -v`
Expected: FAIL - salvage and subrogation share one stream today, so toggling salvage shifts subrogation's firing/timing.

- [ ] **Step 3: Implement per-type sub-streams**

In `internal/domain/transaction/recovery.go`, change the `kinds` loop inside `simulateClaim` so each type draws from its own sub-stream. Replace the loop body opening (`:97-103`) so the Bernoulli, Beta and lag all read from `ksrc`:

```go
	for _, k := range kinds {
		ksrc := src.Split(string(k.t)) // recovery-claim-{id}/SALVAGE, .../SUBROGATION
		if k.p.Probability <= 0 || !ksrc.Bernoulli(k.p.Probability) {
			continue
		}
		share := ksrc.Beta(k.p.MeanShare*k.p.Concentration, (1-k.p.MeanShare)*k.p.Concentration)
		amount := paid.MulFloat(share)
		lag := int(math.Round(ksrc.LogNormal(math.Log(k.p.LagMedianDays), k.p.LagSigma)))
```

Leave the rest of the loop (`if lag < 1`, the gross-paid cap, the `amount < OneCent` skip, the row append, `recovered += amount`) unchanged - the cap stays sequential accounting.

Then update the `Apply` doc comment (`:38-41`) to note per-type streams:

```go
// Apply merges each eligible claim's recovery rows into the runoff output
// after that claim's block, renumbering IDs. Every claim draws from its own
// labelled sub-stream, and within a claim each recovery type draws from its
// own sub-stream (recovery-claim-{id}/SALVAGE, .../SUBROGATION), so toggling
// one recovery type never reshuffles the draws of the other type or any other
// stage.
```

- [ ] **Step 4: Run the recovery tests to verify the new test passes and the others still hold**

Run: `go test ./internal/... -run 'Recover|Salvage|Recoveries' -v`
Expected: PASS - including `TestRecoveriesDoNotShiftOtherStages` (proves claims and non-recovery transactions did not move) and `TestSalvageDoesNotShiftSubrogation`.

- [ ] **Step 5: Regenerate the golden hash**

The salvage change moves recovery-row draws, so `TestGoldenCSVBytes` now fails with a new `got` hash. Because `TestRecoveriesDoNotShiftOtherStages` passed in Step 4, the only CSV diff is recovery rows - nothing upstream moved.

Run: `go test ./internal/application/ -run TestGoldenCSVBytes -v`
Copy the printed `got:` value and paste it into `wantHash` at `internal/application/golden_test.go:19`, replacing the old digest.

- [ ] **Step 6: Re-run to confirm green**

Run: `go test ./internal/application/ -run TestGoldenCSVBytes -v`
Expected: PASS with the new hash.

- [ ] **Step 7: Commit**

```bash
git add internal/domain/transaction/recovery.go internal/application/recoveries_test.go internal/application/golden_test.go
git commit -m "$(cat <<'EOF'
Isolate salvage and subrogation onto per-type sub-streams (SL-5, part B)

Each recovery type now draws from its own labelled sub-stream, so toggling
salvage no longer shifts subrogation's firing or timing. Regenerate the golden
hash for the moved recovery draws (only recovery rows change; the no-shift test
proves nothing upstream moved).

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
EOF
)"
```

---

### Task 3: Document the shift-free knobs

**Files:**
- Modify: `README.md` (near line 65, the reproducibility / labelled sub-stream note)

**Interfaces:**
- Consumes: nothing.
- Produces: doc only.

- [ ] **Step 1: Add the shift-free knobs note**

In `README.md`, after the existing sentence about the inflation path being drawn from its own labelled sub-stream (line 65), add a sentence documenting which knobs are shift-free. Match the surrounding prose style (sentence case, spaced hyphens):

```markdown
Every independent decision is drawn from its own labelled sub-stream keyed by the seed and a label path, so toggling a knob is invisible to unrelated draws: turning nil claims, reopening, salvage, or subrogation on or off never reshuffles the dates or severities of any other claim or stage. (Salvage and subrogation amounts remain linked through the rule that a claim's total recovered stays below its gross paid, which is an accounting constraint, not a random draw.)
```

- [ ] **Step 2: Verify the full suite is green**

Run: `go vet ./... && go test ./...`
Expected: all packages PASS, vet clean.

- [ ] **Step 3: Commit**

```bash
git add README.md
git commit -m "$(cat <<'EOF'
Document the shift-free knobs (SL-5)

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
EOF
)"
```

---

## Self-review

- **Spec coverage:** Change A (Task 1), Change B (Task 2), both no-shift tests (Tasks 1 and 2), golden regen (Task 2), docs including README shift-free note and the two code comments (nil comment in Task 1, recovery doc comment in Task 2, README in Task 3). The spec also mentions tightening `TestRecoveriesDoNotShiftOtherStages`; Task 2's new `TestSalvageDoesNotShiftSubrogation` adds the single-knob isolation the review said was missing, and the existing both-off test is retained - no edit to it is required.
- **Placeholder scan:** none - every code and command step is concrete.
- **Type consistency:** `NilProbability` (field on `lob.ClaimParams`, reached as `req.LOB.Claims.NilProbability`), `Recoveries.Salvage.Probability`, `transaction.Subrogation`, `shared.Date`, and `src.Split(string(k.t))` all match existing usage in the files read.
- **Out of scope:** RF-11 (named recovery-type list) and the nil-severity-model simplification, per the spec.
