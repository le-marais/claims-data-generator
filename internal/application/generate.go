// Package application holds the use cases that orchestrate the domain:
// generating a dataset and evaluating its realism.
package application

import (
	"context"
	"fmt"

	"github.com/le-marais/claimsgen/internal/domain/calendar"
	"github.com/le-marais/claimsgen/internal/domain/claim"
	"github.com/le-marais/claimsgen/internal/domain/lob"
	"github.com/le-marais/claimsgen/internal/domain/policy"
	"github.com/le-marais/claimsgen/internal/domain/premium"
	"github.com/le-marais/claimsgen/internal/domain/shared"
	"github.com/le-marais/claimsgen/internal/domain/transaction"
)

// GenerateRequest describes one generation run.
type GenerateRequest struct {
	LOB             lob.LineOfBusiness
	StartYear       int
	Years           int
	InitialBookSize int
}

// Dataset is the generated output: three linked datasets.
type Dataset struct {
	Binders      []policy.Binder
	Premium      premium.Report
	Policies     []policy.Policy
	Claims       []claim.Claim
	Transactions []transaction.Transaction
}

func (r GenerateRequest) validate() error {
	if r.Years < 1 {
		return fmt.Errorf("years: must be at least 1, got %d", r.Years)
	}
	if r.InitialBookSize < 1 {
		return fmt.Errorf("initial book size: must be at least 1, got %d", r.InitialBookSize)
	}
	return r.LOB.Validate()
}

// GenerateDataset runs the seven simulation stages. Each stage draws from
// its own labelled sub-stream of the given source, so results only depend
// on the master seed and the request - never on ctx, which only decides how
// early an abandoned run stops.
//
// Cancellation is checked between stages rather than inside them: the domain
// stays free of infrastructure concerns, and each stage is bounded work, so a
// caller that walks away waits at most one stage rather than a whole run.
func GenerateDataset(ctx context.Context, src shared.RandomSource, req GenerateRequest) (Dataset, error) {
	if err := req.validate(); err != nil {
		return Dataset{}, err
	}
	if err := ctx.Err(); err != nil {
		return Dataset{}, err
	}
	cal, err := calendar.Lookup(req.LOB.BusinessDays.Calendar)
	if err != nil {
		return Dataset{}, fmt.Errorf("business_days.calendar: %w", err)
	}
	sim := policy.NewBookSimulator(req.LOB.Book, req.LOB.Pricing)
	book := sim.Simulate(src.Split("book"), req.StartYear, req.Years, req.InitialBookSize)
	if err := ctx.Err(); err != nil {
		return Dataset{}, err
	}
	horizon := req.Years
	var binders []policy.Binder
	var report premium.Report
	if req.LOB.Book.AnnualBinders {
		binders = sim.BinderPlans(src.Split("binder-plans"), req.StartYear, req.Years, req.InitialBookSize)
		for _, p := range book {
			horizon = max(horizon, p.CoverEnd.Year()-req.StartYear+1)
		}
	}
	if req.LOB.PremiumReporting.Enabled {
		report = premium.Generate(src.Split("premium-reporting"), req.LOB.PremiumReporting, book)
	}
	inflation := claim.NewInflationIndex(src.Split("inflation"), req.LOB.Claims.Inflation, req.StartYear, horizon)
	events := claim.DrawEvents(src, req.LOB.Claims.Sections, req.StartYear, horizon)
	// A paying claim stays open long enough for the runoff to keep its
	// payments the payment delay after the case is raised.
	delay := int(req.LOB.Runoff.PaymentDelayDays)
	claims := claim.NewClaimSimulator(req.LOB.Claims).
		WithInflation(inflation).
		WithEvents(events).
		WithWindow(req.StartYear, horizon).
		WithPaymentDelay(delay).
		WithSeasonalHoliday(req.LOB.SeasonalHoliday).
		WithBusinessDays(cal, req.LOB.BusinessDays.RollReports).
		Simulate(src.Split("claims"), book)
	claims = claim.NewReopenSimulator(req.LOB.Claims).
		WithInflation(inflation).
		WithPaymentDelay(delay).
		WithSeasonalHoliday(req.LOB.SeasonalHoliday).
		WithCalendar(cal).
		Apply(src.Split("reopening"), claims)
	claims = transaction.NewCaseEstimator(req.LOB.Runoff, req.LOB.Claims.Sections).
		Apply(src.Split("case-estimate"), claims)
	if err := ctx.Err(); err != nil {
		return Dataset{}, err
	}
	txs := transaction.NewRunoffSimulator(req.LOB.Runoff, req.LOB.Claims.Sections).
		WithSeasonalHoliday(req.LOB.SeasonalHoliday).
		WithCalendar(cal).
		Simulate(src.Split("runoff"), claims)
	if err := ctx.Err(); err != nil {
		return Dataset{}, err
	}
	txs = transaction.NewRecoverySimulator(req.LOB.Claims).
		WithCalendar(cal).
		Apply(src.Split("recovery"), claims, txs)
	return Dataset{Binders: binders, Premium: report, Policies: book, Claims: claims, Transactions: txs}, nil
}
