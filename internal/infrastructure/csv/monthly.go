package csv

import (
	"fmt"
	"math"
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
	open, err := dirOpener(dir)
	if err != nil {
		return err
	}
	return writeAggregates(open, ag)
}

func writeAggregates(open opener, ag application.Aggregates) error {
	g := ag.Grid
	rows := g.Origins() * g.DevPeriods
	if err := writeFile(open, "triangles.csv",
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
	return writeFile(open, "exposure.csv",
		"origin_month,premium,exposure_units,policies",
		len(ag.Exposure), func(i int) string {
			e := ag.Exposure[i]
			return fmt.Sprintf("%s,%s,%s,%d",
				e.Month, formatAmount(e.Premium), formatUnits(e.ExposureUnits), e.Policies)
		})
}

// format rounds v to prec decimal places with a single rounding rule and
// renders the already-rounded value. Rounding once, with math.Round (half
// away from zero), and then formatting the result avoids FormatFloat
// rounding the raw value a second time with its own rule (half to even): at
// an exact tie the two rules can disagree, which previously let a value the
// zero-guard was meant to catch reach the tie-breaking rule unrounded and
// print as a signed zero. A value that rounds to zero is normalised to a
// positive zero, so a cell whose movements cancel reads "0.00" rather than
// "-0.00".
func format(v float64, prec int, scale float64) string {
	r := math.Round(v*scale) / scale
	if r == 0 {
		r = 0
	}
	return strconv.FormatFloat(r, 'f', prec, 64)
}

// formatAmount renders a money amount at fixed precision.
func formatAmount(v float64) string { return format(v, 2, 100) }

// formatUnits renders exposure units at fixed precision.
func formatUnits(v float64) string { return format(v, 6, 1e6) }
