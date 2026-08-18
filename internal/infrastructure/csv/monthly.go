package csv

import (
	"fmt"
	"math"
	"os"
	"strconv"

	"github.com/le-marais/claimsgen/internal/application"
)

// WriteAggregates writes triangles.csv and exposure.csv into dir, creating it
// if needed.
//
// triangles.csv holds the incremental monthly grid, one row per cell, ordered
// by origin month then development month. Development is 1-based and runs to
// full runoff, so a valuation-date view is a filter on
// origin_month + dev_month - 1. Every cell is written, zeros included, so the
// file states the grid's exact extent.
//
// exposure.csv holds one row per origin month on the same origin basis.
func WriteAggregates(dir string, ag application.Aggregates) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating output directory: %w", err)
	}
	g := ag.Grid
	rows := g.Origins() * g.DevPeriods
	if err := writeFile(dir, "triangles.csv",
		"origin_month,dev_month,paid,paid_net,incurred,reported_count",
		rows, func(i int) string {
			o, d := i/g.DevPeriods, i%g.DevPeriods
			return fmt.Sprintf("%s,%d,%s,%s,%s,%d",
				g.StartMonth.Add(o), d+1,
				formatAmount(g.Paid[o][d]),
				formatAmount(g.PaidNet[o][d]),
				formatAmount(g.Incurred[o][d]),
				g.Reported[o][d])
		}); err != nil {
		return err
	}
	return writeFile(dir, "exposure.csv",
		"origin_month,premium,exposure_units,policies",
		len(ag.Exposure), func(i int) string {
			e := ag.Exposure[i]
			return fmt.Sprintf("%s,%s,%s,%d",
				e.Month, formatAmount(e.Premium), formatUnits(e.ExposureUnits), e.Policies)
		})
}

// formatAmount renders a money amount at fixed precision. A value that rounds
// to zero is written as a positive zero, so a cell whose movements cancel
// reads "0.00" rather than "-0.00".
func formatAmount(v float64) string {
	if math.Round(v*100) == 0 {
		return "0.00"
	}
	return strconv.FormatFloat(v, 'f', 2, 64)
}

// formatUnits renders exposure units at fixed precision, with the same
// negative-zero guard.
func formatUnits(v float64) string {
	if math.Round(v*1e6) == 0 {
		return "0.000000"
	}
	return strconv.FormatFloat(v, 'f', 6, 64)
}
