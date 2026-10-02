package schedulep_test

import (
	"reflect"
	"strings"
	"testing"
	"testing/fstest"

	refdata "github.com/le-marais/claimsgen/data/reference"
	"github.com/le-marais/claimsgen/internal/domain/triangle"
	"github.com/le-marais/claimsgen/internal/infrastructure/schedulep"
)

const refFile = "../../../data/reference/schedule p/ppauto_pos98-07.csv"

const header = "GRCODE,GRNAME,AccidentYear,DevelopmentYear,DevelopmentLag,IncurredLosses,CumPaidLoss,BulkLoss,EarnedPremDIR,EarnedPremCeded,EarnedPremNet,Single,PostedReserves2007\r\n"

// twoYears is a two-year file in the CAS layout. Company 1 has every cell of
// the square; company 2 lacks accident year 2001 at lag 2.
const twoYears = header +
	"1,One,2000,2000,1,60,20,10,100,10,90,1,0\r\n" +
	"1,One,2000,2001,2,55,45,2,100,10,90,1,0\r\n" +
	"1,One,2001,2001,1,70,25,12,120,12,108,1,0\r\n" +
	"1,One,2001,2002,2,66,50,3,120,12,108,1,0\r\n" +
	"2,Two,2000,2000,1,30,10,5,50,0,50,1,0\r\n" +
	"2,Two,2000,2001,2,28,22,1,50,0,50,1,0\r\n" +
	"2,Two,2001,2001,1,35,12,6,60,0,60,1,0\r\n"

func load(t *testing.T, data string) ([]triangle.ReferenceSet, error) {
	t.Helper()
	return schedulep.LoadFS(fstest.MapFS{"x.csv": {Data: []byte(data)}}, "x.csv")
}

func TestLoadBuildsTheValuationTriangleAndItsDevelopment(t *testing.T) {
	refs, err := load(t, twoYears)
	if err != nil {
		t.Fatal(err)
	}
	want := []triangle.ReferenceSet{{
		Name:              "1",
		Company:           "One",
		Paid:              triangle.Triangle{StartYear: 2000, Cells: [][]float64{{20, 45}, {25}}},
		Incurred:          triangle.Triangle{StartYear: 2000, Cells: [][]float64{{60, 55}, {70}}},
		DevelopedIncurred: triangle.Triangle{StartYear: 2000, Cells: [][]float64{{60, 55}, {70, 66}}},
		EarnedPremium:     []float64{90, 108},
		DirectPremium:     []float64{100, 120},
	}}
	if !reflect.DeepEqual(refs, want) {
		t.Fatalf("got %+v\nwant %+v (company 2 lacks a cell and is left out)", refs, want)
	}
}

func TestLoadRejectsBadFiles(t *testing.T) {
	for _, tc := range []struct {
		name, data, want string
	}{
		{"empty", "", "empty"},
		{"header only", header, "no rows"},
		{"missing column", "GRCODE,GRNAME,AccidentYear,DevelopmentLag,IncurredLosses,CumPaidLoss,EarnedPremDIR\r\n1,One,2000,1,1,1,1\r\n", "EarnedPremNet"},
		{"bad number", header + "1,One,2000,2000,1,x,20,10,100,10,90,1,0\r\n", "line 2: IncurredLosses"},
		{"duplicate row", twoYears + "1,One,2000,2000,1,60,20,10,100,10,90,1,0\r\n", "duplicate"},
		{"no complete company", header + "2,Two,2000,2000,1,30,10,5,50,0,50,1,0\r\n2,Two,2001,2001,1,35,12,6,60,0,60,1,0\r\n", "no company"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := load(t, tc.data)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got error %v, want one containing %q", err, tc.want)
			}
		})
	}
}

func TestLoadFSErrorsOnMissingFile(t *testing.T) {
	if _, err := schedulep.LoadFS(fstest.MapFS{}, "missing.csv"); err == nil {
		t.Fatal("LoadFS on a missing file: want error, got nil")
	}
}

// 143 companies are in the file; 22 lack a cell of the ten-by-ten square.
func TestLoadFileReadsEveryCompleteCompany(t *testing.T) {
	refs, err := schedulep.LoadFile(refFile)
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 121 {
		t.Fatalf("loaded %d reference companies, want 121", len(refs))
	}
	for _, ref := range refs {
		for i, row := range ref.DevelopedIncurred.Cells {
			if len(row) != 10 || len(ref.Paid.Cells[i]) != 10-i || len(ref.Incurred.Cells[i]) != 10-i {
				t.Fatalf("%s origin %d: developed %d, paid %d, incurred %d ages; want 10, %d, %d",
					ref.Name, 1998+i, len(row), len(ref.Paid.Cells[i]), len(ref.Incurred.Cells[i]), 10-i, 10-i)
			}
		}
	}
}

func TestLoadKnownCompany(t *testing.T) {
	refs, err := schedulep.LoadFile(refFile)
	if err != nil {
		t.Fatal(err)
	}
	var ref triangle.ReferenceSet
	for _, r := range refs {
		if r.Name == "10007" {
			ref = r
		}
	}
	if ref.Company != "Nevada General Ins Co" {
		t.Fatalf("company 10007 = %q, want Nevada General Ins Co", ref.Company)
	}
	if ref.Paid.StartYear != 1998 {
		t.Errorf("Paid.StartYear = %d, want 1998", ref.Paid.StartYear)
	}
	if got := ref.Paid.Cells[0][0]; got != 1667 {
		t.Errorf("paid 1998 lag 1 = %v, want 1667", got)
	}
	if got := ref.Paid.Cells[0][9]; got != 3422 {
		t.Errorf("paid 1998 lag 10 = %v, want 3422", got)
	}
	if got := ref.Paid.Cells[9]; !reflect.DeepEqual(got, []float64{2357}) {
		t.Errorf("paid 2007 = %v, want [2357]", got)
	}
	if got := ref.Incurred.Cells[0][0]; got != 3938 {
		t.Errorf("incurred 1998 lag 1 = %v, want 3938", got)
	}
	// Net premium is the loss ratio's denominator; direct premium is kept
	// for the net-to-direct ratio.
	if got := ref.EarnedPremium[0]; got != 8971 {
		t.Errorf("net premium 1998 = %v, want 8971", got)
	}
	if got := ref.DirectPremium[0]; got != 9347 {
		t.Errorf("direct premium 1998 = %v, want 9347", got)
	}
	dev := ref.DevelopedIncurred
	if got := dev.Cells[1][9]; got != 4000 {
		t.Errorf("1999 developed = %v, want 4000", got)
	}
	if got := dev.Cells[9][0]; got != 5329 {
		t.Errorf("2007 lag 1 = %v, want the triangle's 5329", got)
	}
	if got := dev.Cells[9][9]; got != 5468 {
		t.Errorf("2007 developed = %v, want 5468", got)
	}
}

func TestLoadFSEmbeddedMatchesDisk(t *testing.T) {
	embedded, err := schedulep.LoadFS(refdata.Files, refdata.PersonalMotorFile)
	if err != nil {
		t.Fatal(err)
	}
	disk, err := schedulep.LoadFile(refFile)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(embedded, disk) {
		t.Fatal("embedded reference sets differ from disk")
	}
}
