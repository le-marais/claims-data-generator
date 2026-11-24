// Package premium models monthly bordereaux and signed clerical corrections.
package premium

import (
	"fmt"

	"github.com/le-marais/claimsgen/internal/domain/lob"
	"github.com/le-marais/claimsgen/internal/domain/policy"
	"github.com/le-marais/claimsgen/internal/domain/shared"
)

type Batch struct {
	ID        string
	BinderID  int
	Month     shared.Month
	Processed shared.Date
}
type Declaration struct {
	ID, OriginalID, PolicyID, BinderID int
	Inception, Effective, Processed    shared.Date
	BatchID, Type                      string
	Amount                             shared.Money
}
type Report struct {
	Batches      []Batch
	Declarations []Declaration
}

// Generate records the full premium once, except when a clerical error needs
// a later, linked correction. Processing time is shared by each monthly batch.
func Generate(src shared.RandomSource, params lob.PremiumReportingParams, book []policy.Policy) Report {
	var report Report
	batches := map[string]Batch{}
	batch := func(p policy.Policy, month shared.Month) Batch {
		id := fmt.Sprintf("%d-%s", p.BinderID, month)
		if b, ok := batches[id]; ok {
			return b
		}
		r := src.Split("batch-" + id)
		delay := 4 + 2*(p.BinderID%10) + int(10*r.Uniform())
		processed := month.End().AddDays(delay)
		if r.Bernoulli(params.DelayedBatchProbability) {
			processed = processed.AddMonths(2)
		}
		b := Batch{id, p.BinderID, month, processed}
		batches[id] = b
		report.Batches = append(report.Batches, b)
		return b
	}
	for _, p := range book {
		r := src.Split(fmt.Sprintf("declaration-%d", p.ID))
		b := batch(p, p.CoverStart.Month())
		var correction shared.Money
		if p.Premium > 1 && r.Bernoulli(params.CorrectionProbability) {
			correction = max(shared.OneCent, p.Premium.MulFloat(0.03+0.09*r.Uniform()))
			if r.Bernoulli(0.5) {
				correction = -correction
			}
		}
		initial := Declaration{ID: len(report.Declarations) + 1, PolicyID: p.ID, BinderID: p.BinderID, Inception: p.CoverStart, Effective: p.CoverStart, Processed: b.Processed, BatchID: b.ID, Type: "initial", Amount: p.Premium - correction}
		report.Declarations = append(report.Declarations, initial)
		if correction == 0 {
			continue
		}
		// The error is found after the original batch was processed.
		effective := b.Processed.AddDays(7 + int(r.Uniform()*45))
		next := batch(p, effective.Month())
		kind := "additional_correction"
		if correction < 0 {
			kind = "return_correction"
		}
		report.Declarations = append(report.Declarations, Declaration{ID: len(report.Declarations) + 1, OriginalID: initial.ID, PolicyID: p.ID, BinderID: p.BinderID, Inception: p.CoverStart, Effective: effective, Processed: next.Processed, BatchID: next.ID, Type: kind, Amount: correction})
	}
	return report
}
