package web

import "slices"

// formField is one line-of-business parameter in the browser form. Path
// addresses the parameter in the preset JSON (the config package's json
// tags); Label and Tip are what the form shows. Kind, when set, limits a
// section field to sections whose severity is of that kind.
type formField struct {
	Path  []string `json:"path"`
	Label string   `json:"label"`
	Tip   string   `json:"tip"`
	Kind  string   `json:"kind,omitempty"`
}

// fieldGroup is one heading of the form and its fields, in display order.
// Sections, when set, is the path of a section list in the preset: the group
// is shown once per section, headed by the section's name, and its field
// paths are relative to the section.
type fieldGroup struct {
	Label    string      `json:"label"`
	Sections []string    `json:"sections,omitempty"`
	Fields   []formField `json:"fields"`
}

// severityFields are the parameters of each severity kind, relative to a
// section.
var severityFields = []formField{
	{Path: []string{"severity", "median_fraction"}, Kind: "sum_insured_lognormal", Label: "Severity median fraction", Tip: "Median ground-up loss as a fraction of sum insured; losses are capped at the sum insured."},
	{Path: []string{"severity", "sigma"}, Kind: "sum_insured_lognormal", Label: "Severity sigma", Tip: "Sigma of the lognormal loss fraction."},
	{Path: []string{"severity", "scale"}, Kind: "pareto", Label: "Severity scale", Tip: "Pareto scale (minimum loss) in start-year dollars."},
	{Path: []string{"severity", "alpha"}, Kind: "pareto", Label: "Severity alpha", Tip: "Pareto tail index; must exceed 1."},
	{Path: []string{"severity", "median"}, Kind: "lognormal", Label: "Severity median", Tip: "Median ground-up loss in start-year dollars; uncapped by the sum insured."},
	{Path: []string{"severity", "sigma"}, Kind: "lognormal", Label: "Severity sigma", Tip: "Sigma of the lognormal loss."},
	{Path: []string{"severity", "median"}, Kind: "lognormal_pareto", Label: "Severity body median", Tip: "Median of the lognormal body in start-year dollars."},
	{Path: []string{"severity", "sigma"}, Kind: "lognormal_pareto", Label: "Severity body sigma", Tip: "Sigma of the lognormal body."},
	{Path: []string{"severity", "scale"}, Kind: "lognormal_pareto", Label: "Severity tail threshold", Tip: "Loss in start-year dollars where the Pareto tail takes over from the body; the tail's share keeps the density continuous there."},
	{Path: []string{"severity", "alpha"}, Kind: "lognormal_pareto", Label: "Severity tail alpha", Tip: "Pareto tail index above the threshold; must exceed 1."},
}

// formFields is the parameter form's metadata, served at GET /api/fields so
// the browser builds the form generically (RF-13). Every numeric parameter in
// config.LOBParams has exactly one entry, which TestFormFieldsCoverEveryParameter
// enforces; the excess choices table, the names and the section switches are
// built separately or left to the YAML.
var formFields = []fieldGroup{
	{
		Label: "Book",
		Fields: []formField{
			{Path: []string{"book", "growth_factor"}, Label: "Growth factor", Tip: "Year-on-year trend in the number of fleets written, which without fleets is the policy count."},
			{Path: []string{"book", "size_volatility"}, Label: "Size volatility", Tip: "Sigma of the mean-1 lognormal noise on book size."},
			{Path: []string{"book", "spread"}, Label: "Spread", Tip: "Heterogeneity: sigma of sum insured and sd of the risk factor; with fleets, between the vehicles of one fleet."},
			{Path: []string{"book", "sum_insured_median"}, Label: "Sum insured median", Tip: "Year-1 median sum insured of a vehicle in dollars."},
			{Path: []string{"book", "sum_insured_inflation"}, Label: "Sum insured inflation", Tip: "Annual multiplicative drift of the median."},
			{Path: []string{"book", "fleet", "size", "median"}, Label: "Fleet size median", Tip: "Median vehicles on a fleet; each vehicle is a policy. 0 switches fleets off, every policy then its own fleet of one."},
			{Path: []string{"book", "fleet", "size", "sigma"}, Label: "Fleet size sigma", Tip: "Sigma of the lognormal fleet size, rounded to whole vehicles and at least 1."},
			{Path: []string{"book", "fleet", "sum_insured_sigma"}, Label: "Fleet sum insured sigma", Tip: "Lognormal sigma of a fleet's median vehicle sum insured around the book's median: how far fleets' vehicle values differ."},
			{Path: []string{"book", "fleet", "risk_spread"}, Label: "Fleet risk spread", Tip: "Sd of the mean-one gamma fleet risk factor that scales every vehicle's risk factor; 0 gives every fleet 1."},
		},
	},
	{
		Label: "Pricing",
		Fields: []formField{
			{Path: []string{"pricing", "target_loss_ratio"}, Label: "Target loss ratio", Tip: "Assumed loss ratio premium is priced to. Premium = assumed expected loss / target."},
			{Path: []string{"pricing", "adequacy_volatility"}, Label: "Pricing adequacy volatility", Tip: "Sigma of mean-one lognormal noise on each underwriting year's target loss ratio, like an underwriting cycle; 0 switches it off."},
			{Path: []string{"pricing", "nil_probability"}, Label: "Assumed nil claim probability", Tip: "Assumed share of claims that close without payment, for pricing. A nil claim still pays if it reopens."},
			{Path: []string{"pricing", "reopen_probability"}, Label: "Assumed reopen probability", Tip: "Assumed reopen chance feeding the pricing uplift."},
			{Path: []string{"pricing", "reopen_estimate_factor"}, Label: "Assumed reopen estimate factor", Tip: "Assumed reopen estimate factor feeding the pricing uplift."},
			{Path: []string{"pricing", "inflation_mean"}, Label: "Assumed inflation trend", Tip: "Assumed mean annual claims-inflation trend used for pricing, applied to the middle of each policy's cover."},
		},
	},
	{
		Label:    "Pricing",
		Sections: []string{"pricing", "sections"},
		Fields: slices.Concat([]formField{
			{Path: []string{"base_frequency"}, Label: "Assumed base frequency", Tip: "Assumed ground-up frequency per policy-year used for pricing (independent of the true claims frequency)."},
			{Path: []string{"limit"}, Label: "Assumed limit", Tip: "Per-claim limit assumed for pricing, in nominal dollars; 0 is unlimited."},
		}, severityFields),
	},
	{
		Label: "Claims",
		Fields: []formField{
			{Path: []string{"claims", "inflation", "mean"}, Label: "Claims inflation", Tip: "Average annual claims inflation factor, applied by occurrence date (1.0 = flat)."},
			{Path: []string{"claims", "inflation", "volatility"}, Label: "Claims inflation volatility", Tip: "Sigma of the mean-one lognormal noise on each year's inflation factor; 0 gives a smooth trend."},
			{Path: []string{"claims", "nil_probability"}, Label: "Nil claim probability", Tip: "Probability a claim closes without payment at its first close; 0 switches nil claims off."},
			{Path: []string{"claims", "reopening", "probability"}, Label: "Reopen probability", Tip: "Chance a closed claim reopens once; 0 switches reopening off."},
			{Path: []string{"claims", "reopening", "estimate_factor"}, Label: "Reopen estimate factor", Tip: "Mean additional reopen cost as a factor of the claim's ultimate; a sum-insured or limited section is capped at the cover left."},
			{Path: []string{"claims", "reopening", "estimate_sigma"}, Label: "Reopen estimate sigma", Tip: "Sigma of the mean-one lognormal noise on the reopen's additional cost."},
			{Path: []string{"claims", "reopening", "lag_median_days"}, Label: "Reopen lag median days", Tip: "Median days from first close to reopen."},
			{Path: []string{"claims", "reopening", "lag_sigma"}, Label: "Reopen lag sigma", Tip: "Sigma of the lognormal close-to-reopen lag."},
		},
	},
	{
		Label:    "Claims",
		Sections: []string{"claims", "sections"},
		Fields: slices.Concat([]formField{
			{Path: []string{"base_frequency"}, Label: "Base frequency", Tip: "Ground-up claims per policy-year at risk factor 1; 0 switches the section off."},
			{Path: []string{"limit"}, Label: "Limit", Tip: "Most the policy pays on one claim, reopen included, in nominal dollars; 0 is unlimited. Must be 0 on a sum-insured section, whose limit is the sum insured."},
		}, severityFields, []formField{
			{Path: []string{"report_lag", "median"}, Label: "Report lag median", Tip: "Median occurrence-to-report lag in days."},
			{Path: []string{"report_lag", "sigma"}, Label: "Report lag sigma", Tip: "Sigma of the lognormal report lag."},
			{Path: []string{"close_lag", "shape"}, Label: "Close lag shape", Tip: "Gamma shape of the report-to-close lag."},
			{Path: []string{"close_lag", "mean_days"}, Label: "Close lag mean days", Tip: "Mean report-to-close lag of a claim costing the size reference."},
			{Path: []string{"close_lag", "size_reference"}, Label: "Close lag size reference", Tip: "Claim cost in start-year dollars (deflated by claims inflation) that settles in the mean days."},
			{Path: []string{"close_lag", "size_elasticity"}, Label: "Close lag size elasticity", Tip: "Mean lag scales by (size / reference) to this power, so larger claims settle slower; 0 switches it off."},
			{Path: []string{"close_lag", "risk_loading"}, Label: "Close lag risk loading", Tip: "Exponent on the policy risk factor."},
		}),
	},
	{
		Label: "Recoveries",
		Fields: []formField{
			{Path: []string{"claims", "recoveries", "salvage", "probability"}, Label: "Salvage probability", Tip: "Chance a total loss (a written-off vehicle) yields salvage; 0 switches salvage off."},
			{Path: []string{"claims", "recoveries", "salvage", "mean_share"}, Label: "Salvage mean share", Tip: "Average salvage recovery as a share of the claim's gross paid."},
			{Path: []string{"claims", "recoveries", "salvage", "concentration"}, Label: "Salvage concentration", Tip: "Beta concentration of the salvage share; higher clusters shares tighter around the mean."},
			{Path: []string{"claims", "recoveries", "salvage", "lag_median_days"}, Label: "Salvage lag median days", Tip: "Median days from close to receiving the salvage."},
			{Path: []string{"claims", "recoveries", "salvage", "lag_sigma"}, Label: "Salvage lag sigma", Tip: "Sigma of the lognormal close-to-receipt lag for salvage."},
			{Path: []string{"claims", "recoveries", "subrogation", "probability"}, Label: "Subrogation probability", Tip: "Chance a paid claim in a section with recoveries is subrogated; 0 switches subrogation off."},
			{Path: []string{"claims", "recoveries", "subrogation", "mean_share"}, Label: "Subrogation mean share", Tip: "Average subrogation recovery as a share of the claim's gross paid."},
			{Path: []string{"claims", "recoveries", "subrogation", "concentration"}, Label: "Subrogation concentration", Tip: "Beta concentration of the subrogation share; higher clusters shares tighter around the mean."},
			{Path: []string{"claims", "recoveries", "subrogation", "lag_median_days"}, Label: "Subrogation lag median days", Tip: "Median days from close to receiving the subrogation recovery."},
			{Path: []string{"claims", "recoveries", "subrogation", "lag_sigma"}, Label: "Subrogation lag sigma", Tip: "Sigma of the lognormal close-to-receipt lag for subrogation."},
		},
	},
	{
		Label: "Runoff",
		Fields: []formField{
			{Path: []string{"runoff", "case_adequacy_mean"}, Label: "Case adequacy mean", Tip: "True ultimate over the expected opening case: above 1 under-reserves. Moves case reserves, not losses."},
			{Path: []string{"runoff", "case_adequacy_sigma"}, Label: "Case adequacy sigma", Tip: "Noise on each opening case estimate: how wrong individual estimates are."},
			{Path: []string{"runoff", "payments_per_year"}, Label: "Payments per year", Tip: "Poisson intensity of interim payments."},
			{Path: []string{"runoff", "settlement_share"}, Label: "Settlement share", Tip: "Mean fraction of ultimate paid at close, on claims with interim payments."},
			{Path: []string{"runoff", "settlement_concentration"}, Label: "Settlement concentration", Tip: "Beta concentration of each claim's settlement share around the settlement share; 0 fixes it."},
			{Path: []string{"runoff", "concentration"}, Label: "Concentration", Tip: "Dirichlet concentration across interim payments."},
			{Path: []string{"runoff", "revisions_per_year"}, Label: "Revisions per year", Tip: "Poisson intensity of pure case revisions."},
			{Path: []string{"runoff", "revision_sigma"}, Label: "Revision sigma", Tip: "Initial sigma of revision noise."},
		},
	},
}
