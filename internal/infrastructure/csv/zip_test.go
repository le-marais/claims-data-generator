package csv_test

import (
	"archive/zip"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/le-marais/claimsgen/internal/application"
	"github.com/le-marais/claimsgen/internal/domain/triangle"
	csvout "github.com/le-marais/claimsgen/internal/infrastructure/csv"
)

// The zip holds exactly the five files the directory writers produce, byte
// for byte, in a fixed order.
func TestWriteZipHoldsTheFiveCSVs(t *testing.T) {
	ds := dataset(t)
	ag, err := application.Aggregate(ds, 1998, 2, triangle.AccidentMonth)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := csvout.WriteDataset(dir, ds); err != nil {
		t.Fatal(err)
	}
	if err := csvout.WriteAggregates(dir, ag); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := csvout.WriteZip(&buf, ds, ag); err != nil {
		t.Fatal(err)
	}
	r, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"policies.csv", "claims.csv", "transactions.csv", "triangles.csv", "exposure.csv"}
	if len(r.File) != len(want) {
		t.Fatalf("zip holds %d files, want %d", len(r.File), len(want))
	}
	for i, f := range r.File {
		if f.Name != want[i] {
			t.Fatalf("zip entry %d is %s, want %s", i, f.Name, want[i])
		}
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		got, err := io.ReadAll(rc)
		if cerr := rc.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			t.Fatal(err)
		}
		onDisk, err := os.ReadFile(filepath.Join(dir, f.Name))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, onDisk) {
			t.Errorf("zipped %s differs from the file WriteDataset/WriteAggregates write", f.Name)
		}
	}
}

// Equal runs give equal archives: every entry carries the same fixed
// timestamp.
func TestWriteZipIsByteIdentical(t *testing.T) {
	ds := dataset(t)
	ag, err := application.Aggregate(ds, 1998, 2, triangle.AccidentMonth)
	if err != nil {
		t.Fatal(err)
	}
	var a, b bytes.Buffer
	if err := csvout.WriteZip(&a, ds, ag); err != nil {
		t.Fatal(err)
	}
	if err := csvout.WriteZip(&b, ds, ag); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a.Bytes(), b.Bytes()) {
		t.Fatal("two archives of the same run differ")
	}
}
