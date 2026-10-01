package schedulep_test

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"

	refdata "github.com/le-marais/claimsgen/data/reference"
	"github.com/le-marais/claimsgen/internal/infrastructure/schedulep"
)

const refDir = "../../../data/reference/schedule p/ppauto_pos98-07"

func TestLoadDirReadsAllCompanies(t *testing.T) {
	refs, err := schedulep.LoadDir(refDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 96 {
		t.Fatalf("loaded %d reference companies, want 96", len(refs))
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

func TestLoadFSEmbeddedMatchesDisk(t *testing.T) {
	embedded, err := schedulep.LoadFS(refdata.Files, refdata.PersonalMotorDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(embedded) != 96 {
		t.Fatalf("embedded reference sets = %d, want 96", len(embedded))
	}
	disk, err := schedulep.LoadDir(filepath.Join("../../../data/reference", refdata.PersonalMotorDir))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(embedded, disk) {
		t.Fatal("embedded reference sets differ from disk")
	}
}

func TestLoadDirEmptyNamesDirectory(t *testing.T) {
	dir := t.TempDir()
	_, err := schedulep.LoadDir(dir)
	if err == nil {
		t.Fatal("LoadDir on empty dir: want error, got nil")
	}
	if !strings.Contains(err.Error(), dir) {
		t.Fatalf("error %q does not name the directory %q", err, dir)
	}
}

func TestLoadFSErrorsOnEmptyDir(t *testing.T) {
	_, err := schedulep.LoadFS(fstest.MapFS{}, "schedule p/ppauto_pos98-07")
	if err == nil {
		t.Fatal("LoadFS on an empty FS: want error, got nil")
	}
}

// The later reported development completes every origin year to the full
// ten ages, leaving the triangle itself untouched (MR-4).
func TestLoadDevelopsIncurredToFullAge(t *testing.T) {
	ref, err := schedulep.LoadFile(filepath.Join(refDir, "10007.json"))
	if err != nil {
		t.Fatal(err)
	}
	dev := ref.DevelopedIncurred
	if dev.StartYear != 1998 || len(dev.Cells) != 10 {
		t.Fatalf("developed incurred: start %d, %d rows; want 1998, 10", dev.StartYear, len(dev.Cells))
	}
	for i, row := range dev.Cells {
		if len(row) != 10 {
			t.Fatalf("origin %d developed to %d ages, want 10", 1998+i, len(row))
		}
	}
	if got := dev.Cells[0][9]; got != 3422 {
		t.Errorf("1998 developed = %v, want 3422 (already at full age)", got)
	}
	if got := dev.Cells[1][9]; got != 4000 {
		t.Errorf("1999 developed = %v, want 4006 - 6 = 4000", got)
	}
	if got := dev.Cells[9][0]; got != 5329 {
		t.Errorf("2007 age 1 = %v, want the triangle's 5329", got)
	}
	if got := dev.Cells[9][9]; got != 5468 {
		t.Errorf("2007 developed = %v, want 5468", got)
	}
	if len(ref.Incurred.Cells[9]) != 1 {
		t.Errorf("triangle 2007 row = %v, want it left at one age", ref.Incurred.Cells[9])
	}
}

func TestLoadDevelopsEveryCompanyToFullAge(t *testing.T) {
	refs, err := schedulep.LoadDir(refDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, ref := range refs {
		for i, row := range ref.DevelopedIncurred.Cells {
			if len(row) != 10 {
				t.Fatalf("%s origin %d developed to %d ages, want 10", ref.Name, 1998+i, len(row))
			}
		}
	}
}

func TestLoadRejectsFutureDevelopmentOutsideTriangle(t *testing.T) {
	fsys := fstest.MapFS{"refs/x.json": {Data: []byte(`{
		"PaidTriangle": {"TriangleValues": [[1998, [1, 2]], [1999, [1]]]},
		"IncurredTriangle": {"TriangleValues": [[1998, [2, 2]], [1999, [2]]]},
		"FutureIncurred": [[1999, [1]], [2000, [1]]],
		"EarnedPremium": [[1998, 10], [1999, 10]]
	}`)}}
	if _, err := schedulep.LoadFS(fsys, "refs"); err == nil || !strings.Contains(err.Error(), "2000") {
		t.Fatalf("future development for an origin outside the triangle: want error naming 2000, got %v", err)
	}
}
