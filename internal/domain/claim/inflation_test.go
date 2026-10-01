package claim_test

import (
	"math"
	"testing"
	"time"

	"github.com/le-marais/claimsgen/internal/domain/claim"
	"github.com/le-marais/claimsgen/internal/domain/lob"
	"github.com/le-marais/claimsgen/internal/domain/shared"
	"github.com/le-marais/claimsgen/internal/infrastructure/random"
)

// midYear is the anchor date of a non-leap year's annual factor.
func midYear(year int) shared.Date { return shared.NewDate(year, time.July, 2) }

func TestInflationIndexStartYearAnchorIsOne(t *testing.T) {
	idx := claim.NewInflationIndex(random.NewSource(1), lob.InflationParams{Mean: 1.05, Volatility: 0.02}, 1998, 5)
	if got := idx.For(midYear(1998)); got != 1.0 {
		t.Fatalf("start-year anchor = %v, want 1.0", got)
	}
}

func TestInflationIndexIdentityWhenMeanOneNoVol(t *testing.T) {
	idx := claim.NewInflationIndex(random.NewSource(1), lob.InflationParams{Mean: 1.0, Volatility: 0.0}, 1998, 10)
	for d := shared.NewDate(1998, time.January, 1); d.Year() < 2008; d = d.AddDays(17) {
		if got := idx.For(d); got != 1.0 {
			t.Fatalf("identity index For(%s) = %v, want 1.0", d, got)
		}
	}
}

// Without volatility the index is exactly Mean raised to the years since the
// middle of the start year, on every day of the window and beyond the
// anchors at either end.
func TestInflationIndexTrendsSmoothlyAtMeanWithoutVol(t *testing.T) {
	const mean = 1.04
	idx := claim.NewInflationIndex(random.NewSource(1), lob.InflationParams{Mean: mean, Volatility: 0.0}, 2001, 4)
	for d := shared.NewDate(2001, time.January, 1); d.Year() < 2005; d = d.AddDays(1) {
		want := math.Pow(mean, shared.TrendYears(d, 2001))
		if got := idx.For(d); math.Abs(got-want) > 1e-12 {
			t.Fatalf("For(%s) = %v, want %v", d, got, want)
		}
	}
}

// The index no longer steps at the year end (MR-10): 31 December and the
// next 1 January differ by one day's worth of trend, not a year's.
func TestInflationIndexHasNoYearEndStep(t *testing.T) {
	idx := claim.NewInflationIndex(random.NewSource(3), lob.InflationParams{Mean: 1.04, Volatility: 0.05}, 1998, 10)
	for y := 1998; y < 2007; y++ {
		before := idx.For(shared.NewDate(y, time.December, 31))
		after := idx.For(shared.NewDate(y+1, time.January, 1))
		if step := after/before - 1; math.Abs(step) > 0.001 {
			t.Fatalf("year-end %d step = %.4f, want under 0.1%%", y, step)
		}
	}
}

// The simulated annual factors are the anchors: each year's index at mid-year
// is the start-year index compounded by that year's draws, and the calendar
// year average stays close to it.
func TestInflationIndexAnchorsMidYear(t *testing.T) {
	p := lob.InflationParams{Mean: 1.04, Volatility: 0.01}
	idx := claim.NewInflationIndex(random.NewSource(5), p, 1998, 6)
	for y := 1998; y < 2004; y++ {
		anchor := idx.For(midYear(y))
		sum, n := 0.0, 0
		for d := shared.NewDate(y, time.January, 1); d.Year() == y; d = d.AddDays(1) {
			sum += idx.For(d)
			n++
		}
		if avg := sum / float64(n); math.Abs(avg/anchor-1) > 0.005 {
			t.Fatalf("year %d average %.5f strays from its anchor %.5f", y, avg, anchor)
		}
	}
}

func TestInflationIndexZeroValueIsIdentity(t *testing.T) {
	var idx claim.InflationIndex
	if got := idx.For(shared.NewDate(2003, time.May, 5)); got != 1.0 {
		t.Fatalf("zero-value index = %v, want 1.0 (identity)", got)
	}
}
