// Package transaction simulates each claim's case estimate runoff and
// derives payment transactions (steps 3-4 of the simulation).
//
// The design is ultimate-first: the claim's true ultimate cost is drawn up
// front, payments split it over the claim's life, and the case estimate is
// a noisy assessor's view of the remaining cost that converges to zero at
// close. The initial estimate is emitted as the first ESTIMATE row, so a
// claim's outstanding case at any time is the running sum of its ESTIMATE
// amounts. Runoff is developed in episodes: a normal claim runs one episode
// to close, while a reopened claim runs a first episode to its first close,
// a re-raise to the reopen estimate, and a second episode to the final close.
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

// event is an interim payment or case revision strictly between report and
// close. kind 0 (revision) sorts before kind 1 (payment) on the same day.
type event struct {
	offset int
	kind   int
	amount shared.Money // payments only
}

func (s *RunoffSimulator) simulateClaim(src shared.RandomSource, c claim.Claim) []Transaction {
	e := &emitter{claimID: c.ID, report: c.ReportDate}
	e.estimate(0, c.InitialEstimate)

	firstClose := c.CloseDate
	if c.Reopened() {
		firstClose = c.FirstCloseDate
	}
	s.runEpisode(src, e, c.ReportDate, firstClose, c.InitialEstimate, c.Nil, false)

	if c.Reopened() {
		// The case is re-raised on the reopen date, then a second, smaller
		// episode develops and pays the reopen estimate.
		e.reviseTo(shared.DaysBetween(c.ReportDate, c.ReopenDate), c.ReopenEstimate)
		s.runEpisode(src, e, c.ReopenDate, c.CloseDate, c.ReopenEstimate, false, true)
	}
	return e.txs
}

// runEpisode develops one open-close episode: interim payments and pure
// revisions between start and close, a final settlement at close, and the
// outstanding case released to exactly zero. A nil episode emits no
// payments. floorRevisions keeps every revision target at least one cent
// (the nil path's guard), used for reopen episodes whose opening estimates
// can be tiny.
func (s *RunoffSimulator) runEpisode(src shared.RandomSource, e *emitter, start, close shared.Date, opening shared.Money, isNil, floorRevisions bool) {
	base := shared.DaysBetween(e.report, start)
	duration := shared.DaysBetween(start, close)
	years := float64(duration) / 365

	if isNil {
		revisions := s.drawRevisions(src, duration, years)
		sort.SliceStable(revisions, func(i, j int) bool {
			return revisions[i].offset < revisions[j].offset
		})
		for _, ev := range revisions {
			remaining := e.outstanding.Dollars()
			sigma := s.params.RevisionSigma * (1 - float64(ev.offset)/float64(duration))
			target := shared.FromDollars(remaining * shared.MeanOneLogNormal(src, sigma))
			if target < 1 {
				target = 1 // keep the case open so the terminal release lands on the close date
			}
			e.reviseTo(base+ev.offset, target)
		}
		e.reviseTo(base+duration, 0)
		return
	}

	ultimate := s.drawUltimate(src, opening)
	interims := s.drawInterimPayments(src, ultimate, duration, years)
	events := append(s.drawRevisions(src, duration, years), interims...)
	sort.SliceStable(events, func(i, j int) bool {
		if events[i].offset != events[j].offset {
			return events[i].offset < events[j].offset
		}
		return events[i].kind < events[j].kind
	})

	paid := shared.Money(0)
	for _, ev := range events {
		if ev.kind == 1 {
			e.pay(base+ev.offset, ev.amount)
			paid += ev.amount
			continue
		}
		remaining := (ultimate - paid).Dollars()
		sigma := s.params.RevisionSigma * (1 - float64(ev.offset)/float64(duration))
		target := shared.FromDollars(remaining * shared.MeanOneLogNormal(src, sigma))
		if floorRevisions && target < 1 {
			target = 1
		}
		e.reviseTo(base+ev.offset, target)
	}

	// Final settlement clears the remaining ultimate, then the case snaps
	// to exactly zero.
	e.pay(base+duration, ultimate-paid)
	e.reviseTo(base+duration, 0)
}

func (s *RunoffSimulator) drawUltimate(src shared.RandomSource, initial shared.Money) shared.Money {
	sigma := s.params.CaseAdequacySigma
	mu := math.Log(s.params.CaseAdequacyMean) - sigma*sigma/2
	ultimate := initial.MulFloat(src.LogNormal(mu, sigma))
	if ultimate < 1 {
		ultimate = 1
	}
	return ultimate
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
	pool := ultimate.MulFloat(1 - s.params.SettlementShare).Dollars()
	events := make([]event, 0, n)
	paid := shared.Money(0)
	for _, w := range weights {
		amount := shared.FromDollars(pool * w / total)
		if amount <= 0 {
			continue
		}
		events = append(events, event{offset: s.interiorOffset(src, duration), kind: 1, amount: amount})
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
		events[i] = event{offset: s.interiorOffset(src, duration), kind: 0}
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
