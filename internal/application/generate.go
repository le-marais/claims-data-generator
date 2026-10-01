// Package application holds the use cases that orchestrate the domain:
// generating a dataset and evaluating its realism.
package application

import (
	"context"
	"fmt"

	"github.com/le-marais/claimsgen/internal/domain/claim"
	"github.com/le-marais/claimsgen/internal/domain/lob"
	"github.com/le-marais/claimsgen/internal/domain/policy"
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
	book := policy.NewBookSimulator(req.LOB.Book, req.LOB.Pricing).
		Simulate(src.Split("book"), req.StartYear, req.Years, req.InitialBookSize)
	if err := ctx.Err(); err != nil {
		return Dataset{}, err
	}
	// Occurrences are constrained to the window (MF-2), so the inflation index
	// only needs to span the window years.
	inflation := claim.NewInflationIndex(src.Split("inflation"), req.LOB.Claims.Inflation, req.StartYear, req.Years)
	claims := claim.NewClaimSimulator(req.LOB.Claims).
		WithInflation(inflation).
		WithWindow(req.StartYear, req.Years).
		Simulate(src.Split("claims"), book)
	claims = claim.NewReopenSimulator(req.LOB.Claims).
		WithInflation(inflation).
		Apply(src.Split("reopening"), claims)
	claims = transaction.NewCaseEstimator(req.LOB.Runoff).
		Apply(src.Split("case-estimate"), claims)
	if err := ctx.Err(); err != nil {
		return Dataset{}, err
	}
	txs := transaction.NewRunoffSimulator(req.LOB.Runoff).
		Simulate(src.Split("runoff"), claims)
	if err := ctx.Err(); err != nil {
		return Dataset{}, err
	}
	txs = transaction.NewRecoverySimulator(req.LOB.Claims.Recoveries).
		Apply(src.Split("recovery"), claims, txs)
	return Dataset{Policies: book, Claims: claims, Transactions: txs}, nil
}
