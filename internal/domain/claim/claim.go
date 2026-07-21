// Package claim simulates claim events arising from the policy book
// (step 2 of the simulation): occurrence, report and close dates plus the
// initial case estimate.
package claim

import (
	"fmt"
	"math"
	"sort"
	"time"

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
	// OwnDamage is true when the severity mixture picked the own-damage
	// component. Carried to the recovery stage (only own-damage claims
	// yield salvage or subrogation) but never written to CSV.
	OwnDamage bool
	// FirstCloseDate, ReopenDate and ReopenEstimate describe the single
	// optional reopen episode: the claim closed once, the case was re-raised
	// after a lag, and CloseDate above is the final close. Zero values mean
	// the claim never reopens. Carried to the runoff stage but never written
	// to CSV.
	FirstCloseDate shared.Date
	ReopenDate     shared.Date
	ReopenEstimate shared.Money
}

// Reopened reports whether the claim has a reopen episode.
func (c Claim) Reopened() bool {
	return c.ReopenDate != (shared.Date{})
}

// ClaimSimulator generates claim events for a policy book.
type ClaimSimulator struct {
	params              lob.ClaimParams
	inflation           InflationIndex
	sumInsuredInflation float64
	startYear           int
	windowEnd           shared.Date // zero value means no windowing
}

// NewClaimSimulator builds a claim simulator from the claim parameters.
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

// WithBaseYear sets the sum-insured inflation rate and run start year so
// own-damage severity can be expressed in base-year sum-insured terms (SL-4):
// the drifted sum insured is deflated by sum_insured_inflation raised to the
// policy's underwriting-year offset before scaling the loss. Unset (the zero
// value) leaves severity scaled by the nominal sum insured.
func (s *ClaimSimulator) WithBaseYear(sumInsuredInflation float64, startYear int) *ClaimSimulator {
	s.sumInsuredInflation = sumInsuredInflation
	s.startYear = startYear
	return s
}

// baseSumInsured deflates the policy's drifted sum insured back to base-year
// dollars. Identity when the base-year knob is unset.
func (s *ClaimSimulator) baseSumInsured(pol policy.Policy) float64 {
	if s.sumInsuredInflation <= 0 {
		return pol.SumInsured.Dollars()
	}
	offset := pol.CoverStart.Year() - s.startYear
	return pol.SumInsured.Dollars() / math.Pow(s.sumInsuredInflation, float64(offset))
}

// WithWindow constrains claim occurrences to the run window [startYear, startYear+years):
// each policy's frequency is pro-rated by its in-window exposed fraction of the
// cover term, and occurrences are drawn only over the in-window portion of the
// cover. This stops the trailing underwriting year from spilling a partial,
// out-of-window accident year into claims.csv (MF-2). Unset leaves full-term
// behaviour.
func (s *ClaimSimulator) WithWindow(startYear, years int) *ClaimSimulator {
	s.windowEnd = shared.NewDate(startYear+years, time.January, 1)
	return s
}

// exposedFraction is the share of a policy's cover term that lies inside the
// window; 1 when the window is unset or the cover ends before window end.
func (s *ClaimSimulator) exposedFraction(pol policy.Policy) float64 {
	if s.windowEnd.IsZero() {
		return 1
	}
	end := pol.CoverEnd
	if s.windowEnd.Before(end) {
		end = s.windowEnd
	}
	term := shared.DaysBetween(pol.CoverStart, pol.CoverEnd)
	if term <= 0 {
		return 1
	}
	inWindow := shared.DaysBetween(pol.CoverStart, end)
	return float64(inWindow) / float64(term)
}

// Simulate draws claim events for every policy. Claims are returned sorted
// by report date with sequential IDs, resembling a claims system's
// registration order.
func (s *ClaimSimulator) Simulate(src shared.RandomSource, book []policy.Policy) []Claim {
	var claims []Claim
	for _, pol := range book {
		stream := src.Split(fmt.Sprintf("claims-policy-%d", pol.ID))
		n := stream.Poisson(s.params.BaseFrequency * pol.RiskFactor * s.exposedFraction(pol))
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
	end := pol.CoverEnd
	capToWindow := !s.windowEnd.IsZero() && !end.Before(s.windowEnd)
	if capToWindow {
		end = s.windowEnd
	}
	span := shared.DaysBetween(pol.CoverStart, end)
	if !capToWindow {
		span++ // include the final cover day, as the pre-window model did
	}
	occurrence := pol.CoverStart.AddDays(int(src.Uniform() * float64(span)))

	lag := src.LogNormal(math.Log(s.params.ReportLagMedian), s.params.ReportLagSigma)
	report := occurrence.AddDays(int(math.Round(lag)))

	loss, ownDamage := s.drawGroundUpLoss(src, pol)
	// Own damage is expressed in base-year sum-insured terms (baseSumInsured)
	// and trended by the claims index only, applied here; third-party (Pareto)
	// losses carry the same claims index but no sum-insured term at all. Own
	// damage is then capped at the drifted sum insured, representing a total
	// loss.
	loss *= s.inflation.For(occurrence.Year())
	if ownDamage {
		if cap := pol.SumInsured.Dollars(); loss > cap {
			loss = cap
		}
	}
	estimate := loss - pol.Excess.Dollars()
	if estimate <= 0 {
		return Claim{}, false
	}

	closeDate := report.AddDays(int(math.Round(drawCloseLag(src, s.params.CloseLag, estimate, pol.RiskFactor, ownDamage))))

	// Nil claims draw their severity and probability independently of claim
	// size; real withdrawn claims skew small, so this is a known simplification.
	// The Bernoulli is always drawn - Bernoulli(0) still consumes one uniform
	// and returns false - so toggling the nil knob never reshuffles the draws of
	// later claims on the same policy. This is the shift-free contract the reopen
	// and recovery post-passes also uphold.
	isNil := src.Bernoulli(s.params.NilProbability)

	return Claim{
		PolicyID:        pol.ID,
		OccurrenceDate:  occurrence,
		ReportDate:      report,
		CloseDate:       closeDate,
		InitialEstimate: shared.FromDollars(estimate),
		RiskFactor:      pol.RiskFactor,
		Nil:             isNil,
		OwnDamage:       ownDamage,
	}, true
}

// drawGroundUpLoss mixes own-damage losses (lognormal, scaled by sum
// insured) with third party liability losses (Pareto, uncapped), reporting
// which component fired.
func (s *ClaimSimulator) drawGroundUpLoss(src shared.RandomSource, pol policy.Policy) (loss float64, ownDamage bool) {
	sev := s.params.Severity
	if src.Bernoulli(sev.ThirdPartyWeight) {
		return src.Pareto(sev.ThirdPartyScale, sev.ThirdPartyAlpha), false
	}
	fraction := src.LogNormal(math.Log(sev.OwnDamageMedianFraction), sev.OwnDamageSigma)
	return s.baseSumInsured(pol) * fraction, true
}

// closeLagRegime selects the (shape, mean) close-lag gamma parameters for a
// claim: own-damage claims use the base parameters with the size stretch for
// large claims; third-party claims use the long-tail parameters. Risk loading
// applies to both.
func closeLagRegime(cl lob.CloseLagParams, estimate, riskFactor float64, ownDamage bool) (shape, mean float64) {
	if ownDamage {
		shape, mean = cl.Shape, cl.MeanDays
		if estimate > cl.SizeThreshold {
			mean *= cl.SizeMultiplier
		}
	} else {
		shape, mean = cl.ThirdPartyShape, cl.ThirdPartyMeanDays
	}
	mean *= math.Pow(riskFactor, cl.RiskLoading)
	return shape, mean
}

// drawCloseLag draws a report-to-close (or reopen-to-second-close) delay in
// days: gamma distributed, with own-damage and third-party claims drawing from
// separate regimes (see closeLagRegime).
func drawCloseLag(src shared.RandomSource, cl lob.CloseLagParams, estimate, riskFactor float64, ownDamage bool) float64 {
	shape, mean := closeLagRegime(cl, estimate, riskFactor, ownDamage)
	return src.Gamma(shape, mean/shape)
}
