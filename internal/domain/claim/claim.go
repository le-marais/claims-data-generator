// Package claim simulates claim events arising from the policy book
// (step 2 of the simulation): occurrence, report and close dates plus the
// initial case estimate.
package claim

import (
	"fmt"
	"math"
	"sort"

	"github.com/le-marais/claimsgen/internal/domain/lob"
	"github.com/le-marais/claimsgen/internal/domain/policy"
	"github.com/le-marais/claimsgen/internal/domain/shared"
)

// Claim is one reported claim event. All claims close: there is no
// valuation date and every claim develops fully.
type Claim struct {
	ID              int
	PolicyID        int
	OccurrenceDate  shared.Date
	ReportDate      shared.Date
	CloseDate       shared.Date
	InitialEstimate shared.Money
	// RiskFactor is carried from the policy for downstream stages.
	RiskFactor float64
	// Nil is true when the claim closes without any payment. It is carried
	// to the runoff stage but never written to CSV.
	Nil bool
}

// ClaimSimulator generates claim events for a policy book.
type ClaimSimulator struct {
	params    lob.ClaimParams
	inflation InflationIndex
}

func NewClaimSimulator(p lob.ClaimParams) *ClaimSimulator {
	return &ClaimSimulator{params: p}
}

// WithInflation sets the occurrence-year inflation index. The zero-value
// index (the default) is the identity, so a simulator built without this
// call applies no inflation.
func (s *ClaimSimulator) WithInflation(x InflationIndex) *ClaimSimulator {
	s.inflation = x
	return s
}

// Simulate draws claim events for every policy. Claims are returned sorted
// by report date with sequential IDs, resembling a claims system's
// registration order.
func (s *ClaimSimulator) Simulate(src shared.RandomSource, book []policy.Policy) []Claim {
	var claims []Claim
	for _, pol := range book {
		stream := src.Split(fmt.Sprintf("claims-policy-%d", pol.ID))
		n := stream.Poisson(s.params.BaseFrequency * pol.RiskFactor)
		for i := 0; i < n; i++ {
			if c, ok := s.simulateClaim(stream, pol); ok {
				claims = append(claims, c)
			}
		}
	}
	sort.SliceStable(claims, func(i, j int) bool {
		if claims[i].ReportDate != claims[j].ReportDate {
			return claims[i].ReportDate.Before(claims[j].ReportDate)
		}
		if claims[i].PolicyID != claims[j].PolicyID {
			return claims[i].PolicyID < claims[j].PolicyID
		}
		return claims[i].OccurrenceDate.Before(claims[j].OccurrenceDate)
	})
	for i := range claims {
		claims[i].ID = i + 1
	}
	return claims
}

// simulateClaim draws one claim; ok is false when the ground-up loss does
// not exceed the excess, making the claim unreportable.
func (s *ClaimSimulator) simulateClaim(src shared.RandomSource, pol policy.Policy) (Claim, bool) {
	term := shared.DaysBetween(pol.CoverStart, pol.CoverEnd)
	occurrence := pol.CoverStart.AddDays(int(src.Uniform() * float64(term+1)))

	lag := src.LogNormal(math.Log(s.params.ReportLagMedian), s.params.ReportLagSigma)
	report := occurrence.AddDays(int(math.Round(lag)))

	loss := s.drawGroundUpLoss(src, pol)
	loss *= s.inflation.For(occurrence.Year())
	estimate := loss - pol.Excess.Dollars()
	if estimate <= 0 {
		return Claim{}, false
	}

	close := report.AddDays(int(math.Round(s.drawCloseLag(src, estimate, pol.RiskFactor))))

	isNil := s.params.NilProbability > 0 && src.Bernoulli(s.params.NilProbability)

	return Claim{
		PolicyID:        pol.ID,
		OccurrenceDate:  occurrence,
		ReportDate:      report,
		CloseDate:       close,
		InitialEstimate: shared.FromDollars(estimate),
		RiskFactor:      pol.RiskFactor,
		Nil:             isNil,
	}, true
}

// drawGroundUpLoss mixes own-damage losses (lognormal, scaled by sum
// insured) with third party liability losses (Pareto, uncapped).
func (s *ClaimSimulator) drawGroundUpLoss(src shared.RandomSource, pol policy.Policy) float64 {
	sev := s.params.Severity
	if src.Bernoulli(sev.ThirdPartyWeight) {
		return src.Pareto(sev.ThirdPartyScale, sev.ThirdPartyAlpha)
	}
	fraction := src.LogNormal(math.Log(sev.OwnDamageMedianFraction), sev.OwnDamageSigma)
	return pol.SumInsured.Dollars() * fraction
}

// drawCloseLag draws the report-to-close delay in days: gamma distributed,
// with the mean stretched for large claims and risky policyholders.
func (s *ClaimSimulator) drawCloseLag(src shared.RandomSource, estimate, riskFactor float64) float64 {
	cl := s.params.CloseLag
	mean := cl.MeanDays
	if estimate > cl.SizeThreshold {
		mean *= cl.SizeMultiplier
	}
	mean *= math.Pow(riskFactor, cl.RiskLoading)
	return src.Gamma(cl.Shape, mean/cl.Shape)
}
