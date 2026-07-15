package application_test

import (
	"strings"
	"testing"

	"github.com/le-marais/claimsgen/internal/application"
	"github.com/le-marais/claimsgen/internal/infrastructure/config"
	"github.com/le-marais/claimsgen/internal/infrastructure/random"
)

func request(t *testing.T) application.GenerateRequest {
	t.Helper()
	l, err := config.MotorPersonal()
	if err != nil {
		t.Fatal(err)
	}
	return application.GenerateRequest{
		LOB:             l,
		StartYear:       1998,
		Years:           3,
		InitialBookSize: 500,
	}
}

func TestGenerateDatasetLinksTheThreeDatasets(t *testing.T) {
	ds, err := application.GenerateDataset(random.NewSource(42), request(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(ds.Policies) == 0 || len(ds.Claims) == 0 || len(ds.Transactions) == 0 {
		t.Fatalf("empty dataset: %d policies, %d claims, %d transactions",
			len(ds.Policies), len(ds.Claims), len(ds.Transactions))
	}
	policyIDs := map[int]bool{}
	for _, p := range ds.Policies {
		policyIDs[p.ID] = true
	}
	claimIDs := map[int]bool{}
	for _, c := range ds.Claims {
		if !policyIDs[c.PolicyID] {
			t.Fatalf("claim %d references missing policy %d", c.ID, c.PolicyID)
		}
		claimIDs[c.ID] = true
	}
	for _, tx := range ds.Transactions {
		if !claimIDs[tx.ClaimID] {
			t.Fatalf("transaction %d references missing claim %d", tx.ID, tx.ClaimID)
		}
	}
}

func TestGenerateDatasetIsDeterministic(t *testing.T) {
	a, err := application.GenerateDataset(random.NewSource(7), request(t))
	if err != nil {
		t.Fatal(err)
	}
	b, err := application.GenerateDataset(random.NewSource(7), request(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Policies) != len(b.Policies) || len(a.Claims) != len(b.Claims) || len(a.Transactions) != len(b.Transactions) {
		t.Fatal("dataset sizes differ between identical runs")
	}
	for i := range a.Transactions {
		if a.Transactions[i] != b.Transactions[i] {
			t.Fatalf("transaction %d differs between identical runs", i)
		}
	}
}

func TestGenerateDatasetValidatesRequest(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*application.GenerateRequest)
		want   string
	}{
		{"years", func(r *application.GenerateRequest) { r.Years = 0 }, "years"},
		{"book size", func(r *application.GenerateRequest) { r.InitialBookSize = 0 }, "initial book size"},
		{"lob", func(r *application.GenerateRequest) { r.LOB.Book.Spread = -1 }, "book.spread"},
	}
	for _, c := range cases {
		req := request(t)
		c.mutate(&req)
		_, err := application.GenerateDataset(random.NewSource(1), req)
		if err == nil {
			t.Errorf("%s: expected error, got nil", c.name)
			continue
		}
		if !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: error %q does not mention %q", c.name, err.Error(), c.want)
		}
	}
}
