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
	params    lob.ClaimParams
	inflation InflationIndex
}

// NewReopenSimulator builds a reopen simulator from the claim parameters.
func NewReopenSimulator(p lob.ClaimParams) *ReopenSimulator {
	return &ReopenSimulator{params: p}
}

// WithInflation sets the claims inflation index, used to express a reopen's
// additional cost in start-year dollars for the close-lag size stretch. The
// zero-value index (the default) leaves the cost nominal.
func (s *ReopenSimulator) WithInflation(x InflationIndex) *ReopenSimulator {
	s.inflation = x
	return s
}

// Apply mutates reopened claims in place: CloseDate becomes the final
// close and the reopen episode is recorded on the claim. A probability of
// 0 makes no draw at all. Non-reopened claims are returned unchanged.
//
// The reopen's additional cost is capped at the cover the claim has left, so
// total paid never exceeds CoverLimit. A claim already paid up to its limit
// (a total loss) has nothing left to pay and does not reopen.
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
		additional := c.Ultimate.MulFloat(r.EstimateFactor * shared.MeanOneLogNormal(stream, r.EstimateSigma))
		if additional < shared.OneCent {
			additional = shared.OneCent
		}
		if left, limited := c.coverLeft(); limited {
			if left < shared.OneCent {
				continue // paid up to the limit: nothing left to reopen for
			}
			if additional > left {
				additional = left
			}
		}
		baseSize := additional.Dollars() / s.inflation.For(c.OccurrenceDate)
		closeLag := int(math.Round(drawCloseLag(stream, s.params.CloseLag, baseSize, c.RiskFactor, c.OwnDamage)))
		if closeLag < 1 {
			closeLag = 1 // the second close is strictly after the reopen
		}
		c.FirstCloseDate = c.CloseDate
		c.ReopenDate = c.CloseDate.AddDays(lag)
		c.ReopenUltimate = additional
		c.ReopenEstimate = additional // the case-estimate stage replaces this
		c.CloseDate = c.ReopenDate.AddDays(closeLag)
	}
	return claims
}

// coverLeft is the cover still available after the first episode: the limit
// less what that episode pays (nothing for a nil claim). limited is false for
// unlimited (third-party) cover.
func (c Claim) coverLeft() (left shared.Money, limited bool) {
	if c.CoverLimit <= 0 {
		return 0, false
	}
	paid := c.Ultimate
	if c.Nil {
		paid = 0
	}
	return c.CoverLimit - paid, true
}
