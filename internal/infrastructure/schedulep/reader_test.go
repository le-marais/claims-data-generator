package schedulep_test

import (
	"path/filepath"
	"testing"

	"github.com/le-marais/claimsgen/internal/infrastructure/schedulep"
)

const refDir = "../../../data/reference/ppauto_pos98-07"

func TestLoadDirReadsAllCompanies(t *testing.T) {
	refs, err := schedulep.LoadDir(refDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) < 100 {
		t.Fatalf("loaded %d reference companies, want at least 100", len(refs))
	}
}

func TestLoadKnownCompany(t *testing.T) {
	ref, err := schedulep.LoadFile(filepath.Join(refDir, "10007.json"))
	if err != nil {
		t.Fatal(err)
	}
	if ref.Name != "10007" {
		t.Errorf("Name = %q, want 10007", ref.Name)
	}
	if ref.Paid.StartYear != 1998 {
		t.Errorf("Paid.StartYear = %d, want 1998", ref.Paid.StartYear)
	}
	if got := ref.Paid.Cells[0][0]; got != 1667 {
		t.Errorf("paid 1998 dev 0 = %v, want 1667", got)
	}
	if got := ref.Paid.Cells[0][9]; got != 3422 {
		t.Errorf("paid 1998 dev 9 = %v, want 3422", got)
	}
	if got := ref.Incurred.Cells[0][0]; got != 3938 {
		t.Errorf("incurred 1998 dev 0 = %v, want 3938", got)
	}
	if len(ref.Paid.Cells[9]) != 1 || ref.Paid.Cells[9][0] != 2357 {
		t.Errorf("paid 2007 = %v, want [2357]", ref.Paid.Cells[9])
	}
	if len(ref.EarnedPremium) != 10 || ref.EarnedPremium[0] != 9347 {
		t.Errorf("EarnedPremium = %v, want 10 entries starting 9347", ref.EarnedPremium)
	}
}
