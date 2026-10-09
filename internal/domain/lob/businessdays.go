package lob

import (
	"fmt"

	"github.com/le-marais/claimsgen/internal/domain/calendar"
)

// BusinessDayParams puts claims processing on business days: every case
// estimate, payment, close, reopen and recovery rolls to a business day of
// the named calendar, and reports too with RollReports. Occurrences never
// move. The zero value is off.
type BusinessDayParams struct {
	// Calendar names the market's calendar: "" or "none" (off), "weekends",
	// "us", "uk" or "za" (see calendar.Lookup).
	Calendar string
	// RollReports rolls report dates to business days too, as for lines
	// whose claims are reported through an office or broker. Off, reports
	// keep the day they are drawn on, weekends and holidays included.
	RollReports bool
}

func (b BusinessDayParams) validate() error {
	if _, err := calendar.Lookup(b.Calendar); err != nil {
		return fmt.Errorf("business_days.calendar: %w", err)
	}
	return nil
}
