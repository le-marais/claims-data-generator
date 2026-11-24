package csv

import (
	"fmt"

	"github.com/le-marais/claimsgen/internal/application"
)

func writeBinderDataset(open opener, ds application.Dataset) error {
	if err := writeFile(open, "binders.csv", "binder_id,coverholder_id,inception,expiry,year_of_account,estimate_cents", len(ds.Binders), func(i int) string {
		b := ds.Binders[i]
		return fmt.Sprintf("%d,%d,%s,%s,%d,%d", b.ID, b.CoverholderID, b.Inception, b.Expiry, b.YearOfAccount, int64(b.Estimate))
	}); err != nil {
		return err
	}
	if err := writeFile(open, "policy_binders.csv", "policy_id,binder_id", len(ds.Policies), func(i int) string {
		p := ds.Policies[i]
		return fmt.Sprintf("%d,%d", p.ID, p.BinderID)
	}); err != nil {
		return err
	}
	if err := writeFile(open, "premium_batches.csv", "batch_id,binder_id,reporting_month,processed_date", len(ds.Premium.Batches), func(i int) string {
		b := ds.Premium.Batches[i]
		return fmt.Sprintf("%s,%d,%s,%s", b.ID, b.BinderID, b.Month, b.Processed)
	}); err != nil {
		return err
	}
	if err := writeFile(open, "premium_declarations.csv", "declaration_id,original_declaration_id,policy_id,binder_id,policy_inception,effective_date,batch_id,processed_date,type,amount_cents", len(ds.Premium.Declarations), func(i int) string {
		d := ds.Premium.Declarations[i]
		return fmt.Sprintf("%d,%d,%d,%d,%s,%s,%s,%s,%s,%d", d.ID, d.OriginalID, d.PolicyID, d.BinderID, d.Inception, d.Effective, d.BatchID, d.Processed, d.Type, int64(d.Amount))
	}); err != nil {
		return err
	}
	return writeEpisodes(open, ds)
}
