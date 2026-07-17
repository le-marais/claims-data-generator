package application_test

import (
	"reflect"
	"testing"
	"time"

	"github.com/le-marais/claimsgen/internal/application"
	"github.com/le-marais/claimsgen/internal/domain/claim"
	"github.com/le-marais/claimsgen/internal/domain/policy"
	"github.com/le-marais/claimsgen/internal/domain/shared"
	"github.com/le-marais/claimsgen/internal/domain/transaction"
)

// tinyDataset is a hand-built two-year dataset with exactly checkable
// aggregates: one full-year policy and one claim per year, claim 2 paying
// out in 2000 but occurring in 1999.
func tinyDataset() application.Dataset {
	return application.Dataset{
		Policies: []policy.Policy{
			{ID: 1, CoverStart: shared.NewDate(1998, time.January, 1), CoverEnd: shared.NewDate(1998, time.December, 31), Premium: shared.FromDollars(365)},
			{ID: 2, CoverStart: shared.NewDate(1999, time.January, 1), CoverEnd: shared.NewDate(1999, time.December, 31), Premium: shared.FromDollars(730)},
		},
		Claims: []claim.Claim{
			{ID: 1, PolicyID: 1, OccurrenceDate: shared.NewDate(1998, time.June, 1), ReportDate: shared.NewDate(1998, time.June, 11), CloseDate: shared.NewDate(1998, time.December, 1)},
			{ID: 2, PolicyID: 2, OccurrenceDate: shared.NewDate(1999, time.March, 1), ReportDate: shared.NewDate(1999, time.March, 31), CloseDate: shared.NewDate(2000, time.March, 31)},
		},
		Transactions: []transaction.Transaction{
			{ID: 1, ClaimID: 1, Date: shared.NewDate(1998, time.June, 11), Type: transaction.Estimate, Amount: shared.FromDollars(1200)},
			{ID: 2, ClaimID: 1, Date: shared.NewDate(1998, time.August, 1), Type: transaction.Payment, Amount: shared.FromDollars(600)},
			{ID: 3, ClaimID: 1, Date: shared.NewDate(1998, time.December, 1), Type: transaction.Payment, Amount: shared.FromDollars(400)},
			{ID: 4, ClaimID: 2, Date: shared.NewDate(2000, time.March, 31), Type: transaction.Payment, Amount: shared.FromDollars(500)},
		},
	}
}

func TestSummarize(t *testing.T) {
	got := application.Summarize(tinyDataset(), 1998, 2)
	want := application.SummaryReport{
		Years: []application.YearSummary{
			{Year: 1998, Policies: 1, Claims: 1, EarnedPremium: 365, Paid: 1000},
			{Year: 1999, Policies: 1, Claims: 1, EarnedPremium: 730, Paid: 500},
		},
		Total: application.YearSummary{Policies: 2, Claims: 2, EarnedPremium: 1095, Paid: 1500},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Summarize = %+v, want %+v", got, want)
	}
}

func TestYearSummaryLossRatio(t *testing.T) {
	lr, ok := application.YearSummary{EarnedPremium: 500, Paid: 400}.LossRatio()
	if !ok || lr != 0.8 {
		t.Fatalf("LossRatio = %v, %v, want 0.8, true", lr, ok)
	}
	if _, ok := (application.YearSummary{Paid: 100}).LossRatio(); ok {
		t.Fatal("LossRatio with zero premium: want ok=false")
	}
}

func TestSummarizeCountsNilClaims(t *testing.T) {
	ds := application.Dataset{
		Policies: []policy.Policy{
			{ID: 1, CoverStart: shared.NewDate(1998, time.January, 1), CoverEnd: shared.NewDate(1998, time.December, 31), Premium: shared.FromDollars(365)},
		},
		Claims: []claim.Claim{
			{ID: 1, PolicyID: 1, OccurrenceDate: shared.NewDate(1998, time.March, 1), ReportDate: shared.NewDate(1998, time.March, 11), CloseDate: shared.NewDate(1998, time.June, 1)},
			{ID: 2, PolicyID: 1, OccurrenceDate: shared.NewDate(1998, time.April, 1), ReportDate: shared.NewDate(1998, time.April, 11), CloseDate: shared.NewDate(1998, time.July, 1), Nil: true},
		},
		Transactions: []transaction.Transaction{
			{ID: 1, ClaimID: 1, Date: shared.NewDate(1998, time.March, 11), Type: transaction.Estimate, Amount: shared.FromDollars(1000)},
			{ID: 2, ClaimID: 1, Date: shared.NewDate(1998, time.June, 1), Type: transaction.Payment, Amount: shared.FromDollars(1000)},
			{ID: 3, ClaimID: 2, Date: shared.NewDate(1998, time.April, 11), Type: transaction.Estimate, Amount: shared.FromDollars(800)},
			{ID: 4, ClaimID: 2, Date: shared.NewDate(1998, time.July, 1), Type: transaction.Estimate, Amount: shared.FromDollars(-800)},
		},
	}
	got := application.Summarize(ds, 1998, 1)
	if got.Years[0].Claims != 2 {
		t.Fatalf("claims = %d, want 2", got.Years[0].Claims)
	}
	if got.Years[0].NilClaims != 1 {
		t.Fatalf("nil claims = %d, want 1", got.Years[0].NilClaims)
	}
	if got.Total.NilClaims != 1 {
		t.Fatalf("total nil claims = %d, want 1", got.Total.NilClaims)
	}
}

func TestSummarizeCountsRecoveredByOccurrenceYear(t *testing.T) {
	ds := tinyDataset()
	// A salvage recovery on claim 1 (occurred 1998), received in 1999: it
	// books to the occurrence year, like Paid.
	ds.Transactions = append(ds.Transactions, transaction.Transaction{
		ID: 5, ClaimID: 1, Date: shared.NewDate(1999, time.February, 1), Type: transaction.Salvage, Amount: shared.FromDollars(150),
	})
	got := application.Summarize(ds, 1998, 2)
	if got.Years[0].Recovered != 150 {
		t.Errorf("1998 recovered = %v, want 150", got.Years[0].Recovered)
	}
	if got.Years[1].Recovered != 0 {
		t.Errorf("1999 recovered = %v, want 0", got.Years[1].Recovered)
	}
	if got.Total.Recovered != 150 {
		t.Errorf("total recovered = %v, want 150", got.Total.Recovered)
	}
	// Paid stays gross.
	if got.Years[0].Paid != 1000 {
		t.Errorf("1998 paid = %v, want 1000 (gross)", got.Years[0].Paid)
	}
}
