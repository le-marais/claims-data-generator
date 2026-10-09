package transaction

import (
	"fmt"
	"math"
	"sort"

	"github.com/le-marais/claimsgen/internal/domain/calendar"
	"github.com/le-marais/claimsgen/internal/domain/claim"
	"github.com/le-marais/claimsgen/internal/domain/lob"
	"github.com/le-marais/claimsgen/internal/domain/shared"
)

// Salvage and Subrogation are money-in recovery transactions: the insured
// vehicle's wreck is sold, or the payout is recovered from an at-fault
// third party. Both land after the claim closes and never touch the case
// estimate, which stays gross.
const (
	Salvage     Type = "SALVAGE"
	Subrogation Type = "SUBROGATION"
)

// IsRecovery reports whether the type is money coming back on a claim.
func (t Type) IsRecovery() bool {
	return t == Salvage || t == Subrogation
}

// RecoverySimulator draws salvage and subrogation transactions for eligible
// claims after the runoff stage.
type RecoverySimulator struct {
	params   lob.RecoveryParams
	sections []lob.SectionParams
	calendar calendar.Calendar
}

// NewRecoverySimulator builds a recovery simulator from the claim parameters:
// the recovery parameters, and the sections that say which claims are
// eligible.
func NewRecoverySimulator(p lob.ClaimParams) *RecoverySimulator {
	return &RecoverySimulator{params: p.Recoveries, sections: p.Sections}
}

// WithCalendar rolls every recovery to the calendar's next business day; a
// recovery stays strictly after the close. The default, an off calendar,
// moves nothing.
func (s *RecoverySimulator) WithCalendar(c calendar.Calendar) *RecoverySimulator {
	s.calendar = c
	return s
}

// Apply merges each eligible claim's recovery rows into the runoff output
// after that claim's block, renumbering IDs. Every claim draws from its own
// labelled sub-stream, and within a claim each recovery type draws from its
// own sub-stream (recovery-claim-{id}/SALVAGE, .../SUBROGATION), so toggling
// one recovery type never reshuffles the draws of the other type or any other
// stage.
func (s *RecoverySimulator) Apply(src shared.RandomSource, claims []claim.Claim, txs []Transaction) []Transaction {
	paid := make(map[int]shared.Money, len(claims))
	for _, tx := range txs {
		if tx.Type == Payment {
			paid[tx.ClaimID] += tx.Amount
		}
	}
	recoveries := map[int][]Transaction{}
	total := 0
	for _, c := range claims {
		rows := s.simulateClaim(src.Split(fmt.Sprintf("recovery-claim-%d", c.ID)), c, paid[c.ID])
		if len(rows) > 0 {
			recoveries[c.ID] = rows
			total += len(rows)
		}
	}
	if total == 0 {
		return txs
	}
	merged := make([]Transaction, 0, len(txs)+total)
	for i, tx := range txs {
		merged = append(merged, tx)
		// The runoff emits each claim's rows as one contiguous block; append
		// the claim's recoveries at the end of its block. Deleting after append
		// guards against double insertion if claims are ever interleaved.
		if i+1 == len(txs) || txs[i+1].ClaimID != tx.ClaimID {
			merged = append(merged, recoveries[tx.ClaimID]...)
			delete(recoveries, tx.ClaimID)
		}
	}
	for i := range merged {
		merged[i].ID = i + 1
	}
	return merged
}

// simulateClaim draws at most one salvage and one subrogation row. Only
// claims that paid something in a section that allows recoveries are
// eligible, and salvage, the sale of the written-off vehicle, only on a total
// loss on a sum-insured section whose first episode paid the write-off
// (MR-7); for a total loss gross paid is the sum insured less excess, so
// salvage is sized off the vehicle's value. A liability claim settled at its
// limit also reaches its cover limit, but leaves no wreck to sell. The total
// recovered stays strictly below the claim's gross paid. A nil claim that
// never reopens has paid 0 and stays ineligible through the paid check
// alone; a reopened nil claim that paid in its second episode is
// subrogation-eligible like any other paying claim.
func (s *RecoverySimulator) simulateClaim(src shared.RandomSource, c claim.Claim, paid shared.Money) []Transaction {
	if !s.sections[c.Section].Recoveries || paid <= 0 {
		return nil
	}
	kinds := []struct {
		t Type
		p lob.RecoveryTypeParams
	}{
		{Salvage, s.params.Salvage},
		{Subrogation, s.params.Subrogation},
	}
	var rows []Transaction
	recovered := shared.Money(0)
	for _, k := range kinds {
		if k.t == Salvage && (s.sections[c.Section].Severity.Kind != lob.SumInsuredLognormal || !c.TotalLoss() || c.Nil()) {
			continue // only a written-off vehicle the claim paid for is sold for salvage
		}
		ksrc := src.Split(string(k.t)) // recovery-claim-{id}/SALVAGE, .../SUBROGATION
		if k.p.Probability <= 0 || !ksrc.Bernoulli(k.p.Probability) {
			continue
		}
		share := ksrc.Beta(k.p.MeanShare*k.p.Concentration, (1-k.p.MeanShare)*k.p.Concentration)
		amount := paid.MulFloat(share)
		lag := int(math.Round(ksrc.LogNormal(math.Log(k.p.LagMedianDays), k.p.LagSigma)))
		if lag < 1 {
			lag = 1 // recoveries land strictly after close
		}
		if recovered+amount >= paid {
			amount = paid - recovered - shared.OneCent // keep total recovered strictly below gross paid
		}
		if amount < shared.OneCent {
			continue // sub-cent recovery: emit no row
		}
		rows = append(rows, Transaction{
			ClaimID: c.ID,
			Date:    s.calendar.Following(c.CloseDate().AddDays(lag)),
			Type:    k.t,
			Amount:  amount,
		})
		recovered += amount
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].Date.Before(rows[j].Date) })
	return rows
}
