package application_test

import (
	"fmt"
	"math"
	"reflect"
	"sync"
	"testing"

	refdata "github.com/le-marais/claimsgen/data/reference"
	"github.com/le-marais/claimsgen/internal/application"
	"github.com/le-marais/claimsgen/internal/domain/transaction"
	"github.com/le-marais/claimsgen/internal/domain/triangle"
	"github.com/le-marais/claimsgen/internal/infrastructure/random"
	"github.com/le-marais/claimsgen/internal/infrastructure/schedulep"
)

// personalMotorRefs is the realism gate's reference pool: the private
// passenger auto companies embedded in the binary.
func personalMotorRefs(t *testing.T) []triangle.ReferenceSet {
	t.Helper()
	refs, err := schedulep.LoadFS(refdata.Files, refdata.PersonalMotorFile)
	if err != nil {
		t.Fatal(err)
	}
	return refs
}

// TestDefaultPresetIsRealistic is the MVP realism gate: data generated with
// the shipped motor-personal preset must land inside the bands observed
// across the Schedule P reference companies.
func TestDefaultPresetIsRealistic(t *testing.T) {
	refs := personalMotorRefs(t)
	req := request(t)
	req.StartYear = 1998
	req.Years = 10
	// 40k keeps the loss-ratio drift metric's seed-to-seed sampling noise
	// small enough for the 1.10 drift band: at 10k book size, heavy-tail
	// claim-sampling noise pushes ~12.5% of seeds outside [0.909, 1.10] even
	// with no systematic drift; at ~40k the metric stabilizes to about ±0.05.
	req.InitialBookSize = 40000
	// Run the gate on several seeds so a calibration that only happens to
	// pass on one seed is caught here.
	for _, seed := range []uint64{1, 42, 7} {
		seed := seed
		t.Run(fmt.Sprintf("seed=%d", seed), func(t *testing.T) {
			ds, err := application.GenerateDataset(t.Context(), random.NewSource(seed), req)
			if err != nil {
				t.Fatal(err)
			}
			report, err := application.EvaluateRealism(ds, req.StartYear, req.Years, req.LOB.Claims.ScoredSections(), refs)
			if err != nil {
				t.Fatal(err)
			}
			if !report.Pass() {
				t.Errorf("generated data outside Schedule P bands:\n%s", report)
			}
		})
	}
}

func TestEvaluateRealismProducesChecksAtEveryAge(t *testing.T) {
	refs := personalMotorRefs(t)
	req := request(t)
	req.Years = 10
	req.InitialBookSize = 2000
	ds, err := application.GenerateDataset(t.Context(), random.NewSource(1), req)
	if err != nil {
		t.Fatal(err)
	}
	report, err := application.EvaluateRealism(ds, req.StartYear, req.Years, req.LOB.Claims.ScoredSections(), refs)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.PaidATA) != 9 {
		t.Errorf("paid ATA checks = %d, want 9 (10 development years)", len(report.PaidATA))
	}
	if len(report.IncurredATA) != 9 {
		t.Errorf("incurred ATA checks = %d, want 9", len(report.IncurredATA))
	}
	if report.LossRatio.Value <= 0 {
		t.Errorf("loss ratio = %v, want positive", report.LossRatio.Value)
	}
}

// The reference is a liability line, so the gate must score the scored
// third-party sections alone: own-damage settlement speed cannot move them.
func TestRealismScoresOnlyTheScoredSections(t *testing.T) {
	refs := personalMotorRefs(t)
	report := func(ownDamageMeanDays float64) triangle.Report {
		req := request(t)
		req.Years = 10
		req.InitialBookSize = 3000
		req.LOB.Claims.Sections[ownDamage].CloseLag.MeanDays = ownDamageMeanDays
		ds, err := application.GenerateDataset(t.Context(), random.NewSource(5), req)
		if err != nil {
			t.Fatal(err)
		}
		report, err := application.EvaluateRealism(ds, req.StartYear, req.Years, req.LOB.Claims.ScoredSections(), refs)
		if err != nil {
			t.Fatal(err)
		}
		return report
	}
	if fast, slow := report(20), report(2000); !reflect.DeepEqual(fast, slow) {
		t.Fatalf("own-damage close lag moved the realism report:\nfast:\n%s\nslow:\n%s", fast, slow)
	}
}

func TestScoredSectionPremiumAndClaims(t *testing.T) {
	req := request(t)
	ds, err := application.GenerateDataset(t.Context(), random.NewSource(8), req)
	if err != nil {
		t.Fatal(err)
	}
	ag, err := application.Aggregate(ds, req.StartYear, req.Years, triangle.AccidentMonth)
	if err != nil {
		t.Fatal(err)
	}
	liability, err := application.SectionComparison(ds, req.StartYear, req.Years, req.LOB.Claims.ScoredSections())
	if err != nil {
		t.Fatal(err)
	}
	for i, ep := range liability.EarnedPremium {
		if ep <= 0 || ep >= ag.EarnedPremium[i] {
			t.Fatalf("year %d liability earned premium %v not a proper share of %v", i, ep, ag.EarnedPremium[i])
		}
	}
	tpPaid := 0.0
	section := map[int]int{}
	for _, c := range ds.Claims {
		section[c.ID] = c.Section
	}
	for _, tx := range ds.Transactions {
		if tx.Type == transaction.Payment && (section[tx.ClaimID] == thirdPartyProperty || section[tx.ClaimID] == thirdPartyInjury) {
			tpPaid += tx.Amount.Dollars()
		}
	}
	row := func(tr triangle.Triangle) float64 {
		sum := 0.0
		for _, r := range tr.Cells {
			sum += r[len(r)-1]
		}
		return sum
	}
	// Third-party claims carry no recoveries, so their net paid is gross paid.
	if got := row(liability.Paid); math.Abs(got-tpPaid) > 0.01 {
		t.Fatalf("liability paid triangle holds %v, want the third-party claims' total paid %v", got, tpPaid)
	}
	if row(liability.Paid) >= row(ag.Annual.Paid) {
		t.Fatal("liability triangle should exclude own-damage payments")
	}
}

// pooledLiabilityDrift is the liability section's loss-ratio drift - the
// second-half accident years' loss ratio over the first half's - pooled over
// seeds, so claim-sampling noise averages out. The seeds generate in
// parallel; the pooling order is fixed, so the result is deterministic.
func pooledLiabilityDrift(t *testing.T, req application.GenerateRequest, seeds []uint64) float64 {
	t.Helper()
	comps := make([]triangle.Comparison, len(seeds))
	errs := make([]error, len(seeds))
	var wg sync.WaitGroup
	for i, seed := range seeds {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ds, err := application.GenerateDataset(t.Context(), random.NewSource(seed), req)
			if err == nil {
				comps[i], err = application.SectionComparison(ds, req.StartYear, req.Years, req.LOB.Claims.ScoredSections())
			}
			errs[i] = err
		}()
	}
	wg.Wait()
	half := req.Years / 2
	var inc1, ep1, inc2, ep2 float64
	for i, c := range comps {
		if errs[i] != nil {
			t.Fatal(errs[i])
		}
		for j, row := range c.Incurred.Cells {
			switch {
			case j < half:
				inc1, ep1 = inc1+row[len(row)-1], ep1+c.EarnedPremium[j]
			case j >= req.Years-half:
				inc2, ep2 = inc2+row[len(row)-1], ep2+c.EarnedPremium[j]
			}
		}
	}
	return (inc2 / ep2) / (inc1 / ep1)
}

// systematicDriftTolerance bounds the preset's loss-ratio drift once the
// randomness pricing cannot know about is switched off. What remains is claim
// sampling: at a 40k book one seed's drift has a standard deviation of about
// 0.025 (mean 1.005 over 30 seeds), about 0.008 pooled over ten seeds, so
// +/-3.5% is over four standard deviations. Pricing that trends 1% a year apart from claims drifts by about
// 5% over the window.
const systematicDriftTolerance = 0.035

// MR-13: the realism report scores drift against the reference companies'
// wide spread, so this test is the guard against systematic drift: with the
// inflation path and pricing adequacy noise off, the model's loss ratio must
// not trend across the window. The second half checks that the guard can
// fail: pricing that trends 2% a year below claims must trip it.
func TestPresetHasNoSystematicLossRatioDrift(t *testing.T) {
	req := request(t)
	req.StartYear, req.Years, req.InitialBookSize = 1998, 10, 40000
	req.LOB.Claims.Inflation.Volatility = 0
	req.LOB.Pricing.AdequacyVolatility = 0

	if d := pooledLiabilityDrift(t, req, []uint64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}); math.Abs(d-1) > systematicDriftTolerance {
		t.Errorf("noise-free loss-ratio drift %.4f, want within %.3f of 1", d, systematicDriftTolerance)
	}

	lagging := req
	lagging.LOB.Pricing.InflationMean = req.LOB.Claims.Inflation.Mean - 0.02
	if d := pooledLiabilityDrift(t, lagging, []uint64{1, 2, 3}); math.Abs(d-1) <= systematicDriftTolerance {
		t.Errorf("pricing trending 2%% a year below claims gave drift %.4f, want outside %.3f of 1", d, systematicDriftTolerance)
	}
}

// Scoring several sections scores their union: their claims against their
// combined premium. Scoring every section is the whole book.
func TestRealismScoresTheUnionOfScoredSections(t *testing.T) {
	req := request(t)
	ds, err := application.GenerateDataset(t.Context(), random.NewSource(8), req)
	if err != nil {
		t.Fatal(err)
	}
	all, err := application.SectionComparison(ds, req.StartYear, req.Years, []int{ownDamage, thirdPartyProperty, thirdPartyInjury})
	if err != nil {
		t.Fatal(err)
	}
	book, err := application.SectionComparison(ds, req.StartYear, req.Years, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(all.Paid, book.Paid) || !reflect.DeepEqual(all.Incurred, book.Incurred) {
		t.Fatal("scoring every section should give the whole book's triangles")
	}
	// A policy's premium is rounded to the cent apart from its section
	// premiums, so the sum of sections can differ from it by a cent or two.
	for i, ep := range book.EarnedPremium {
		if math.Abs(all.EarnedPremium[i]-ep) > 1e-6*ep {
			t.Fatalf("year %d: sections' earned premium %v, whole book %v", i, all.EarnedPremium[i], ep)
		}
	}
}
