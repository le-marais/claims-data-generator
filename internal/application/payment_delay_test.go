package application_test

import (
	"testing"

	"github.com/le-marais/claimsgen/internal/application"
	"github.com/le-marais/claimsgen/internal/domain/shared"
	"github.com/le-marais/claimsgen/internal/domain/transaction"
	"github.com/le-marais/claimsgen/internal/infrastructure/random"
)

// TestPaymentsWaitForThePaymentDelay checks the payment delay across the
// whole pipeline: the claim stage keeps each paying claim and reopen open
// long enough, and the runoff puts every payment at least the delay after
// the last ESTIMATE row that raised its claim's case, and after the claim's
// previous payment.
func TestPaymentsWaitForThePaymentDelay(t *testing.T) {
	const delay = 7
	req := request(t)
	req.InitialBookSize = 3000
	req.LOB.Runoff.PaymentDelayDays = delay
	ds, err := application.GenerateDataset(t.Context(), random.NewSource(19), req)
	if err != nil {
		t.Fatal(err)
	}
	payments, reopens := 0, 0
	lastRaise := map[int]shared.Date{}
	lastPay := map[int]shared.Date{}
	for _, tx := range ds.Transactions {
		switch {
		case tx.Type == transaction.Estimate && tx.Amount > 0:
			lastRaise[tx.ClaimID] = tx.Date
		case tx.Type == transaction.Payment:
			payments++
			if d := shared.DaysBetween(lastRaise[tx.ClaimID], tx.Date); d < delay {
				t.Fatalf("claim %d: payment on %s %d days after the case was raised, want at least %d", tx.ClaimID, tx.Date, d, delay)
			}
			if prev, ok := lastPay[tx.ClaimID]; ok && shared.DaysBetween(prev, tx.Date) < delay {
				t.Fatalf("claim %d: payment on %s %d days after the previous one, want at least %d", tx.ClaimID, tx.Date, shared.DaysBetween(prev, tx.Date), delay)
			}
			lastPay[tx.ClaimID] = tx.Date
		}
	}
	for _, c := range ds.Claims {
		if c.Reopened() {
			reopens++
		}
	}
	if payments == 0 || reopens == 0 {
		t.Fatalf("got %d payments and %d reopened claims, want some of each", payments, reopens)
	}
}
