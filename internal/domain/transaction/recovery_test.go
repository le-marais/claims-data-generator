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

// recoveryFixture runs the runoff over a mixed book - own-damage, third
// party, and nil claims - then applies recoveries with the given params.
func recoveryFixture(t *testing.T, p lob.RecoveryParams, seed uint64) ([]claim.Claim, []transaction.Transaction) {
	t.Helper()
	claims := testClaims(300)
	for i := range claims {
		claims[i].OwnDamage = i%3 != 0 // two thirds own damage
		if i%10 == 0 {
			claims[i].Nil = true
		}
	}
	txs := transaction.NewRunoffSimulator(params()).Simulate(random.NewSource(seed), claims)
	return claims, transaction.NewRecoverySimulator(p).Apply(random.NewSource(seed), claims, txs)
}

func TestRecoveriesOnlyOnOwnDamageNonNilClaims(t *testing.T) {
	certain := recoveryParams()
	certain.Salvage.Probability = 1
	certain.Subrogation.Probability = 1
	claims, txs := recoveryFixture(t, certain, 1)

	eligible := map[int]bool{}
	for _, c := range claims {
		eligible[c.ID] = c.OwnDamage && !c.Nil
	}
	got := map[int]bool{}
	for _, tx := range txs {
		if tx.Type.IsRecovery() {
			if !eligible[tx.ClaimID] {
				t.Fatalf("recovery on ineligible claim %d", tx.ClaimID)
			}
			got[tx.ClaimID] = true
		}
	}
	for _, c := range claims {
		if eligible[c.ID] && !got[c.ID] {
			t.Fatalf("eligible claim %d has no recovery with probability 1", c.ID)
		}
	}
}

func TestRecoveryBoundsAndDates(t *testing.T) {
	claims, txs := recoveryFixture(t, recoveryParams(), 2)
	closeDate := map[int]shared.Date{}
	for _, c := range claims {
		closeDate[c.ID] = c.CloseDate
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
	claims := testClaims(100)
	for i := range claims {
		claims[i].OwnDamage = true
	}
	before := transaction.NewRunoffSimulator(params()).Simulate(random.NewSource(4), claims)
	after := transaction.NewRecoverySimulator(off).Apply(random.NewSource(4), claims, before)
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
		lastDate[c.ID] = c.ReportDate
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
		closeDate[c.ID] = c.CloseDate
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
