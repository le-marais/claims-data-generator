package transaction_test

import (
	"testing"

	"github.com/le-marais/claimsgen/internal/domain/claim"
	"github.com/le-marais/claimsgen/internal/domain/lob"
	"github.com/le-marais/claimsgen/internal/domain/shared"
	"github.com/le-marais/claimsgen/internal/domain/transaction"
	"github.com/le-marais/claimsgen/internal/infrastructure/random"
)

// tokenSettlement is a settlement whose share is usually near 0, so the
// interim payments pay almost everything unless the final settlement has a
// floor (MR-23).
var tokenSettlement = lob.SettlementParams{Share: 0.02, Concentration: 2}

// episodeRows splits a claim's rows by the episode their date falls in.
func episodeRows(c claim.Claim, rows []transaction.Transaction) [][]transaction.Transaction {
	out := make([][]transaction.Transaction, len(c.Episodes))
	for _, tx := range rows {
		for i, ep := range c.Episodes {
			if !tx.Date.Before(ep.Open) && !tx.Date.After(ep.Close) {
				out[i] = append(out[i], tx)
				break
			}
		}
	}
	return out
}

func TestFinalSettlementIsAtLeastTheMinimumPayment(t *testing.T) {
	for _, delay := range []float64{0, 7} {
		claims := delayClaims(1000, 7)
		p := params()
		p.MinPayment = 50
		p.PaymentDelayDays = delay
		p.PaymentsPerYear = 6
		txs := transaction.NewRunoffSimulator(p, sections(tokenSettlement)).Simulate(random.NewSource(81), claims)
		grouped := byClaim(txs)
		instalments := 0
		for _, c := range claims {
			for i, rows := range episodeRows(c, grouped[c.ID]) {
				var pays []shared.Money
				for _, tx := range rows {
					if tx.Type == transaction.Payment {
						pays = append(pays, tx.Amount)
					}
				}
				if len(pays) < 2 {
					continue
				}
				instalments++
				if final := pays[len(pays)-1]; final < shared.FromDollars(50) {
					t.Fatalf("delay %v: claim %d episode %d settles for %v after %d instalments, below the 50 minimum", delay, c.ID, i+1, final, len(pays)-1)
				}
			}
		}
		if instalments == 0 {
			t.Fatalf("delay %v: no episode paid in instalments; the test proves nothing", delay)
		}
	}
}

func TestCaseStaysOpenUntilTheClose(t *testing.T) {
	for _, delay := range []float64{0, 7} {
		claims := delayClaims(1000, 7)
		p := params()
		p.MinPayment = 50
		p.PaymentDelayDays = delay
		p.PaymentsPerYear = 6
		p.CaseAdequacyMean = 1.5 // deficient cases, which the bills top up
		txs := transaction.NewRunoffSimulator(p, sections(tokenSettlement)).Simulate(random.NewSource(82), claims)
		grouped := byClaim(txs)
		for _, c := range claims {
			for i, rows := range episodeRows(c, grouped[c.ID]) {
				ep := c.Episodes[i]
				outstanding := shared.Money(0)
				for _, tx := range rows {
					if tx.Type == transaction.Estimate {
						outstanding += tx.Amount
					}
					if tx.Date.Before(ep.Close) && tx.Type != transaction.Payment && outstanding <= 0 {
						t.Fatalf("delay %v: claim %d episode %d has no case on %v, before its close %v", delay, c.ID, i+1, tx.Date, ep.Close)
					}
				}
			}
		}
	}
}
