package triangle

import (
	"time"

	"github.com/le-marais/claimsgen/internal/domain/shared"
)

// PeriodKind is the grain of a triangle's origin and development axes.
type PeriodKind int

const (
	Monthly PeriodKind = iota
	Quarterly
	Annual
)

// epoch is an arbitrary fixed month, so a monthly period number is a stable
// integer whose differences are month counts.
var epoch = shared.NewMonth(0, time.January)

// index returns the absolute period number the month falls in, so that the
// difference of two indices is a whole number of periods.
func (k PeriodKind) index(m shared.Month) int {
	switch k {
	case Quarterly:
		return m.Year()*4 + m.Quarter() - 1
	case Annual:
		return m.Year()
	default:
		return shared.MonthsBetween(epoch, m)
	}
}

// IncrementalSet is a set of incremental triangles sharing one origin and
// development grain. Row p is origin period p counted from StartMonth's
// period, and slice index d holds development period d+1.
type IncrementalSet struct {
	Basis      OriginBasis
	Kind       PeriodKind
	StartMonth shared.Month
	Paid       [][]float64
	PaidNet    [][]float64
	Incurred   [][]float64
	Reported   [][]int
}

// Coarsen aggregates the incremental monthly grid onto a coarser grain. When
// devPeriods is positive every origin row is exactly devPeriods wide, and
// development beyond it is folded into the last period when foldTail is set
// and dropped otherwise. When devPeriods is zero rows take the grain's
// natural extent.
//
// Both axes are keyed on the calendar period the month falls in, not on whole
// multiples of the monthly development period. An accident in March 1998 paid
// in January 1999 is development year 2, because the payment falls in the next
// calendar year - which is what the annual triangles have always measured.
//
// Rows are zero-padded rather than left ragged when devPeriods is given. The
// annual triangles allocate a full rectangle and ATAFactors counts an origin
// at an age only when its row reaches that far, so a ragged row would drop
// short-tail origins out of the late-age factors and move them.
func (g MonthlyGrid) Coarsen(kind PeriodKind, devPeriods int, foldTail bool) IncrementalSet {
	set := IncrementalSet{Basis: g.Basis, Kind: kind, StartMonth: g.StartMonth}
	origins := g.Origins()
	if origins == 0 {
		return set
	}
	startIndex := kind.index(g.StartMonth)
	rows := kind.index(g.StartMonth.Add(origins-1)) - startIndex + 1

	// visit walks every monthly cell once, handing the coarse row and 1-based
	// coarse development period along with the monthly indices. Sizing and
	// filling share the walk so they cannot disagree.
	visit := func(f func(row, dev, origin, d int)) {
		for o := 0; o < origins; o++ {
			originMonth := g.StartMonth.Add(o)
			row := kind.index(originMonth) - startIndex
			originIndex := kind.index(originMonth)
			for d := 0; d < g.DevPeriods; d++ {
				dev := kind.index(originMonth.Add(d)) - originIndex + 1
				if devPeriods > 0 && dev > devPeriods {
					if !foldTail {
						continue
					}
					dev = devPeriods
				}
				f(row, dev, o, d)
			}
		}
	}

	widths := make([]int, rows)
	if devPeriods > 0 {
		for i := range widths {
			widths[i] = devPeriods
		}
	} else {
		visit(func(row, dev, _, _ int) {
			if dev > widths[row] {
				widths[row] = dev
			}
		})
	}
	set.Paid = raggedFloat(widths)
	set.PaidNet = raggedFloat(widths)
	set.Incurred = raggedFloat(widths)
	set.Reported = raggedInt(widths)
	visit(func(row, dev, o, d int) {
		set.Paid[row][dev-1] += g.Paid[o][d]
		set.PaidNet[row][dev-1] += g.PaidNet[o][d]
		set.Incurred[row][dev-1] += g.Incurred[o][d]
		set.Reported[row][dev-1] += g.Reported[o][d]
	})
	return set
}

// Cumulative returns the running-sum view of one measure as a Triangle. Its
// StartYear is the calendar year of StartMonth, which is what the annual
// grain the realism gate and the UI read needs.
func (s IncrementalSet) Cumulative(m Measure) Triangle {
	src := s.measure(m)
	cells := make([][]float64, len(src))
	for i, row := range src {
		out := make([]float64, len(row))
		running := 0.0
		for d, v := range row {
			running += v
			out[d] = running
		}
		cells[i] = out
	}
	return Triangle{StartYear: s.StartMonth.Year(), Cells: cells}
}

// measure returns one measure's incremental cells as float64, converting the
// reported counts.
func (s IncrementalSet) measure(m Measure) [][]float64 {
	switch m {
	case MeasurePaidNet:
		return s.PaidNet
	case MeasureIncurred:
		return s.Incurred
	case MeasureReported:
		out := make([][]float64, len(s.Reported))
		for i, row := range s.Reported {
			out[i] = make([]float64, len(row))
			for d, v := range row {
				out[i][d] = float64(v)
			}
		}
		return out
	default:
		return s.Paid
	}
}

// AnnualSet holds the cumulative annual triangles the realism gate and the UI
// read.
type AnnualSet struct {
	Paid     Triangle // gross of recoveries
	NetPaid  Triangle // net of salvage and subrogation
	Incurred Triangle // gross case plus net paid
}

// AnnualTriangles coarsens the grid to a yearly origin with devYears
// development years, folding later development into the last column, and
// returns the cumulative triangles. Reported counts are one
// Coarsen(...).Cumulative(MeasureReported) call away when something needs
// them.
func (g MonthlyGrid) AnnualTriangles(devYears int) AnnualSet {
	set := g.Coarsen(Annual, devYears, true)
	return AnnualSet{
		Paid:     set.Cumulative(MeasurePaid),
		NetPaid:  set.Cumulative(MeasurePaidNet),
		Incurred: set.Cumulative(MeasureIncurred),
	}
}

func raggedFloat(widths []int) [][]float64 {
	cells := make([][]float64, len(widths))
	for i, w := range widths {
		cells[i] = make([]float64, w)
	}
	return cells
}

func raggedInt(widths []int) [][]int {
	cells := make([][]int, len(widths))
	for i, w := range widths {
		cells[i] = make([]int, w)
	}
	return cells
}
