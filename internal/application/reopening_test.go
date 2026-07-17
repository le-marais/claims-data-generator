package application_test

import (
	"testing"

	"github.com/le-marais/claimsgen/internal/application"
	"github.com/le-marais/claimsgen/internal/domain/shared"
	"github.com/le-marais/claimsgen/internal/domain/transaction"
	"github.com/le-marais/claimsgen/internal/infrastructure/random"
)

// TestReopeningProbabilityZeroMeansOneRelease is the spec's output-level
// off-switch check: with reopening off, once a claim's outstanding case
// reaches zero it stays there - no case activity ever follows a release.
func TestReopeningProbabilityZeroMeansOneRelease(t *testing.T) {
	req := request(t)
	req.LOB.Claims.Reopening.Probability = 0
	ds, err := application.GenerateDataset(random.NewSource(17), req)
	if err != nil {
		t.Fatal(err)
	}
	outstanding := map[int]shared.Money{}
	released := map[int]bool{}
	for _, tx := range ds.Transactions {
		if tx.Type.IsRecovery() {
			continue
		}
		if released[tx.ClaimID] {
			t.Fatalf("claim %d has case activity after its release to zero with reopening off", tx.ClaimID)
		}
		if tx.Type == transaction.Estimate {
			outstanding[tx.ClaimID] += tx.Amount
			if outstanding[tx.ClaimID] == 0 {
				released[tx.ClaimID] = true
			}
		}
	}
	for _, c := range ds.Claims {
		if c.Reopened() {
			t.Fatalf("claim %d reopened with probability 0", c.ID)
		}
	}
}

// TestDefaultPresetGeneratesReopenedClaims proves the feature is on by
// default, including the nil-reopen pattern.
func TestDefaultPresetGeneratesReopenedClaims(t *testing.T) {
	req := request(t)
	req.InitialBookSize = 2000
	ds, err := application.GenerateDataset(random.NewSource(18), req)
	if err != nil {
		t.Fatal(err)
	}
	reopened, nilReopened := 0, 0
	for _, c := range ds.Claims {
		if c.Reopened() {
			reopened++
			if c.Nil {
				nilReopened++
			}
		}
	}
	if reopened == 0 {
		t.Fatal("default preset generated no reopened claims")
	}
	if nilReopened == 0 {
		t.Fatal("default preset generated no reopened nil claims (expected some at 8% nil x 4% reopen with 2000 policies x 3 years)")
	}
}

// TestReopeningDoesNotShiftOtherStages is the spec's sub-stream
// independence check: enabling reopening leaves the book and every
// non-reopened claim (and its transactions) byte-identical.
func TestReopeningDoesNotShiftOtherStages(t *testing.T) {
	off := request(t)
	off.LOB.Claims.Reopening.Probability = 0
	dsOff, err := application.GenerateDataset(random.NewSource(19), off)
	if err != nil {
		t.Fatal(err)
	}
	dsOn, err := application.GenerateDataset(random.NewSource(19), request(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(dsOn.Policies) != len(dsOff.Policies) || len(dsOn.Claims) != len(dsOff.Claims) {
		t.Fatalf("book or claim count changed: %d/%d policies, %d/%d claims",
			len(dsOn.Policies), len(dsOff.Policies), len(dsOn.Claims), len(dsOff.Claims))
	}
	reopened := map[int]bool{}
	for i := range dsOn.Claims {
		if dsOn.Claims[i].Reopened() {
			reopened[dsOn.Claims[i].ID] = true
			continue
		}
		if dsOn.Claims[i] != dsOff.Claims[i] {
			t.Fatalf("non-reopened claim %d differs with reopening on", dsOn.Claims[i].ID)
		}
	}
	if len(reopened) == 0 {
		t.Fatal("expected reopened claims with the default preset")
	}
	byClaimOn := map[int][]transaction.Transaction{}
	for _, tx := range dsOn.Transactions {
		byClaimOn[tx.ClaimID] = append(byClaimOn[tx.ClaimID], tx)
	}
	byClaimOff := map[int][]transaction.Transaction{}
	for _, tx := range dsOff.Transactions {
		byClaimOff[tx.ClaimID] = append(byClaimOff[tx.ClaimID], tx)
	}
	for id, offRows := range byClaimOff {
		if reopened[id] {
			continue
		}
		onRows := byClaimOn[id]
		if len(onRows) != len(offRows) {
			t.Fatalf("non-reopened claim %d has %d rows with reopening on, %d off", id, len(onRows), len(offRows))
		}
		for i := range onRows {
			a, b := onRows[i], offRows[i]
			a.ID, b.ID = 0, 0 // IDs shift because reopened claims add rows
			if a != b {
				t.Fatalf("non-reopened claim %d row %d differs with reopening on: %+v vs %+v", id, i, a, b)
			}
		}
	}
}
