package application_test

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/le-marais/claimsgen/internal/application"
	"github.com/le-marais/claimsgen/internal/domain/policy"
	"github.com/le-marais/claimsgen/internal/domain/premium"
	"github.com/le-marais/claimsgen/internal/domain/shared"
	"github.com/le-marais/claimsgen/internal/infrastructure/config"
	csvout "github.com/le-marais/claimsgen/internal/infrastructure/csv"
	"github.com/le-marais/claimsgen/internal/infrastructure/random"
)

func TestPropertyBinderExport(t *testing.T) {
	l, err := config.LoadFile("../../data/property.yaml")
	if err != nil {
		t.Fatal(err)
	}
	ds, err := application.GenerateDataset(t.Context(), random.NewSource(71), application.GenerateRequest{LOB: l, StartYear: 2017, Years: 3, InitialBookSize: 1200})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := csvout.WriteDataset(dir, ds); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"binders.csv", "policy_binders.csv", "premium_batches.csv", "premium_declarations.csv", "claim_episodes.csv"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Fatal(err)
		}
	}
}

func TestPropertyDeclarationsAndBinderGrid(t *testing.T) {
	l, err := config.LoadFile("../../data/property.yaml")
	if err != nil {
		t.Fatal(err)
	}
	req := application.GenerateRequest{LOB: l, StartYear: 2017, Years: 3, InitialBookSize: 1200}
	run := func(req application.GenerateRequest) application.Dataset {
		ds, err := application.GenerateDataset(t.Context(), random.NewSource(71), req)
		if err != nil {
			t.Fatal(err)
		}
		return ds
	}
	ds := run(req)
	ag, err := application.Aggregate(ds, 2017, 3, "binder")
	if err != nil {
		t.Fatal(err)
	}
	bmap := map[int]policy.Binder{}
	for _, b := range ds.Binders {
		bmap[b.ID] = b
	}
	sums := map[int]shared.Money{}
	initials := map[int]int{}
	batchRows := map[string]int{}
	signs := map[string]bool{}
	cells := map[[2]int]int64{}
	for _, d := range ds.Premium.Declarations {
		b := bmap[d.BinderID]
		if d.Processed.Before(d.Effective) || d.Effective.Before(d.Inception) {
			t.Fatal(d)
		}
		if d.Type == "initial" {
			initials[d.PolicyID]++
		} else {
			orig := ds.Premium.Declarations[d.OriginalID-1]
			if orig.PolicyID != d.PolicyID || orig.Type != "initial" || d.Processed.Before(orig.Processed) {
				t.Fatal(d)
			}
			if (d.Type == "return_correction") != (d.Amount < 0) {
				t.Fatal(d)
			}
		}
		sums[d.PolicyID] += d.Amount
		batchRows[d.BatchID]++
		signs[d.Type] = true
		if b.YearOfAccount >= 2017 {
			row := (b.YearOfAccount - 2017) * 12
			col := shared.MonthsBetween(shared.NewMonth(b.YearOfAccount, 1), d.Processed.Month())
			cells[[2]int{row, col}] += int64(d.Amount)
		}
	}
	futurePolicy := false
	futureClaim := false
	for _, p := range ds.Policies {
		b := bmap[p.BinderID]
		if p.CoverStart.Before(b.Inception) || p.CoverStart.After(b.Expiry) || p.Premium != sums[p.ID] || initials[p.ID] != 1 {
			t.Fatal(p)
		}
		if p.BinderYear == 2019 && p.CoverStart.Year() > 2019 {
			futurePolicy = true
		}
	}
	for _, c := range ds.Claims {
		if c.OccurrenceDate.Year() >= 2021 {
			futureClaim = true
		}
	}
	if !futurePolicy || !futureClaim || len(signs) != 3 || len(ds.Premium.Declarations)*2 >= 3*len(ds.Policies) {
		t.Fatal("missing runoff, both correction signs, or majority initial-only business")
	}
	delayedShared := false
	for _, b := range ds.Premium.Batches {
		if batchRows[b.ID] > 1 && shared.MonthsBetween(b.Month, b.Processed.Month()) > 1 {
			delayedShared = true
		}
	}
	if !delayedShared {
		t.Fatal("missing shared delayed batches")
	}
	for row, cs := range ag.Grid.DeclaredPremiumCents {
		for col, v := range cs {
			if v != cells[[2]int{row, col}] {
				t.Fatalf("row %d dev %d got %d", row, col, v)
			}
			delete(cells, [2]int{row, col})
		}
	}
	if len(cells) != 0 {
		t.Fatal("lost runoff")
	}
	req.LOB.PremiumReporting.Enabled = false
	legacy := run(req)
	if !reflect.DeepEqual(ds.Policies, legacy.Policies) || !reflect.DeepEqual(ds.Claims, legacy.Claims) || !reflect.DeepEqual(ds.Transactions, legacy.Transactions) || !reflect.DeepEqual(ds.Binders, legacy.Binders) {
		t.Fatal("reporting moved book or claims")
	}
	req.LOB.Book.SizeVolatility = 0.4
	changed := run(req)
	if !reflect.DeepEqual(changed.Binders, ds.Binders) || reflect.DeepEqual(changed.Policies, ds.Policies) {
		t.Fatal("inception plans depend on realised policies")
	}
}

func TestJulyBinderFollowingMarchAttachment(t *testing.T) {
	b := policy.Binder{ID: 20253, YearOfAccount: 2025, Inception: shared.NewDate(2025, 7, 1), Expiry: shared.NewDate(2026, 6, 30)}
	p := policy.Policy{ID: 7, BinderID: b.ID, BinderYear: b.YearOfAccount, CoverStart: shared.NewDate(2026, 3, 10), CoverEnd: shared.NewDate(2027, 3, 9)}
	d := premium.Declaration{PolicyID: p.ID, BinderID: b.ID, Processed: shared.NewDate(2026, 4, 15), Amount: 1277}
	ds := application.Dataset{Policies: []policy.Policy{p}, Binders: []policy.Binder{b}, Premium: premium.Report{Declarations: []premium.Declaration{d}}}
	ag, err := application.Aggregate(ds, 2025, 1, "binder")
	if err != nil {
		t.Fatal(err)
	}
	if ag.Grid.DeclaredPremiumCents[0][15] != 1277 {
		t.Fatal("April 2026 declaration must enter 2025 development quarter six")
	}
}
