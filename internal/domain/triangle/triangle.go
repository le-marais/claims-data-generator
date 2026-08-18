// Package triangle holds reserving's development triangle concepts:
// aggregation of generated data into paid and incurred triangles, and the
// realism comparison of those triangles against reference data.
package triangle

import "math"

// Triangle is a cumulative development triangle: Cells[origin][dev] is the
// cumulative amount for an origin year at the end of a development year.
// Rows may be ragged.
type Triangle struct {
	StartYear int
	Cells     [][]float64
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
