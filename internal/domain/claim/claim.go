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
//
// A claim has two parts. Record is what a claims system would hold and
// claims.csv carries. Development is what the simulation knows about how the
// claim develops - its true cost, its type, its reopen episode - which later
// stages need and no CSV writes (RF-14). Both are embedded, so their fields
// read as c.ID or c.Nil; the split makes the CSV surface explicit in the type
// rather than in a comment on each field.
type Claim struct {
	Record
	Development
}

// Record is the persisted claim: exactly the claims.csv columns.
type Record struct {
	ID             int
	PolicyID       int
	OccurrenceDate shared.Date
	ReportDate     shared.Date
	CloseDate      shared.Date
	// InitialEstimate is the opening case estimate on the report date. The
	// claim stage sets it to Ultimate; the case-estimate stage replaces it
	// with the claims handler's view (see transaction.CaseEstimator).
	InitialEstimate shared.Money
}

// Development is the simulation's knowledge of how a claim develops, passed
// from the claim stage to the reopen, case-estimate, runoff and recovery
// stages. None of it is written to CSV.
type Development struct {
	// Ultimate is the claim's true cost: the ground-up loss net of excess,
	// capped at the cover for own damage. It is what the first episode pays
	// in full (nothing, for a nil claim). The severity model sizes this, not
	// the case estimate, so the case adequacy knobs move reserves, never the
	// loss cost.
	Ultimate shared.Money
	// CoverLimit is the most the policy pays on the claim over its whole
	// life, reopen included: sum insured minus excess for own damage, zero
	// (unlimited) for third party.
	CoverLimit shared.Money
	// RiskFactor is the policy's risk factor, kept for the reopen pass's
	// close-lag draw, which runs after the claim stage has let go of the
	// policy.
	RiskFactor float64
	// Nil is true when the claim's first episode closes without payment.
	Nil bool
	// OwnDamage is true when the severity mixture picked the own-damage
	// component. Recovery eligibility depends on it: only own-damage claims
	// yield salvage or subrogation.
	OwnDamage bool
	// FirstCloseDate, ReopenDate, ReopenUltimate and ReopenEstimate describe
	// the single optional reopen episode: the claim closed once, the case was
	// re-raised after a lag, and CloseDate is the final close.
	// ReopenUltimate is the episode's true additional cost and ReopenEstimate
	// the case it re-opens at, which the case-estimate stage sets. Zero values
	// mean the claim never reopens.
	FirstCloseDate shared.Date
	ReopenDate     shared.Date
	ReopenUltimate shared.Money
	ReopenEstimate shared.Money
}

// Cost is the claim's true total cost over its life: the first episode's
// Ultimate unless the claim is nil, plus the reopen episode's ReopenUltimate.
// It is what the claim eventually pays, before recoveries.
func (c Claim) Cost() shared.Money {
	cost := c.ReopenUltimate
	if !c.Nil {
		cost += c.Ultimate
	}
	return cost
}

// TotalLoss reports whether the claim wrote the vehicle off: an own-damage
// claim whose true cost reached its cover limit, the sum insured less excess.
func (c Claim) TotalLoss() bool {
	return c.OwnDamage && c.CoverLimit > 0 && c.Ultimate >= c.CoverLimit
}

// Reopened reports whether the claim has a reopen episode.
func (c Claim) Reopened() bool {
	return c.ReopenDate != (shared.Date{})
}

// ClaimSimulator generates claim events for a policy book.
type ClaimSimulator struct {
	params      lob.ClaimParams
	inflation   InflationIndex
	windowStart shared.Date // zero value means no windowing
	windowEnd   shared.Date // exclusive
}

// NewClaimSimulator builds a claim simulator from the claim parameters.
func NewClaimSimulator(p lob.ClaimParams) *ClaimSimulator {
	return &ClaimSimulator{params: p}
}

// WithInflation sets the occurrence-date inflation index. The zero-value
// index (the default) is the identity, so a simulator built without this
// call applies no inflation.
func (s *ClaimSimulator) WithInflation(x InflationIndex) *ClaimSimulator {
	s.inflation = x
	return s
}

// baseSumInsured is the policy's sum insured in start-year dollars, which the
// book stage records (Policy.BaseSumInsured). A hand-built policy without it
// falls back to the nominal sum insured.
func (s *ClaimSimulator) baseSumInsured(pol policy.Policy) float64 {
	if pol.BaseSumInsured > 0 {
		return pol.BaseSumInsured
	}
	return pol.SumInsured.Dollars()
}

// WithWindow constrains claim occurrences to the run window [startYear, startYear+years):
// each policy's frequency is pro-rated by its in-window exposed fraction of the
// cover term, and occurrences are drawn only over the in-window portion of the
// cover. This stops the trailing underwriting year from spilling a partial,
// out-of-window accident year into claims.csv (MF-2), and the warm-up
// underwriting year before the window from adding claims before it (MR-6).
// Unset leaves full-term behaviour.
func (s *ClaimSimulator) WithWindow(startYear, years int) *ClaimSimulator {
	s.windowStart = shared.NewDate(startYear, time.January, 1)
	s.windowEnd = shared.NewDate(startYear+years, time.January, 1)
	return s
}

// occurrenceSpan is the part of a policy's cover that claims can occur in:
// its first day and its number of days. Cover runs from CoverStart to
// CoverEnd inclusive; the window, when set, clips it to [windowStart,
// windowEnd). days is zero or negative when the cover misses the window.
func (s *ClaimSimulator) occurrenceSpan(pol policy.Policy) (first shared.Date, days int) {
	first, end := pol.CoverStart, pol.CoverEnd.AddDays(1) // end is exclusive
	if !s.windowStart.IsZero() {
		if first.Before(s.windowStart) {
			first = s.windowStart
		}
		if s.windowEnd.Before(end) {
			end = s.windowEnd
		}
	}
	return first, shared.DaysBetween(first, end)
}

// exposedFraction is the share of a policy's cover days that lie inside the
// window: 1 when the window is unset or holds the whole cover, 0 when the
// cover misses it.
func (s *ClaimSimulator) exposedFraction(pol policy.Policy) float64 {
	coverDays := shared.DaysBetween(pol.CoverStart, pol.CoverEnd) + 1
	_, days := s.occurrenceSpan(pol)
	return math.Max(0, float64(days)) / float64(coverDays)
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
// not exceed the excess, making the claim unreportable. The severity draw is
// the claim's true cost (Ultimate); the case estimate is a separate, later
// view of it.
func (s *ClaimSimulator) simulateClaim(src shared.RandomSource, pol policy.Policy) (Claim, bool) {
	first, span := s.occurrenceSpan(pol)
	occurrence := first.AddDays(int(src.Uniform() * float64(span)))

	// The report lag's normal deviate is drawn here, before the claim type is
	// known, and scaled by the type's lag parameters below, so the draw order
	// is the same for both types and a third-party lag never moves another
	// draw.
	lagDeviate := math.Log(src.LogNormal(0, 1))

	loss, ownDamage := s.drawGroundUpLoss(src, pol)
	lag := math.Exp(math.Log(s.reportLagMedian(ownDamage)) + s.reportLagSigma(ownDamage)*lagDeviate)
	report := occurrence.AddDays(int(math.Round(lag)))
	// Own damage is expressed in base-year sum-insured terms (baseSumInsured)
	// and trended by the claims index only, applied here; third-party (Pareto)
	// losses carry the same claims index but no sum-insured term at all. Own
	// damage is then capped at the drifted sum insured, representing a total
	// loss.
	loss *= s.inflation.For(occurrence)
	coverLimit := shared.Money(0) // third-party liability is unlimited
	if ownDamage {
		if cap := pol.SumInsured.Dollars(); loss > cap {
			loss = cap
		}
		coverLimit = pol.SumInsured - pol.Excess
	}
	cost := loss - pol.Excess.Dollars()
	if cost <= 0 {
		return Claim{}, false
	}
	ultimate := shared.FromDollars(cost)
	if ultimate < shared.OneCent {
		ultimate = shared.OneCent // a reportable claim always costs something
	}

	// The size stretch compares the cost in start-year dollars with the
	// threshold, so claims inflation does not push a growing share of claims
	// over it and slow settlement year on year (MR-5).
	baseCost := cost / s.inflation.For(occurrence)
	closeDate := report.AddDays(int(math.Round(drawCloseLag(src, s.params.CloseLag, baseCost, pol.RiskFactor, ownDamage))))

	// Nil claims draw their severity and probability independently of claim
	// size; real withdrawn claims skew small, so this is a known simplification.
	// The Bernoulli is always drawn - Bernoulli(0) still consumes one uniform
	// and returns false - so toggling the nil knob never reshuffles the draws of
	// later claims on the same policy. This is the shift-free contract the reopen
	// and recovery post-passes also uphold.
	isNil := src.Bernoulli(s.params.NilProbability)

	return Claim{
		Record: Record{
			PolicyID:        pol.ID,
			OccurrenceDate:  occurrence,
			ReportDate:      report,
			CloseDate:       closeDate,
			InitialEstimate: ultimate,
		},
		Development: Development{
			Ultimate:   ultimate,
			CoverLimit: coverLimit,
			RiskFactor: pol.RiskFactor,
			Nil:        isNil,
			OwnDamage:  ownDamage,
		},
	}, true
}

// reportLagMedian and reportLagSigma are the lognormal report-lag parameters
// for a claim type: third-party claims use their own when a third-party
// median is set, and the shared ones otherwise.
func (s *ClaimSimulator) reportLagMedian(ownDamage bool) float64 {
	if !ownDamage && s.params.ThirdPartyReportLagMedian > 0 {
		return s.params.ThirdPartyReportLagMedian
	}
	return s.params.ReportLagMedian
}

func (s *ClaimSimulator) reportLagSigma(ownDamage bool) float64 {
	if !ownDamage && s.params.ThirdPartyReportLagMedian > 0 {
		return s.params.ThirdPartyReportLagSigma
	}
	return s.params.ReportLagSigma
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
// claims above the threshold; third-party claims use the long-tail parameters,
// with the mean scaled smoothly by size when ThirdPartySizeElasticity is set.
// Risk loading applies to both. baseSize is the claim's cost in start-year
// dollars, deflated by the claims inflation index.
func closeLagRegime(cl lob.CloseLagParams, baseSize, riskFactor float64, ownDamage bool) (shape, mean float64) {
	if ownDamage {
		shape, mean = cl.Shape, cl.MeanDays
		if baseSize > cl.SizeThreshold {
			mean *= cl.SizeMultiplier
		}
	} else {
		shape, mean = cl.ThirdPartyShape, cl.ThirdPartyMeanDays
		if cl.ThirdPartySizeElasticity > 0 {
			mean *= math.Pow(baseSize/cl.ThirdPartySizeReference, cl.ThirdPartySizeElasticity)
		}
	}
	mean *= math.Pow(riskFactor, cl.RiskLoading)
	return shape, mean
}

// drawCloseLag draws a report-to-close (or reopen-to-second-close) delay in
// days: gamma distributed, with own-damage and third-party claims drawing from
// separate regimes (see closeLagRegime).
func drawCloseLag(src shared.RandomSource, cl lob.CloseLagParams, baseSize, riskFactor float64, ownDamage bool) float64 {
	shape, mean := closeLagRegime(cl, baseSize, riskFactor, ownDamage)
	return src.Gamma(shape, mean/shape)
}
