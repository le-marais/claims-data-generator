package claim_test

import (
	"math"
	"testing"

	"github.com/le-marais/claimsgen/internal/domain/claim"
	"github.com/le-marais/claimsgen/internal/domain/lob"
	"github.com/le-marais/claimsgen/internal/infrastructure/random"
)

func TestInflationIndexStartYearIsOne(t *testing.T) {
	idx := claim.NewInflationIndex(random.NewSource(1), lob.InflationParams{Mean: 1.05, Volatility: 0.02}, 1998, 5)
	if got := idx.For(1998); got != 1.0 {
		t.Fatalf("start-year index = %v, want 1.0", got)
	}
}

func TestInflationIndexIdentityWhenMeanOneNoVol(t *testing.T) {
	idx := claim.NewInflationIndex(random.NewSource(1), lob.InflationParams{Mean: 1.0, Volatility: 0.0}, 1998, 10)
	for y := 1998; y < 2008; y++ {
		if got := idx.For(y); got != 1.0 {
			t.Fatalf("identity index For(%d) = %v, want 1.0", y, got)
		}
	}
}

func TestInflationIndexCompoundsAtMeanWithoutVol(t *testing.T) {
	idx := claim.NewInflationIndex(random.NewSource(1), lob.InflationParams{Mean: 1.04, Volatility: 0.0}, 2000, 4)
	for i, want := range []float64{1.0, 1.04, 1.04 * 1.04, 1.04 * 1.04 * 1.04} {
		if got := idx.For(2000 + i); math.Abs(got-want) > 1e-9 {
			t.Fatalf("For(%d) = %v, want %v", 2000+i, got, want)
		}
	}
}

func TestInflationIndexZeroValueIsIdentity(t *testing.T) {
	var idx claim.InflationIndex
	if got := idx.For(2003); got != 1.0 {
		t.Fatalf("zero-value index = %v, want 1.0 (identity)", got)
	}
}

func TestInflationIndexClampsOutOfRange(t *testing.T) {
	idx := claim.NewInflationIndex(random.NewSource(1), lob.InflationParams{Mean: 1.04, Volatility: 0.0}, 2000, 3)
	// Years before the window read as the start-year index (1.0); years after
	// read as the last simulated index.
	if got := idx.For(1990); got != 1.0 {
		t.Fatalf("For(before window) = %v, want 1.0", got)
	}
	if got, want := idx.For(2050), idx.For(2002); got != want {
		t.Fatalf("For(after window) = %v, want last index %v", got, want)
	}
}
