package triangle

import (
	"fmt"

	"github.com/le-marais/claimsgen/internal/domain/claim"
	"github.com/le-marais/claimsgen/internal/domain/policy"
	"github.com/le-marais/claimsgen/internal/domain/shared"
	"github.com/le-marais/claimsgen/internal/domain/transaction"
)

// Measure names one of the quantities a triangle carries.
type Measure int

const (
	MeasurePaid     Measure = iota // payments, gross of recoveries
	MeasurePaidNet                 // payments net of salvage and subrogation
	MeasureIncurred                // gross case movements plus net paid
	MeasureReported                // claim counts, by report month
	MeasureIBNR                    // pure IBNR: true cost of claims occurred but not yet reported
)

// MonthlyGrid holds incremental monthly triangles: row o is origin month
// StartMonth.Add(o), and slice index d holds development period d+1, so index
// 0 is development period 1 - the origin month itself. Every row is DevPeriods
// wide.
//
// Cells are incremental, not cumulative: a cell is the movement in that
// development month. Increments sum, so any coarser origin or development
// grain is a plain sum over cells (see Coarsen) and a cumulative view is a
// running sum along a row.
type MonthlyGrid struct {
	Basis      OriginBasis
	StartMonth shared.Month
	DevPeriods int
	Paid       [][]float64
	PaidNet    [][]float64
	Incurred   [][]float64
	Reported   [][]int
	// IBNR is pure IBNR held at its true value: each claim's Cost is booked
	// in its occurrence month and released in its report month, so the
	// running sum at a valuation is the cost of claims that have occurred but
	// are not yet reported. Incurred plus IBNR is the generated counterpart of
	// Schedule P total incurred, with a perfect IBNR estimate and no bulk
	// reserve.
	IBNR [][]float64
}

// Origins returns the number of origin months in the grid.
func (g MonthlyGrid) Origins() int { return len(g.Paid) }

// Cell reads one cell with a 1-based development period. Reported counts
// convert to float64. Indices outside the grid read as zero.
func (g MonthlyGrid) Cell(m Measure, origin, dev int) float64 {
	if m == MeasureReported {
		if origin < 0 || origin >= len(g.Reported) || dev < 1 || dev > len(g.Reported[origin]) {
			return 0
		}
		return float64(g.Reported[origin][dev-1])
	}
	var cells [][]float64
	switch m {
	case MeasurePaidNet:
		cells = g.PaidNet
	case MeasureIncurred:
		cells = g.Incurred
	case MeasureIBNR:
		cells = g.IBNR
	default:
		cells = g.Paid
	}
	if origin < 0 || origin >= len(cells) || dev < 1 || dev > len(cells[origin]) {
		return 0
	}
	return cells[origin][dev-1]
}

// BuildMonthlyGrid aggregates a dataset into incremental monthly triangles.
// originMonths origin months are keyed from startMonth; a claim whose origin
// falls outside that span is skipped, along with all of its movements.
//
// Development runs to full runoff: DevPeriods is the widest development period
// any in-span claim reaches, so no movement is dropped, including development
// after the run window ends. A caller wanting a valuation-date view filters
// cells where origin + dev - 1 exceeds the valuation month.
func BuildMonthlyGrid(
	policies []policy.Policy,
	claims []claim.Claim,
	txs []transaction.Transaction,
	startMonth shared.Month,
	originMonths int,
	basis OriginBasis,
) (MonthlyGrid, error) {
	if err := basis.Validate(); err != nil {
		return MonthlyGrid{}, err
	}
	if originMonths < 1 {
		return MonthlyGrid{}, fmt.Errorf("origin months: must be at least 1, got %d", originMonths)
	}
	rows, err := originRows(policies, claims, basis, startMonth, originMonths)
	if err != nil {
		return MonthlyGrid{}, err
	}

	// Pass one sizes the rectangle: the widest development period reached by
	// any movement of any in-span claim, floored at one column.
	devPeriods := 1
	widen := func(claimID int, event shared.Month) {
		row, ok := rows[claimID]
		if !ok {
			return
		}
		if d := devPeriod(startMonth, row, event); d > devPeriods {
			devPeriods = d
		}
	}
	for _, c := range claims {
		widen(c.ID, c.ReportDate.Month())
	}
	for _, tx := range txs {
		widen(tx.ClaimID, tx.Date.Month())
	}

	g := MonthlyGrid{
		Basis:      basis,
		StartMonth: startMonth,
		DevPeriods: devPeriods,
		Paid:       newFloatCells(originMonths, devPeriods),
		PaidNet:    newFloatCells(originMonths, devPeriods),
		Incurred:   newFloatCells(originMonths, devPeriods),
		Reported:   newIntCells(originMonths, devPeriods),
		IBNR:       newFloatCells(originMonths, devPeriods),
	}

	// Pass two places every movement. The weights match the annual triangles
	// this grid replaces: paid counts payments only; net paid subtracts
	// recoveries; incurred adds every case movement and payment and subtracts
	// recoveries, so it is gross case plus net paid.
	for _, c := range claims {
		row, ok := rows[c.ID]
		if !ok {
			continue
		}
		reported := devPeriod(startMonth, row, c.ReportDate.Month()) - 1
		g.Reported[row][reported]++
		// A claim reported in the month it occurred is never IBNR at a month
		// end; skipping it keeps the cell free of a +cost-cost rounding residue.
		if occurred := devPeriod(startMonth, row, c.OccurrenceDate.Month()) - 1; occurred < reported {
			cost := c.Cost().Dollars()
			g.IBNR[row][occurred] += cost
			g.IBNR[row][reported] -= cost
		}
	}
	for _, tx := range txs {
		row, ok := rows[tx.ClaimID]
		if !ok {
			continue
		}
		d := devPeriod(startMonth, row, tx.Date.Month()) - 1
		amount := tx.Amount.Dollars()
		switch {
		case tx.Type == transaction.Payment:
			g.Paid[row][d] += amount
			g.PaidNet[row][d] += amount
			g.Incurred[row][d] += amount
		case tx.Type.IsRecovery():
			g.PaidNet[row][d] -= amount
			g.Incurred[row][d] -= amount
		default:
			g.Incurred[row][d] += amount
		}
	}
	return g, nil
}

// originRows maps each claim to its grid row, dropping claims whose origin
// falls outside the grid. On the underwriting basis a claim whose policy is
// absent from the book is an error rather than a silent drop.
func originRows(policies []policy.Policy, claims []claim.Claim, basis OriginBasis, startMonth shared.Month, originMonths int) (map[int]int, error) {
	var coverStart map[int]shared.Month
	if basis == UnderwritingMonth {
		coverStart = make(map[int]shared.Month, len(policies))
		for _, p := range policies {
			coverStart[p.ID] = p.CoverStart.Month()
		}
	}
	rows := make(map[int]int, len(claims))
	for _, c := range claims {
		origin := c.OccurrenceDate.Month()
		if basis == UnderwritingMonth {
			m, ok := coverStart[c.PolicyID]
			if !ok {
				return nil, fmt.Errorf("claim %d: policy %d is not in the book", c.ID, c.PolicyID)
			}
			origin = m
		}
		row := shared.MonthsBetween(startMonth, origin)
		if row < 0 || row >= originMonths {
			continue
		}
		rows[c.ID] = row
	}
	return rows, nil
}

// devPeriod returns the 1-based development month of an event for the origin
// row, clamped to at least 1. The clamp cannot fire on generated data - a
// policy's cover start precedes its claims' occurrences, which precede their
// report dates, which precede their transactions - but this is a public domain
// function and must not index out of range on hand-built input.
func devPeriod(startMonth shared.Month, row int, event shared.Month) int {
	d := shared.MonthsBetween(startMonth.Add(row), event) + 1
	if d < 1 {
		return 1
	}
	return d
}

func newFloatCells(rows, cols int) [][]float64 {
	cells := make([][]float64, rows)
	for i := range cells {
		cells[i] = make([]float64, cols)
	}
	return cells
}

func newIntCells(rows, cols int) [][]int {
	cells := make([][]int, rows)
	for i := range cells {
		cells[i] = make([]int, cols)
	}
	return cells
}
