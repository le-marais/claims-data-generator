package application_test

import (
	"testing"
	"time"

	"github.com/le-marais/claimsgen/internal/application"
	"github.com/le-marais/claimsgen/internal/domain/shared"
	"github.com/le-marais/claimsgen/internal/infrastructure/random"
)

// TestNoClaimsOutsideWindow proves MF-2: no generated claim occurs on or after
// Jan 1 of startYear+years, so the CSV cannot carry a trailing partial year.
func TestNoClaimsOutsideWindow(t *testing.T) {
	req := request(t)
	req.StartYear = 1998
	req.Years = 10
	req.InitialBookSize = 4000
	ds, err := application.GenerateDataset(random.NewSource(1), req)
	if err != nil {
		t.Fatal(err)
	}
	windowEnd := shared.NewDate(req.StartYear+req.Years, time.January, 1)
	for _, c := range ds.Claims {
		if !c.OccurrenceDate.Before(windowEnd) {
			t.Fatalf("claim %d occurred %s, outside window", c.ID, c.OccurrenceDate)
		}
	}
}

// TestHeaderCountMatchesSummary proves the header (len(ds.Claims)) and the
// per-year summary agree once no claim falls outside the window.
func TestHeaderCountMatchesSummary(t *testing.T) {
	req := request(t)
	req.StartYear = 1998
	req.Years = 10
	req.InitialBookSize = 4000
	ds, err := application.GenerateDataset(random.NewSource(1), req)
	if err != nil {
		t.Fatal(err)
	}
	report := application.Summarize(ds, req.StartYear, req.Years)
	if len(ds.Claims) != report.Total.Claims {
		t.Fatalf("header count %d != summary total %d", len(ds.Claims), report.Total.Claims)
	}
}
