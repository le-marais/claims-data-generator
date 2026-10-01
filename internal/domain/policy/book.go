// Package policy simulates the policy book: the exposure that claims arise
// from (step 1 of the simulation).
package policy

import (
	"fmt"
	"math"
	"time"

	"github.com/le-marais/claimsgen/internal/domain/lob"
	"github.com/le-marais/claimsgen/internal/domain/shared"
)

// Policy is one 12-month motor policy covering one vehicle.
type Policy struct {
	ID         int
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
	// ThirdPartyPremium is the third-party liability section of Premium,
	// priced the same way on that section's expected loss. The realism gate
	// scores the liability claims against it, because the Schedule P
	// reference is a liability line. Never written to CSV.
	ThirdPartyPremium shared.Money
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
// Each later year's size is the previous year's size times the growth factor
// times a mean-1 lognormal noise, so the book trends upward but can shrink in
// individual years. Each year's policies are priced to that year's target
// loss ratio: the pricing target times mean-1 lognormal noise of sigma
// AdequacyVolatility, drawn on its own sub-stream so the knob never moves any
// other draw.
func (s *BookSimulator) Simulate(src shared.RandomSource, startYear, years, initialSize int) []Policy {
	sizeSrc := src.Split("book-size")
	adequacySrc := src.Split("pricing-adequacy")
	var book []Policy
	size := warmUpSize(s.book, initialSize)
	id := 1
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
		year := startYear + y
		lossRatio := s.pricing.TargetLossRatio * shared.MeanOneLogNormal(adequacySrc, s.pricing.AdequacyVolatility)
		medianSI := s.book.SumInsuredMedian * math.Pow(s.book.SumInsuredInflation, float64(y))
		siDrift := math.Pow(s.book.SumInsuredInflation, float64(y))
		for i := 0; i < size; i++ {
			book = append(book, s.simulatePolicy(src.Split(fmt.Sprintf("policy-%d", id)), id, startYear, year, lossRatio, medianSI, siDrift))
			id++
		}
	}
	return book
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

// ProjectedSize is the number of policies a run would write if every year's
// size noise came out at its mean of 1: the warm-up year plus the initial size
// compounded by the growth factor, summed over the years. It mirrors the
// growth rule in Simulate and lets a caller size a run before paying for it.
// The result is a float64 because a large growth factor over many years
// overflows an int long before the run would ever finish.
func ProjectedSize(book lob.BookParams, years, initialSize int) float64 {
	total, size := float64(warmUpSize(book, initialSize)), float64(initialSize)
	for y := 0; y < years; y++ {
		if y > 0 {
			size = math.Max(size*book.GrowthFactor, 1) // Simulate floors each year at 1
		}
		total += size
	}
	return total
}

// simulatePolicy draws one policy and prices it to lossRatio, the target loss
// ratio of its underwriting year.
func (s *BookSimulator) simulatePolicy(src shared.RandomSource, id, startYear, year int, lossRatio, medianSI, siDrift float64) Policy {
	yearStart := shared.NewDate(year, time.January, 1)
	daysInYear := shared.DaysBetween(yearStart, shared.NewDate(year+1, time.January, 1))
	start := yearStart.AddDays(int(src.Uniform() * float64(daysInYear)))

	// Trend the assumed loss cost to the middle of the cover, where the
	// average claim occurs, on the same time axis as the claims inflation
	// index (MR-3).
	midCover := start.AddDays(182)
	inflation := math.Pow(s.pricing.InflationMean, shared.TrendYears(midCover, startYear))

	sumInsured := src.LogNormal(math.Log(medianSI), s.book.Spread)

	// Gamma with mean 1 and standard deviation equal to the spread knob.
	spread2 := s.book.Spread * s.book.Spread
	riskFactor := src.Gamma(1/spread2, spread2)

	excess := s.drawExcess(src)
	ownDamageLoss, thirdPartyLoss := s.pricing.ExpectedSectionLoss(sumInsured, excess, riskFactor, inflation, siDrift)
	premium := (ownDamageLoss + thirdPartyLoss) / lossRatio

	return Policy{
		ID:         id,
		CoverStart: start,
		CoverEnd:   start.AddDays(364),
		SumInsured: shared.FromDollars(sumInsured),

		BaseSumInsured: shared.FromDollars(sumInsured).Dollars() / siDrift,
		Excess:         shared.FromDollars(excess),
		RiskFactor:     riskFactor,
		Premium:        shared.FromDollars(premium),

		ThirdPartyPremium: shared.FromDollars(thirdPartyLoss / lossRatio),
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
