// Package csv writes the generated dataset and its aggregates as CSV files
// with stable formatting, so identical datasets produce byte-identical files:
// policies.csv, claims.csv and transactions.csv from WriteDataset, plus
// triangles.csv and exposure.csv from WriteAggregates, or all five as one zip
// archive from WriteZip.
package csv

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"

	"github.com/le-marais/claimsgen/internal/application"
)

// opener opens one output file by name for writing; done finishes it.
type opener func(name string) (w io.Writer, done func() error, err error)

// dirOpener opens files in dir, creating the directory first.
func dirOpener(dir string) (opener, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("creating output directory: %w", err)
	}
	return func(name string) (io.Writer, func() error, error) {
		f, err := os.Create(filepath.Join(dir, name))
		if err != nil {
			return nil, nil, err
		}
		return f, f.Close, nil
	}, nil
}

// WriteDataset writes policies.csv, claims.csv and transactions.csv into
// dir, creating it if needed.
func WriteDataset(dir string, ds application.Dataset) error {
	open, err := dirOpener(dir)
	if err != nil {
		return err
	}
	return writeDataset(open, ds)
}

func writeDataset(open opener, ds application.Dataset) error {
	// Every field below is numeric, an ISO-8601 date, or a fixed enum - none can
	// contain a comma or newline - so the rows need no CSV quoting and plain
	// fmt.Sprintf is safe. If a free-text column is ever added, switch to
	// encoding/csv.
	if err := writeFile(open, "policies.csv",
		"policy_id,fleet_id,cover_start,cover_end,sum_insured,excess,risk_factor,premium",
		len(ds.Policies), func(i int) string {
			p := ds.Policies[i]
			return fmt.Sprintf("%d,%d,%s,%s,%s,%s,%s,%s",
				p.ID, p.FleetID, p.CoverStart, p.CoverEnd, p.SumInsured, p.Excess,
				FormatRiskFactor(p.RiskFactor), p.Premium)
		}); err != nil {
		return err
	}
	if err := writeFile(open, "claims.csv",
		"claim_id,policy_id,occurrence_date,report_date,close_date,initial_estimate",
		len(ds.Claims), func(i int) string {
			c := ds.Claims[i].Record() // claims.csv carries the record only, never the development context
			return fmt.Sprintf("%d,%d,%s,%s,%s,%s",
				c.ID, c.PolicyID, c.OccurrenceDate, c.ReportDate, c.CloseDate, c.InitialEstimate)
		}); err != nil {
		return err
	}
	return writeFile(open, "transactions.csv",
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

func writeFile(open opener, name, header string, rows int, row func(int) string) (err error) {
	f, done, err := open(name)
	if err != nil {
		return fmt.Errorf("creating %s: %w", name, err)
	}
	defer func() {
		if cerr := done(); cerr != nil && err == nil {
			err = fmt.Errorf("closing %s: %w", name, cerr)
		}
	}()
	w := bufio.NewWriter(f)
	fmt.Fprintln(w, header)
	for i := 0; i < rows; i++ {
		fmt.Fprintln(w, row(i))
	}
	if err := w.Flush(); err != nil {
		return fmt.Errorf("writing %s: %w", name, err)
	}
	return nil
}
