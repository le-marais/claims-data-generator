package transaction

import (
	"fmt"

	"github.com/le-marais/claimsgen/internal/domain/claim"
	"github.com/le-marais/claimsgen/internal/domain/lob"
	"github.com/le-marais/claimsgen/internal/domain/shared"
)

// CaseEstimator sets the case estimate each claim opens at: the claims
// handler's first view of a true cost the claim stage already fixed. It runs
// after reopening and before the runoff, from its own labelled sub-stream per
// claim, so the adequacy knobs move case reserves and never the loss cost or
// any payment.
type CaseEstimator struct {
	mean, sigma float64
}

// NewCaseEstimator builds a case estimator from the runoff parameters' case
// adequacy mean and sigma.
func NewCaseEstimator(p lob.RunoffParams) *CaseEstimator {
	return &CaseEstimator{mean: p.CaseAdequacyMean, sigma: p.CaseAdequacySigma}
}

// Apply sets InitialEstimate from Ultimate and, for a reopened claim,
// ReopenEstimate from ReopenUltimate, mutating claims in place. Each estimate
// is the true cost times mean-one lognormal noise, divided by the adequacy
// mean, so across claims the true cost over the opening case averages the
// adequacy mean: above 1 the case starts deficient, below 1 redundant. A zero
// sigma draws nothing.
func (s *CaseEstimator) Apply(src shared.RandomSource, claims []claim.Claim) []claim.Claim {
	for i := range claims {
		c := &claims[i]
		stream := src.Split(fmt.Sprintf("case-estimate-claim-%d", c.ID))
		c.InitialEstimate = s.estimate(stream, c.Ultimate)
		if c.Reopened() {
			c.ReopenEstimate = s.estimate(stream, c.ReopenUltimate)
		}
	}
	return claims
}

// estimate is one noisy view of a true cost, floored at one cent so the case
// always opens.
func (s *CaseEstimator) estimate(src shared.RandomSource, cost shared.Money) shared.Money {
	e := cost.MulFloat(shared.MeanOneLogNormal(src, s.sigma) / s.mean)
	if e < shared.OneCent {
		e = shared.OneCent
	}
	return e
}
