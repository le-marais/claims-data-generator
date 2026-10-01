// Package claim simulates claim events arising from the policy book:
// occurrence, report and close dates, true cost, and the optional reopen
// episode.
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
// A claim's life is a sequence of episodes, each open from a start date to a
// close date: the first runs from the report date to the first close, and a
// reopened claim has a second. The claim stage writes the first episode and
// the reopen stage appends the second; no stage rewrites an episode's dates or
// cost. The other fields are what the simulation knows about the claim from
// its policy, which later stages need. Record is the claims.csv view of it.
type Claim struct {
	ID             int
	PolicyID       int
	OccurrenceDate shared.Date
	Episodes       []Episode
	// CoverLimit is the most the policy pays on the claim over its whole
	// life, reopen included: sum insured minus excess for own damage, zero
	// (unlimited) for third party.
	CoverLimit shared.Money
	// RiskFactor is the policy's risk factor, kept for the reopen pass's
	// close-lag draw, which runs after the claim stage has let go of the
	// policy.
	RiskFactor float64
	// OwnDamage is true when the severity mixture picked the own-damage
	// component. Recovery eligibility depends on it: only own-damage claims
	// yield salvage or subrogation.
	OwnDamage bool
}

// Episode is one open-to-close stretch of a claim's development.
type Episode struct {
	Open  shared.Date
	Close shared.Date
	// Ultimate is the episode's true cost. For the first episode it is the
	// ground-up loss net of excess, capped at the cover for own damage; for a
	// reopen it is the additional cost. The severity model sizes it, not the
	// case estimate, so the case adequacy knobs move reserves, never the loss
	// cost.
	Ultimate shared.Money
	// Nil is true when the episode closes without paying its Ultimate.
	Nil bool
	// OpeningCase is the case estimate the episode opens at, the claims
	// handler's first view of Ultimate, which the case-estimate stage sets
	// (see transaction.CaseEstimator).
	OpeningCase shared.Money
}

// Paid is what the episode pays: its Ultimate, or nothing when it is nil.
func (e Episode) Paid() shared.Money {
	if e.Nil {
		return 0
	}
	return e.Ultimate
}

// Record is the persisted claim: exactly the claims.csv columns, as a claims
// system would hold them. CloseDate is the final close after any reopen.
type Record struct {
	ID              int
	PolicyID        int
	OccurrenceDate  shared.Date
	ReportDate      shared.Date
	CloseDate       shared.Date
	InitialEstimate shared.Money
}

// Record is the claim's claims.csv row.
func (c Claim) Record() Record {
	return Record{
		ID:              c.ID,
		PolicyID:        c.PolicyID,
		OccurrenceDate:  c.OccurrenceDate,
		ReportDate:      c.ReportDate(),
		CloseDate:       c.CloseDate(),
		InitialEstimate: c.InitialEstimate(),
	}
}

// ReportDate is the day the claim was reported, when its first episode opens.
func (c Claim) ReportDate() shared.Date { return c.Episodes[0].Open }

// CloseDate is the claim's final close, after any reopen.
func (c Claim) CloseDate() shared.Date { return c.Episodes[len(c.Episodes)-1].Close }

// InitialEstimate is the case estimate the claim opens at on its report date.
func (c Claim) InitialEstimate() shared.Money { return c.Episodes[0].OpeningCase }

// Nil reports whether the claim's first episode closes without payment.
func (c Claim) Nil() bool { return c.Episodes[0].Nil }

// Reopened reports whether the claim has a reopen episode.
func (c Claim) Reopened() bool { return len(c.Episodes) > 1 }

// Cost is the claim's true total cost over its life: what its episodes pay,
// before recoveries.
func (c Claim) Cost() shared.Money {
	cost := shared.Money(0)
	for _, e := range c.Episodes {
		cost += e.Paid()
	}
	return cost
}

// TotalLoss reports whether the claim wrote the vehicle off: an own-damage
// claim whose true cost reached its cover limit, the sum insured less excess.
func (c Claim) TotalLoss() bool {
	return c.OwnDamage && c.CoverLimit > 0 && c.Episodes[0].Ultimate >= c.CoverLimit
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
		if ri, rj := claims[i].ReportDate(), claims[j].ReportDate(); ri != rj {
			return ri.Before(rj)
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
// the first episode's true cost (Ultimate); the case estimate is a separate,
// later view of it.
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
		PolicyID:       pol.ID,
		OccurrenceDate: occurrence,
		Episodes:       []Episode{{Open: report, Close: closeDate, Ultimate: ultimate, Nil: isNil}},
		CoverLimit:     coverLimit,
		RiskFactor:     pol.RiskFactor,
		OwnDamage:      ownDamage,
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
