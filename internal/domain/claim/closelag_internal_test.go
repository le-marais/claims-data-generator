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

func TestCloseLagRegimeSelectsByComponent(t *testing.T) {
	cl := lob.CloseLagParams{
		Shape: 1.2, MeanDays: 120, SizeThreshold: 20000, SizeMultiplier: 6,
		RiskLoading: 0, ThirdPartyShape: 1.0, ThirdPartyMeanDays: 1200,
	}
	// Own damage, small: base params, no stretch.
	if s, m := closeLagRegime(cl, 5000, 1, true); !approxf(s, 1.2) || !approxf(m, 120) {
		t.Errorf("own-damage small = (%v, %v), want (1.2, 120)", s, m)
	}
	// Own damage, large: base shape, stretched mean.
	if s, m := closeLagRegime(cl, 50000, 1, true); !approxf(s, 1.2) || !approxf(m, 720) {
		t.Errorf("own-damage large = (%v, %v), want (1.2, 720)", s, m)
	}
	// Third party: long-tail params, no size stretch even when large.
	if s, m := closeLagRegime(cl, 50000, 1, false); !approxf(s, 1.0) || !approxf(m, 1200) {
		t.Errorf("third-party = (%v, %v), want (1.0, 1200)", s, m)
	}
	// Third party with risk loading: mean stretches by riskFactor^RiskLoading.
	clRisk := lob.CloseLagParams{
		Shape: 1.2, MeanDays: 120, SizeThreshold: 20000, SizeMultiplier: 6,
		RiskLoading: 0.5, ThirdPartyShape: 1.0, ThirdPartyMeanDays: 1200,
	}
	if s, m := closeLagRegime(clRisk, 50000, 2, false); !approxf(s, 1.0) || !approxf(m, 1200*math.Sqrt(2)) {
		t.Errorf("third-party risk-loaded = (%v, %v), want (1.0, %v)", s, m, 1200*math.Sqrt(2))
	}
}

// sizeStretchParams is an own-damage-only class whose median claim sits at
// half the size threshold before inflation, so about a quarter of claims are
// stretched in the start year.
func sizeStretchParams() lob.ClaimParams {
	return lob.ClaimParams{
		BaseFrequency:   1,
		ReportLagMedian: 1,
		ReportLagSigma:  0.1,
		Severity: lob.SeverityParams{
			ThirdPartyWeight: 0, OwnDamageMedianFraction: 0.002, OwnDamageSigma: 1,
		},
		CloseLag: lob.CloseLagParams{
			Shape: 1.2, MeanDays: 40, SizeThreshold: 40000, SizeMultiplier: 3,
			ThirdPartyShape: 1, ThirdPartyMeanDays: 400,
		},
		Reopening: lob.ReopeningParams{Probability: 0.9, EstimateFactor: 1, LagMedianDays: 30, LagSigma: 0.1},
	}
}

// steepInflation compounds 30% a year, so by 2007 the index is about 10 and
// nominal claim costs sit far above the threshold.
func steepInflation() InflationIndex {
	return NewInflationIndex(random.NewSource(1), lob.InflationParams{Mean: 1.3}, 1998, 10)
}

// MR-5: the size stretch compares start-year dollars with the threshold, so
// claims inflation does not slow own-damage settlement year on year.
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
			// Nominally 100k, about 10k in start-year dollars: under the threshold.
			Episodes: []Episode{{
				Open:     occurred,
				Close:    occurred.AddDays(30),
				Ultimate: shared.FromDollars(100000),
			}},
			RiskFactor: 1,
			OwnDamage:  true,
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

// MR-9: third-party settlement time grows smoothly with size.
func TestThirdPartyCloseLagScalesWithSize(t *testing.T) {
	cl := lob.CloseLagParams{
		Shape: 1.2, MeanDays: 40, SizeThreshold: 20000, SizeMultiplier: 3,
		ThirdPartyShape: 1, ThirdPartyMeanDays: 300,
		ThirdPartySizeElasticity: 0.5, ThirdPartySizeReference: 5000,
	}
	for _, c := range []struct{ size, want float64 }{{5000, 300}, {20000, 600}, {1250, 150}} {
		if _, m := closeLagRegime(cl, c.size, 1, false); !approxf(m, c.want) {
			t.Errorf("size %v: mean %v, want %v", c.size, m, c.want)
		}
	}
	cl.ThirdPartySizeElasticity = 0
	if _, m := closeLagRegime(cl, 20000, 1, false); !approxf(m, 300) {
		t.Errorf("elasticity 0: mean %v, want the flat 300", m)
	}
}
