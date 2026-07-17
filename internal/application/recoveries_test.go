package application_test

import (
	"testing"

	"github.com/le-marais/claimsgen/internal/application"
	"github.com/le-marais/claimsgen/internal/domain/transaction"
	"github.com/le-marais/claimsgen/internal/infrastructure/random"
)

// TestRecoveryProbabilitiesZeroLeaveNoRecoveries is the spec's output-level
// off-switch check: with both probabilities 0, no recovery rows exist.
func TestRecoveryProbabilitiesZeroLeaveNoRecoveries(t *testing.T) {
	req := request(t)
	req.LOB.Claims.Recoveries.Salvage.Probability = 0
	req.LOB.Claims.Recoveries.Subrogation.Probability = 0
	ds, err := application.GenerateDataset(random.NewSource(7), req)
	if err != nil {
		t.Fatal(err)
	}
	if len(ds.Transactions) == 0 {
		t.Fatal("expected transactions")
	}
	for _, tx := range ds.Transactions {
		if tx.Type.IsRecovery() {
			t.Fatalf("transaction %d is a %s with both recovery probabilities 0", tx.ID, tx.Type)
		}
	}
}

// TestDefaultPresetGeneratesBothRecoveryTypes proves the feature is on by
// default and both types appear in the output.
func TestDefaultPresetGeneratesBothRecoveryTypes(t *testing.T) {
	ds, err := application.GenerateDataset(random.NewSource(8), request(t))
	if err != nil {
		t.Fatal(err)
	}
	var salvage, subro int
	for _, tx := range ds.Transactions {
		switch tx.Type {
		case transaction.Salvage:
			salvage++
		case transaction.Subrogation:
			subro++
		}
	}
	if salvage == 0 || subro == 0 {
		t.Fatalf("default preset generated %d salvage and %d subrogation rows, want both positive", salvage, subro)
	}
}

// TestRecoveriesDoNotShiftOtherStages is the spec's sub-stream independence
// check: enabling recoveries only appends rows - policies, claims, and every
// non-recovery transaction are unchanged draw-for-draw.
func TestRecoveriesDoNotShiftOtherStages(t *testing.T) {
	off := request(t)
	off.LOB.Claims.Recoveries.Salvage.Probability = 0
	off.LOB.Claims.Recoveries.Subrogation.Probability = 0
	dsOff, err := application.GenerateDataset(random.NewSource(13), off)
	if err != nil {
		t.Fatal(err)
	}
	dsOn, err := application.GenerateDataset(random.NewSource(13), request(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(dsOn.Policies) != len(dsOff.Policies) || len(dsOn.Claims) != len(dsOff.Claims) {
		t.Fatalf("book or claims changed: %d/%d policies, %d/%d claims",
			len(dsOn.Policies), len(dsOff.Policies), len(dsOn.Claims), len(dsOff.Claims))
	}
	for i := range dsOn.Claims {
		if dsOn.Claims[i] != dsOff.Claims[i] {
			t.Fatalf("claim %d differs with recoveries on", dsOn.Claims[i].ID)
		}
	}
	var core []transaction.Transaction
	for _, tx := range dsOn.Transactions {
		if !tx.Type.IsRecovery() {
			core = append(core, tx)
		}
	}
	if len(core) != len(dsOff.Transactions) {
		t.Fatalf("non-recovery transaction count %d, want %d", len(core), len(dsOff.Transactions))
	}
	for i := range core {
		got, want := core[i], dsOff.Transactions[i]
		// IDs are renumbered by the merge; everything else must match.
		got.ID, want.ID = 0, 0
		if got != want {
			t.Fatalf("non-recovery transaction %d differs with recoveries on: %+v vs %+v", i, got, want)
		}
	}
}
