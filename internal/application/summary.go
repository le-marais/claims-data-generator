package application

import (
	"github.com/le-marais/claimsgen/internal/domain/transaction"
	"github.com/le-marais/claimsgen/internal/domain/triangle"
)

// YearSummary aggregates one calendar year of the run: policies by cover
// start year, claims and paid amounts by occurrence year. Every claim runs
// to closure, so Paid is the ultimate and equals final incurred.
type YearSummary struct {
	Year          int
	Policies      int
	Claims        int
	NilClaims     int
	EarnedPremium float64
	Paid          float64
}

// LossRatio is Paid over EarnedPremium; ok is false when there is no premium.
func (s YearSummary) LossRatio() (float64, bool) {
	if s.EarnedPremium <= 0 {
		return 0, false
	}
	return s.Paid / s.EarnedPremium, true
}

// SummaryReport is the per-year table plus a grand total row (Total.Year is
// zero and unused).
type SummaryReport struct {
	Years []YearSummary
	Total YearSummary
}

// Summarize aggregates the dataset per calendar year of the run window.
func Summarize(ds Dataset, startYear, years int) SummaryReport {
	rows := make([]YearSummary, years)
	for i := range rows {
		rows[i].Year = startYear + i
	}
	for _, p := range ds.Policies {
		if i := p.CoverStart.Year() - startYear; i >= 0 && i < years {
			rows[i].Policies++
		}
	}
	for i, ep := range triangle.EarnedPremiumByYear(ds.Policies, startYear, years) {
		rows[i].EarnedPremium = ep
	}
	occurrenceYear := make(map[int]int, len(ds.Claims))
	for _, c := range ds.Claims {
		occurrenceYear[c.ID] = c.OccurrenceDate.Year()
		if i := c.OccurrenceDate.Year() - startYear; i >= 0 && i < years {
			rows[i].Claims++
			if c.Nil {
				rows[i].NilClaims++
			}
		}
	}
	for _, tx := range ds.Transactions {
		if tx.Type != transaction.Payment {
			continue
		}
		if i := occurrenceYear[tx.ClaimID] - startYear; i >= 0 && i < years {
			rows[i].Paid += tx.Amount.Dollars()
		}
	}
	var total YearSummary
	for _, r := range rows {
		total.Policies += r.Policies
		total.Claims += r.Claims
		total.NilClaims += r.NilClaims
		total.EarnedPremium += r.EarnedPremium
		total.Paid += r.Paid
	}
	return SummaryReport{Years: rows, Total: total}
}
