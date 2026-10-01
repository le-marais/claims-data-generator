package csv_test

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/le-marais/claimsgen/internal/application"
	"github.com/le-marais/claimsgen/internal/domain/claim"
	"github.com/le-marais/claimsgen/internal/domain/policy"
	"github.com/le-marais/claimsgen/internal/domain/shared"
	"github.com/le-marais/claimsgen/internal/domain/transaction"
	"github.com/le-marais/claimsgen/internal/domain/triangle"
	csvout "github.com/le-marais/claimsgen/internal/infrastructure/csv"
)

// aggregateFixture is one policy incepting 1 January 1998 and one claim
// occurring in March 1998, paid $1,000 in April 1998 with $150 of salvage in
// June 1998.
func aggregateFixture(t *testing.T) application.Aggregates {
	t.Helper()
	ds := application.Dataset{
		Policies: []policy.Policy{{
			ID:         1,
			CoverStart: shared.NewDate(1998, time.January, 1),
			CoverEnd:   shared.NewDate(1998, time.December, 31),
			Premium:    shared.FromDollars(365),
		}},
		Claims: []claim.Claim{{
			Record: claim.Record{
				ID:             1,
				PolicyID:       1,
				OccurrenceDate: shared.NewDate(1998, time.March, 1),
				ReportDate:     shared.NewDate(1998, time.March, 5),
			},
		}},
		Transactions: []transaction.Transaction{
			{ID: 1, ClaimID: 1, Date: shared.NewDate(1998, time.March, 5), Type: transaction.Estimate, Amount: shared.FromDollars(1000)},
			{ID: 2, ClaimID: 1, Date: shared.NewDate(1998, time.April, 1), Type: transaction.Payment, Amount: shared.FromDollars(1000)},
			{ID: 3, ClaimID: 1, Date: shared.NewDate(1998, time.April, 1), Type: transaction.Estimate, Amount: shared.FromDollars(-1000)},
			{ID: 4, ClaimID: 1, Date: shared.NewDate(1998, time.June, 1), Type: transaction.Salvage, Amount: shared.FromDollars(150)},
		},
	}
	ag, err := application.Aggregate(ds, 1998, 1, triangle.AccidentMonth)
	if err != nil {
		t.Fatal(err)
	}
	return ag
}

func writeFixture(t *testing.T) (dir string, ag application.Aggregates) {
	t.Helper()
	dir = t.TempDir()
	ag = aggregateFixture(t)
	if err := csvout.WriteAggregates(dir, ag); err != nil {
		t.Fatal(err)
	}
	return dir, ag
}

// readLines is defined in writer_test.go and reused here.

func TestWriteAggregatesTrianglesHeaderAndShape(t *testing.T) {
	dir, ag := writeFixture(t)
	lines := readLines(t, filepath.Join(dir, "triangles.csv"))
	if lines[0] != "origin_month,dev_month,paid,paid_net,incurred,reported_count" {
		t.Errorf("header = %q", lines[0])
	}
	wantRows := ag.Grid.Origins() * ag.Grid.DevPeriods
	if got := len(lines) - 1; got != wantRows {
		t.Errorf("got %d rows, want %d (%d origins x %d development months)",
			got, wantRows, ag.Grid.Origins(), ag.Grid.DevPeriods)
	}
	// Rows are ordered by origin month then development month, and the first
	// development month is 1.
	if !strings.HasPrefix(lines[1], "1998-01,1,") {
		t.Errorf("first row = %q, want it to start 1998-01,1,", lines[1])
	}
	if !strings.HasPrefix(lines[2], "1998-01,2,") {
		t.Errorf("second row = %q, want it to start 1998-01,2,", lines[2])
	}
}

func TestWriteAggregatesTriangleValues(t *testing.T) {
	dir, _ := writeFixture(t)
	lines := readLines(t, filepath.Join(dir, "triangles.csv"))
	find := func(origin string, dev int) string {
		t.Helper()
		prefix := origin + "," + strconv.Itoa(dev) + ","
		for _, l := range lines[1:] {
			if strings.HasPrefix(l, prefix) {
				return l
			}
		}
		t.Fatalf("no row for %s dev %d", origin, dev)
		return ""
	}
	// March origin, development 1: the case estimate is raised, nothing paid,
	// one claim reported.
	if got, want := find("1998-03", 1), "1998-03,1,0.00,0.00,1000.00,1"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	// Development 2 is April: pay 1,000 and release the case, so incurred is flat.
	if got, want := find("1998-03", 2), "1998-03,2,1000.00,1000.00,0.00,0"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	// Development 4 is June: salvage of 150 comes back.
	if got, want := find("1998-03", 4), "1998-03,4,0.00,-150.00,-150.00,0"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestWriteAggregatesExposure(t *testing.T) {
	dir, ag := writeFixture(t)
	lines := readLines(t, filepath.Join(dir, "exposure.csv"))
	if lines[0] != "origin_month,premium,exposure_units,policies" {
		t.Errorf("header = %q", lines[0])
	}
	if got := len(lines) - 1; got != len(ag.Exposure) {
		t.Errorf("got %d rows, want %d", got, len(ag.Exposure))
	}
	// A 365-day policy on $365 earns a dollar a day, so January earns 31.
	if got, want := lines[1], "1998-01,31.00,0.084873,1"; got != want {
		t.Errorf("January row = %q, want %q", got, want)
	}
}

func TestWriteAggregatesNeverWritesNegativeZero(t *testing.T) {
	dir, _ := writeFixture(t)
	for _, name := range []string{"triangles.csv", "exposure.csv"} {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(b), "-0.00") || strings.Contains(string(b), "-0.000000") {
			t.Errorf("%s contains a negative zero", name)
		}
	}
}

func TestWriteAggregatesIsByteStable(t *testing.T) {
	ag := aggregateFixture(t)
	dirA, dirB := t.TempDir(), t.TempDir()
	if err := csvout.WriteAggregates(dirA, ag); err != nil {
		t.Fatal(err)
	}
	if err := csvout.WriteAggregates(dirB, ag); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"triangles.csv", "exposure.csv"} {
		a, err := os.ReadFile(filepath.Join(dirA, name))
		if err != nil {
			t.Fatal(err)
		}
		b, err := os.ReadFile(filepath.Join(dirB, name))
		if err != nil {
			t.Fatal(err)
		}
		if string(a) != string(b) {
			t.Errorf("%s differs between two writes of the same aggregate", name)
		}
	}
}

func TestWriteAggregatesCreatesTheDirectory(t *testing.T) {
	ag := aggregateFixture(t)
	dir := filepath.Join(t.TempDir(), "nested", "out")
	if err := csvout.WriteAggregates(dir, ag); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "triangles.csv")); err != nil {
		t.Errorf("triangles.csv missing: %v", err)
	}
}
