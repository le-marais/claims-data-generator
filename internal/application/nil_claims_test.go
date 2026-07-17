package application_test

import (
	"testing"

	"github.com/le-marais/claimsgen/internal/application"
	"github.com/le-marais/claimsgen/internal/domain/transaction"
	"github.com/le-marais/claimsgen/internal/infrastructure/config"
	"github.com/le-marais/claimsgen/internal/infrastructure/random"
)

// TestNilProbabilityZeroLeavesNoUnpaidClaims is the spec's output-level
// off-switch check: with nil_probability 0, every closed claim has at least
// one payment.
func TestNilProbabilityZeroLeavesNoUnpaidClaims(t *testing.T) {
	l, err := config.MotorPersonal()
	if err != nil {
		t.Fatal(err)
	}
	l.Claims.NilProbability = 0
	ds, err := application.GenerateDataset(random.NewSource(7), application.GenerateRequest{
		LOB: l, StartYear: 1998, Years: 4, InitialBookSize: 3000,
	})
	if err != nil {
		t.Fatal(err)
	}
	paid := make(map[int]bool, len(ds.Claims))
	for _, tx := range ds.Transactions {
		if tx.Type == transaction.Payment {
			paid[tx.ClaimID] = true
		}
	}
	if len(ds.Claims) == 0 {
		t.Fatal("expected claims")
	}
	for _, c := range ds.Claims {
		if !paid[c.ID] {
			t.Fatalf("claim %d has no payment with nil_probability 0", c.ID)
		}
	}
}
