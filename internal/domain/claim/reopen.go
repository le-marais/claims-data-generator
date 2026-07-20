package claim

import (
	"fmt"
	"math"

	"github.com/le-marais/claimsgen/internal/domain/lob"
	"github.com/le-marais/claimsgen/internal/domain/shared"
)

// ReopenSimulator decides which closed claims reopen once. It runs as a
// post-pass after claim IDs are assigned, drawing from a labelled
// sub-stream per claim so that enabling reopening never reshuffles the
// draws of any other stage.
type ReopenSimulator struct {
	params lob.ClaimParams
}

func NewReopenSimulator(p lob.ClaimParams) *ReopenSimulator {
	return &ReopenSimulator{params: p}
}

// Apply mutates reopened claims in place: CloseDate becomes the final
// close and the reopen episode is recorded on the claim. A probability of
// 0 makes no draw at all. Non-reopened claims are returned unchanged.
func (s *ReopenSimulator) Apply(src shared.RandomSource, claims []Claim) []Claim {
	r := s.params.Reopening
	if r.Probability <= 0 {
		return claims
	}
	for i := range claims {
		c := &claims[i]
		stream := src.Split(fmt.Sprintf("reopen-claim-%d", c.ID))
		if !stream.Bernoulli(r.Probability) {
			continue
		}
		lag := int(math.Round(stream.LogNormal(math.Log(r.LagMedianDays), r.LagSigma)))
		if lag < 1 {
			lag = 1 // the reopen is strictly after the first close
		}
		estimate := c.InitialEstimate.MulFloat(r.EstimateFactor * shared.MeanOneLogNormal(stream, r.EstimateSigma))
		if estimate < 1 {
			estimate = 1
		}
		closeLag := int(math.Round(drawCloseLag(stream, s.params.CloseLag, estimate.Dollars(), c.RiskFactor, c.OwnDamage)))
		if closeLag < 1 {
			closeLag = 1 // the second close is strictly after the reopen
		}
		c.FirstCloseDate = c.CloseDate
		c.ReopenDate = c.CloseDate.AddDays(lag)
		c.ReopenEstimate = estimate
		c.CloseDate = c.ReopenDate.AddDays(closeLag)
	}
	return claims
}
