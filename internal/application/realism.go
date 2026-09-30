package application

import "github.com/le-marais/claimsgen/internal/domain/triangle"

// EvaluateRealism scores the third-party liability section's accident-year
// triangles and earned premium against the bands observed across the
// reference companies. The Schedule P private passenger auto reference is a
// liability line with no physical damage in it, so own-damage claims are left
// out rather than bent to liability development speed. Paid is net of salvage
// and subrogation to match Schedule P, which reports paid losses net of
// recoveries. Used as a test gate; it also backs the UI's realism view.
func EvaluateRealism(ag Aggregates, refs []triangle.ReferenceSet) triangle.Report {
	return triangle.CompareToReference(triangle.Comparison{
		Paid:          ag.Liability.NetPaid,
		Incurred:      ag.Liability.Incurred,
		EarnedPremium: ag.LiabilityEarnedPremium,
	}, refs)
}
