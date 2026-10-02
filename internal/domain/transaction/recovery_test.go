package transaction_test

import (
	"testing"

	"github.com/le-marais/claimsgen/internal/domain/claim"
	"github.com/le-marais/claimsgen/internal/domain/lob"
	"github.com/le-marais/claimsgen/internal/domain/shared"
	"github.com/le-marais/claimsgen/internal/domain/transaction"
	"github.com/le-marais/claimsgen/internal/infrastructure/random"
)

func recoveryParams() lob.RecoveryParams {
	return lob.RecoveryParams{
		Salvage:     lob.RecoveryTypeParams{Probability: 0.5, MeanShare: 0.15, Concentration: 10, LagMedianDays: 21, LagSigma: 0.5},
		Subrogation: lob.RecoveryTypeParams{Probability: 0.5, MeanShare: 0.8, Concentration: 10, LagMedianDays: 180, LagSigma: 0.7},
	}
}

// Section indices in withSections: own damage allows recoveries, third party
// does not.
const (
	ownDamage  = 0
	thirdParty = 1
)

// withSections wraps recovery parameters in the claim parameters the
// recovery simulator reads: they decide which sections are eligible.
func withSections(p lob.RecoveryParams) lob.ClaimParams {
	return lob.ClaimParams{
		Sections: []lob.SectionParams{
			{Name: "own_damage", Severity: lob.SeverityParams{Kind: lob.SumInsuredLognormal}, Recoveries: true},
			{Name: "third_party"},
		},
		Recoveries: p,
	}
}

// recoveryFixture runs the runoff over a mixed book - own-damage, third
// party, and nil claims - then applies recoveries with the given params.
func recoveryFixture(t *testing.T, p lob.RecoveryParams, seed uint64) ([]claim.Claim, []transaction.Transaction) {
	t.Helper()
	claims := testClaims(300)
	for i := range claims {
		claims[i].Section = thirdParty
		if i%3 != 0 { // two thirds own damage
			claims[i].Section = ownDamage
		}
		if claims[i].Section == ownDamage && i%4 == 1 {
			claims[i].CoverLimit = claims[i].Episodes[0].Ultimate // a total loss: paid up to the cover limit
		}
		if i%10 == 0 {
			claims[i].Episodes[0].Nil = true
		}
	}
	txs := transaction.NewRunoffSimulator(params()).Simulate(random.NewSource(seed), claims)
	return claims, transaction.NewRecoverySimulator(withSections(p)).Apply(random.NewSource(seed), claims, txs)
}

// Subrogation attaches to paid claims in a section with recoveries, salvage
// only to paid total losses on a sum-insured section (MR-7).
func TestRecoveriesOnlyOnEligibleClaims(t *testing.T) {
	certain := recoveryParams()
	certain.Salvage.Probability = 1
	certain.Subrogation.Probability = 1
	claims, txs := recoveryFixture(t, certain, 1)

	eligible := map[transaction.Type]map[int]bool{transaction.Salvage: {}, transaction.Subrogation: {}}
	for _, c := range claims {
		eligible[transaction.Subrogation][c.ID] = c.Section == ownDamage && !c.Nil()
		eligible[transaction.Salvage][c.ID] = c.Section == ownDamage && c.TotalLoss() && !c.Nil()
	}
	got := map[transaction.Type]map[int]bool{transaction.Salvage: {}, transaction.Subrogation: {}}
	for _, tx := range txs {
		if tx.Type.IsRecovery() {
			if !eligible[tx.Type][tx.ClaimID] {
				t.Fatalf("%s on ineligible claim %d", tx.Type, tx.ClaimID)
			}
			got[tx.Type][tx.ClaimID] = true
		}
	}
	for typ, ids := range eligible {
		n := 0
		for id, ok := range ids {
			if ok {
				n++
				if !got[typ][id] {
					t.Fatalf("eligible claim %d has no %s with probability 1", id, typ)
				}
			}
		}
		if n == 0 {
			t.Fatalf("fixture has no claim eligible for %s", typ)
		}
	}
}

func TestRecoveryBoundsAndDates(t *testing.T) {
	claims, txs := recoveryFixture(t, recoveryParams(), 2)
	closeDate := map[int]shared.Date{}
	for _, c := range claims {
		closeDate[c.ID] = c.CloseDate()
	}
	paid := map[int]shared.Money{}
	recovered := map[int]shared.Money{}
	sawRecovery := false
	for _, tx := range txs {
		switch {
		case tx.Type == transaction.Payment:
			paid[tx.ClaimID] += tx.Amount
		case tx.Type.IsRecovery():
			sawRecovery = true
			if tx.Amount <= 0 {
				t.Fatalf("recovery %d amount %v not positive", tx.ID, tx.Amount)
			}
			if !closeDate[tx.ClaimID].Before(tx.Date) {
				t.Fatalf("recovery %d on %s not strictly after close %s", tx.ID, tx.Date, closeDate[tx.ClaimID])
			}
			recovered[tx.ClaimID] += tx.Amount
		}
	}
	if !sawRecovery {
		t.Fatal("fixture produced no recoveries")
	}
	for id, r := range recovered {
		if r >= paid[id] {
			t.Fatalf("claim %d recovered %v >= gross paid %v", id, r, paid[id])
		}
	}
}

func TestRecoveryOffSwitchPerType(t *testing.T) {
	noSalvage := recoveryParams()
	noSalvage.Salvage.Probability = 0
	_, txs := recoveryFixture(t, noSalvage, 3)
	sawSubro := false
	for _, tx := range txs {
		if tx.Type == transaction.Salvage {
			t.Fatalf("salvage row %d with salvage probability 0", tx.ID)
		}
		if tx.Type == transaction.Subrogation {
			sawSubro = true
		}
	}
	if !sawSubro {
		t.Fatal("expected subrogation rows with subrogation still on")
	}
}

func TestRecoveriesOffReturnsRunoffUnchanged(t *testing.T) {
	off := lob.RecoveryParams{
		Salvage:     lob.RecoveryTypeParams{Probability: 0, MeanShare: 0.15, Concentration: 10, LagMedianDays: 21, LagSigma: 0.5},
		Subrogation: lob.RecoveryTypeParams{Probability: 0, MeanShare: 0.8, Concentration: 10, LagMedianDays: 180, LagSigma: 0.7},
	}
	claims := testClaims(100) // every claim in the own-damage section
	before := transaction.NewRunoffSimulator(params()).Simulate(random.NewSource(4), claims)
	after := transaction.NewRecoverySimulator(withSections(off)).Apply(random.NewSource(4), claims, before)
	if len(after) != len(before) {
		t.Fatalf("lengths differ: %d vs %d", len(after), len(before))
	}
	for i := range after {
		if after[i] != before[i] {
			t.Fatalf("transaction %d changed with recoveries off", i)
		}
	}
}

func TestRecoveryMergeKeepsIDsSequentialAndClaimsChronological(t *testing.T) {
	claims, txs := recoveryFixture(t, recoveryParams(), 5)
	lastDate := map[int]shared.Date{}
	for _, c := range claims {
		lastDate[c.ID] = c.ReportDate()
	}
	for i, tx := range txs {
		if tx.ID != i+1 {
			t.Fatalf("transaction %d has ID %d, want %d", i, tx.ID, i+1)
		}
		if tx.Date.Before(lastDate[tx.ClaimID]) {
			t.Fatalf("claim %d rows not chronological at transaction %d", tx.ClaimID, tx.ID)
		}
		lastDate[tx.ClaimID] = tx.Date
	}
}

func TestRecoveryApplyIsDeterministic(t *testing.T) {
	_, a := recoveryFixture(t, recoveryParams(), 6)
	_, b := recoveryFixture(t, recoveryParams(), 6)
	if len(a) != len(b) {
		t.Fatalf("lengths differ: %d vs %d", len(a), len(b))
	}
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("transaction %d differs between identical runs", i)
		}
	}
}

func TestSalvageArrivesSoonerThanSubrogationOnAverage(t *testing.T) {
	claims, txs := recoveryFixture(t, recoveryParams(), 7)
	closeDate := map[int]shared.Date{}
	for _, c := range claims {
		closeDate[c.ID] = c.CloseDate()
	}
	var salvageSum, salvageN, subroSum, subroN float64
	for _, tx := range txs {
		lag := float64(shared.DaysBetween(closeDate[tx.ClaimID], tx.Date))
		switch tx.Type {
		case transaction.Salvage:
			salvageSum += lag
			salvageN++
		case transaction.Subrogation:
			subroSum += lag
			subroN++
		}
	}
	if salvageN == 0 || subroN == 0 {
		t.Fatalf("fixture drew %v salvage and %v subrogation rows, want both", salvageN, subroN)
	}
	if salvageSum/salvageN >= subroSum/subroN {
		t.Errorf("mean salvage lag %v days >= mean subrogation lag %v days, want salvage sooner",
			salvageSum/salvageN, subroSum/subroN)
	}
}

// A liability claim settled at its limit reaches its cover limit too, but
// there is no wreck to sell: salvage needs a sum-insured section, while
// subrogation still applies.
func TestNoSalvageOnALimitedSection(t *testing.T) {
	certain := recoveryParams()
	certain.Salvage.Probability = 1
	certain.Subrogation.Probability = 1
	for _, sev := range []lob.SeverityParams{
		{Kind: lob.Pareto, Scale: 4000, Alpha: 2.2},
		{Kind: lob.Lognormal, Median: 2000, Sigma: 0.8},
	} {
		p := lob.ClaimParams{
			Sections:   []lob.SectionParams{{Name: "liability", Severity: sev, Limit: 3000, Recoveries: true}},
			Recoveries: certain,
		}
		claims := testClaims(100)
		for i := range claims {
			claims[i].CoverLimit = claims[i].Episodes[0].Ultimate // settled at the limit
		}
		txs := transaction.NewRunoffSimulator(params()).Simulate(random.NewSource(1), claims)
		txs = transaction.NewRecoverySimulator(p).Apply(random.NewSource(1), claims, txs)
		subrogated := 0
		for _, tx := range txs {
			switch tx.Type {
			case transaction.Salvage:
				t.Fatalf("%s: salvage on limited liability claim %d", sev.Kind, tx.ClaimID)
			case transaction.Subrogation:
				subrogated++
			}
		}
		if subrogated == 0 {
			t.Fatalf("%s: no subrogation; the fixture did not exercise recoveries", sev.Kind)
		}
	}
}
