package transaction_test

import (
	"math"
	"testing"

	"github.com/le-marais/claimsgen/internal/domain/claim"
	"github.com/le-marais/claimsgen/internal/domain/lob"
	"github.com/le-marais/claimsgen/internal/domain/shared"
	"github.com/le-marais/claimsgen/internal/domain/transaction"
	"github.com/le-marais/claimsgen/internal/infrastructure/random"
)

func estimated(p lob.RunoffParams, claims []claim.Claim, seed uint64) []claim.Claim {
	out := append([]claim.Claim(nil), claims...)
	return transaction.NewCaseEstimator(p, sections()).Apply(random.NewSource(seed), out)
}

func TestCaseAdequacyMeanIsTheUltimateOverTheOpeningCase(t *testing.T) {
	claims := testClaims(5000)
	for _, mean := range []float64{0.8, 1.0, 1.25} {
		p := params()
		p.CaseAdequacyMean = mean
		ultimate, opening := 0.0, 0.0
		for _, c := range estimated(p, claims, 2) {
			ultimate += c.Episodes[0].Ultimate.Dollars()
			opening += c.InitialEstimate().Dollars()
		}
		if got := ultimate / opening; math.Abs(got-mean) > 0.05*mean {
			t.Errorf("adequacy mean %v: total ultimate / total opening case = %v", mean, got)
		}
	}
}

func TestCaseEstimatorLeavesTheTrueCostAlone(t *testing.T) {
	claims := testClaims(200)
	p := params()
	p.CaseAdequacyMean = 1.25
	for i, c := range estimated(p, claims, 3) {
		if c.Cost() != claims[i].Cost() {
			t.Fatalf("claim %d true cost changed by case estimation", c.ID)
		}
		if c.InitialEstimate() < shared.OneCent {
			t.Fatalf("claim %d opens at %v, want at least one cent", c.ID, c.InitialEstimate())
		}
	}
}

func TestExactCaseEstimatesWithNoNoiseAndNoBias(t *testing.T) {
	claims := testClaims(50)
	reopen := claims[0].CloseDate().AddDays(30)
	claims[0].Episodes = append(claims[0].Episodes, claim.Episode{Open: reopen, Close: reopen.AddDays(30), Ultimate: shared.FromDollars(700)})
	p := params()
	p.CaseAdequacySigma = 0
	for _, c := range estimated(p, claims, 4) {
		for j, e := range c.Episodes {
			if e.OpeningCase != e.Ultimate {
				t.Fatalf("claim %d episode %d opens at %v, want exactly its ultimate %v", c.ID, j+1, e.OpeningCase, e.Ultimate)
			}
		}
	}
}

// Case adequacy is a reserving knob: it must move the case, never a payment.
func TestCaseAdequacyNeverMovesPayments(t *testing.T) {
	claims := testClaims(300)
	payments := func(mean float64) []transaction.Transaction {
		p := params()
		p.CaseAdequacyMean = mean
		var out []transaction.Transaction
		for _, tx := range transaction.NewRunoffSimulator(p, sections()).Simulate(random.NewSource(6), estimated(p, claims, 6)) {
			if tx.Type == transaction.Payment {
				tx.ID = 0 // IDs shift with the number of case rows
				out = append(out, tx)
			}
		}
		return out
	}
	a, b := payments(1.0), payments(1.3)
	if len(a) != len(b) {
		t.Fatalf("payment count moved with case adequacy: %d vs %d", len(a), len(b))
	}
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("payment %d moved with case adequacy: %+v vs %+v", i, a[i], b[i])
		}
	}
}

// sectionMean is a runoff section set whose first section overrides the case
// adequacy mean and whose second does not.
func sectionMean(mean float64) []lob.SectionParams {
	secs := sections(lob.SettlementParams{Share: 0.4}, lob.SettlementParams{Share: 0.4})
	secs[0].CaseAdequacyMean = mean
	return secs
}

func TestSectionCaseAdequacyMeanSetsEveryEpisodeOpeningCase(t *testing.T) {
	p := params()
	p.CaseAdequacySigma = 0
	p.CaseAdequacyMean = 1.0
	claims := testClaims(2)
	claims[1].Section = 1
	for i := range claims {
		first := claims[i].Episodes[0]
		first.Ultimate = shared.FromDollars(10000)
		reopen := first.Close.AddDays(30)
		claims[i].Episodes = []claim.Episode{first, {Open: reopen, Close: reopen.AddDays(30), Ultimate: shared.FromDollars(10000)}}
	}
	got := transaction.NewCaseEstimator(p, sectionMean(1/0.75)).Apply(random.NewSource(1), claims)
	for j, e := range got[0].Episodes {
		if want := shared.FromDollars(7500); e.OpeningCase != want {
			t.Errorf("overridden section episode %d opens at %v, want %v", j+1, e.OpeningCase, want)
		}
	}
	for j, e := range got[1].Episodes {
		if e.OpeningCase != e.Ultimate {
			t.Errorf("section without override episode %d opens at %v, want the runoff mean's %v", j+1, e.OpeningCase, e.Ultimate)
		}
	}
}

// A deficient section opens low and its revisions stay low early, then close
// the gap towards the true cost; dates and payments do not move.
func TestSectionCaseAdequacyMeanPersistsThenConverges(t *testing.T) {
	p := params()
	p.CaseAdequacySigma = 0
	p.RevisionSigma = 0
	p.RevisionsPerYear = 12
	c := testClaims(1)[0]
	c.Episodes[0].Ultimate = shared.FromDollars(10000)
	c.Episodes[0].Close = c.Episodes[0].Open.AddDays(700)
	claims := []claim.Claim{c}

	run := func(mean float64) []transaction.Transaction {
		secs := sections()
		secs[0].CaseAdequacyMean = mean
		est := transaction.NewCaseEstimator(p, secs).Apply(random.NewSource(5), append([]claim.Claim(nil), claims...))
		return transaction.NewRunoffSimulator(p, secs).Simulate(random.NewSource(5), est)
	}
	rows := run(1 / 0.75)
	base := run(1.0)

	var incurred, paid shared.Money
	var series []shared.Money // incurred after each ESTIMATE row
	for _, tx := range rows {
		switch tx.Type {
		case transaction.Estimate:
			incurred += tx.Amount
			series = append(series, paid+incurred)
		case transaction.Payment:
			paid += tx.Amount
		}
	}
	// series: opening, revisions..., terminal release (incurred = paid).
	if len(series) < 4 {
		t.Fatalf("only %d estimate rows", len(series))
	}
	ultimate := shared.FromDollars(10000).Dollars()
	if first := series[1].Dollars(); first >= 0.85*ultimate {
		t.Errorf("first revision incurred %v, want below 0.85 x ultimate", first)
	}
	if last := series[len(series)-2].Dollars(); last <= 0.97*ultimate {
		t.Errorf("last revision before close incurred %v, want above 0.97 x ultimate", last)
	}

	// Payments are identical to the unbiased run; only ESTIMATE rows differ.
	pay := func(txs []transaction.Transaction) (out []transaction.Transaction) {
		for _, tx := range txs {
			if tx.Type == transaction.Payment {
				tx.ID = 0
				out = append(out, tx)
			}
		}
		return out
	}
	a, b := pay(rows), pay(base)
	if len(a) != len(b) {
		t.Fatalf("payment rows %d vs %d", len(a), len(b))
	}
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("payment %d moved: %+v vs %+v", i, a[i], b[i])
		}
	}
	if rows[0].Amount == base[0].Amount {
		t.Errorf("opening case unchanged by the override: %v", rows[0].Amount)
	}
}
