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
	params lob.BookParams
}

// NewBookSimulator builds a book simulator from the book parameters.
func NewBookSimulator(p lob.BookParams) *BookSimulator {
	return &BookSimulator{params: p}
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
			noise := shared.MeanOneLogNormal(sizeSrc, s.params.SizeVolatility)
			size = int(math.Round(float64(size) * s.params.GrowthFactor * noise))
			if size < 1 {
				size = 1
			}
		}
		year := startYear + y
		medianSI := s.params.SumInsuredMedian * math.Pow(s.params.SumInsuredInflation, float64(y))
		for i := 0; i < size; i++ {
			book = append(book, s.simulatePolicy(src.Split(fmt.Sprintf("policy-%d", id)), id, year, medianSI))
			id++
		}
	}
	return book
}

func (s *BookSimulator) simulatePolicy(src shared.RandomSource, id, year int, medianSI float64) Policy {
	yearStart := shared.NewDate(year, time.January, 1)
	daysInYear := shared.DaysBetween(yearStart, shared.NewDate(year+1, time.January, 1))
	start := yearStart.AddDays(int(src.Uniform() * float64(daysInYear)))

	sumInsured := src.LogNormal(math.Log(medianSI), s.params.Spread)

	// Gamma with mean 1 and standard deviation equal to the spread knob.
	spread2 := s.params.Spread * s.params.Spread
	riskFactor := src.Gamma(1/spread2, spread2)

	return Policy{
		ID:         id,
		CoverStart: start,
		CoverEnd:   start.AddDays(364),
		SumInsured: shared.FromDollars(sumInsured),
		Excess:     shared.FromDollars(s.drawExcess(src)),
		RiskFactor: riskFactor,
		Premium:    shared.FromDollars(sumInsured * s.params.PremiumRateFactor * riskFactor),
	}
}

func (s *BookSimulator) drawExcess(src shared.RandomSource) float64 {
	total := 0.0
	for _, c := range s.params.ExcessChoices {
		total += c.Weight
	}
	u := src.Uniform() * total
	for _, c := range s.params.ExcessChoices {
		u -= c.Weight
		if u < 0 {
			return c.Value
		}
	}
	return s.params.ExcessChoices[len(s.params.ExcessChoices)-1].Value
}
