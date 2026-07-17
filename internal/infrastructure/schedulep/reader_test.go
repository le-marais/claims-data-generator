package schedulep_test

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"

	refdata "github.com/le-marais/claimsgen/data/reference"
	"github.com/le-marais/claimsgen/internal/domain/triangle"
	"github.com/le-marais/claimsgen/internal/infrastructure/schedulep"
)

const refDir = "../../../data/reference/schedule p/dec2025/ppauto_pos98-07"

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

func TestLoadFSEmbeddedMatchesDisk(t *testing.T) {
	embedded, err := schedulep.LoadFS(refdata.Files, refdata.PersonalMotorDirs...)
	if err != nil {
		t.Fatal(err)
	}
	if len(embedded) != 289 {
		t.Fatalf("embedded reference sets = %d, want 289", len(embedded))
	}
	var disk []triangle.ReferenceSet
	for _, dir := range refdata.PersonalMotorDirs {
		refs, err := schedulep.LoadDir(filepath.Join("../../../data/reference", dir))
		if err != nil {
			t.Fatal(err)
		}
		disk = append(disk, refs...)
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

const minimalRef = `{"ClassId":1,` +
	`"PaidTriangle":{"TriangleValues":[[1998,[100,150]],[1999,[120]]]},` +
	`"IncurredTriangle":{"TriangleValues":[[1998,[200,210]],[1999,[220]]]},` +
	`"EarnedPremium":[[1998,400],[1999,450]]}`

func TestLoadFSMergesDirsWithQualifiedNames(t *testing.T) {
	fsys := fstest.MapFS{
		"schedule p/dec2025/ppauto/10007.json": {Data: []byte(minimalRef)},
		"schedule p/sep2011/auto/10007.json":   {Data: []byte(minimalRef)},
	}
	refs, err := schedulep.LoadFS(fsys, "schedule p/dec2025/ppauto", "schedule p/sep2011/auto")
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 2 {
		t.Fatalf("loaded %d reference sets, want 2", len(refs))
	}
	if refs[0].Name != "dec2025/10007" || refs[1].Name != "sep2011/10007" {
		t.Errorf("names = %q, %q; want dec2025/10007, sep2011/10007", refs[0].Name, refs[1].Name)
	}
}

func TestLoadFSErrorsWhenAnyDirIsEmpty(t *testing.T) {
	fsys := fstest.MapFS{
		"schedule p/dec2025/ppauto/10007.json": {Data: []byte(minimalRef)},
	}
	_, err := schedulep.LoadFS(fsys, "schedule p/dec2025/ppauto", "schedule p/sep2011/auto")
	if err == nil {
		t.Fatal("LoadFS with an empty dir: want error, got nil")
	}
	if !strings.Contains(err.Error(), "schedule p/sep2011/auto") {
		t.Fatalf("error %q does not name the empty directory", err)
	}
}

func TestLoadFSErrorsOnNoDirs(t *testing.T) {
	_, err := schedulep.LoadFS(fstest.MapFS{})
	if err == nil {
		t.Fatal("LoadFS with no dirs: want error, got nil")
	}
}

func TestLoadDirQualifiesNamesByVintage(t *testing.T) {
	refs, err := schedulep.LoadDir(refDir)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(refs[0].Name, "dec2025/") {
		t.Errorf("Name = %q, want dec2025/ prefix", refs[0].Name)
	}
}
