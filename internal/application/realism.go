package application

import "github.com/le-marais/claimsgen/internal/domain/triangle"

// scheduleP triangles have ten development years.
const developmentYears = 10

// EvaluateRealism aggregates a generated dataset into paid and incurred
// triangles plus earned premium, and scores them against the bands observed
// across the reference companies. Used as a test gate in the MVP; can back
// an in-app report later.
func EvaluateRealism(ds Dataset, refs []triangle.ReferenceSet, startYear, years int) triangle.Report {
	comparison := triangle.Comparison{
		Paid:          triangle.PaidTriangle(ds.Claims, ds.Transactions, startYear, years, developmentYears),
		Incurred:      triangle.IncurredTriangle(ds.Claims, ds.Transactions, startYear, years, developmentYears),
		EarnedPremium: triangle.EarnedPremiumByYear(ds.Policies, startYear, years),
	}
	return triangle.CompareToReference(comparison, refs)
}
