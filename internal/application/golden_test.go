package application_test

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
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
const wantHash = "0c9df2b63bfc3a9107d8be7b308eecfed110359cdba1d01ab173ce95a08031ed"

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
const wantAggregateHash = "3b29bf1d47a657ff8e5e9c6c197229705921f236dd4724f3455a8a4786456ae0"

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

// wantAnnualHash pins ag.Annual's three cumulative triangles - Paid, NetPaid
// and Incurred - plus the section comparison the realism gate scores (its
// net paid and incurred triangles and earned premium, from
// SectionComparison), for the same small deterministic dataset. It exists because
// the annual triangles are a coarsened view of the monthly grid, derived by
// Coarsen keying both axes on calendar period rather than computed directly,
// and the branch that introduced that derivation stated as an invariant that
// the annual triangles are unchanged, cell for cell, from what the prior
// direct computation produced. TestDefaultPresetIsRealistic's P5-P95 bands
// are wide enough to absorb a real shift without failing, so this digest is
// the only thing that would catch one: the Schedule P realism bands and the
// shipped preset's calibration both depend on these cells not moving.
// Regenerate it the same way as wantHash: run the test once, it prints the
// actual value, paste it back in. Do not update it to hide an unintended
// change.
const wantAnnualHash = "994d4856b0d35c959d0174ef20bc3a56b493418922e6baf5432c5f7261b065f8"

func TestGoldenAnnualTriangles(t *testing.T) {
	req := request(t)
	ds, err := application.GenerateDataset(t.Context(), random.NewSource(1), req)
	if err != nil {
		t.Fatal(err)
	}
	ag, err := application.Aggregate(ds, req.StartYear, req.Years, triangle.AccidentMonth)
	if err != nil {
		t.Fatal(err)
	}
	liability, err := application.SectionComparison(ds, req.StartYear, req.Years, req.LOB.Claims.ScoredSection())
	if err != nil {
		t.Fatal(err)
	}
	h := sha256.New()
	for _, tri := range []triangle.Triangle{
		ag.Annual.Paid, ag.Annual.NetPaid, ag.Annual.Incurred,
		liability.Paid, liability.Incurred,
	} {
		for o, row := range tri.Cells {
			for d, v := range row {
				fmt.Fprintf(h, "%d,%d,%s\n", o, d, strconv.FormatFloat(v, 'f', 2, 64))
			}
		}
	}
	for y, ep := range liability.EarnedPremium {
		fmt.Fprintf(h, "ep,%d,%s\n", y, strconv.FormatFloat(ep, 'f', 2, 64))
	}
	got := hex.EncodeToString(h.Sum(nil))
	if got != wantAnnualHash {
		t.Fatalf("golden annual triangle hash mismatch:\n got: %s\nwant: %s", got, wantAnnualHash)
	}
}
