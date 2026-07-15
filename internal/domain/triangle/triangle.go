// Package triangle holds reserving's development triangle concepts:
// aggregation of generated data into paid and incurred triangles, and the
// realism comparison of those triangles against reference data.
package triangle

import (
	"math"
	"time"

	"github.com/le-marais/claimsgen/internal/domain/claim"
	"github.com/le-marais/claimsgen/internal/domain/policy"
	"github.com/le-marais/claimsgen/internal/domain/shared"
	"github.com/le-marais/claimsgen/internal/domain/transaction"
)

// Triangle is a cumulative development triangle: Cells[origin][dev] is the
// cumulative amount for an origin year at the end of a development year.
// Rows may be ragged.
type Triangle struct {
	StartYear int
	Cells     [][]float64
}

// PaidTriangle aggregates payments into a cumulative triangle by occurrence
// year. Development years beyond the last column are accumulated into it.
func PaidTriangle(claims []claim.Claim, txs []transaction.Transaction, startYear, origins, devs int) Triangle {
	return aggregate(claims, txs, startYear, origins, devs, func(t transaction.Transaction) bool {
		return t.Type == transaction.Payment
	})
}

// IncurredTriangle aggregates paid plus outstanding case estimates into a
// cumulative triangle by occurrence year.
func IncurredTriangle(claims []claim.Claim, txs []transaction.Transaction, startYear, origins, devs int) Triangle {
	return aggregate(claims, txs, startYear, origins, devs, func(t transaction.Transaction) bool {
		return true // payments and estimate movements both move incurred
	})
}

func aggregate(claims []claim.Claim, txs []transaction.Transaction, startYear, origins, devs int, include func(transaction.Transaction) bool) Triangle {
	occurrenceYear := make(map[int]int, len(claims))
	for _, c := range claims {
		occurrenceYear[c.ID] = c.OccurrenceDate.Year()
	}
	incremental := make([][]float64, origins)
	for i := range incremental {
		incremental[i] = make([]float64, devs)
	}
	for _, tx := range txs {
		if !include(tx) {
			continue
		}
		occ := occurrenceYear[tx.ClaimID]
		origin := occ - startYear
		if origin < 0 || origin >= origins {
			continue
		}
		dev := tx.Date.Year() - occ
		if dev < 0 {
			dev = 0
		}
		if dev >= devs {
			dev = devs - 1
		}
		incremental[origin][dev] += tx.Amount.Dollars()
	}
	for _, row := range incremental {
		for d := 1; d < len(row); d++ {
			row[d] += row[d-1]
		}
	}
	return Triangle{StartYear: startYear, Cells: incremental}
}

// EarnedPremiumByYear spreads each policy's premium over its cover period
// and sums the portion earned in each calendar year of the window.
func EarnedPremiumByYear(policies []policy.Policy, startYear, years int) []float64 {
	earned := make([]float64, years)
	for _, p := range policies {
		termDays := shared.DaysBetween(p.CoverStart, p.CoverEnd) + 1
		if termDays <= 0 {
			continue
		}
		perDay := p.Premium.Dollars() / float64(termDays)
		for y := 0; y < years; y++ {
			overlap := overlapDays(p.CoverStart, p.CoverEnd, startYear+y)
			earned[y] += perDay * float64(overlap)
		}
	}
	return earned
}

// overlapDays counts the days of [start, end] (inclusive) falling in year.
func overlapDays(start, end shared.Date, year int) int {
	yearStart := shared.NewDate(year, time.January, 1)
	yearEnd := shared.NewDate(year, time.December, 31)
	if start.Before(yearStart) {
		start = yearStart
	}
	if end.After(yearEnd) {
		end = yearEnd
	}
	days := shared.DaysBetween(start, end) + 1
	if days < 0 {
		return 0
	}
	return days
}

// ATAFactors returns volume-weighted age-to-age development factors:
// factor[j] develops cumulative dev j to dev j+1 across all origins that
// have both. Ages with no usable data are NaN.
func (t Triangle) ATAFactors() []float64 {
	maxLen := 0
	for _, row := range t.Cells {
		if len(row) > maxLen {
			maxLen = len(row)
		}
	}
	if maxLen < 2 {
		return nil
	}
	factors := make([]float64, maxLen-1)
	for age := range factors {
		num, den := 0.0, 0.0
		for _, row := range t.Cells {
			if len(row) > age+1 {
				num += row[age+1]
				den += row[age]
			}
		}
		if den != 0 {
			factors[age] = num / den
		} else {
			factors[age] = math.NaN()
		}
	}
	return factors
}

// latestDiagonal returns the last available cumulative value per origin.
func (t Triangle) latestDiagonal() []float64 {
	latest := make([]float64, 0, len(t.Cells))
	for _, row := range t.Cells {
		if len(row) > 0 {
			latest = append(latest, row[len(row)-1])
		}
	}
	return latest
}
