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
	params    lob.ClaimParams
	inflation InflationIndex
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

	loss, ownDamage := s.drawGroundUpLoss(src, pol)
	// The own-damage component scales with a sum insured that already drifts by
	// sum_insured_inflation, so its effective severity trend is the product of
	// sum_insured_inflation and claims inflation; third-party (Pareto) losses
	// carry only the claims index applied here.
	loss *= s.inflation.For(occurrence.Year())
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
	return pol.SumInsured.Dollars() * fraction, true
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
