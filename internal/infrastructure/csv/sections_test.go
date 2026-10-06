package csv_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/le-marais/claimsgen/internal/application"
	"github.com/le-marais/claimsgen/internal/domain/claim"
	"github.com/le-marais/claimsgen/internal/domain/policy"
	"github.com/le-marais/claimsgen/internal/domain/shared"
	"github.com/le-marais/claimsgen/internal/domain/triangle"
	csvout "github.com/le-marais/claimsgen/internal/infrastructure/csv"
)

// sectionFixture is one 365-day policy of $365 split 100/265 over two
// sections, with a claim in each section, so a day earns $1 and January earns
// 31 days.
func sectionFixture(t *testing.T) (application.Dataset, application.Aggregates) {
	t.Helper()
	ds := application.Dataset{
		Policies: []policy.Policy{{
			ID:              1,
			CoverStart:      shared.NewDate(1998, time.January, 1),
			CoverEnd:        shared.NewDate(1998, time.December, 31),
			Premium:         shared.FromDollars(365),
			SectionPremiums: []shared.Money{shared.FromDollars(100), shared.FromDollars(265)},
		}},
		Claims: []claim.Claim{
			{ID: 1, PolicyID: 1, Section: 1, OccurrenceDate: shared.NewDate(1998, time.March, 1), Episodes: []claim.Episode{{Open: shared.NewDate(1998, time.March, 5)}}},
			{ID: 2, PolicyID: 1, Section: 0, OccurrenceDate: shared.NewDate(1998, time.March, 2), Episodes: []claim.Episode{{Open: shared.NewDate(1998, time.March, 5)}}},
		},
	}
	ag, err := application.Aggregate(ds, 1998, 1, triangle.AccidentMonth)
	if err != nil {
		t.Fatal(err)
	}
	return ds, ag
}

func TestWriteSectionDetail(t *testing.T) {
	ds, ag := sectionFixture(t)
	dir := t.TempDir()
	if err := csvout.WriteSectionDetail(dir, []string{"alpha", "beta"}, ds, ag); err != nil {
		t.Fatal(err)
	}

	claims := readLines(t, filepath.Join(dir, "claim_sections.csv"))
	wantClaims := []string{"claim_id,section", "1,beta", "2,alpha"}
	if len(claims) != len(wantClaims) {
		t.Fatalf("claim_sections.csv = %q, want %q", claims, wantClaims)
	}
	for i := range wantClaims {
		if claims[i] != wantClaims[i] {
			t.Errorf("claim_sections.csv line %d = %q, want %q", i, claims[i], wantClaims[i])
		}
	}

	exposure := readLines(t, filepath.Join(dir, "exposure_sections.csv"))
	if got, want := len(exposure), 1+12*2; got != want {
		t.Fatalf("exposure_sections.csv has %d lines, want %d", got, want)
	}
	for i, want := range []string{
		"origin_month,section,premium",
		"1998-01,alpha,8.49", // 100/365 a day for 31 days
		"1998-01,beta,22.51", // 265/365 a day for 31 days
		"1998-02,alpha,7.67", // 28 days
		"1998-02,beta,20.33",
	} {
		if exposure[i] != want {
			t.Errorf("exposure_sections.csv line %d = %q, want %q", i, exposure[i], want)
		}
	}
}

func TestWriteSectionDetailRejectsMismatchedSections(t *testing.T) {
	ds, ag := sectionFixture(t)
	for _, tc := range []struct {
		name     string
		sections []string
	}{
		{"too few names for the claims", []string{"alpha"}},
		{"too many names for the exposure", []string{"alpha", "beta", "gamma"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := csvout.WriteSectionDetail(t.TempDir(), tc.sections, ds, ag); err == nil {
				t.Error("expected an error")
			}
		})
	}
}
