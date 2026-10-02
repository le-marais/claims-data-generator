package claim_test

import (
	"slices"
	"testing"

	"github.com/le-marais/claimsgen/internal/domain/claim"
	"github.com/le-marais/claimsgen/internal/domain/lob"
	"github.com/le-marais/claimsgen/internal/domain/shared"
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

// limitedThirdParty sets a per-claim limit, in dollars, on the Pareto
// third-party section, low enough that it binds often.
func limitedThirdParty(p lob.ClaimParams, limit float64) lob.ClaimParams {
	p.Sections = slices.Clone(p.Sections)
	p.Sections[thirdParty].Limit = limit
	return p
}

// A limited section pays at most its limit on a claim, and its claims carry
// the limit as their cover limit.
func TestLimitedSectionCostStaysWithinTheLimit(t *testing.T) {
	const limitDollars = 10000.0
	limit := shared.FromDollars(limitDollars)
	p := only(limitedThirdParty(params(), limitDollars), thirdParty, 0.15)
	claims := claim.NewClaimSimulator(p).Simulate(random.NewSource(7), fixedBook(20000, 10000, 300, 1.0))
	if len(claims) == 0 {
		t.Fatal("expected claims")
	}
	atLimit := 0
	for _, c := range claims {
		if c.CoverLimit != limit {
			t.Fatalf("claim %d cover limit %v, want the section limit %v", c.ID, c.CoverLimit, limit)
		}
		if c.Episodes[0].Ultimate > limit {
			t.Fatalf("claim %d costs %v, above the limit %v", c.ID, c.Episodes[0].Ultimate, limit)
		}
		if c.Episodes[0].Ultimate == limit {
			atLimit++
		}
	}
	if atLimit == 0 {
		t.Fatal("no claim reached the limit; the cap was never exercised")
	}
}

// A limited claim's reopen stays within what the limit has left, so the
// claim never pays more than the limit over its life.
func TestReopenNeverPaysBeyondTheLimit(t *testing.T) {
	const limitDollars = 10000.0
	limit := shared.FromDollars(limitDollars)
	p := limitedThirdParty(reopeningParams(), limitDollars)
	p.Reopening.Probability = 1
	p.Reopening.EstimateFactor = 3 // reopens larger than the claim push hard on the cap
	p = only(p, thirdParty, 0.15)
	claims := claim.NewClaimSimulator(p).Simulate(random.NewSource(8), fixedBook(20000, 10000, 300, 1.0))
	claims = claim.NewReopenSimulator(p).Apply(random.NewSource(8), claims)
	capped, atLimit, atLimitReopened := 0, 0, 0
	for _, c := range claims {
		if c.Cost() > limit {
			t.Fatalf("claim %d pays %v over its life, above the limit %v", c.ID, c.Cost(), limit)
		}
		if !c.Nil() && c.Episodes[0].Ultimate == limit {
			atLimit++
			if c.Reopened() {
				atLimitReopened++
			}
		}
		if c.Reopened() && c.Cost() == limit {
			capped++
		}
	}
	if capped == 0 || atLimit == 0 {
		t.Fatalf("fixture did not exercise the cap: %d claims at the limit, %d capped reopens", atLimit, capped)
	}
	if atLimitReopened != 0 {
		t.Fatalf("%d claims settled at the limit reopened with no cover left, want 0", atLimitReopened)
	}
}
