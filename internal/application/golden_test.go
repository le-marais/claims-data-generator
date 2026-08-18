package application_test

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/le-marais/claimsgen/internal/application"
	"github.com/le-marais/claimsgen/internal/domain/triangle"
	csvout "github.com/le-marais/claimsgen/internal/infrastructure/csv"
	"github.com/le-marais/claimsgen/internal/infrastructure/random"
)

// wantHash pins the byte-stable CSV output for a small deterministic dataset.
// It guards against unintended changes to the generated data or its CSV
// encoding. If a change to the output is intentional, regenerate this digest
// by running the test once (it prints the actual value) and paste it back in.
const wantHash = "c68a402d4702cdc4d37dc9850072f3409db2da91496e9752b4c5f526ee372828"

func TestGoldenCSVBytes(t *testing.T) {
	ds, err := application.GenerateDataset(t.Context(), random.NewSource(1), request(t))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := csvout.WriteDataset(dir, ds); err != nil {
		t.Fatal(err)
	}
	h := sha256.New()
	for _, name := range []string{"policies.csv", "claims.csv", "transactions.csv"} {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		h.Write(b)
	}
	got := hex.EncodeToString(h.Sum(nil))
	if got != wantHash {
		t.Fatalf("golden CSV hash mismatch:\n got: %s\nwant: %s", got, wantHash)
	}
}

// wantAggregateHash pins the byte-stable aggregate CSV output for the same
// small deterministic dataset. Regenerate it the same way as wantHash: run the
// test once, it prints the actual value, paste it back in. Do not update it to
// hide an unintended change.
const wantAggregateHash = "0caf6b5dd893de0c1d4d3da19c6f420e679ba7a4d30851c4e90598d5945d0950"

func TestGoldenAggregateCSVBytes(t *testing.T) {
	req := request(t)
	ds, err := application.GenerateDataset(t.Context(), random.NewSource(1), req)
	if err != nil {
		t.Fatal(err)
	}
	ag, err := application.Aggregate(ds, req.StartYear, req.Years, triangle.AccidentMonth)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := csvout.WriteAggregates(dir, ag); err != nil {
		t.Fatal(err)
	}
	h := sha256.New()
	for _, name := range []string{"triangles.csv", "exposure.csv"} {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		h.Write(b)
	}
	got := hex.EncodeToString(h.Sum(nil))
	if got != wantAggregateHash {
		t.Fatalf("golden aggregate CSV hash mismatch:\n got: %s\nwant: %s", got, wantAggregateHash)
	}
}
