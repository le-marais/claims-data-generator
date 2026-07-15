package csv_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/le-marais/claimsgen/internal/application"
	csvout "github.com/le-marais/claimsgen/internal/infrastructure/csv"
	"github.com/le-marais/claimsgen/internal/infrastructure/config"
	"github.com/le-marais/claimsgen/internal/infrastructure/random"
)

func dataset(t *testing.T) application.Dataset {
	t.Helper()
	l, err := config.MotorPersonal()
	if err != nil {
		t.Fatal(err)
	}
	ds, err := application.GenerateDataset(random.NewSource(42), application.GenerateRequest{
		LOB: l, StartYear: 1998, Years: 2, InitialBookSize: 200,
	})
	if err != nil {
		t.Fatal(err)
	}
	return ds
}

func readLines(t *testing.T, path string) []string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimRight(string(b), "\n"), "\n")
}

func TestWriteDatasetProducesThreeLinkedCSVs(t *testing.T) {
	ds := dataset(t)
	dir := t.TempDir()
	if err := csvout.WriteDataset(dir, ds); err != nil {
		t.Fatal(err)
	}

	policies := readLines(t, filepath.Join(dir, "policies.csv"))
	if policies[0] != "policy_id,cover_start,cover_end,sum_insured,excess,risk_factor,premium" {
		t.Errorf("policies header = %q", policies[0])
	}
	if len(policies)-1 != len(ds.Policies) {
		t.Errorf("policies rows = %d, want %d", len(policies)-1, len(ds.Policies))
	}

	claims := readLines(t, filepath.Join(dir, "claims.csv"))
	if claims[0] != "claim_id,policy_id,occurrence_date,report_date,close_date,initial_estimate" {
		t.Errorf("claims header = %q", claims[0])
	}
	if len(claims)-1 != len(ds.Claims) {
		t.Errorf("claims rows = %d, want %d", len(claims)-1, len(ds.Claims))
	}

	txs := readLines(t, filepath.Join(dir, "transactions.csv"))
	if txs[0] != "transaction_id,claim_id,date,type,amount" {
		t.Errorf("transactions header = %q", txs[0])
	}
	if len(txs)-1 != len(ds.Transactions) {
		t.Errorf("transactions rows = %d, want %d", len(txs)-1, len(ds.Transactions))
	}

	// Spot-check first data rows match the in-memory dataset.
	p := ds.Policies[0]
	wantPolicy := strings.Join([]string{
		"1", p.CoverStart.String(), p.CoverEnd.String(), p.SumInsured.String(),
		p.Excess.String(), formatRisk(t, p.RiskFactor), p.Premium.String(),
	}, ",")
	if policies[1] != wantPolicy {
		t.Errorf("first policy row = %q, want %q", policies[1], wantPolicy)
	}
	tx := ds.Transactions[0]
	wantTx := strings.Join([]string{"1", "1", tx.Date.String(), string(tx.Type), tx.Amount.String()}, ",")
	if txs[1] != wantTx {
		t.Errorf("first transaction row = %q, want %q", txs[1], wantTx)
	}
}

func formatRisk(t *testing.T, r float64) string {
	t.Helper()
	s := csvout.FormatRiskFactor(r)
	if !strings.Contains(s, ".") {
		t.Fatalf("risk factor %q not decimal formatted", s)
	}
	return s
}

func TestWriteDatasetIsByteIdentical(t *testing.T) {
	ds := dataset(t)
	dirA, dirB := t.TempDir(), t.TempDir()
	if err := csvout.WriteDataset(dirA, ds); err != nil {
		t.Fatal(err)
	}
	if err := csvout.WriteDataset(dirB, ds); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"policies.csv", "claims.csv", "transactions.csv"} {
		a, err := os.ReadFile(filepath.Join(dirA, name))
		if err != nil {
			t.Fatal(err)
		}
		b, err := os.ReadFile(filepath.Join(dirB, name))
		if err != nil {
			t.Fatal(err)
		}
		if string(a) != string(b) {
			t.Errorf("%s differs between identical writes", name)
		}
	}
}

func TestWriteDatasetCreatesDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "out")
	if err := csvout.WriteDataset(dir, dataset(t)); err != nil {
		t.Fatalf("WriteDataset to missing directory failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "claims.csv")); err != nil {
		t.Fatal(err)
	}
}
