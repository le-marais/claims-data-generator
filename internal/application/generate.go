// Package application holds the use cases that orchestrate the domain:
// generating a dataset and evaluating its realism.
package application

import (
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

// GenerateDataset runs the three simulation stages. Each stage draws from
// its own labelled sub-stream of the given source, so results only depend
// on the master seed and the request.
func GenerateDataset(src shared.RandomSource, req GenerateRequest) (Dataset, error) {
	if err := req.validate(); err != nil {
		return Dataset{}, err
	}
	book := policy.NewBookSimulator(req.LOB.Book).
		Simulate(src.Split("book"), req.StartYear, req.Years, req.InitialBookSize)
	claims := claim.NewClaimSimulator(req.LOB.Claims).
		Simulate(src.Split("claims"), book)
	txs := transaction.NewRunoffSimulator(req.LOB.Runoff).
		Simulate(src.Split("runoff"), claims)
	return Dataset{Policies: book, Claims: claims, Transactions: txs}, nil
}
