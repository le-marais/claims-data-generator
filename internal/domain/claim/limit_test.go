package claim_test

import (
	"testing"

	"github.com/le-marais/claimsgen/internal/domain/claim"
	"github.com/le-marais/claimsgen/internal/infrastructure/random"
)

// A small sum insured makes total losses common, so the cap binds often.
func TestOwnDamageCostStaysWithinTheCover(t *testing.T) {
	book := fixedBook(4000, 4000, 300, 1.0)
	claims := claim.NewClaimSimulator(params()).Simulate(random.NewSource(5), book)
	capped := 0
	for _, c := range claims {
		if c.Section != ownDamage {
			if c.CoverLimit != 0 {
				t.Fatalf("third-party claim %d has cover limit %v, want 0 (unlimited)", c.ID, c.CoverLimit)
			}
			continue
		}
		limit := book[c.PolicyID-1].SumInsured - book[c.PolicyID-1].Excess
		if c.CoverLimit != limit {
			t.Fatalf("own-damage claim %d cover limit %v, want sum insured minus excess %v", c.ID, c.CoverLimit, limit)
		}
		if c.Episodes[0].Ultimate > c.CoverLimit {
			t.Fatalf("own-damage claim %d costs %v, above its cover %v", c.ID, c.Episodes[0].Ultimate, c.CoverLimit)
		}
		if c.Episodes[0].Ultimate == c.CoverLimit {
			capped++
		}
	}
	if capped == 0 {
		t.Fatal("fixture produced no total losses; the cap was never exercised")
	}
}

func TestReopenNeverPaysBeyondTheCover(t *testing.T) {
	p := reopeningParams()
	p.Reopening.Probability = 0.9
	p.Reopening.EstimateFactor = 3 // reopens larger than the claim push hard on the cap
	p.NilProbability = 0.3
	claims := claim.NewClaimSimulator(p).Simulate(random.NewSource(8), fixedBook(4000, 4000, 300, 1.0))
	claims = claim.NewReopenSimulator(p).Apply(random.NewSource(8), claims)
	var totalLosses, totalLossesReopened, cappedReopens int
	for _, c := range claims {
		if c.Section != ownDamage {
			continue
		}
		firstPaid := c.Episodes[0].Paid()
		if !c.Nil() && c.Episodes[0].Ultimate == c.CoverLimit {
			totalLosses++
			if c.Reopened() {
				totalLossesReopened++
			}
		}
		if !c.Reopened() {
			continue
		}
		reopenCost := c.Episodes[1].Ultimate
		if reopenCost <= 0 {
			t.Fatalf("reopened claim %d has reopen cost %v, want positive", c.ID, reopenCost)
		}
		if firstPaid+reopenCost > c.CoverLimit {
			t.Fatalf("claim %d pays %v + %v, above its cover %v", c.ID, firstPaid, reopenCost, c.CoverLimit)
		}
		if firstPaid+reopenCost == c.CoverLimit {
			cappedReopens++
		}
	}
	if totalLosses == 0 || cappedReopens == 0 {
		t.Fatalf("fixture did not exercise the cap: %d total losses, %d capped reopens", totalLosses, cappedReopens)
	}
	if totalLossesReopened != 0 {
		t.Fatalf("%d total losses reopened with no cover left, want 0", totalLossesReopened)
	}
}
