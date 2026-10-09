// Package claim simulates claim events arising from the policy book:
// occurrence, report and close dates, true cost, and the optional reopen
// episode.
package claim

import (
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/le-marais/claimsgen/internal/domain/calendar"
	"github.com/le-marais/claimsgen/internal/domain/lob"
	"github.com/le-marais/claimsgen/internal/domain/policy"
	"github.com/le-marais/claimsgen/internal/domain/shared"
)

// Claim is one reported claim event. All claims close: there is no
// valuation date and every claim develops fully.
//
// A claim's life is a sequence of episodes, each open from a start date to a
// close date: the first runs from the day the claim is opened - the report
// date, or with a business-day calendar the next business day - to the first
// close, and a reopened claim has a second. The claim stage writes the first episode and
// the reopen stage appends the second; no stage rewrites an episode's dates or
// cost. The other fields are what the simulation knows about the claim from
// its policy, which later stages need. Record is the claims.csv view of it.
type Claim struct {
	ID             int
	PolicyID       int
	OccurrenceDate shared.Date
	// Reported is the day the claim was reported. The first episode opens on
	// it, or on the next business day when the calendar closes on it. Zero
	// means the first episode's open, as on a hand-built claim.
	Reported shared.Date
	Episodes []Episode
	// Section is the index of the claim's section of cover in the line of
	// business's sections.
	Section int
	// Seq is the claim's place, from 1, among the losses its policy's
	// section drew, reportable or not. With PolicyID and Section it names
	// the claim independently of the registration order ID follows, so the
	// claim's later random streams survive a change that moves report dates
	// (MR-24). Zero means unset, as on a hand-built claim.
	Seq int
	// CoverLimit is the most the policy pays on the claim over its whole
	// life, reopen included: sum insured minus the excess the section takes
	// for a sum-insured severity, the section's limit for a limited one, zero
	// (unlimited) otherwise.
	CoverLimit shared.Money
	// RiskFactor is the policy's risk factor, kept for the reopen pass's
	// close-lag draw, which runs after the claim stage has let go of the
	// policy.
	RiskFactor float64
}

// Episode is one open-to-close stretch of a claim's development.
type Episode struct {
	Open  shared.Date
	Close shared.Date
	// Ultimate is the episode's true cost. For the first episode it is the
	// ground-up loss net of excess, capped at the cover for a sum-insured
	// severity; for a reopen it is the additional cost. The severity model sizes it, not the
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

// StreamKey labels the claim's own random streams in the later stages: its
// policy, section and sequence, so the streams do not depend on the order
// claims are registered in. A hand-built claim without a sequence uses its
// ID.
func (c Claim) StreamKey() string {
	if c.Seq == 0 {
		return fmt.Sprint(c.ID)
	}
	return fmt.Sprintf("p%d-s%d-%d", c.PolicyID, c.Section, c.Seq)
}

// ReportDate is the day the claim was reported. Its first episode opens, with
// the case, on that day or the next business day.
func (c Claim) ReportDate() shared.Date {
	if c.Reported.IsZero() {
		return c.Episodes[0].Open
	}
	return c.Reported
}

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

// TotalLoss reports whether the claim's true cost reached its cover limit,
// which on a sum-insured section is a write-off of the insured property.
func (c Claim) TotalLoss() bool {
	return c.CoverLimit > 0 && c.Episodes[0].Ultimate >= c.CoverLimit
}

// ClaimSimulator generates claim events for a policy book.
type ClaimSimulator struct {
	params       lob.ClaimParams
	inflation    InflationIndex
	windowStart  shared.Date // zero value means no windowing
	windowEnd    shared.Date // exclusive
	paymentDelay int
	holiday      lob.SeasonalHolidayParams
	calendar     calendar.Calendar
	rollReports  bool
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

// WithPaymentDelay keeps every paying claim open at least days after report,
// so its payment can be processed that long after the case opens: its close
// lag is the delay plus the drawn lag. A nil claim, which pays nothing, keeps
// its drawn lag. The default, 0, adds nothing.
func (s *ClaimSimulator) WithPaymentDelay(days int) *ClaimSimulator {
	s.paymentDelay = days
	return s
}

// WithSeasonalHoliday defers a share of the reports, and of the paying
// closes, dated in the holiday window to the same day of the next month. A
// deferred report moves the claim's whole timeline back. The default, off,
// takes no draws.
func (s *ClaimSimulator) WithSeasonalHoliday(h lob.SeasonalHolidayParams) *ClaimSimulator {
	s.holiday = h
	return s
}

// WithBusinessDays opens every claim, and closes it, on a business day of the
// calendar: the first episode opens on the report date or the next business
// day after it, and the close lag runs from the opening. With rollReports the
// report date itself rolls too. The default, an off calendar, moves nothing.
func (s *ClaimSimulator) WithBusinessDays(c calendar.Calendar, rollReports bool) *ClaimSimulator {
	s.calendar, s.rollReports = c, rollReports
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

// Simulate draws claim events for every policy. Each section of each policy
// draws its claims from its own sub-stream, so a change to one section never
// moves another's draws. Claims are returned sorted by report date with
// sequential IDs, resembling a claims system's registration order.
func (s *ClaimSimulator) Simulate(src shared.RandomSource, book []policy.Policy) []Claim {
	var claims []Claim
	for _, pol := range book {
		stream := src.Split(fmt.Sprintf("claims-policy-%d", pol.ID))
		exposed := s.exposedFraction(pol)
		for i, sec := range s.params.Sections {
			sectionStream := stream.Split(sec.Name)
			var holiday shared.RandomSource
			if s.holiday.Enabled() {
				holiday = sectionStream.Split("seasonal-holiday")
			}
			n := sectionStream.Poisson(sec.BaseFrequency * pol.RiskFactor * exposed)
			for seq := 1; seq <= n; seq++ {
				if c, ok := s.simulateClaim(sectionStream, holiday, pol, i); ok {
					c.Seq = seq
					claims = append(claims, c)
				}
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

// simulateClaim draws one claim in the given section; ok is false when the
// ground-up loss does not exceed the excess, making the claim unreportable.
// A section that takes no excess reports every loss.
// The severity draw is the first episode's true cost (Ultimate); the case
// estimate is a separate, later view of it. Every claim takes the same draws
// in the same order, reportable or not.
//
// holiday is the section's seasonal-holiday stream, nil when the holiday is
// off. Every claim takes two draws from it, one for its report and one for
// its close, so the holiday never moves a draw of src.
func (s *ClaimSimulator) simulateClaim(src, holiday shared.RandomSource, pol policy.Policy, section int) (Claim, bool) {
	uReport, uClose := 1.0, 1.0 // 1 never defers
	if holiday != nil {
		uReport, uClose = holiday.Uniform(), holiday.Uniform()
	}
	sec := s.params.Sections[section]
	first, span := s.occurrenceSpan(pol)
	occurrence := first.AddDays(int(src.Uniform() * float64(span)))
	lag := src.LogNormal(math.Log(sec.ReportLag.Median), sec.ReportLag.Sigma)
	report := s.holiday.Defer(occurrence.AddDays(int(math.Round(lag))), uReport, s.holiday.ReportShare)
	if s.rollReports {
		report = s.calendar.Following(report)
	}
	// The claim is opened, and its case set, on a business day; the close
	// lag runs from then.
	open := s.calendar.Following(report)

	// Losses are drawn in start-year dollars and trended by the claims index
	// at the occurrence date. A sum-insured loss is then capped at the drifted
	// sum insured, representing a total loss; a Pareto or lognormal loss is
	// uncapped, though the cost after excess is capped at the section's limit.
	loss := s.drawGroundUpLoss(src, pol, sec.Severity) * s.inflation.For(occurrence)
	excess := pol.Excess
	if sec.NoExcess {
		excess = 0
	}
	coverLimit := shared.Money(0) // unlimited
	if sec.Severity.Kind == lob.SumInsuredLognormal {
		if cap := pol.SumInsured.Dollars(); loss > cap {
			loss = cap
		}
		coverLimit = pol.SumInsured - excess
	}
	cost := loss - excess.Dollars()
	if cost <= 0 {
		return Claim{}, false
	}
	// The limit is a nominal contract term, so it is not trended. The close
	// lag below reads the capped cost: a claim settled at the limit settles
	// like a claim of the limit's size.
	if sec.Limit > 0 {
		if cost > sec.Limit {
			cost = sec.Limit
		}
		coverLimit = shared.FromDollars(sec.Limit)
	}
	ultimate := shared.FromDollars(cost)
	if ultimate < shared.OneCent {
		ultimate = shared.OneCent // a reportable claim always costs something
	}

	// The close lag reads the cost in start-year dollars, so claims inflation
	// does not lengthen settlement year on year (MR-5).
	baseCost := cost / s.inflation.For(occurrence)
	closeLag := int(math.Round(drawCloseLag(src, sec.CloseLag, baseCost, pol.RiskFactor)))

	// Nil claims draw their severity and probability independently of claim
	// size; real withdrawn claims skew small, so this is a known simplification.
	// The Bernoulli is always drawn - Bernoulli(0) still consumes one uniform
	// and returns false - so toggling the nil knob never reshuffles the draws of
	// later claims on the same policy. This is the shift-free contract the reopen
	// and recovery post-passes also uphold.
	isNil := src.Bernoulli(s.params.NilProbability)
	closeDate := open.AddDays(closeLag)
	if !isNil {
		// The close carries the final settlement, so a paying close can be
		// deferred; a nil close pays nothing and stays.
		closeDate = s.holiday.Defer(closeDate.AddDays(s.paymentDelay), uClose, s.holiday.PaymentShare)
	}
	closeDate = s.calendar.Following(closeDate) // a close is processed on a business day

	return Claim{
		PolicyID:       pol.ID,
		OccurrenceDate: occurrence,
		Reported:       report,
		Episodes:       []Episode{{Open: open, Close: closeDate, Ultimate: ultimate, Nil: isNil}},
		Section:        section,
		CoverLimit:     coverLimit,
		RiskFactor:     pol.RiskFactor,
	}, true
}

// drawGroundUpLoss draws a loss in start-year dollars from a section's
// severity: a lognormal fraction of the policy's base-year sum insured, a
// Pareto amount, a lognormal amount, or a spliced lognormal-Pareto amount.
// Every kind takes one draw.
func (s *ClaimSimulator) drawGroundUpLoss(src shared.RandomSource, pol policy.Policy, sev lob.SeverityParams) float64 {
	switch sev.Kind {
	case lob.Pareto:
		return src.Pareto(sev.Scale, sev.Alpha)
	case lob.Lognormal:
		return src.LogNormal(math.Log(sev.Median), sev.Sigma)
	case lob.LognormalPareto:
		return sev.LognormalParetoLoss(src.Uniform())
	}
	return s.baseSumInsured(pol) * src.LogNormal(math.Log(sev.MedianFraction), sev.Sigma)
}

// closeLagMean is the mean report-to-close lag of a claim costing baseSize in
// start-year dollars on a policy with the given risk factor: the section's
// MeanDays scaled smoothly by size and by risk.
func closeLagMean(cl lob.CloseLagParams, baseSize, riskFactor float64) float64 {
	mean := cl.MeanDays
	if cl.SizeElasticity > 0 {
		mean *= math.Pow(baseSize/cl.SizeReference, cl.SizeElasticity)
	}
	return mean * math.Pow(riskFactor, cl.RiskLoading)
}

// drawCloseLag draws a report-to-close (or reopen-to-second-close) delay in
// days: gamma distributed with the section's shape and the mean closeLagMean
// gives.
func drawCloseLag(src shared.RandomSource, cl lob.CloseLagParams, baseSize, riskFactor float64) float64 {
	return src.Gamma(cl.Shape, closeLagMean(cl, baseSize, riskFactor)/cl.Shape)
}
