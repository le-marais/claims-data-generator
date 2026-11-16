package transaction

import (
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
	// means is each section's adequacy mean, by section index: its override,
	// else the runoff mean.
	means []float64
	sigma float64
}

// NewCaseEstimator builds a case estimator from the runoff parameters' case
// adequacy mean and sigma, with each section's own mean where it sets one.
func NewCaseEstimator(p lob.RunoffParams, sections []lob.SectionParams) *CaseEstimator {
	return &CaseEstimator{means: adequacyMeans(p, sections), sigma: p.CaseAdequacySigma}
}

// adequacyMeans resolves each section's case adequacy mean: the section's
// override when set, else the runoff default.
func adequacyMeans(p lob.RunoffParams, sections []lob.SectionParams) []float64 {
	means := make([]float64, len(sections))
	for i, sec := range sections {
		means[i] = p.CaseAdequacyMean
		if sec.CaseAdequacyMean > 0 {
			means[i] = sec.CaseAdequacyMean
		}
	}
	return means
}

// Apply sets every episode's opening case from its true cost, updating claims
// in place. Each estimate is the true cost times mean-one lognormal noise,
// divided by the adequacy mean, so across claims the true cost over the
// opening case averages the adequacy mean: above 1 the case starts deficient,
// below 1 redundant. A zero sigma draws nothing.
func (s *CaseEstimator) Apply(src shared.RandomSource, claims []claim.Claim) []claim.Claim {
	for i := range claims {
		c := &claims[i]
		stream := src.Split("case-estimate-claim-" + c.StreamKey())
		// A fresh slice, so a copy of the claim taken before this stage keeps
		// its own episodes.
		episodes := make([]claim.Episode, len(c.Episodes))
		for j, e := range c.Episodes {
			e.OpeningCase = s.estimate(stream, e.Ultimate, s.means[c.Section])
			episodes[j] = e
		}
		c.Episodes = episodes
	}
	return claims
}

// estimate is one noisy view of a true cost, floored at one cent so the case
// always opens.
func (s *CaseEstimator) estimate(src shared.RandomSource, cost shared.Money, mean float64) shared.Money {
	e := cost.MulFloat(shared.MeanOneLogNormal(src, s.sigma) / mean)
	if e < shared.OneCent {
		e = shared.OneCent
	}
	return e
}
