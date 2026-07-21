package application_test

import (
	"testing"

	"github.com/le-marais/claimsgen/internal/application"
	"github.com/le-marais/claimsgen/internal/infrastructure/random"
)

// meanOwnDamageBySeverityYear returns mean own-damage InitialEstimate for the
// first and last accident year of the run.
func meanOwnDamageByYear(ds application.Dataset, firstYear, lastYear int) (first, last float64) {
	var fs, ls, fn, ln float64
	for _, c := range ds.Claims {
		if !c.OwnDamage {
			continue
		}
		switch c.OccurrenceDate.Year() {
		case firstYear:
			fs += c.InitialEstimate.Dollars()
			fn++
		case lastYear:
			ls += c.InitialEstimate.Dollars()
			ln++
		}
	}
	if fn > 0 {
		first = fs / fn
	}
	if ln > 0 {
		last = ls / ln
	}
	return first, last
}

// TestOwnDamageTrendIsClaimsIndexNotProduct proves SL-4: with the claims index
// held at 1.0, own-damage severity does NOT trend even though the sum insured
// drifts at 3%/yr. Before the rebase, severity trended at ~sum_insured_inflation.
func TestOwnDamageTrendIsClaimsIndexNotProduct(t *testing.T) {
	req := request(t)
	req.StartYear = 1998
	req.Years = 8
	req.InitialBookSize = 20000
	req.LOB.Claims.Inflation.Mean = 1.0 // claims index off
	req.LOB.Claims.Inflation.Volatility = 0.0
	req.LOB.Book.SumInsuredInflation = 1.10 // strong SI drift
	ds, err := application.GenerateDataset(random.NewSource(1), req)
	if err != nil {
		t.Fatal(err)
	}
	first, last := meanOwnDamageByYear(ds, 1998, 2005)
	if first == 0 || last == 0 {
		t.Fatal("missing own-damage claims in a boundary year")
	}
	ratio := last / first
	// Rebased: ~flat. Old double-count would be ~1.10^7 ≈ 1.95.
	if ratio < 0.90 || ratio > 1.15 {
		t.Fatalf("own-damage severity trended with SI drift: ratio %.3f, want ~1.0", ratio)
	}
}
