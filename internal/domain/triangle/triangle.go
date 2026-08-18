// Package triangle holds reserving's development triangle concepts:
// aggregation of generated data into paid and incurred triangles, and the
// realism comparison of those triangles against reference data.
package triangle

import (
	"math"
	"time"

	"github.com/le-marais/claimsgen/internal/domain/claim"
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

// PaidTriangle aggregates gross payments into a cumulative annual triangle by
// occurrence year. Development years beyond the last column are accumulated
// into it.
//
// Deprecated: a temporary shim over the monthly grid, removed once
// application.Aggregates owns the aggregation. Call BuildMonthlyGrid and
// AnnualTriangles instead.
func PaidTriangle(claims []claim.Claim, txs []transaction.Transaction, startYear, origins, devs int) Triangle {
	return annualShim(claims, txs, startYear, origins, devs).Paid
}

// NetPaidTriangle aggregates payments net of recoveries: salvage and
// subrogation rows subtract, so cumulative net paid can develop downward at
// late ages. Schedule P paid losses are net of salvage and subrogation, so
// this is the triangle the realism comparison uses.
//
// Deprecated: a temporary shim over the monthly grid, as PaidTriangle.
func NetPaidTriangle(claims []claim.Claim, txs []transaction.Transaction, startYear, origins, devs int) Triangle {
	return annualShim(claims, txs, startYear, origins, devs).NetPaid
}

// IncurredTriangle aggregates gross case plus net paid into a cumulative
// annual triangle by occurrence year: estimate movements and payments add,
// recoveries subtract.
//
// Deprecated: a temporary shim over the monthly grid, as PaidTriangle.
func IncurredTriangle(claims []claim.Claim, txs []transaction.Transaction, startYear, origins, devs int) Triangle {
	return annualShim(claims, txs, startYear, origins, devs).Incurred
}

// annualShim builds an accident-month grid over the same window and coarsens
// it back to years. The error cannot fire for these arguments - the basis is
// a constant and origins is at least one wherever the callers use it - so a
// failure yields empty triangles rather than a panic.
func annualShim(claims []claim.Claim, txs []transaction.Transaction, startYear, origins, devs int) AnnualSet {
	g, err := BuildMonthlyGrid(nil, claims, txs, shared.NewMonth(startYear, time.January), origins*12, AccidentMonth)
	if err != nil {
		return AnnualSet{}
	}
	return g.AnnualTriangles(devs)
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
