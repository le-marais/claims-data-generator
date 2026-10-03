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
	"github.com/le-marais/claimsgen/internal/infrastructure/config"
	csvout "github.com/le-marais/claimsgen/internal/infrastructure/csv"
	"github.com/le-marais/claimsgen/internal/infrastructure/random"
)

// wantHash pins the byte-stable CSV output for a small deterministic dataset.
// It guards against unintended changes to the generated data or its CSV
// encoding. If a change to the output is intentional, regenerate this digest
// by running the test once (it prints the actual value) and paste it back in.
const wantHash = "b34450d80eef0346e2676079ce347e5d5632b1b4bfe21c656126ca7329e90b36"

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
const wantAggregateHash = "3cb53e456f13073edbc444da37e4d210d28d7df062736bd71148a065dd363391"

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
// direct computation produced. TestPresetsAreRealistic's P5-P95 bands
// are wide enough to absorb a real shift without failing, so this digest is
// the only thing that would catch one: the Schedule P realism bands and the
// shipped preset's calibration both depend on these cells not moving.
// Regenerate it the same way as wantHash: run the test once, it prints the
// actual value, paste it back in. Do not update it to hide an unintended
// change.
const wantAnnualHash = "bf0436a10a708b47788102e664b45b60c7bbe6921f86abbaba9f1717e68f7944"

// wantCommercialAnnualHash pins the same cells for the commercial motor
// preset, whose calibration depends on them in the same way. Regenerate it
// the same way.
const wantCommercialAnnualHash = "424acfc5b273cfdf5e0deb10536eac8b62940d1e10921831902cfdf6c19aca35"

func TestGoldenAnnualTriangles(t *testing.T) {
	commercial := request(t)
	l, err := config.Preset("motor-commercial")
	if err != nil {
		t.Fatal(err)
	}
	commercial.LOB, commercial.InitialBookSize = l, 150
	for _, c := range []struct {
		preset string
		req    application.GenerateRequest
		want   string
	}{
		{"motor-personal", request(t), wantAnnualHash},
		{"motor-commercial", commercial, wantCommercialAnnualHash},
	} {
		if got := annualHash(t, c.preset, c.req); got != c.want {
			t.Errorf("%s: golden annual triangle hash mismatch:\n got: %s\nwant: %s", c.preset, got, c.want)
		}
	}
}

// annualHash digests a run's annual triangles and its realism comparison.
func annualHash(t *testing.T, presetID string, req application.GenerateRequest) string {
	t.Helper()
	ds, err := application.GenerateDataset(t.Context(), random.NewSource(1), req)
	if err != nil {
		t.Fatal(err)
	}
	ag, err := application.Aggregate(ds, req.StartYear, req.Years, triangle.AccidentMonth)
	if err != nil {
		t.Fatal(err)
	}
	liability, err := application.SectionComparison(ds, req.StartYear, req.Years, scoredSections(t, presetID, req.LOB))
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
	return hex.EncodeToString(h.Sum(nil))
}
