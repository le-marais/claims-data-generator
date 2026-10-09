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

	"github.com/le-marais/claimsgen/internal/domain/calendar"
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
	// settlement is each section's settlement, by section index.
	settlement []lob.SettlementParams
	holiday    lob.SeasonalHolidayParams
	calendar   calendar.Calendar
}

// NewRunoffSimulator builds a runoff simulator from the runoff parameters
// and the line's sections of cover, whose settlements shape each claim's
// payments.
func NewRunoffSimulator(p lob.RunoffParams, sections []lob.SectionParams) *RunoffSimulator {
	settlement := make([]lob.SettlementParams, len(sections))
	for i, sec := range sections {
		settlement[i] = sec.Settlement
	}
	return &RunoffSimulator{params: p, settlement: settlement}
}

// WithSeasonalHoliday defers a share of the interim payments dated in the
// holiday window to the same day of the next month. One that then falls past
// the episode's last interim day is paid with the final settlement. The
// default, off, takes no draws.
func (s *RunoffSimulator) WithSeasonalHoliday(h lob.SeasonalHolidayParams) *RunoffSimulator {
	s.holiday = h
	return s
}

// WithCalendar puts the runoff on business days: revisions and payments roll
// to the next business day, a revision rolled onto the close is dropped, and
// a payment's bill rolls back to the business day before, so it stays at
// least the payment delay ahead. Episodes must open and close on business
// days, as the claim and reopen stages place them. The default, an off
// calendar, moves nothing.
func (s *RunoffSimulator) WithCalendar(c calendar.Calendar) *RunoffSimulator {
	s.calendar = c
	return s
}

// roll is the offset from open of the business day on or after offset.
func (s *RunoffSimulator) roll(open shared.Date, offset int) int {
	return shared.DaysBetween(open, s.calendar.Following(open.AddDays(offset)))
}

// billOffset is the day of a payment's bill: the payment delay before it,
// rolled back to a business day and never before the open.
func (s *RunoffSimulator) billOffset(open shared.Date, payment int) int {
	bill := max(0, payment-int(s.params.PaymentDelayDays))
	return max(0, shared.DaysBetween(open, s.calendar.Preceding(open.AddDays(bill))))
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
	kindBill     = 2
)

// event is a case revision, a payment, or the bill a payment delay puts
// before a payment, on a day of the episode. On the same day revisions come
// first, then payments, then bills.
type event struct {
	offset int
	kind   int
	amount shared.Money // payments and bills only
}

// simulateClaim develops the claim's episodes in order, each settling the
// way the claim's section does. Each opens by moving the case to the
// episode's opening case on its open date: on the report date that is the
// claim's first ESTIMATE row, and on a reopen it re-raises the case from
// zero. With a seasonal holiday, the claim's deferral draws come from its
// own seasonal-holiday stream, so they never move a runoff draw.
func (s *RunoffSimulator) simulateClaim(src shared.RandomSource, c claim.Claim) []Transaction {
	e := &emitter{claimID: c.ID, report: c.ReportDate()}
	var holiday shared.RandomSource
	if s.holiday.Enabled() {
		holiday = src.Split("seasonal-holiday")
	}
	for _, ep := range c.Episodes {
		e.reviseTo(shared.DaysBetween(e.report, ep.Open), ep.OpeningCase)
		s.runEpisode(src, holiday, e, ep, s.settlement[c.Section])
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
//
// With a payment delay, each payment has a bill that many days before it.
// A bill above the case raises the case to the handler's view of the
// remaining cost, and at least to the bill. A revision from the bill's day
// to the payment keeps the case at or above the payment, and after the
// bill's day may lower it but not raise it, so every payment comes at least
// the delay after the case was last raised.
//
// holiday, nil when the seasonal holiday is off, draws the interim payments'
// deferrals.
func (s *RunoffSimulator) runEpisode(src, holiday shared.RandomSource, e *emitter, ep claim.Episode, st lob.SettlementParams) {
	ultimate, isNil := ep.Ultimate, ep.Nil
	base := shared.DaysBetween(e.report, ep.Open)
	duration := shared.DaysBetween(ep.Open, ep.Close)
	years := float64(duration) / 365
	delay := int(s.params.PaymentDelayDays)

	// payments are the interim payments and the final settlement, in date
	// order.
	var payments []event
	if !isNil {
		if ultimate < shared.OneCent {
			ultimate = shared.OneCent // guards hand-built claims; generated claims always cost something
		}
		payments = s.drawInterimPayments(src, holiday, st, ep.Open, ultimate, duration, years)
		final := ultimate
		for _, p := range payments {
			final -= p.amount
		}
		payments = append(payments, event{offset: duration, kind: kindPayment, amount: final})
	}
	events := append(s.drawRevisions(src, ep.Open, duration, years), payments...)
	if delay > 0 {
		for _, p := range payments {
			events = append(events, event{offset: s.billOffset(ep.Open, p.offset), kind: kindBill, amount: p.amount})
		}
	}
	sort.SliceStable(events, func(i, j int) bool {
		if events[i].offset != events[j].offset {
			return events[i].offset < events[j].offset
		}
		return events[i].kind < events[j].kind
	})

	paid := shared.Money(0)
	next := 0 // index of the next payment due
	aim := func(u float64) float64 {
		if isNil {
			return e.outstanding.Dollars()
		}
		return (ultimate - paid).Dollars() * s.adequacyBias(u)
	}
	elapsed := func(offset int) float64 {
		if duration == 0 {
			return 1
		}
		return float64(offset) / float64(duration)
	}
	// cover is the case a payment needs before it is made: the payment plus
	// the handler's view of what remains after it, read at the payment's
	// bill, so an interim payment never clears the case of an open claim
	// (MR-23). From the bill to the payment nothing is paid, so it is the
	// same throughout.
	cover := func(p event) shared.Money {
		rest := ultimate - paid - p.amount
		if rest <= 0 {
			return p.amount
		}
		return p.amount + max(shared.OneCent, shared.FromDollars(rest.Dollars()*s.adequacyBias(elapsed(s.billOffset(ep.Open, p.offset)))))
	}
	for _, ev := range events {
		switch ev.kind {
		case kindPayment:
			if c := cover(ev); e.outstanding < c {
				// Only without a payment delay; with one, the bill has
				// already raised the case.
				e.reviseTo(base+ev.offset, c)
			}
			e.pay(base+ev.offset, ev.amount)
			paid += ev.amount
			next++
		case kindBill:
			if c := cover(payments[next]); e.outstanding < c {
				e.reviseTo(base+ev.offset, max(c, shared.FromDollars(aim(elapsed(ev.offset)))))
			}
		default:
			u := elapsed(ev.offset)
			target := shared.FromDollars(aim(u) * shared.MeanOneLogNormal(src, s.params.RevisionSigma*(1-u)))
			if target < shared.OneCent {
				target = shared.OneCent // keep the case open so the terminal release lands on the close date
			}
			if delay > 0 && next < len(payments) {
				// From a payment's bill to the payment the case keeps
				// covering the payment and what remains after it, so the
				// bill has nothing to raise; after the bill's day it is
				// never raised.
				if bill := s.billOffset(ep.Open, payments[next].offset); ev.offset >= bill {
					if ev.offset > bill {
						target = min(target, e.outstanding)
					}
					target = max(target, cover(payments[next]))
				}
			}
			e.reviseTo(base+ev.offset, target)
		}
	}
	// The final settlement has cleared the remaining ultimate; the case
	// snaps to exactly zero.
	e.reviseTo(base+duration, 0)
}

// drawInterimPayments draws an episode's interim payments, on days strictly
// between open and close and at least the payment delay from both; the final
// settlement at close pays the rest. A lump-sum episode, drawn at the
// settlement's LumpSumProbability, has none, and so has an episode whose
// Poisson count is zero. Otherwise the payments share (1 - settlement share)
// of the ultimate by Dirichlet weights, given out in date order, and a
// payment below MinPayment, or less than the payment delay after the
// previous one, is held over to the next. When they would leave the final
// settlement below MinPayment, the last is paid with it instead.
//
// With a seasonal holiday, each payment day in the window may first be
// deferred a month, taking one draw from holiday per payment; one deferred
// past the last interim day is held over to the final settlement.
func (s *RunoffSimulator) drawInterimPayments(src, holiday shared.RandomSource, st lob.SettlementParams, open shared.Date, ultimate shared.Money, duration int, years float64) []event {
	delay := int(s.params.PaymentDelayDays)
	edge := max(1, delay)
	if duration < 2*edge {
		return nil
	}
	if st.LumpSumProbability > 0 && src.Bernoulli(st.LumpSumProbability) {
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
	pool := ultimate.MulFloat(1 - settlementShare(src, st)).Dollars()
	offsets := make([]int, n)
	for i := range offsets {
		offsets[i] = offsetBetween(src, edge, duration-edge)
	}
	if holiday != nil {
		for i, off := range offsets {
			deferred := s.holiday.Defer(open.AddDays(off), holiday.Uniform(), s.holiday.PaymentShare)
			offsets[i] = shared.DaysBetween(open, deferred)
		}
	}
	for i, off := range offsets {
		offsets[i] = s.roll(open, off) // one rolled past the last interim day is held over below
	}
	sort.Ints(offsets)
	minimum := shared.FromDollars(s.params.MinPayment)
	events := make([]event, 0, n)
	paid, held := shared.Money(0), shared.Money(0)
	last := 0 // the previous payment's day; the episode opens on day 0
	for i, w := range weights {
		amount := shared.FromDollars(pool*w/total) + held
		if amount <= 0 || amount < minimum || offsets[i]-last < delay || offsets[i] > duration-edge {
			held = amount // too small, too soon or too late to pay on its own: paid with the next, or at close
			continue
		}
		events = append(events, event{offset: offsets[i], kind: kindPayment, amount: amount})
		paid += amount
		held = 0
		last = offsets[i]
	}
	if paid >= ultimate {
		// Rounding degenerate: fall back to settling everything at close.
		return nil
	}
	if n := len(events); n > 0 && ultimate-paid < minimum {
		// The final settlement has the same floor as an interim payment
		// (MR-23): the last interim payment, itself at least the minimum, is
		// paid with it instead.
		events = events[:n-1]
	}
	return events
}

// settlementShare is the share of the ultimate an episode with interim
// payments leaves for its final settlement: the settlement's Share, or a Beta
// draw with that mean when its Concentration is above 0.
func settlementShare(src shared.RandomSource, st lob.SettlementParams) float64 {
	m, k := st.Share, st.Concentration
	if k == 0 {
		return m
	}
	return src.Beta(m*k, (1-m)*k)
}

// drawRevisions draws an episode's pure case revisions, on days strictly
// between open and close, each rolled to a business day; one rolled onto
// the close is dropped. Every revision is drawn either way, so dropping one
// moves no other draw.
func (s *RunoffSimulator) drawRevisions(src shared.RandomSource, open shared.Date, duration int, years float64) []event {
	if duration < 2 {
		return nil
	}
	n := src.Poisson(s.params.RevisionsPerYear * years)
	events := make([]event, 0, n)
	for range n {
		if off := s.roll(open, s.interiorOffset(src, duration)); off < duration {
			events = append(events, event{offset: off, kind: kindRevision})
		}
	}
	return events
}

// interiorOffset draws a day strictly between report (0) and close (duration).
func (s *RunoffSimulator) interiorOffset(src shared.RandomSource, duration int) int {
	return offsetBetween(src, 1, duration-1)
}

// offsetBetween draws a day from lo to hi inclusive, uniformly.
func offsetBetween(src shared.RandomSource, lo, hi int) int {
	return lo + int(src.Uniform()*float64(hi-lo+1))
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
