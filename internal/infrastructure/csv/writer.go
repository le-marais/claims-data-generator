// Package csv writes the generated dataset as three linked CSV files with
// stable formatting, so identical datasets produce byte-identical files.
package csv

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/le-marais/claimsgen/internal/application"
)

// WriteDataset writes policies.csv, claims.csv and transactions.csv into
// dir, creating it if needed.
func WriteDataset(dir string, ds application.Dataset) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating output directory: %w", err)
	}
	if err := writeFile(dir, "policies.csv",
		"policy_id,cover_start,cover_end,sum_insured,excess,risk_factor,premium",
		len(ds.Policies), func(i int) string {
			p := ds.Policies[i]
			return fmt.Sprintf("%d,%s,%s,%s,%s,%s,%s",
				p.ID, p.CoverStart, p.CoverEnd, p.SumInsured, p.Excess,
				FormatRiskFactor(p.RiskFactor), p.Premium)
		}); err != nil {
		return err
	}
	if err := writeFile(dir, "claims.csv",
		"claim_id,policy_id,occurrence_date,report_date,close_date,initial_estimate",
		len(ds.Claims), func(i int) string {
			c := ds.Claims[i]
			return fmt.Sprintf("%d,%d,%s,%s,%s,%s",
				c.ID, c.PolicyID, c.OccurrenceDate, c.ReportDate, c.CloseDate, c.InitialEstimate)
		}); err != nil {
		return err
	}
	return writeFile(dir, "transactions.csv",
		"transaction_id,claim_id,date,type,amount",
		len(ds.Transactions), func(i int) string {
			tx := ds.Transactions[i]
			return fmt.Sprintf("%d,%d,%s,%s,%s", tx.ID, tx.ClaimID, tx.Date, tx.Type, tx.Amount)
		})
}

// FormatRiskFactor renders a risk factor with fixed precision so output is
// byte-stable.
func FormatRiskFactor(r float64) string {
	return strconv.FormatFloat(r, 'f', 6, 64)
}

func writeFile(dir, name, header string, rows int, row func(int) string) error {
	f, err := os.Create(filepath.Join(dir, name))
	if err != nil {
		return fmt.Errorf("creating %s: %w", name, err)
	}
	w := bufio.NewWriter(f)
	fmt.Fprintln(w, header)
	for i := 0; i < rows; i++ {
		fmt.Fprintln(w, row(i))
	}
	if err := w.Flush(); err != nil {
		f.Close()
		return fmt.Errorf("writing %s: %w", name, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("closing %s: %w", name, err)
	}
	return nil
}
