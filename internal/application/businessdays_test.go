package application_test

import (
	"testing"

	"github.com/le-marais/claimsgen/internal/application"
	"github.com/le-marais/claimsgen/internal/domain/calendar"
	"github.com/le-marais/claimsgen/internal/infrastructure/random"
)

// Every processing date in a run - each transaction, each close and each
// reopen - is a business day, and reports are rolled only when asked.
func TestDatasetProcessesOnBusinessDays(t *testing.T) {
	for _, tc := range []struct {
		calendar    string
		rollReports bool
	}{{"us", false}, {"uk", true}, {"za", false}} {
		req := request(t)
		req.LOB.BusinessDays.Calendar = tc.calendar
		req.LOB.BusinessDays.RollReports = tc.rollReports
		cal, err := calendar.Lookup(tc.calendar)
		if err != nil {
			t.Fatal(err)
		}
		ds, err := application.GenerateDataset(t.Context(), random.NewSource(5), req)
		if err != nil {
			t.Fatal(err)
		}
		for _, tx := range ds.Transactions {
			if !cal.IsBusinessDay(tx.Date) {
				t.Fatalf("%s: claim %d %s row on %v, a %v", tc.calendar, tx.ClaimID, tx.Type, tx.Date, tx.Date.Weekday())
			}
		}
		offDayReports := 0
		for _, c := range ds.Claims {
			for i, ep := range c.Episodes {
				if !cal.IsBusinessDay(ep.Close) || (i > 0 && !cal.IsBusinessDay(ep.Open)) {
					t.Fatalf("%s: claim %d episode %d runs %v to %v", tc.calendar, c.ID, i+1, ep.Open, ep.Close)
				}
			}
			if !cal.IsBusinessDay(c.ReportDate()) {
				offDayReports++
			}
		}
		if tc.rollReports && offDayReports > 0 {
			t.Errorf("%s: %d reports on non-business days with roll_reports", tc.calendar, offDayReports)
		}
		if !tc.rollReports && offDayReports == 0 {
			t.Errorf("%s: no report on a weekend or holiday without roll_reports", tc.calendar)
		}
	}
}
