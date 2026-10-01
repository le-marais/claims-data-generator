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

// Apply appends a reopen episode to each claim that reopens: the case is
// re-raised a lag after the first close, and the second episode pays the
// reopen's additional cost and closes the claim for good. A probability of 0
// makes no draw at all. Claims that do not reopen are returned unchanged.
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
		first := c.Episodes[0]
		lag := int(math.Round(stream.LogNormal(math.Log(r.LagMedianDays), r.LagSigma)))
		if lag < 1 {
			lag = 1 // the reopen is strictly after the first close
		}
		additional := first.Ultimate.MulFloat(r.EstimateFactor * shared.MeanOneLogNormal(stream, r.EstimateSigma))
		if additional < shared.OneCent {
			additional = shared.OneCent
		}
		if c.CoverLimit > 0 {
			left := c.CoverLimit - first.Paid()
			if left < shared.OneCent {
				continue // paid up to the limit: nothing left to reopen for
			}
			if additional > left {
				additional = left
			}
		}
		baseSize := additional.Dollars() / s.inflation.For(c.OccurrenceDate)
		closeLag := int(math.Round(drawCloseLag(stream, s.params.Sections[c.Section].CloseLag, baseSize, c.RiskFactor)))
		if closeLag < 1 {
			closeLag = 1 // the second close is strictly after the reopen
		}
		reopen := first.Close.AddDays(lag)
		// The capped slice makes append copy, so a copy of the claim taken
		// before this pass keeps its single episode.
		c.Episodes = append(c.Episodes[:1:1], Episode{Open: reopen, Close: reopen.AddDays(closeLag), Ultimate: additional})
	}
	return claims
}
