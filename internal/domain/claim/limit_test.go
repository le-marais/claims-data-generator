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
		if !c.OwnDamage {
			if c.CoverLimit != 0 {
				t.Fatalf("third-party claim %d has cover limit %v, want 0 (unlimited)", c.ID, c.CoverLimit)
			}
			continue
		}
		limit := book[c.PolicyID-1].SumInsured - book[c.PolicyID-1].Excess
		if c.CoverLimit != limit {
			t.Fatalf("own-damage claim %d cover limit %v, want sum insured minus excess %v", c.ID, c.CoverLimit, limit)
		}
		if c.Ultimate > c.CoverLimit {
			t.Fatalf("own-damage claim %d costs %v, above its cover %v", c.ID, c.Ultimate, c.CoverLimit)
		}
		if c.Ultimate == c.CoverLimit {
			capped++
		}
		if c.InitialEstimate != c.Ultimate {
			t.Fatalf("claim %d opens at %v before case estimation, want its ultimate %v", c.ID, c.InitialEstimate, c.Ultimate)
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
		if !c.OwnDamage {
			continue
		}
		firstPaid := c.Ultimate
		if c.Nil {
			firstPaid = 0
		}
		if !c.Nil && c.Ultimate == c.CoverLimit {
			totalLosses++
			if c.Reopened() {
				totalLossesReopened++
			}
		}
		if !c.Reopened() {
			continue
		}
		if c.ReopenUltimate <= 0 {
			t.Fatalf("reopened claim %d has reopen cost %v, want positive", c.ID, c.ReopenUltimate)
		}
		if firstPaid+c.ReopenUltimate > c.CoverLimit {
			t.Fatalf("claim %d pays %v + %v, above its cover %v", c.ID, firstPaid, c.ReopenUltimate, c.CoverLimit)
		}
		if firstPaid+c.ReopenUltimate == c.CoverLimit {
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
