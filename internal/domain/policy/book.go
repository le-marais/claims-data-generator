// Package policy simulates the policy book: the exposure that claims arise
// from.
package policy

import (
	"fmt"
	"math"
	"time"

	"github.com/le-marais/claimsgen/internal/domain/lob"
	"github.com/le-marais/claimsgen/internal/domain/shared"
)

// Policy is one 12-month motor policy covering one vehicle. On a fleet book
// it is one vehicle on a fleet's contract, sharing the fleet's cover dates and
// excess.
type Policy struct {
	ID int
	// FleetID is the fleet the vehicle is on. Without a fleet book every
	// policy is its own fleet, and FleetID equals ID.
	FleetID    int
	CoverStart shared.Date
	CoverEnd   shared.Date
	SumInsured shared.Money
	Excess     shared.Money
	RiskFactor float64
	Premium    shared.Money
	// BaseSumInsured is the sum insured in start-year dollars: SumInsured
	// deflated by the book's sum-insured drift to the policy's underwriting
	// year. Own-damage severity is sized off it, so the claim stage reads a
	// self-describing field instead of needing the book's drift rate. Never
	// written to CSV.
	BaseSumInsured float64
	// SectionPremiums splits Premium by section of cover, in the order of the
	// line of business's sections, each priced the same way on that section's
	// expected loss. The realism gate scores a section's claims against its
	// premium. Never written to CSV.
	SectionPremiums []shared.Money
}

// BookSimulator generates the policy book for a run.
type BookSimulator struct {
	book    lob.BookParams
	pricing lob.PricingParams
}

// NewBookSimulator builds a book simulator from the book and pricing
// parameters; pricing parameters drive premium (independent of the claims
// model that generates experience).
func NewBookSimulator(book lob.BookParams, pricing lob.PricingParams) *BookSimulator {
	return &BookSimulator{book: book, pricing: pricing}
}

// Simulate produces the book: policies written over the given calendar
// years, plus a warm-up underwriting year before the first. The warm-up
// year's policies are in force when the run window opens, so the first
// accident year has a full book behind it rather than one ramping up from
// nothing (MR-6); the claim stage keeps only their in-window occurrences. It
// is sized initialSize / GrowthFactor, with no noise, so initialSize stays the
// size of the first window year.
//
// A year's size is a number of fleets; without a fleet book each fleet is one
// policy. Each later year's size is the previous year's size times the growth
// factor times a mean-1 lognormal noise, so the book trends upward but can
// shrink in individual years. Each year's policies are priced to that year's
// target loss ratio: the pricing target times mean-1 lognormal noise of sigma
// AdequacyVolatility, drawn on its own sub-stream so the knob never moves any
// other draw.
func (s *BookSimulator) Simulate(src shared.RandomSource, startYear, years, initialSize int) []Policy {
	sizeSrc := src.Split("book-size")
	adequacySrc := src.Split("pricing-adequacy")
	var book []Policy
	size := warmUpSize(s.book, initialSize)
	fleetID := 0
	for y := -1; y < years; y++ {
		switch {
		case y == 0:
			size = initialSize
		case y > 0:
			noise := shared.MeanOneLogNormal(sizeSrc, s.book.SizeVolatility)
			size = int(math.Round(float64(size) * s.book.GrowthFactor * noise))
			if size < 1 {
				size = 1
			}
		}
		uw := underwritingYear{
			startYear: startYear,
			year:      startYear + y,
			lossRatio: s.pricing.TargetLossRatio * shared.MeanOneLogNormal(adequacySrc, s.pricing.AdequacyVolatility),
			medianSI:  s.book.SumInsuredMedian * math.Pow(s.book.SumInsuredInflation, float64(y)),
			siDrift:   math.Pow(s.book.SumInsuredInflation, float64(y)),
		}
		for i := 0; i < size; i++ {
			fleetID++
			if s.book.Fleet.Enabled() {
				book = s.simulateFleet(src, book, fleetID, uw)
				continue
			}
			book = append(book, s.simulatePolicy(src.Split(fmt.Sprintf("policy-%d", fleetID)), fleetID, uw))
		}
	}
	return book
}

// underwritingYear is what every policy written in one underwriting year
// shares.
type underwritingYear struct {
	startYear, year int
	// lossRatio is the target loss ratio the year's policies are priced to.
	lossRatio float64
	// medianSI is the book's median sum insured, drifted to the year, and
	// siDrift the drift.
	medianSI, siDrift float64
}

// warmUpSize is the size of the warm-up underwriting year: the first window
// year's size with one year of growth taken off, at least one policy. A
// non-positive growth factor, which validation rejects but ProjectedSize may
// see first, takes nothing off.
func warmUpSize(book lob.BookParams, initialSize int) int {
	if !(book.GrowthFactor > 0) {
		return max(1, initialSize)
	}
	return max(1, int(math.Round(float64(initialSize)/book.GrowthFactor)))
}

// ProjectedSize is about the number of policies a run would write if every
// year's size noise came out at its mean of 1: the warm-up year plus the
// initial size compounded by the growth factor, summed over the years, times
// the expected vehicles per fleet on a fleet book. It mirrors the growth rule
// in Simulate and lets a caller size a run before paying for it. The result is
// a float64 because a large growth factor over many years overflows an int
// long before the run would ever finish.
func ProjectedSize(book lob.BookParams, years, initialSize int) float64 {
	total, size := float64(warmUpSize(book, initialSize)), float64(initialSize)
	for y := 0; y < years; y++ {
		if y > 0 {
			size = math.Max(size*book.GrowthFactor, 1) // Simulate floors each year at 1
		}
		total += size
	}
	return total * book.Fleet.ExpectedSize()
}

// simulatePolicy draws a policy that is a fleet of one, from its own stream:
// cover start, sum insured, risk factor and excess, in that order.
func (s *BookSimulator) simulatePolicy(src shared.RandomSource, id int, uw underwritingYear) Policy {
	start := drawStart(src, uw.year)
	sumInsured, riskFactor := s.drawVehicle(src, uw.medianSI, 1)
	excess := s.drawExcess(src)
	return s.price(id, id, start, sumInsured, riskFactor, excess, uw)
}

// simulateFleet writes one fleet's vehicles onto the book. The fleet draws
// what its vehicles share from its own stream, in a fixed order: cover start,
// excess, vehicle count, median vehicle sum insured and risk factor. Each
// vehicle then draws its own sum insured and risk factor around the fleet's
// from its policy stream, so the fleet sets the level and the book's spread
// is what is left between its vehicles.
func (s *BookSimulator) simulateFleet(src shared.RandomSource, book []Policy, fleetID int, uw underwritingYear) []Policy {
	fleetSrc := src.Split(fmt.Sprintf("fleet-%d", fleetID))
	f := s.book.Fleet
	start := drawStart(fleetSrc, uw.year)
	excess := s.drawExcess(fleetSrc)
	vehicles := max(1, int(math.Round(fleetSrc.LogNormal(math.Log(f.Size.Median), f.Size.Sigma))))
	medianSI := uw.medianSI * fleetSrc.LogNormal(0, f.SumInsuredSigma)
	risk := 1.0
	if f.RiskSpread > 0 {
		risk = meanOneGamma(fleetSrc, f.RiskSpread)
	}
	for range vehicles {
		id := len(book) + 1
		sumInsured, riskFactor := s.drawVehicle(src.Split(fmt.Sprintf("policy-%d", id)), medianSI, risk)
		book = append(book, s.price(id, fleetID, start, sumInsured, riskFactor, excess, uw))
	}
	return book
}

// drawStart draws a cover start uniformly through the year.
func drawStart(src shared.RandomSource, year int) shared.Date {
	yearStart := shared.NewDate(year, time.January, 1)
	daysInYear := shared.DaysBetween(yearStart, shared.NewDate(year+1, time.January, 1))
	return yearStart.AddDays(int(src.Uniform() * float64(daysInYear)))
}

// drawVehicle draws a vehicle's sum insured, lognormal around medianSI with
// sigma Spread, and its risk factor, a mean-one gamma with standard deviation
// Spread scaled by the fleet's risk factor.
func (s *BookSimulator) drawVehicle(src shared.RandomSource, medianSI, fleetRisk float64) (sumInsured, riskFactor float64) {
	sumInsured = src.LogNormal(math.Log(medianSI), s.book.Spread)
	return sumInsured, fleetRisk * meanOneGamma(src, s.book.Spread)
}

// meanOneGamma draws a gamma with mean 1 and the given standard deviation.
func meanOneGamma(src shared.RandomSource, sd float64) float64 {
	sd2 := sd * sd
	return src.Gamma(1/sd2, sd2)
}

// price prices one vehicle to its underwriting year's target loss ratio.
func (s *BookSimulator) price(id, fleetID int, start shared.Date, sumInsured, riskFactor, excess float64, uw underwritingYear) Policy {
	// Trend the assumed loss cost to the middle of the cover, where the
	// average claim occurs, on the same time axis as the claims inflation
	// index (MR-3).
	midCover := start.AddDays(182)
	inflation := math.Pow(s.pricing.InflationMean, shared.TrendYears(midCover, uw.startYear))

	total := 0.0
	sections := make([]shared.Money, len(s.pricing.Sections))
	for i := range sections {
		loss := s.pricing.ExpectedSectionLoss(i, sumInsured, excess, riskFactor, inflation, uw.siDrift)
		total += loss
		sections[i] = shared.FromDollars(loss / uw.lossRatio)
	}

	return Policy{
		ID:         id,
		FleetID:    fleetID,
		CoverStart: start,
		CoverEnd:   start.AddDays(364),
		SumInsured: shared.FromDollars(sumInsured),

		BaseSumInsured: shared.FromDollars(sumInsured).Dollars() / uw.siDrift,
		Excess:         shared.FromDollars(excess),
		RiskFactor:     riskFactor,
		Premium:        shared.FromDollars(total / uw.lossRatio),

		SectionPremiums: sections,
	}
}

func (s *BookSimulator) drawExcess(src shared.RandomSource) float64 {
	total := 0.0
	for _, c := range s.book.ExcessChoices {
		total += c.Weight
	}
	u := src.Uniform() * total
	for _, c := range s.book.ExcessChoices {
		u -= c.Weight
		if u < 0 {
			return c.Value
		}
	}
	return s.book.ExcessChoices[len(s.book.ExcessChoices)-1].Value
}
