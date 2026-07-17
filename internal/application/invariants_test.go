package application_test

import (
	"testing"

	"github.com/le-marais/claimsgen/internal/application"
	"github.com/le-marais/claimsgen/internal/domain/shared"
	"github.com/le-marais/claimsgen/internal/domain/transaction"
	"github.com/le-marais/claimsgen/internal/infrastructure/random"
)

// TestDatasetInvariants sweeps every consistency rule from the spec over a
// full dataset generated with the shipped preset.
func TestDatasetInvariants(t *testing.T) {
	req := request(t)
	req.Years = 5
	req.InitialBookSize = 2000
	ds, err := application.GenerateDataset(random.NewSource(99), req)
	if err != nil {
		t.Fatal(err)
	}

	policies := map[int]struct {
		start, end shared.Date
		excess     shared.Money
	}{}
	for _, p := range ds.Policies {
		policies[p.ID] = struct {
			start, end shared.Date
			excess     shared.Money
		}{p.CoverStart, p.CoverEnd, p.Excess}
	}

	type claimInfo struct {
		report, close shared.Date
		firstClose    shared.Date
		reopen        shared.Date
		reopened      bool
		isNil, ownDmg bool
	}
	claims := map[int]claimInfo{}
	for _, c := range ds.Claims {
		pol, ok := policies[c.PolicyID]
		if !ok {
			t.Fatalf("claim %d references missing policy %d", c.ID, c.PolicyID)
		}
		if c.OccurrenceDate.Before(pol.start) || c.OccurrenceDate.After(pol.end) {
			t.Fatalf("claim %d occurrence %s outside cover %s..%s", c.ID, c.OccurrenceDate, pol.start, pol.end)
		}
		if c.ReportDate.Before(c.OccurrenceDate) {
			t.Fatalf("claim %d reported %s before occurrence %s", c.ID, c.ReportDate, c.OccurrenceDate)
		}
		if c.CloseDate.Before(c.ReportDate) {
			t.Fatalf("claim %d closed %s before report %s", c.ID, c.CloseDate, c.ReportDate)
		}
		if c.InitialEstimate <= 0 {
			t.Fatalf("claim %d initial estimate %v not positive", c.ID, c.InitialEstimate)
		}
		if c.Reopened() {
			if !c.ReopenDate.After(c.FirstCloseDate) {
				t.Fatalf("claim %d reopen %s not strictly after first close %s", c.ID, c.ReopenDate, c.FirstCloseDate)
			}
			if !c.CloseDate.After(c.ReopenDate) {
				t.Fatalf("claim %d final close %s not strictly after reopen %s", c.ID, c.CloseDate, c.ReopenDate)
			}
			if c.ReopenEstimate <= 0 {
				t.Fatalf("claim %d reopen estimate %v not positive", c.ID, c.ReopenEstimate)
			}
			if c.FirstCloseDate.Before(c.ReportDate) {
				t.Fatalf("claim %d first close %s before report %s", c.ID, c.FirstCloseDate, c.ReportDate)
			}
		}
		firstClose := c.CloseDate
		if c.Reopened() {
			firstClose = c.FirstCloseDate
		}
		claims[c.ID] = claimInfo{c.ReportDate, c.CloseDate, firstClose, c.ReopenDate, c.Reopened(), c.Nil, c.OwnDamage}
	}

	type state struct {
		outstanding shared.Money
		paid        shared.Money
		recovered   shared.Money
		rows        int
		first       transaction.Transaction
		last        transaction.Transaction
		lastCase    transaction.Transaction // last non-recovery row
		afterReopen bool
	}
	perClaim := map[int]*state{}
	for _, tx := range ds.Transactions {
		c, ok := claims[tx.ClaimID]
		if !ok {
			t.Fatalf("transaction %d references missing claim %d", tx.ID, tx.ClaimID)
		}
		if tx.Type.IsRecovery() {
			// Recoveries are the only post-close activity, strictly after close.
			if !c.close.Before(tx.Date) {
				t.Fatalf("recovery %d on %s not strictly after close %s", tx.ID, tx.Date, c.close)
			}
		} else if tx.Date.Before(c.report) || tx.Date.After(c.close) {
			t.Fatalf("transaction %d on %s outside claim window %s..%s", tx.ID, tx.Date, c.report, c.close)
		}
		s := perClaim[tx.ClaimID]
		if s == nil {
			s = &state{first: tx}
			perClaim[tx.ClaimID] = s
		}
		if !tx.Type.IsRecovery() && c.reopened && !s.afterReopen && tx.Date.After(c.firstClose) {
			if s.outstanding != 0 {
				t.Fatalf("claim %d outstanding at first close = %v, want 0", tx.ClaimID, s.outstanding)
			}
			if tx.Type != transaction.Estimate || tx.Amount <= 0 || tx.Date != c.reopen {
				t.Fatalf("claim %d first row after first close %+v is not a positive re-raise on the reopen date %s", tx.ClaimID, tx, c.reopen)
			}
			s.afterReopen = true
		}
		if c.isNil && tx.Type == transaction.Payment && !tx.Date.After(c.firstClose) {
			t.Fatalf("nil claim %d paid %v on %s, before its first close %s", tx.ClaimID, tx.Amount, tx.Date, c.firstClose)
		}
		if s.rows > 0 && tx.Date.Before(s.last.Date) {
			t.Fatalf("claim %d transactions out of order at transaction %d", tx.ClaimID, tx.ID)
		}
		switch tx.Type {
		case transaction.Estimate:
			s.outstanding += tx.Amount
		case transaction.Payment:
			if tx.Amount <= 0 {
				t.Fatalf("transaction %d payment amount %v not positive", tx.ID, tx.Amount)
			}
			s.paid += tx.Amount
		case transaction.Salvage, transaction.Subrogation:
			if tx.Amount <= 0 {
				t.Fatalf("transaction %d recovery amount %v not positive", tx.ID, tx.Amount)
			}
			if !c.ownDmg {
				t.Fatalf("recovery %d on non-own-damage claim %d", tx.ID, tx.ClaimID)
			}
			s.recovered += tx.Amount
		default:
			t.Fatalf("transaction %d has unknown type %q", tx.ID, tx.Type)
		}
		if s.outstanding < 0 {
			t.Fatalf("claim %d outstanding case went negative at transaction %d", tx.ClaimID, tx.ID)
		}
		s.rows++
		s.last = tx
		if !tx.Type.IsRecovery() {
			s.lastCase = tx
		}
	}

	for _, c := range ds.Claims {
		s := perClaim[c.ID]
		if s == nil {
			t.Fatalf("claim %d has no transactions", c.ID)
		}
		if s.first.Type != transaction.Estimate || s.first.Amount != c.InitialEstimate || s.first.Date != c.ReportDate {
			t.Fatalf("claim %d first transaction %+v is not the initial estimate on the report date", c.ID, s.first)
		}
		if s.outstanding != 0 {
			t.Fatalf("claim %d outstanding at close = %v, want 0", c.ID, s.outstanding)
		}
		if c.Nil && !c.Reopened() {
			if s.paid != 0 {
				t.Fatalf("nil claim %d total paid %v, want 0", c.ID, s.paid)
			}
		} else if s.paid <= 0 {
			t.Fatalf("claim %d total paid %v not positive", c.ID, s.paid)
		}
		if c.Reopened() && !s.afterReopen {
			t.Fatalf("reopened claim %d has no transactions after its first close", c.ID)
		}
		if s.recovered > 0 && s.recovered >= s.paid {
			t.Fatalf("claim %d recovered %v >= gross paid %v", c.ID, s.recovered, s.paid)
		}
		if s.lastCase.Date != c.CloseDate {
			t.Fatalf("claim %d last case activity on %s, want close date %s", c.ID, s.lastCase.Date, c.CloseDate)
		}
	}
}
