package application

import "github.com/le-marais/claimsgen/internal/domain/triangle"

// EvaluateRealism scores an aggregate's accident-year triangles and earned
// premium against the bands observed across the reference companies. Paid is
// net of salvage and subrogation to match Schedule P, which reports paid
// losses net of recoveries. Used as a test gate in the MVP; it also backs the
// UI's realism view.
func EvaluateRealism(ag Aggregates, refs []triangle.ReferenceSet) triangle.Report {
	return triangle.CompareToReference(triangle.Comparison{
		Paid:          ag.Annual.NetPaid,
		Incurred:      ag.Annual.Incurred,
		EarnedPremium: ag.EarnedPremium,
	}, refs)
}
