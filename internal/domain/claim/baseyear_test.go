package claim

import (
	"testing"
	"time"

	"github.com/le-marais/claimsgen/internal/domain/lob"
	"github.com/le-marais/claimsgen/internal/domain/policy"
	"github.com/le-marais/claimsgen/internal/domain/shared"
	"github.com/le-marais/claimsgen/internal/infrastructure/random"
)

// bookForCap builds a small book of identical policies for severity tests.
func bookForCap(n int) []policy.Policy {
	var b []policy.Policy
	start := shared.NewDate(2000, time.January, 1)
	for i := 1; i <= n; i++ {
		b = append(b, policy.Policy{
			ID:         i,
			CoverStart: start,
			CoverEnd:   start.AddDays(364),
			SumInsured: shared.FromDollars(20000),
			Excess:     shared.FromDollars(300),
			RiskFactor: 1.0,
		})
	}
	return b
}

func TestOwnDamageIsCappedAtSumInsured(t *testing.T) {
	params := lob.ClaimParams{
		BaseFrequency:   2.0, // many claims per policy so the tail is exercised
		ReportLagMedian: 2,
		ReportLagSigma:  1.2,
		Severity: lob.SeverityParams{
			ThirdPartyWeight:        0,   // pure own damage
			OwnDamageMedianFraction: 0.5, // heavy, so the cap bites often
			OwnDamageSigma:          1.5,
			ThirdPartyScale:         4000,
			ThirdPartyAlpha:         2.2,
		},
		CloseLag: lob.CloseLagParams{Shape: 1.2, MeanDays: 120, SizeThreshold: 20000, SizeMultiplier: 6, RiskLoading: 0.3, ThirdPartyShape: 1.0, ThirdPartyMeanDays: 680},
	}
	claims := NewClaimSimulator(params).Simulate(random.NewSource(1), bookForCap(500))
	if len(claims) == 0 {
		t.Fatal("no claims generated")
	}
	for _, c := range claims {
		if !c.OwnDamage {
			continue
		}
		groundUp := c.InitialEstimate.Dollars() + 300 // + excess
		if groundUp > 20000+1e-6 {
			t.Fatalf("claim %d own-damage ground-up %.2f exceeds sum insured 20000", c.ID, groundUp)
		}
	}
}

// RF-14: own-damage severity is sized off the policy's BaseSumInsured, so the
// claim stage needs no book parameter; a policy without one falls back to the
// nominal sum insured.
func TestOwnDamageSeverityReadsBaseSumInsured(t *testing.T) {
	params := lob.ClaimParams{
		BaseFrequency: 1, ReportLagMedian: 2, ReportLagSigma: 1.2,
		Severity: lob.SeverityParams{ThirdPartyWeight: 0, OwnDamageMedianFraction: 0.01, OwnDamageSigma: 0.5},
		CloseLag: lob.CloseLagParams{Shape: 1.2, MeanDays: 40, SizeThreshold: 20000, SizeMultiplier: 3, ThirdPartyShape: 1, ThirdPartyMeanDays: 400},
	}
	book := func(base float64) []policy.Policy {
		var b []policy.Policy
		start := shared.NewDate(2000, time.January, 1)
		for i := 1; i <= 300; i++ {
			b = append(b, policy.Policy{
				ID: i, CoverStart: start, CoverEnd: start.AddDays(364), RiskFactor: 1,
				SumInsured: shared.FromDollars(1e7), BaseSumInsured: base, // far above any loss, so no cap
			})
		}
		return b
	}
	nominal := NewClaimSimulator(params).Simulate(random.NewSource(3), book(0))
	base := NewClaimSimulator(params).Simulate(random.NewSource(3), book(5e6))
	if len(nominal) == 0 || len(nominal) != len(base) {
		t.Fatalf("claim counts %d and %d, want equal and non-zero", len(nominal), len(base))
	}
	for i := range nominal {
		// Half the base sum insured, half the loss, to the cent.
		if got, want := base[i].Ultimate.Dollars(), nominal[i].Ultimate.Dollars()/2; got < want-0.01 || got > want+0.01 {
			t.Fatalf("claim %d: ultimate %.2f at half the base sum insured, want %.2f", i, got, want)
		}
	}
}
