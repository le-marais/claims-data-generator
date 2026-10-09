package application_test

import (
	"slices"
	"testing"

	"github.com/le-marais/claimsgen/internal/application"
	"github.com/le-marais/claimsgen/internal/domain/claim"
	"github.com/le-marais/claimsgen/internal/domain/lob"
	"github.com/le-marais/claimsgen/internal/domain/shared"
	"github.com/le-marais/claimsgen/internal/domain/transaction"
	"github.com/le-marais/claimsgen/internal/infrastructure/random"
)

// claimDraws is what a claim's own random streams decide, apart from dates:
// its opening estimate, its episodes' costs, and its ledger's amounts and
// recoveries in order.
type claimDraws struct {
	initial   shared.Money
	ultimates []shared.Money
	amounts   []string
}

type claimIdentity struct {
	policy, section int
	occurrence      shared.Date
}

func drawsByClaim(t *testing.T, ds application.Dataset, ledgerAmounts bool) map[claimIdentity]claimDraws {
	t.Helper()
	rows := map[int][]transaction.Transaction{}
	for _, tx := range ds.Transactions {
		rows[tx.ClaimID] = append(rows[tx.ClaimID], tx)
	}
	out := map[claimIdentity]claimDraws{}
	for _, c := range ds.Claims {
		k := claimIdentity{c.PolicyID, c.Section, c.OccurrenceDate}
		if _, dup := out[k]; dup {
			t.Fatalf("two claims share %+v", k)
		}
		d := claimDraws{initial: c.InitialEstimate()}
		for _, ep := range c.Episodes {
			d.ultimates = append(d.ultimates, ep.Ultimate)
		}
		for _, tx := range rows[c.ID] {
			if ledgerAmounts || tx.Type.IsRecovery() {
				d.amounts = append(d.amounts, string(tx.Type)+" "+tx.Amount.String())
			}
		}
		out[k] = d
	}
	return out
}

func compareDraws(t *testing.T, name string, base, moved application.Dataset, ledgerAmounts bool, renumbered func([]claim.Claim, []claim.Claim) int) {
	t.Helper()
	a, b := drawsByClaim(t, base, ledgerAmounts), drawsByClaim(t, moved, ledgerAmounts)
	if len(a) != len(b) {
		t.Fatalf("%s: %d claims, want %d", name, len(b), len(a))
	}
	if renumbered(base.Claims, moved.Claims) == 0 {
		t.Fatalf("%s: no claim was renumbered; the test proves nothing", name)
	}
	for k, want := range a {
		got := b[k]
		if got.initial != want.initial || !slices.Equal(got.ultimates, want.ultimates) || !slices.Equal(got.amounts, want.amounts) {
			t.Fatalf("%s: claim %+v drew differently:\n got %+v\nwant %+v", name, k, got, want)
		}
	}
}

func renumberedCount(a, b []claim.Claim) int {
	ids := map[claimIdentity]int{}
	for _, c := range a {
		ids[claimIdentity{c.PolicyID, c.Section, c.OccurrenceDate}] = c.ID
	}
	n := 0
	for _, c := range b {
		if ids[claimIdentity{c.PolicyID, c.Section, c.OccurrenceDate}] != c.ID {
			n++
		}
	}
	return n
}

func generate(t *testing.T, req application.GenerateRequest) application.Dataset {
	t.Helper()
	ds, err := application.GenerateDataset(t.Context(), random.NewSource(17), req)
	if err != nil {
		t.Fatal(err)
	}
	return ds
}

// MR-24: a setting that moves report dates renumbers claims, but must not
// redraw any claim's case estimates, reopen, payments or recoveries.
func TestMovingReportDatesRedrawsNoClaim(t *testing.T) {
	base := request(t)
	base.LOB.BusinessDays = lob.BusinessDayParams{}
	base.LOB.SeasonalHoliday = lob.SeasonalHolidayParams{}

	// A report deferral moves a claim's whole timeline by a month, so with
	// no calendar every ledger amount stays the same.
	deferred := base
	deferred.LOB.SeasonalHoliday = lob.SeasonalHolidayParams{Hemisphere: lob.Northern, ReportShare: 1}
	compareDraws(t, "holiday report deferral", generate(t, base), generate(t, deferred), true, renumberedCount)

	// Rolling reports moves claims by a day or two and rolling every date
	// changes durations, so compare what does not depend on them: opening
	// estimates, reopens and costs, and recoveries.
	cal := base
	cal.LOB.BusinessDays = lob.BusinessDayParams{Calendar: "us"}
	rolled := cal
	rolled.LOB.BusinessDays.RollReports = true
	compareDraws(t, "roll_reports", generate(t, cal), generate(t, rolled), false, renumberedCount)
}
