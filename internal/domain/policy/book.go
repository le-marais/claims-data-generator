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
// years. Each year's size is the previous year's size times the growth
// factor times a mean-1 lognormal noise, so the book trends upward but can
// shrink in individual years.
func (s *BookSimulator) Simulate(src shared.RandomSource, startYear, years, initialSize int) []Policy {
	sizeSrc := src.Split("book-size")
	var book []Policy
	size := initialSize
	id := 1
	for y := 0; y < years; y++ {
		if y > 0 {
			noise := shared.MeanOneLogNormal(sizeSrc, s.book.SizeVolatility)
			size = int(math.Round(float64(size) * s.book.GrowthFactor * noise))
			if size < 1 {
				size = 1
			}
		}
		year := startYear + y
		medianSI := s.book.SumInsuredMedian * math.Pow(s.book.SumInsuredInflation, float64(y))
		inflation := math.Pow(s.pricing.InflationMean, float64(y))
		siDrift := math.Pow(s.book.SumInsuredInflation, float64(y))
		for i := 0; i < size; i++ {
			book = append(book, s.simulatePolicy(src.Split(fmt.Sprintf("policy-%d", id)), id, year, medianSI, inflation, siDrift))
			id++
		}
	}
	return book
}

// ProjectedSize is the number of policies a run would write if every year's
// size noise came out at its mean of 1: the initial size compounded by the
// growth factor, summed over the years. It mirrors the growth rule in
// Simulate and lets a caller size a run before paying for it. The result is a
// float64 because a large growth factor over many years overflows an int
// long before the run would ever finish.
func ProjectedSize(book lob.BookParams, years, initialSize int) float64 {
	total, size := 0.0, float64(initialSize)
	for y := 0; y < years; y++ {
		if y > 0 {
			size = math.Max(size*book.GrowthFactor, 1) // Simulate floors each year at 1
		}
		total += size
	}
	return total
}

func (s *BookSimulator) simulatePolicy(src shared.RandomSource, id, year int, medianSI, inflation, siDrift float64) Policy {
	yearStart := shared.NewDate(year, time.January, 1)
	daysInYear := shared.DaysBetween(yearStart, shared.NewDate(year+1, time.January, 1))
	start := yearStart.AddDays(int(src.Uniform() * float64(daysInYear)))

	sumInsured := src.LogNormal(math.Log(medianSI), s.book.Spread)

	// Gamma with mean 1 and standard deviation equal to the spread knob.
	spread2 := s.book.Spread * s.book.Spread
	riskFactor := src.Gamma(1/spread2, spread2)

	excess := s.drawExcess(src)
	ownDamageLoss, thirdPartyLoss := s.pricing.ExpectedSectionLoss(sumInsured, excess, riskFactor, inflation, siDrift)
	premium := (ownDamageLoss + thirdPartyLoss) / s.pricing.TargetLossRatio

	return Policy{
		ID:         id,
		CoverStart: start,
		CoverEnd:   start.AddDays(364),
		SumInsured: shared.FromDollars(sumInsured),
		Excess:     shared.FromDollars(excess),
		RiskFactor: riskFactor,
		Premium:    shared.FromDollars(premium),

		ThirdPartyPremium: shared.FromDollars(thirdPartyLoss / s.pricing.TargetLossRatio),
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
