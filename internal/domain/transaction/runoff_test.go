package transaction_test

import (
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/le-marais/claimsgen/internal/domain/claim"
	"github.com/le-marais/claimsgen/internal/domain/lob"
	"github.com/le-marais/claimsgen/internal/domain/shared"
	"github.com/le-marais/claimsgen/internal/domain/transaction"
	"github.com/le-marais/claimsgen/internal/infrastructure/random"
)

func params() lob.RunoffParams {
	return lob.RunoffParams{
		CaseAdequacyMean:  1.0,
		CaseAdequacySigma: 0.35,
		PaymentsPerYear:   2.5,
		Concentration:     1.0,
		RevisionsPerYear:  4,
		RevisionSigma:     0.3,
	}
}

// sections returns one section of cover per settlement, in order, or one
// section with a fixed settlement share of 0.4 when none is given. Claims
// reach their section by index.
func sections(settlements ...lob.SettlementParams) []lob.SectionParams {
	if len(settlements) == 0 {
		settlements = []lob.SettlementParams{{Share: 0.4}}
	}
	secs := make([]lob.SectionParams, len(settlements))
	for i, st := range settlements {
		secs[i] = lob.SectionParams{Name: fmt.Sprintf("section-%d", i), Settlement: st}
	}
	return secs
}

// testClaims builds n claims with varying sizes and durations, each opening
// at its true cost.
func testClaims(n int) []claim.Claim {
	claims := make([]claim.Claim, n)
	for i := range claims {
		report := shared.NewDate(1998, time.March, 1).AddDays(i % 300)
		durations := []int{0, 10, 45, 180, 700}
		estimates := []float64{800, 3000, 12000, 40000, 250000}
		claims[i] = claim.Claim{
			ID:             i + 1,
			PolicyID:       i + 1,
			OccurrenceDate: report.AddDays(-2),
			Episodes: []claim.Episode{{
				Open:        report,
				Close:       report.AddDays(durations[i%5]),
				Ultimate:    shared.FromDollars(estimates[(i+2)%5]),
				OpeningCase: shared.FromDollars(estimates[(i+2)%5]),
			}},
			RiskFactor: 1.0,
		}
	}
	return claims
}

// byClaim groups transactions per claim, preserving order.
func byClaim(txs []transaction.Transaction) map[int][]transaction.Transaction {
	m := map[int][]transaction.Transaction{}
	for _, tx := range txs {
		m[tx.ClaimID] = append(m[tx.ClaimID], tx)
	}
	return m
}

func TestRunoffInvariants(t *testing.T) {
	claims := testClaims(500)
	sim := transaction.NewRunoffSimulator(params(), sections())
	txs := sim.Simulate(random.NewSource(1), claims)
	grouped := byClaim(txs)
	if len(grouped) != len(claims) {
		t.Fatalf("transactions cover %d claims, want %d", len(grouped), len(claims))
	}
	for _, c := range claims {
		rows := grouped[c.ID]
		if len(rows) < 3 {
			t.Fatalf("claim %d has %d transactions, want at least initial estimate, payment, and closing movement", c.ID, len(rows))
		}
		first := rows[0]
		if first.Type != transaction.Estimate || first.Amount != c.InitialEstimate() || first.Date != c.ReportDate() {
			t.Fatalf("claim %d first row = %+v, want initial ESTIMATE %v on %s", c.ID, first, c.InitialEstimate(), c.ReportDate())
		}
		outstanding := shared.Money(0)
		paid := shared.Money(0)
		prevDate := c.ReportDate()
		for _, tx := range rows {
			if tx.Date.Before(prevDate) {
				t.Fatalf("claim %d transactions not chronological", c.ID)
			}
			prevDate = tx.Date
			if tx.Date.Before(c.ReportDate()) || tx.Date.After(c.CloseDate()) {
				t.Fatalf("claim %d transaction on %s outside report..close %s..%s", c.ID, tx.Date, c.ReportDate(), c.CloseDate())
			}
			switch tx.Type {
			case transaction.Estimate:
				outstanding += tx.Amount
			case transaction.Payment:
				if tx.Amount <= 0 {
					t.Fatalf("claim %d non-positive payment %v", c.ID, tx.Amount)
				}
				paid += tx.Amount
			default:
				t.Fatalf("claim %d unknown transaction type %q", c.ID, tx.Type)
			}
			if outstanding < 0 {
				t.Fatalf("claim %d outstanding went negative", c.ID)
			}
		}
		if outstanding != 0 {
			t.Fatalf("claim %d outstanding at close = %v, want 0", c.ID, outstanding)
		}
		if paid <= 0 {
			t.Fatalf("claim %d total paid = %v, want positive", c.ID, paid)
		}
		last := rows[len(rows)-1]
		if last.Date != c.CloseDate() {
			t.Fatalf("claim %d last transaction on %s, want close date %s", c.ID, last.Date, c.CloseDate())
		}
	}
}

// finalShares returns, for each claim with an interim payment, its final
// payment as a share of its ultimate. Every test claim has one episode.
func finalShares(claims []claim.Claim, txs []transaction.Transaction) []float64 {
	grouped := byClaim(txs)
	var shares []float64
	for _, c := range claims {
		var payments []shared.Money
		for _, tx := range grouped[c.ID] {
			if tx.Type == transaction.Payment {
				payments = append(payments, tx.Amount)
			}
		}
		if len(payments) < 2 {
			continue
		}
		shares = append(shares, payments[len(payments)-1].Dollars()/c.Episodes[0].Ultimate.Dollars())
	}
	return shares
}

func TestSettlementShareIsFixedWithoutConcentration(t *testing.T) {
	claims := testClaims(500)
	txs := transaction.NewRunoffSimulator(params(), sections()).Simulate(random.NewSource(4), claims)
	shares := finalShares(claims, txs)
	if len(shares) < 50 {
		t.Fatalf("only %d claims have an interim payment, want at least 50", len(shares))
	}
	for i, s := range shares {
		// Interim payments round to the cent each, so allow a few cents.
		if math.Abs(s-0.4) > 0.001 {
			t.Fatalf("claim %d: final payment share = %v, want 0.4", i, s)
		}
	}
}

func TestSettlementShareVariesAroundItsMean(t *testing.T) {
	claims := testClaims(2000)
	sim := transaction.NewRunoffSimulator(params(), sections(lob.SettlementParams{Share: 0.4, Concentration: 4}))
	txs := sim.Simulate(random.NewSource(5), claims)
	shares := finalShares(claims, txs)
	if len(shares) < 200 {
		t.Fatalf("only %d claims have an interim payment, want at least 200", len(shares))
	}
	mean, sq := 0.0, 0.0
	for _, s := range shares {
		mean += s
		sq += s * s
	}
	mean /= float64(len(shares))
	sd := math.Sqrt(sq/float64(len(shares)) - mean*mean)
	if math.Abs(mean-0.4) > 0.03 {
		t.Errorf("mean final payment share = %.3f, want about 0.4", mean)
	}
	// Beta(1.6, 2.4) has a standard deviation of about 0.22.
	if sd < 0.15 || sd > 0.3 {
		t.Errorf("final payment share standard deviation = %.3f, want about 0.22", sd)
	}
}

// paymentsByClaim returns each claim's payment amounts in date order.
func paymentsByClaim(txs []transaction.Transaction) map[int][]shared.Money {
	pays := map[int][]shared.Money{}
	for _, tx := range txs {
		if tx.Type == transaction.Payment {
			pays[tx.ClaimID] = append(pays[tx.ClaimID], tx.Amount)
		}
	}
	return pays
}

func TestLumpSumPaysInOneSettlement(t *testing.T) {
	claims := testClaims(2000)
	paysOnce := func(st lob.SettlementParams) float64 {
		txs := transaction.NewRunoffSimulator(params(), sections(st)).Simulate(random.NewSource(6), claims)
		n := 0
		for _, pays := range paymentsByClaim(txs) {
			if len(pays) == 1 {
				n++
			}
		}
		return float64(n) / float64(len(claims))
	}
	if got := paysOnce(lob.SettlementParams{LumpSumProbability: 1}); got != 1 {
		t.Errorf("lump_sum_probability 1: %.3f of claims pay once, want all of them", got)
	}
	base := paysOnce(lob.SettlementParams{Share: 0.4})
	half := paysOnce(lob.SettlementParams{LumpSumProbability: 0.5, Share: 0.4})
	if want := base + (1-base)/2; math.Abs(half-want) > 0.03 {
		t.Errorf("lump_sum_probability 0.5: %.3f of claims pay once, want about %.3f (%.3f without lump sums)", half, want, base)
	}
}

func TestMinPaymentHoldsSmallPaymentsOver(t *testing.T) {
	claims := testClaims(1000)
	minimum := shared.FromDollars(500)
	small := func(p lob.RunoffParams) int {
		txs := transaction.NewRunoffSimulator(p, sections()).Simulate(random.NewSource(7), claims)
		pays := paymentsByClaim(txs)
		n := 0
		for _, c := range claims {
			total := shared.Money(0)
			for i, a := range pays[c.ID] {
				total += a
				if i < len(pays[c.ID])-1 && a < minimum {
					n++
				}
			}
			if total != c.Episodes[0].Ultimate {
				t.Fatalf("claim %d paid %v, want its ultimate %v", c.ID, total, c.Episodes[0].Ultimate)
			}
		}
		return n
	}
	if n := small(params()); n == 0 {
		t.Fatal("no interim payment below 500 without a minimum: the test claims cannot show the minimum")
	}
	p := params()
	p.MinPayment = 500
	if n := small(p); n != 0 {
		t.Errorf("%d interim payments below the 500 minimum", n)
	}
}

func TestClaimsSettleBySection(t *testing.T) {
	claims := testClaims(1000)
	for i := range claims {
		claims[i].Section = i % 2
	}
	sim := transaction.NewRunoffSimulator(params(), sections(lob.SettlementParams{Share: 0.4}, lob.SettlementParams{Share: 0.7}))
	txs := sim.Simulate(random.NewSource(8), claims)
	for sec, want := range []float64{0.4, 0.7} {
		var mine []claim.Claim
		for _, c := range claims {
			if c.Section == sec {
				mine = append(mine, c)
			}
		}
		shares := finalShares(mine, txs)
		if len(shares) < 20 {
			t.Fatalf("section %d: only %d claims have an interim payment", sec, len(shares))
		}
		for _, s := range shares {
			if math.Abs(s-want) > 0.001 {
				t.Fatalf("section %d: final payment share %v, want its section's %v", sec, s, want)
			}
		}
	}
}

// delayBreaches counts the payments that come less than delay days after
// the last ESTIMATE row that raised their claim's case, or less than delay
// days after the claim's previous payment.
func delayBreaches(txs []transaction.Transaction, delay int) int {
	n := 0
	for _, rows := range byClaim(txs) {
		var lastRaise, lastPay shared.Date
		paid := false
		for _, tx := range rows {
			switch {
			case tx.Type == transaction.Estimate && tx.Amount > 0:
				lastRaise = tx.Date
			case tx.Type == transaction.Payment:
				if shared.DaysBetween(lastRaise, tx.Date) < delay || (paid && shared.DaysBetween(lastPay, tx.Date) < delay) {
					n++
				}
				lastPay, paid = tx.Date, true
			}
		}
	}
	return n
}

// delayClaims are test claims that stay open at least delay days, opening at
// half their cost so bills have to raise the case; every fourth also reopens
// short of its additional cost.
func delayClaims(n, delay int) []claim.Claim {
	var claims []claim.Claim
	for _, c := range testClaims(n) {
		first := &c.Episodes[0]
		if shared.DaysBetween(first.Open, first.Close) < delay {
			continue
		}
		first.OpeningCase = first.Ultimate.MulFloat(0.5)
		if c.ID%4 == 0 {
			reopen := first.Close.AddDays(30)
			c.Episodes = append(c.Episodes, claim.Episode{
				Open:        reopen,
				Close:       reopen.AddDays(delay + c.ID%90),
				Ultimate:    first.Ultimate.MulFloat(0.3),
				OpeningCase: first.Ultimate.MulFloat(0.1),
			})
		}
		claims = append(claims, c)
	}
	return claims
}

func TestPaymentDelayFollowsEveryRaise(t *testing.T) {
	claims := delayClaims(1000, 7)
	p := params()
	p.PaymentDelayDays = 7
	p.Concentration = 4
	txs := transaction.NewRunoffSimulator(p, sections(lob.SettlementParams{Share: 0.4, Concentration: 4})).Simulate(random.NewSource(10), claims)
	if n := delayBreaches(txs, 7); n != 0 {
		t.Errorf("%d payments within 7 days of a raise or of the previous payment", n)
	}
	grouped := byClaim(txs)
	for _, c := range claims {
		outstanding, paid := shared.Money(0), shared.Money(0)
		for _, tx := range grouped[c.ID] {
			switch tx.Type {
			case transaction.Estimate:
				outstanding += tx.Amount
			case transaction.Payment:
				paid += tx.Amount
			}
			if outstanding < 0 {
				t.Fatalf("claim %d: outstanding went negative", c.ID)
			}
		}
		want := shared.Money(0)
		for _, ep := range c.Episodes {
			want += ep.Ultimate
		}
		if paid != want || outstanding != 0 {
			t.Fatalf("claim %d: paid %v with %v outstanding at close, want %v and 0", c.ID, paid, outstanding, want)
		}
	}
	// Without the delay the same claims break the rule, so the test can see it.
	p.PaymentDelayDays = 0
	plain := transaction.NewRunoffSimulator(p, sections(lob.SettlementParams{Share: 0.4, Concentration: 4})).Simulate(random.NewSource(10), claims)
	if delayBreaches(plain, 7) == 0 {
		t.Fatal("no breaches without the delay: the test claims cannot show it")
	}
}

func TestEveryPaymentHasMatchingEstimateReduction(t *testing.T) {
	claims := testClaims(200)
	sim := transaction.NewRunoffSimulator(params(), sections())
	txs := sim.Simulate(random.NewSource(2), claims)
	for i, tx := range txs {
		if tx.Type != transaction.Payment {
			continue
		}
		if i+1 >= len(txs) {
			t.Fatal("payment is the last transaction overall; missing estimate reduction")
		}
		next := txs[i+1]
		if next.Type != transaction.Estimate || next.ClaimID != tx.ClaimID || next.Date != tx.Date || next.Amount != -tx.Amount {
			t.Fatalf("payment %+v not followed by matching estimate reduction, got %+v", tx, next)
		}
	}
}

func TestTotalPaidIsExactlyTheUltimate(t *testing.T) {
	claims := testClaims(400)
	// Open every case well away from the truth: payments must not follow it.
	for i := range claims {
		claims[i].Episodes[0].OpeningCase = claims[i].Episodes[0].Ultimate.MulFloat(0.5)
	}
	txs := transaction.NewRunoffSimulator(params(), sections()).Simulate(random.NewSource(3), claims)
	paid := map[int]shared.Money{}
	for _, tx := range txs {
		if tx.Type == transaction.Payment {
			paid[tx.ClaimID] += tx.Amount
		}
	}
	for _, c := range claims {
		if paid[c.ID] != c.Episodes[0].Ultimate {
			t.Fatalf("claim %d paid %v, want its ultimate %v", c.ID, paid[c.ID], c.Episodes[0].Ultimate)
		}
	}
}

func TestSameDayCloseSettlesInFull(t *testing.T) {
	c := claim.Claim{
		ID:             1,
		PolicyID:       1,
		OccurrenceDate: shared.NewDate(1998, time.May, 1),
		Episodes: []claim.Episode{{
			Open:        shared.NewDate(1998, time.May, 3),
			Close:       shared.NewDate(1998, time.May, 3),
			Ultimate:    shared.FromDollars(1000),
			OpeningCase: shared.FromDollars(1000),
		}},
		RiskFactor: 1.0,
	}
	sim := transaction.NewRunoffSimulator(params(), sections())
	txs := sim.Simulate(random.NewSource(4), []claim.Claim{c})
	outstanding := shared.Money(0)
	paid := shared.Money(0)
	for _, tx := range txs {
		if tx.Date != c.ReportDate() {
			t.Fatalf("transaction on %s, want all on %s", tx.Date, c.ReportDate())
		}
		if tx.Type == transaction.Estimate {
			outstanding += tx.Amount
		} else {
			paid += tx.Amount
		}
	}
	if outstanding != 0 || paid <= 0 {
		t.Fatalf("same-day close: outstanding %v (want 0), paid %v (want > 0)", outstanding, paid)
	}
}

func TestTransactionIDsSequential(t *testing.T) {
	claims := testClaims(100)
	sim := transaction.NewRunoffSimulator(params(), sections())
	txs := sim.Simulate(random.NewSource(5), claims)
	for i, tx := range txs {
		if tx.ID != i+1 {
			t.Fatalf("transaction %d has ID %d, want %d", i, tx.ID, i+1)
		}
	}
}

func TestRunoffIsDeterministic(t *testing.T) {
	claims := testClaims(200)
	sim := transaction.NewRunoffSimulator(params(), sections())
	a := sim.Simulate(random.NewSource(42), claims)
	b := sim.Simulate(random.NewSource(42), claims)
	if len(a) != len(b) {
		t.Fatalf("lengths differ: %d vs %d", len(a), len(b))
	}
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("transaction %d differs between identical runs", i)
		}
	}
}

func TestLongClaimsReviseMoreThanShortClaims(t *testing.T) {
	claims := testClaims(1000)
	sim := transaction.NewRunoffSimulator(params(), sections())
	txs := sim.Simulate(random.NewSource(6), claims)
	grouped := byClaim(txs)
	var shortSum, shortN, longSum, longN float64
	for _, c := range claims {
		n := float64(len(grouped[c.ID]))
		if shared.DaysBetween(c.ReportDate(), c.CloseDate()) >= 180 {
			longSum += n
			longN++
		} else {
			shortSum += n
			shortN++
		}
	}
	if longSum/longN <= shortSum/shortN {
		t.Errorf("long claims average %v transactions, short %v; want more on long claims",
			longSum/longN, shortSum/shortN)
	}
}

func TestNilClaimHasNoPaymentsAndClosesToZero(t *testing.T) {
	c := claim.Claim{
		ID:             1,
		PolicyID:       1,
		OccurrenceDate: shared.NewDate(2000, time.January, 1),
		Episodes: []claim.Episode{{
			Open:        shared.NewDate(2000, time.January, 10),
			Close:       shared.NewDate(2001, time.June, 1),
			Nil:         true,
			OpeningCase: shared.FromDollars(5000),
		}},
		RiskFactor: 1.0,
	}
	sim := transaction.NewRunoffSimulator(params(), sections())
	txs := sim.Simulate(random.NewSource(1), []claim.Claim{c})

	if len(txs) == 0 {
		t.Fatal("expected transactions for the nil claim")
	}
	outstanding := shared.Money(0)
	paid := shared.Money(0)
	for _, tx := range txs {
		switch tx.Type {
		case transaction.Payment:
			paid += tx.Amount
		case transaction.Estimate:
			outstanding += tx.Amount
		}
	}
	if paid != 0 {
		t.Fatalf("nil claim paid %v, want 0", paid)
	}
	if outstanding != 0 {
		t.Fatalf("nil claim outstanding at close %v, want 0", outstanding)
	}
	first := txs[0]
	if first.Type != transaction.Estimate || first.Amount != c.InitialEstimate() || first.Date != c.ReportDate() {
		t.Fatalf("first row %+v is not the initial estimate on the report date", first)
	}
	if last := txs[len(txs)-1]; last.Date != c.CloseDate() {
		t.Fatalf("last row on %s, want close date %s", last.Date, c.CloseDate())
	}
}

// reopenedClaim builds one claim with a reopen episode.
func reopenedClaim(isNil bool) claim.Claim {
	return claim.Claim{
		ID:             1,
		PolicyID:       1,
		OccurrenceDate: shared.NewDate(2000, time.January, 1),
		Episodes: []claim.Episode{
			{
				Open:        shared.NewDate(2000, time.January, 5),
				Close:       shared.NewDate(2000, time.June, 1),
				Ultimate:    shared.FromDollars(8000),
				Nil:         isNil,
				OpeningCase: shared.FromDollars(8000),
			},
			{
				Open:        shared.NewDate(2000, time.September, 1),
				Close:       shared.NewDate(2001, time.February, 1),
				Ultimate:    shared.FromDollars(3000),
				OpeningCase: shared.FromDollars(3000),
			},
		},
		RiskFactor: 1.0,
	}
}

func TestReopenedClaimRunsTwoEpisodes(t *testing.T) {
	c := reopenedClaim(false)
	txs := transaction.NewRunoffSimulator(params(), sections()).Simulate(random.NewSource(11), []claim.Claim{c})

	outstanding := shared.Money(0)
	outstandingAtFirstClose := shared.Money(-1)
	var reopenRow *transaction.Transaction
	for i, tx := range txs {
		if tx.Type == transaction.Estimate {
			outstanding += tx.Amount
		}
		if !tx.Date.After(c.Episodes[0].Close) {
			outstandingAtFirstClose = outstanding
		} else if reopenRow == nil {
			reopenRow = &txs[i]
		}
	}
	if outstandingAtFirstClose != 0 {
		t.Fatalf("outstanding at first close = %v, want 0", outstandingAtFirstClose)
	}
	if reopenRow == nil {
		t.Fatal("no transactions after the first close")
	}
	reopen := c.Episodes[1]
	if reopenRow.Type != transaction.Estimate || reopenRow.Amount != reopen.OpeningCase || reopenRow.Date != reopen.Open {
		t.Fatalf("re-raise row %+v, want ESTIMATE %v on %s", *reopenRow, reopen.OpeningCase, reopen.Open)
	}
	if outstanding != 0 {
		t.Fatalf("outstanding at final close = %v, want 0", outstanding)
	}
	if last := txs[len(txs)-1]; last.Date != c.CloseDate() {
		t.Fatalf("last transaction on %s, want final close %s", last.Date, c.CloseDate())
	}
}

func TestReopenedNilClaimPaysOnlyInEpisodeTwo(t *testing.T) {
	c := reopenedClaim(true)
	txs := transaction.NewRunoffSimulator(params(), sections()).Simulate(random.NewSource(12), []claim.Claim{c})

	paidBeforeReopen := shared.Money(0)
	paidAfterReopen := shared.Money(0)
	for _, tx := range txs {
		if tx.Type != transaction.Payment {
			continue
		}
		if tx.Date.Before(c.Episodes[1].Open) {
			paidBeforeReopen += tx.Amount
		} else {
			paidAfterReopen += tx.Amount
		}
	}
	if paidBeforeReopen != 0 {
		t.Fatalf("reopened nil claim paid %v before the reopen, want 0", paidBeforeReopen)
	}
	if paidAfterReopen != c.Episodes[1].Ultimate {
		t.Fatalf("reopened nil claim paid %v in episode 2, want its reopen ultimate %v", paidAfterReopen, c.Episodes[1].Ultimate)
	}
}

func TestReopenedClaimRowsChronological(t *testing.T) {
	claims := testClaims(50)
	for i := range claims {
		if i%4 == 0 {
			reopen := claims[i].CloseDate().AddDays(60)
			claims[i].Episodes = append(claims[i].Episodes, claim.Episode{
				Open: reopen, Close: reopen.AddDays(90), Ultimate: shared.FromDollars(2000), OpeningCase: shared.FromDollars(2000),
			})
		}
	}
	txs := transaction.NewRunoffSimulator(params(), sections()).Simulate(random.NewSource(13), claims)
	for id, rows := range byClaim(txs) {
		for i := 1; i < len(rows); i++ {
			if rows[i].Date.Before(rows[i-1].Date) {
				t.Fatalf("claim %d rows out of order", id)
			}
		}
	}
}

func TestTinyReopenEstimateStillClosesOnFinalCloseDate(t *testing.T) {
	c := reopenedClaim(false)
	c.Episodes[1].Ultimate = shared.Money(2) // two cents over a five-month episode
	c.Episodes[1].OpeningCase = shared.Money(2)
	for seed := uint64(1); seed <= 25; seed++ {
		txs := transaction.NewRunoffSimulator(params(), sections()).Simulate(random.NewSource(seed), []claim.Claim{c})
		outstanding := shared.Money(0)
		for _, tx := range txs {
			if tx.Type == transaction.Estimate {
				outstanding += tx.Amount
			}
		}
		if outstanding != 0 {
			t.Fatalf("seed %d: outstanding at final close = %v, want 0", seed, outstanding)
		}
		if last := txs[len(txs)-1]; last.Date != c.CloseDate() {
			t.Fatalf("seed %d: last transaction on %s, want final close %s", seed, last.Date, c.CloseDate())
		}
	}
}

func TestNilClaimTinyEstimateStillClosesOnCloseDate(t *testing.T) {
	// A tiny initial estimate over a long duration is the case where revision
	// targets can round to zero; the terminal release must still land on the
	// close date.
	c := claim.Claim{
		ID:             1,
		PolicyID:       1,
		OccurrenceDate: shared.NewDate(2000, time.January, 1),
		Episodes: []claim.Episode{{
			Open:        shared.NewDate(2000, time.January, 2),
			Close:       shared.NewDate(2003, time.January, 2),
			Nil:         true,
			OpeningCase: shared.FromDollars(0.02),
		}},
		RiskFactor: 1.0,
	}
	sim := transaction.NewRunoffSimulator(params(), sections())
	// Try several seeds so at least one exercises revisions that round toward zero.
	for seed := uint64(1); seed <= 25; seed++ {
		txs := sim.Simulate(random.NewSource(seed), []claim.Claim{c})
		if len(txs) == 0 {
			t.Fatalf("seed %d: no transactions", seed)
		}
		outstanding := shared.Money(0)
		paid := shared.Money(0)
		for _, tx := range txs {
			switch tx.Type {
			case transaction.Payment:
				paid += tx.Amount
			case transaction.Estimate:
				outstanding += tx.Amount
			}
		}
		if paid != 0 {
			t.Fatalf("seed %d: nil claim paid %v, want 0", seed, paid)
		}
		if outstanding != 0 {
			t.Fatalf("seed %d: outstanding at close %v, want 0", seed, outstanding)
		}
		if last := txs[len(txs)-1]; last.Date != c.CloseDate() {
			t.Fatalf("seed %d: last transaction on %s, want close date %s", seed, last.Date, c.CloseDate())
		}
	}
}

// incurredShare is the average across claims of incurred (case plus paid) as
// a share of the true cost, valued day days after report.
func incurredShare(txs []transaction.Transaction, claims []claim.Claim, day int) float64 {
	rows := byClaim(txs)
	total := 0.0
	for _, c := range claims {
		valuation := c.ReportDate().AddDays(day)
		incurred := shared.Money(0)
		for _, tx := range rows[c.ID] {
			if !tx.Date.After(valuation) {
				incurred += tx.Amount // ESTIMATE movements and payments both add to case plus paid
			}
		}
		total += incurred.Dollars() / c.Episodes[0].Ultimate.Dollars()
	}
	return total / float64(len(claims))
}

// SL-7: the opening case's adequacy bias decays over the claim's life rather
// than vanishing at the first revision. Cases open 25% redundant (mean 0.8)
// and the gap closes geometrically, CaseAdequacyMean^(u-1) at elapsed share u.
func TestCaseAdequacyBiasDecaysOverTheClaimLife(t *testing.T) {
	const duration = 1000
	claimsFor := func(mean float64) []claim.Claim {
		claims := make([]claim.Claim, 3000)
		for i := range claims {
			report := shared.NewDate(1998, time.March, 1)
			claims[i] = claim.Claim{
				ID:             i + 1,
				PolicyID:       i + 1,
				OccurrenceDate: report,
				Episodes: []claim.Episode{{
					Open:        report,
					Close:       report.AddDays(duration),
					Ultimate:    shared.FromDollars(10000),
					OpeningCase: shared.FromDollars(10000 / mean),
				}},
			}
		}
		return claims
	}
	p := params()
	p.PaymentsPerYear = 0   // case alone carries incurred until the settlement at close
	p.RevisionsPerYear = 12 // revise often, so the case tracks its aim closely
	for _, mean := range []float64{0.8, 1.0} {
		p.CaseAdequacyMean = mean
		claims := claimsFor(mean)
		txs := transaction.NewRunoffSimulator(p, sections()).Simulate(random.NewSource(9), claims)
		prev := math.Inf(1)
		for _, u := range []float64{0.25, 0.5, 0.75} {
			got := incurredShare(txs, claims, int(u*duration))
			want := math.Pow(mean, u-1)
			if math.Abs(got/want-1) > 0.03 {
				t.Errorf("mean %.1f at %.0f%% of the claim's life: incurred %.3f of the truth, want about %.3f", mean, 100*u, got, want)
			}
			if mean != 1 && got >= prev {
				t.Errorf("mean %.1f: the adequacy gap did not narrow by %.0f%% of the life (%.3f after %.3f)", mean, 100*u, got, prev)
			}
			prev = got
		}
		if got := incurredShare(txs, claims, duration); math.Abs(got-1) > 1e-9 {
			t.Errorf("mean %.1f at close: incurred %.6f of the truth, want exactly 1", mean, got)
		}
	}
}
