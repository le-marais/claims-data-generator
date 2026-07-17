package transaction

import (
	"fmt"
	"math"
	"sort"

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
	params lob.RecoveryParams
}

func NewRecoverySimulator(p lob.RecoveryParams) *RecoverySimulator {
	return &RecoverySimulator{params: p}
}

// Apply merges each eligible claim's recovery rows into the runoff output
// after that claim's block, renumbering IDs. Every claim draws from its own
// labelled sub-stream, so enabling recoveries never reshuffles the draws of
// other stages, and a claim's recoveries do not depend on any other claim.
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
// own-damage claims that paid something are eligible; the total recovered
// stays strictly below the claim's gross paid.
func (s *RecoverySimulator) simulateClaim(src shared.RandomSource, c claim.Claim, paid shared.Money) []Transaction {
	if !c.OwnDamage || c.Nil || paid <= 0 {
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
		if k.p.Probability <= 0 || !src.Bernoulli(k.p.Probability) {
			continue
		}
		share := src.Beta(k.p.MeanShare*k.p.Concentration, (1-k.p.MeanShare)*k.p.Concentration)
		amount := paid.MulFloat(share)
		lag := int(math.Round(src.LogNormal(math.Log(k.p.LagMedianDays), k.p.LagSigma)))
		if lag < 1 {
			lag = 1 // recoveries land strictly after close
		}
		if recovered+amount >= paid {
			amount = paid - recovered - 1 // keep total recovered strictly below gross paid
		}
		if amount < 1 {
			continue // sub-cent recovery: emit no row
		}
		rows = append(rows, Transaction{
			ClaimID: c.ID,
			Date:    c.CloseDate.AddDays(lag),
			Type:    k.t,
			Amount:  amount,
		})
		recovered += amount
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].Date.Before(rows[j].Date) })
	return rows
}
