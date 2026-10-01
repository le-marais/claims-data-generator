package claim

import (
	"math"
	"testing"
	"time"

	"github.com/le-marais/claimsgen/internal/domain/lob"
	"github.com/le-marais/claimsgen/internal/domain/policy"
	"github.com/le-marais/claimsgen/internal/domain/shared"
	"github.com/le-marais/claimsgen/internal/infrastructure/random"
)

func approxf(a, b float64) bool { return a-b < 1e-9 && b-a < 1e-9 }

// MR-9: settlement time grows smoothly with size and with risk.
func TestCloseLagMeanScalesWithSizeAndRisk(t *testing.T) {
	cl := lob.CloseLagParams{Shape: 1, MeanDays: 300, SizeReference: 5000, SizeElasticity: 0.5}
	for _, c := range []struct{ size, want float64 }{{5000, 300}, {20000, 600}, {1250, 150}} {
		if m := closeLagMean(cl, c.size, 1); !approxf(m, c.want) {
			t.Errorf("size %v: mean %v, want %v", c.size, m, c.want)
		}
	}
	cl.RiskLoading = 0.5
	if m := closeLagMean(cl, 5000, 2); !approxf(m, 300*math.Sqrt(2)) {
		t.Errorf("risk-loaded mean %v, want %v", m, 300*math.Sqrt(2))
	}
	cl.SizeElasticity, cl.RiskLoading = 0, 0
	if m := closeLagMean(cl, 20000, 1); !approxf(m, 300) {
		t.Errorf("elasticity 0: mean %v, want the flat 300", m)
	}
}

// sizeStretchParams is a single-section class whose close lag doubles for
// every fourfold rise in claim size, with its median claim at twice the size
// reference in start-year dollars.
func sizeStretchParams() lob.ClaimParams {
	p := ownDamageOnly(1, 0.002, 1, lob.CloseLagParams{Shape: 1.2, MeanDays: 40, SizeReference: 10000, SizeElasticity: 0.5})
	p.Sections[0].ReportLag = lob.ReportLagParams{Median: 1, Sigma: 0.1}
	p.Reopening = lob.ReopeningParams{Probability: 0.9, EstimateFactor: 1, LagMedianDays: 30, LagSigma: 0.1}
	return p
}

// steepInflation compounds 30% a year, so by 2007 the index is about 10 and
// nominal claim costs sit far above their start-year size.
func steepInflation() InflationIndex {
	return NewInflationIndex(random.NewSource(1), lob.InflationParams{Mean: 1.3}, 1998, 10)
}

// MR-5: the close lag reads claim size in start-year dollars, so claims
// inflation does not slow settlement year on year.
func TestSizeStretchIgnoresClaimsInflation(t *testing.T) {
	var book []policy.Policy
	for i := 0; i < 8000; i++ {
		year := 1998 + 9*(i%2) // alternate 1998 and 2007 underwriting years
		start := shared.NewDate(year, time.January, 1).AddDays(i % 300)
		book = append(book, policy.Policy{
			ID: i + 1, CoverStart: start, CoverEnd: start.AddDays(364),
			SumInsured: shared.FromDollars(1e7), RiskFactor: 1,
		})
	}
	claims := NewClaimSimulator(sizeStretchParams()).
		WithInflation(steepInflation()).
		Simulate(random.NewSource(2), book)
	meanLag := map[int]float64{}
	count := map[int]float64{}
	for _, c := range claims {
		if y := c.OccurrenceDate.Year(); y == 1998 || y == 2007 {
			meanLag[y] += float64(shared.DaysBetween(c.ReportDate(), c.CloseDate()))
			count[y]++
		}
	}
	early, late := meanLag[1998]/count[1998], meanLag[2007]/count[2007]
	if ratio := late / early; ratio < 0.9 || ratio > 1.1 {
		t.Fatalf("mean close lag %.1f days in 2007 against %.1f in 1998 (ratio %.2f), want flat", late, early, ratio)
	}
}

// A reopen's second close lag reads the additional cost in start-year dollars
// too, through the same index.
func TestReopenSizeStretchIgnoresClaimsInflation(t *testing.T) {
	var claims []Claim
	for i := 0; i < 4000; i++ {
		occurred := shared.NewDate(2007, time.March, 1)
		claims = append(claims, Claim{
			ID:             i + 1,
			OccurrenceDate: occurred,
			// Nominally 100k, about 10k in start-year dollars: the size reference.
			Episodes: []Episode{{
				Open:     occurred,
				Close:    occurred.AddDays(30),
				Ultimate: shared.FromDollars(100000),
			}},
			RiskFactor: 1,
		})
	}
	secondLag := func(sim *ReopenSimulator) float64 {
		in := append([]Claim(nil), claims...)
		total, n := 0.0, 0.0
		for _, c := range sim.Apply(random.NewSource(3), in) {
			if c.Reopened() {
				total += float64(shared.DaysBetween(c.Episodes[1].Open, c.CloseDate()))
				n++
			}
		}
		return total / n
	}
	nominal := secondLag(NewReopenSimulator(sizeStretchParams()))
	deflated := secondLag(NewReopenSimulator(sizeStretchParams()).WithInflation(steepInflation()))
	if deflated > 50 || nominal < 100 {
		t.Fatalf("second close lag %.1f days deflated, %.1f nominal; want about 40 and 120", deflated, nominal)
	}
}
