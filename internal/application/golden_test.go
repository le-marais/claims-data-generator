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
const wantHash = "973b8d03e89a4e3e81de29f2cd7e9b1215747e970ef0354a08935c5b67ee5721"

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
const wantAggregateHash = "1d0321d4e5e982a07eb684225351b06f2db15b7e0e2e548cce8152a5b9ac5928"

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
// and Incurred - plus the liability comparison the realism gate scores (its
// net paid and incurred triangles and earned premium, from
// LiabilityComparison), for the same small deterministic dataset. It exists because
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
const wantAnnualHash = "f3ef89a47f451f79cb0077fdec57a398b10007e4b03a51cf49bccc7aa7590391"

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
	liability, err := application.LiabilityComparison(ds, req.StartYear, req.Years)
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
