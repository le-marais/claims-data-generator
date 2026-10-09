package claim

import (
	"fmt"
	"math"

	"github.com/le-marais/claimsgen/internal/domain/calendar"
	"github.com/le-marais/claimsgen/internal/domain/lob"
	"github.com/le-marais/claimsgen/internal/domain/shared"
)

// ReopenSimulator decides which closed claims reopen once. It runs as a
// post-pass after claim IDs are assigned, drawing from a labelled
// sub-stream per claim so that enabling reopening never reshuffles the
// draws of any other stage.
type ReopenSimulator struct {
	params       lob.ClaimParams
	inflation    InflationIndex
	paymentDelay int
	holiday      lob.SeasonalHolidayParams
	calendar     calendar.Calendar
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

// WithPaymentDelay keeps every reopen episode open at least days, so its
// payment can be processed that long after the case is re-raised: a reopen
// always pays, so its close lag is the delay plus the drawn lag. The
// default, 0, adds nothing.
func (s *ReopenSimulator) WithPaymentDelay(days int) *ReopenSimulator {
	s.paymentDelay = days
	return s
}

// WithSeasonalHoliday defers reopens and reopen closes dated in the holiday
// window to the same day of the next month: a reopen at the report share, as
// the claimant coming back, and its close at the payment share, as a reopen
// always pays and its close carries the settlement. The default, off, takes
// no draws.
func (s *ReopenSimulator) WithSeasonalHoliday(h lob.SeasonalHolidayParams) *ReopenSimulator {
	s.holiday = h
	return s
}

// WithCalendar rolls every reopen and second close to the calendar's next
// business day. The default, an off calendar, moves nothing.
func (s *ReopenSimulator) WithCalendar(c calendar.Calendar) *ReopenSimulator {
	s.calendar = c
	return s
}

// Apply appends a reopen episode to each claim that reopens: the case is
// re-raised a lag after the first close, and the second episode pays the
// reopen's additional cost and closes the claim for good. A probability of 0
// makes no draw at all. Claims that do not reopen are returned unchanged.
//
// The reopen's additional cost is capped at the cover the claim has left, so
// total paid never exceeds CoverLimit, the sum insured less excess or the
// section's limit. A claim already paid up to its limit (a total loss, or a
// liability claim settled at its limit) has nothing left to pay and does not
// reopen.
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
		closeLag := s.paymentDelay + int(math.Round(drawCloseLag(stream, s.params.Sections[c.Section].CloseLag, baseSize, c.RiskFactor)))
		if closeLag < 1 {
			closeLag = 1 // the second close is strictly after the reopen
		}
		reopen := first.Close.AddDays(lag)
		var holiday shared.RandomSource
		if s.holiday.Enabled() {
			holiday = stream.Split("seasonal-holiday")
			// A reopen is the claimant coming back, so it is deferred like a
			// report; the second close lag then runs from the deferred date.
			reopen = s.holiday.Defer(reopen, holiday.Uniform(), s.holiday.ReportShare)
		}
		reopen = s.calendar.Following(reopen)
		secondClose := reopen.AddDays(closeLag)
		if holiday != nil {
			secondClose = s.holiday.Defer(secondClose, holiday.Uniform(), s.holiday.PaymentShare)
		}
		secondClose = s.calendar.Following(secondClose)
		// The capped slice makes append copy, so a copy of the claim taken
		// before this pass keeps its single episode.
		c.Episodes = append(c.Episodes[:1:1], Episode{Open: reopen, Close: secondClose, Ultimate: additional})
	}
	return claims
}
