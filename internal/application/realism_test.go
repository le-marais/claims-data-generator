package application_test

import (
	"fmt"
	"math"
	"reflect"
	"sync"
	"testing"
	"time"

	refdata "github.com/le-marais/claimsgen/data/reference"
	"github.com/le-marais/claimsgen/internal/application"
	"github.com/le-marais/claimsgen/internal/domain/claim"
	"github.com/le-marais/claimsgen/internal/domain/policy"
	"github.com/le-marais/claimsgen/internal/domain/shared"
	"github.com/le-marais/claimsgen/internal/domain/transaction"
	"github.com/le-marais/claimsgen/internal/domain/triangle"
	"github.com/le-marais/claimsgen/internal/infrastructure/random"
	"github.com/le-marais/claimsgen/internal/infrastructure/schedulep"
)

// personalMotorRefs is the realism gate's reference pool: the embedded
// private passenger auto companies that PersonalMotorCriteria selects.
func personalMotorRefs(t *testing.T) []triangle.ReferenceSet {
	t.Helper()
	all, err := schedulep.LoadFS(refdata.Files, refdata.LineFiles[application.PrivatePassengerAuto])
	if err != nil {
		t.Fatal(err)
	}
	return triangle.SelectReferences(all, application.PersonalMotorCriteria())
}

// TestDefaultPresetIsRealistic is the MVP realism gate: data generated with
// the shipped motor-personal preset must land inside the bands observed
// across the Schedule P reference companies.
func TestDefaultPresetIsRealistic(t *testing.T) {
	refs := personalMotorRefs(t)
	req := request(t)
	req.StartYear = 1998
	req.Years = 10
	// 100k keeps claim sampling noise in the late single-origin factors and
	// in the drift below the spread of the reference pool, which is made of
	// books of $5m a year and up: at 40k the incurred factor at age 9-10 left
	// its band on 4 of 60 seeds by luck, at 100k on none of 30.
	req.InitialBookSize = 100000
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
	if len(report.PaidShares) != 9 {
		t.Errorf("paid share checks = %d, want 9", len(report.PaidShares))
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

// MR-15: the gate's pool is the complete companies with steady premium and
// reinsurance that write at least $5m a year, less reinsurers.
func TestPersonalMotorPool(t *testing.T) {
	all, err := schedulep.LoadFS(refdata.Files, refdata.LineFiles[application.PrivatePassengerAuto])
	if err != nil {
		t.Fatal(err)
	}
	c := application.PersonalMotorCriteria()
	var got []string
	for _, r := range triangle.SelectReferences(all, c) {
		got = append(got, r.Name)
	}
	want := []string{
		"353", "460", "620", "1066", "1090", "1538", "1716", "1767", "2003", "2143",
		"2208", "3240", "4839", "5185", "6947", "7080", "8427", "8672", "10007", "10022",
		"13420", "13501", "13889", "14044", "14176", "14257", "14311", "14443", "15024", "15199",
		"15997", "18163", "19119", "23574", "25755", "27022", "27065", "29440", "31062", "31550",
		"34509", "34592", "35173", "37028", "41041",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("pool = %v\nwant %v", got, want)
	}
	reasons := map[string]string{}
	for _, r := range all {
		reasons[r.Name] = c.Reason(r)
	}
	for name, want := range map[string]string{
		"29297": "net premium varies too much (CV 0.975)",         // fronts: keeps 5% of its direct premium
		"13641": "net-to-direct ratio varies too much (CV 0.392)", // kept $4k of $14.0m direct in 2007
		"10308": "net premium varies too much (CV 0.519)",         // about $70k a year, shrinking
		"20430": "too small (mean net premium 2986)",              // cedes a steady 75%, which net premium makes fair
		"33499": "reinsurer (Dorinco Rein Co)",
	} {
		if reasons[name] != want {
			t.Errorf("company %s: Reason = %q, want %q", name, reasons[name], want)
		}
	}
}

// The commercial auto pool is the complete companies inside Meyers' limits
// for the line that write at least $1m a year. No reinsurer passes the
// limits, so none is excluded by name.
func TestCommercialAutoPool(t *testing.T) {
	all, err := schedulep.LoadFS(refdata.Files, refdata.LineFiles[application.CommercialAuto])
	if err != nil {
		t.Fatal(err)
	}
	c := application.CommercialAutoCriteria()
	var got []string
	for _, r := range triangle.SelectReferences(all, c) {
		got = append(got, r.Name)
	}
	want := []string{
		"353", "620", "833", "965", "1066", "1090", "1538", "1767", "2135", "2143",
		"2712", "3240", "4839", "5185", "6408", "6947", "7080", "8079", "10022", "11126",
		"12866", "13439", "13501", "13528", "13889", "14044", "14176", "14257", "14370", "18163",
		"18767", "19020", "20690", "21172", "21270", "23574", "23663", "31550", "40568", "41300",
		"44130", "44415",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("pool = %v\nwant %v", got, want)
	}
	reasons := map[string]string{}
	for _, r := range all {
		reasons[r.Name] = c.Reason(r)
	}
	for name, want := range map[string]string{
		"1716":  "too small (mean net premium 913)",
		"27065": "too small (mean net premium 999)",
	} {
		if reasons[name] != want {
			t.Errorf("company %s: Reason = %q, want %q", name, reasons[name], want)
		}
	}
}

// MR-18: Schedule P values every company at age 10, so the gate drops
// generated development after age 10 rather than folding it into the last
// age, which on a long-tail line would compare an ultimate with a reference
// short of it.
func TestSectionComparisonStopsAtAgeTen(t *testing.T) {
	ds := application.Dataset{
		Policies: []policy.Policy{{
			ID: 1, CoverStart: shared.NewDate(1998, time.January, 1), CoverEnd: shared.NewDate(1998, time.December, 31),
			Premium: shared.FromDollars(1000),
		}},
		Claims: []claim.Claim{{
			ID: 1, PolicyID: 1, OccurrenceDate: shared.NewDate(1998, time.March, 1),
			Episodes: []claim.Episode{{
				Open: shared.NewDate(1998, time.April, 1), Close: shared.NewDate(2009, time.June, 1), Ultimate: shared.FromDollars(500),
			}},
		}},
		Transactions: []transaction.Transaction{
			{ID: 1, ClaimID: 1, Type: transaction.Payment, Date: shared.NewDate(1998, time.May, 1), Amount: shared.FromDollars(300)},
			// Development year 12.
			{ID: 2, ClaimID: 1, Type: transaction.Payment, Date: shared.NewDate(2009, time.June, 1), Amount: shared.FromDollars(200)},
		},
	}
	c, err := application.SectionComparison(ds, 1998, 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := c.Paid.Cells[0]; len(got) != 10 || got[9] != 300 {
		t.Fatalf("paid 1998 = %v, want ten ages ending at the 300 paid by age 10", got)
	}
}
