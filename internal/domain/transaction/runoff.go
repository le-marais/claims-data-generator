// Package transaction simulates each claim's case estimate runoff and
// derives payment transactions (steps 3-4 of the simulation).
//
// The design is ultimate-first: the claim stage fixes each claim's true
// ultimate cost, the case-estimate stage (CaseEstimator) sets the case it
// opens at, payments split the ultimate over the claim's life, and the case
// estimate is a noisy assessor's view of the remaining cost that converges to
// zero at close. The initial estimate is emitted as the first ESTIMATE row, so a
// claim's outstanding case at any time is the running sum of its ESTIMATE
// amounts. Runoff is developed one episode at a time: a reopened claim's
// second episode re-raises the case from zero to its opening case and
// develops to the final close.
package transaction

import (
	"fmt"
	"math"
	"sort"

	"github.com/le-marais/claimsgen/internal/domain/claim"
	"github.com/le-marais/claimsgen/internal/domain/lob"
	"github.com/le-marais/claimsgen/internal/domain/shared"
)

type Type string

const (
	Payment  Type = "PAYMENT"
	Estimate Type = "ESTIMATE"
)

// Transaction is one movement on a claim: money paid to the customer or a
// signed change in the outstanding case estimate.
type Transaction struct {
	ID      int
	ClaimID int
	Date    shared.Date
	Type    Type
	Amount  shared.Money
}

// RunoffSimulator generates the transactions for each claim.
type RunoffSimulator struct {
	params lob.RunoffParams
}

// NewRunoffSimulator builds a runoff simulator from the runoff parameters.
func NewRunoffSimulator(p lob.RunoffParams) *RunoffSimulator {
	return &RunoffSimulator{params: p}
}

// Simulate produces every claim's transactions in claim order, each claim's
// rows chronological, with sequential IDs.
func (s *RunoffSimulator) Simulate(src shared.RandomSource, claims []claim.Claim) []Transaction {
	var txs []Transaction
	for _, c := range claims {
		txs = append(txs, s.simulateClaim(src.Split(fmt.Sprintf("runoff-claim-%d", c.ID)), c)...)
	}
	for i := range txs {
		txs[i].ID = i + 1
	}
	return txs
}

const (
	kindRevision = 0
	kindPayment  = 1
)

// event is an interim payment or case revision strictly between report and
// close. kindRevision sorts before kindPayment on the same day.
type event struct {
	offset int
	kind   int
	amount shared.Money // payments only
}

// simulateClaim develops the claim's episodes in order. Each opens by moving
// the case to the episode's opening case on its open date: on the report date
// that is the claim's first ESTIMATE row, and on a reopen it re-raises the
// case from zero.
func (s *RunoffSimulator) simulateClaim(src shared.RandomSource, c claim.Claim) []Transaction {
	e := &emitter{claimID: c.ID, report: c.ReportDate()}
	for _, ep := range c.Episodes {
		e.reviseTo(shared.DaysBetween(e.report, ep.Open), ep.OpeningCase)
		s.runEpisode(src, e, ep)
	}
	return e.txs
}

// adequacyBias is the case a revision aims at as a share of the true
// remaining cost, at elapsed share u of the episode: CaseAdequacyMean^(u-1).
// The case opens at about 1/CaseAdequacyMean of the truth (CaseEstimator), and
// the bias closes geometrically to parity at close instead of vanishing at the
// first revision, so incurred develops the way IBNER methods expect (SL-7). A
// mean of 1 makes every revision unbiased.
func (s *RunoffSimulator) adequacyBias(u float64) float64 {
	return math.Pow(s.params.CaseAdequacyMean, u-1)
}

// runEpisode develops one open-close episode: interim payments and pure
// revisions between its open and close dates, a final settlement at close
// that brings total paid in the episode to exactly ultimate, and the
// outstanding case released to exactly zero. A nil episode emits no payments
// and ignores ultimate.
//
// Each revision moves the case to its aim times mean-one lognormal noise
// whose sigma decays to zero at close. A paying episode aims at the remaining
// cost (ultimate - paid) times the adequacy bias; a nil episode, whose handler
// does not know it will pay nothing, aims at the current case. Every target
// is floored at one cent, so the case stays open until the close date.
func (s *RunoffSimulator) runEpisode(src shared.RandomSource, e *emitter, ep claim.Episode) {
	ultimate, isNil := ep.Ultimate, ep.Nil
	base := shared.DaysBetween(e.report, ep.Open)
	duration := shared.DaysBetween(ep.Open, ep.Close)
	years := float64(duration) / 365

	var interims []event
	if !isNil {
		if ultimate < shared.OneCent {
			ultimate = shared.OneCent // guards hand-built claims; generated claims always cost something
		}
		interims = s.drawInterimPayments(src, ultimate, duration, years)
	}
	events := append(s.drawRevisions(src, duration, years), interims...)
	sort.SliceStable(events, func(i, j int) bool {
		if events[i].offset != events[j].offset {
			return events[i].offset < events[j].offset
		}
		return events[i].kind < events[j].kind
	})

	paid := shared.Money(0)
	aim := func(u float64) float64 {
		if isNil {
			return e.outstanding.Dollars()
		}
		return (ultimate - paid).Dollars() * s.adequacyBias(u)
	}
	for _, ev := range events {
		if ev.kind == kindPayment {
			e.pay(base+ev.offset, ev.amount)
			paid += ev.amount
			continue
		}
		u := float64(ev.offset) / float64(duration)
		target := shared.FromDollars(aim(u) * shared.MeanOneLogNormal(src, s.params.RevisionSigma*(1-u)))
		if target < shared.OneCent {
			target = shared.OneCent // keep the case open so the terminal release lands on the close date
		}
		e.reviseTo(base+ev.offset, target)
	}

	// A paying episode's final settlement clears the remaining ultimate; then
	// the case snaps to exactly zero.
	if !isNil {
		e.pay(base+duration, ultimate-paid)
	}
	e.reviseTo(base+duration, 0)
}

// drawInterimPayments splits (1 - settlement share) of the ultimate across
// a Poisson number of payments on days strictly between report and close,
// weighted by a Dirichlet draw. The remainder is paid at close.
func (s *RunoffSimulator) drawInterimPayments(src shared.RandomSource, ultimate shared.Money, duration int, years float64) []event {
	if duration < 2 {
		return nil
	}
	n := src.Poisson(s.params.PaymentsPerYear * years)
	if n == 0 {
		return nil
	}
	weights := make([]float64, n)
	total := 0.0
	for i := range weights {
		weights[i] = src.Gamma(s.params.Concentration, 1)
		total += weights[i]
	}
	if total <= 0 {
		// Degenerate Dirichlet draw (all weights underflowed): settle at close.
		return nil
	}
	pool := ultimate.MulFloat(1 - s.params.SettlementShare).Dollars()
	events := make([]event, 0, n)
	paid := shared.Money(0)
	for _, w := range weights {
		amount := shared.FromDollars(pool * w / total)
		if amount <= 0 {
			continue
		}
		events = append(events, event{offset: s.interiorOffset(src, duration), kind: kindPayment, amount: amount})
		paid += amount
	}
	if paid >= ultimate {
		// Rounding degenerate: fall back to settling everything at close.
		return nil
	}
	return events
}

func (s *RunoffSimulator) drawRevisions(src shared.RandomSource, duration int, years float64) []event {
	if duration < 2 {
		return nil
	}
	n := src.Poisson(s.params.RevisionsPerYear * years)
	events := make([]event, n)
	for i := range events {
		events[i] = event{offset: s.interiorOffset(src, duration), kind: kindRevision}
	}
	return events
}

// interiorOffset draws a day strictly between report (0) and close (duration).
func (s *RunoffSimulator) interiorOffset(src shared.RandomSource, duration int) int {
	return 1 + int(src.Uniform()*float64(duration-1))
}

// emitter tracks the outstanding case estimate and appends transactions,
// keeping the outstanding amount non-negative by construction.
type emitter struct {
	claimID     int
	report      shared.Date
	outstanding shared.Money
	txs         []Transaction
}

func (e *emitter) estimate(offset int, movement shared.Money) {
	if movement == 0 {
		return
	}
	e.txs = append(e.txs, Transaction{
		ClaimID: e.claimID,
		Date:    e.report.AddDays(offset),
		Type:    Estimate,
		Amount:  movement,
	})
	e.outstanding += movement
}

// reviseTo moves the outstanding case to the target via one ESTIMATE row.
func (e *emitter) reviseTo(offset int, target shared.Money) {
	e.estimate(offset, target-e.outstanding)
}

// pay emits a payment and its matching case reduction, strengthening the
// case first when the payment exceeds the current outstanding.
func (e *emitter) pay(offset int, amount shared.Money) {
	if amount <= 0 {
		return
	}
	if e.outstanding < amount {
		e.estimate(offset, amount-e.outstanding)
	}
	e.txs = append(e.txs, Transaction{
		ClaimID: e.claimID,
		Date:    e.report.AddDays(offset),
		Type:    Payment,
		Amount:  amount,
	})
	e.estimate(offset, -amount)
}
