package application_test

import (
	"fmt"
	"math"
	"reflect"
	"testing"

	refdata "github.com/le-marais/claimsgen/data/reference"
	"github.com/le-marais/claimsgen/internal/application"
	"github.com/le-marais/claimsgen/internal/domain/transaction"
	"github.com/le-marais/claimsgen/internal/domain/triangle"
	"github.com/le-marais/claimsgen/internal/infrastructure/random"
	"github.com/le-marais/claimsgen/internal/infrastructure/schedulep"
)

// TestDefaultPresetIsRealistic is the MVP realism gate: data generated with
// the shipped motor-personal preset must land inside the bands observed
// across the Schedule P reference companies.
func TestDefaultPresetIsRealistic(t *testing.T) {
	refs, err := schedulep.LoadFS(refdata.Files, refdata.PersonalMotorDir)
	if err != nil {
		t.Fatal(err)
	}
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
			ag, err := application.Aggregate(ds, req.StartYear, req.Years, triangle.AccidentMonth)
			if err != nil {
				t.Fatal(err)
			}
			report := application.EvaluateRealism(ag, refs)
			if !report.Pass() {
				t.Errorf("generated data outside Schedule P bands:\n%s", report)
			}
		})
	}
}

func TestEvaluateRealismProducesChecksAtEveryAge(t *testing.T) {
	refs, err := schedulep.LoadFS(refdata.Files, refdata.PersonalMotorDir)
	if err != nil {
		t.Fatal(err)
	}
	req := request(t)
	req.Years = 10
	req.InitialBookSize = 2000
	ds, err := application.GenerateDataset(t.Context(), random.NewSource(1), req)
	if err != nil {
		t.Fatal(err)
	}
	ag, err := application.Aggregate(ds, req.StartYear, req.Years, triangle.AccidentMonth)
	if err != nil {
		t.Fatal(err)
	}
	report := application.EvaluateRealism(ag, refs)
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

// The reference is a liability line, so the gate must score the third-party
// section alone: own-damage settlement speed cannot move it.
func TestRealismScoresOnlyTheLiabilitySection(t *testing.T) {
	refs, err := schedulep.LoadFS(refdata.Files, refdata.PersonalMotorDir)
	if err != nil {
		t.Fatal(err)
	}
	report := func(ownDamageMeanDays float64) triangle.Report {
		req := request(t)
		req.Years = 10
		req.InitialBookSize = 3000
		req.LOB.Claims.CloseLag.MeanDays = ownDamageMeanDays
		ds, err := application.GenerateDataset(t.Context(), random.NewSource(5), req)
		if err != nil {
			t.Fatal(err)
		}
		ag, err := application.Aggregate(ds, req.StartYear, req.Years, triangle.AccidentMonth)
		if err != nil {
			t.Fatal(err)
		}
		return application.EvaluateRealism(ag, refs)
	}
	if fast, slow := report(20), report(2000); !reflect.DeepEqual(fast, slow) {
		t.Fatalf("own-damage close lag moved the realism report:\nfast:\n%s\nslow:\n%s", fast, slow)
	}
}

func TestLiabilitySectionPremiumAndClaims(t *testing.T) {
	req := request(t)
	ds, err := application.GenerateDataset(t.Context(), random.NewSource(8), req)
	if err != nil {
		t.Fatal(err)
	}
	ag, err := application.Aggregate(ds, req.StartYear, req.Years, triangle.AccidentMonth)
	if err != nil {
		t.Fatal(err)
	}
	for i, ep := range ag.LiabilityEarnedPremium {
		if ep <= 0 || ep >= ag.EarnedPremium[i] {
			t.Fatalf("year %d liability earned premium %v not a proper share of %v", i, ep, ag.EarnedPremium[i])
		}
	}
	tpPaid := 0.0
	byClaim := map[int]bool{}
	for _, c := range ds.Claims {
		byClaim[c.ID] = c.OwnDamage
	}
	for _, tx := range ds.Transactions {
		if tx.Type == transaction.Payment && !byClaim[tx.ClaimID] {
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
	if got := row(ag.Liability.Paid); math.Abs(got-tpPaid) > 0.01 {
		t.Fatalf("liability paid triangle holds %v, want the third-party claims' total paid %v", got, tpPaid)
	}
	if row(ag.Liability.Paid) >= row(ag.Annual.Paid) {
		t.Fatal("liability triangle should exclude own-damage payments")
	}
}
