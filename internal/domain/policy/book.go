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
}

// BookSimulator generates the policy book for a run.
type BookSimulator struct {
	book   lob.BookParams
	claims lob.ClaimParams
}

// NewBookSimulator builds a book simulator from the book and claim
// parameters; claim parameters drive expected-loss pricing.
func NewBookSimulator(book lob.BookParams, claims lob.ClaimParams) *BookSimulator {
	return &BookSimulator{book: book, claims: claims}
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
		inflation := math.Pow(s.claims.Inflation.Mean, float64(y))
		for i := 0; i < size; i++ {
			book = append(book, s.simulatePolicy(src.Split(fmt.Sprintf("policy-%d", id)), id, year, medianSI, inflation))
			id++
		}
	}
	return book
}

func (s *BookSimulator) simulatePolicy(src shared.RandomSource, id, year int, medianSI, inflation float64) Policy {
	yearStart := shared.NewDate(year, time.January, 1)
	daysInYear := shared.DaysBetween(yearStart, shared.NewDate(year+1, time.January, 1))
	start := yearStart.AddDays(int(src.Uniform() * float64(daysInYear)))

	sumInsured := src.LogNormal(math.Log(medianSI), s.book.Spread)

	// Gamma with mean 1 and standard deviation equal to the spread knob.
	spread2 := s.book.Spread * s.book.Spread
	riskFactor := src.Gamma(1/spread2, spread2)

	excess := s.drawExcess(src)
	premium := s.claims.ExpectedPolicyLoss(sumInsured, excess, riskFactor, inflation) / s.book.TargetLossRatio

	return Policy{
		ID:         id,
		CoverStart: start,
		CoverEnd:   start.AddDays(364),
		SumInsured: shared.FromDollars(sumInsured),
		Excess:     shared.FromDollars(excess),
		RiskFactor: riskFactor,
		Premium:    shared.FromDollars(premium),
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
