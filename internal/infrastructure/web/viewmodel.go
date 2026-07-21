package web

import (
	"math"

	"github.com/le-marais/claimsgen/internal/application"
	"github.com/le-marais/claimsgen/internal/domain/triangle"
)

// scheduleP triangles have ten development years; the UI's triangles match
// the realism gate's shape.
const developmentYears = 10

type lobInfoJSON struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type generateResponseJSON struct {
	Run           runInfoJSON       `json:"run"`
	Summary       summaryJSON       `json:"summary"`
	Triangles     trianglesJSON     `json:"triangles"`
	Distributions distributionsJSON `json:"distributions"`
	Realism       realismJSON       `json:"realism"`
}

type runInfoJSON struct {
	LOB             string `json:"lob"`
	Seed            string `json:"seed"`
	StartYear       int    `json:"start_year"`
	Years           int    `json:"years"`
	InitialBookSize int    `json:"initial_book_size"`
	OutDir          string `json:"out_dir"`
	Policies        int    `json:"policies"`
	Claims          int    `json:"claims"`
	Transactions    int    `json:"transactions"`
}

type summaryJSON struct {
	Years []summaryRowJSON `json:"years"`
	Total summaryRowJSON   `json:"total"`
}

type summaryRowJSON struct {
	Year          int      `json:"year"`
	Policies      int      `json:"policies"`
	Claims        int      `json:"claims"`
	NilClaims     int      `json:"nil_claims"`
	Reopened      int      `json:"reopened"`
	EarnedPremium float64  `json:"earned_premium"`
	Paid          float64  `json:"paid"`
	Recovered     float64  `json:"recovered"`
	LossRatio     *float64 `json:"loss_ratio"`
}

type trianglesJSON struct {
	Paid     triangleJSON `json:"paid"`
	NetPaid  triangleJSON `json:"net_paid"`
	Incurred triangleJSON `json:"incurred"`
}

type triangleJSON struct {
	StartYear int         `json:"start_year"`
	Cells     [][]float64 `json:"cells"`
	ATA       []*float64  `json:"ata"`
}

type distributionsJSON struct {
	Severity      histogramJSON `json:"severity"`
	ReportLagDays histogramJSON `json:"report_lag_days"`
	CloseLagDays  histogramJSON `json:"close_lag_days"`
}

type histogramJSON struct {
	Bins []binJSON `json:"bins"`
}

type binJSON struct {
	Lo    float64 `json:"lo"`
	Hi    float64 `json:"hi"`
	Count int     `json:"count"`
}

type realismJSON struct {
	Pass           bool           `json:"pass"`
	PaidATA        []ageCheckJSON `json:"paid_ata"`
	IncurredATA    []ageCheckJSON `json:"incurred_ata"`
	LossRatio      checkJSON      `json:"loss_ratio"`
	LossRatioDrift checkJSON      `json:"loss_ratio_drift"`
}

type ageCheckJSON struct {
	Age    int     `json:"age"`
	Value  float64 `json:"value"`
	Lo     float64 `json:"lo"`
	Hi     float64 `json:"hi"`
	Min    float64 `json:"min"`
	Max    float64 `json:"max"`
	Within bool    `json:"within"`
}

type checkJSON struct {
	Value  float64 `json:"value"`
	Lo     float64 `json:"lo"`
	Hi     float64 `json:"hi"`
	Min    float64 `json:"min"`
	Max    float64 `json:"max"`
	Within bool    `json:"within"`
}

func buildResponse(req generateRequest, ds application.Dataset, refs []triangle.ReferenceSet) generateResponseJSON {
	paid := triangle.PaidTriangle(ds.Claims, ds.Transactions, req.StartYear, req.Years, developmentYears)
	incurred := triangle.IncurredTriangle(ds.Claims, ds.Transactions, req.StartYear, req.Years, developmentYears)
	netPaid := triangle.NetPaidTriangle(ds.Claims, ds.Transactions, req.StartYear, req.Years, developmentYears)
	return generateResponseJSON{
		Run: runInfoJSON{
			LOB:             req.Params.Name,
			Seed:            req.Seed,
			StartYear:       req.StartYear,
			Years:           req.Years,
			InitialBookSize: req.InitialBookSize,
			OutDir:          req.OutDir,
			Policies:        len(ds.Policies),
			Claims:          len(ds.Claims),
			Transactions:    len(ds.Transactions),
		},
		Summary:       summaryView(application.Summarize(ds, req.StartYear, req.Years)),
		Triangles:     trianglesJSON{Paid: triangleView(paid), NetPaid: triangleView(netPaid), Incurred: triangleView(incurred)},
		Distributions: distributionsView(application.ComputeDistributions(ds)),
		Realism:       realismView(application.EvaluateRealism(ds, refs, req.StartYear, req.Years)),
	}
}

func summaryView(r application.SummaryReport) summaryJSON {
	rows := make([]summaryRowJSON, len(r.Years))
	for i, y := range r.Years {
		rows[i] = summaryRowView(y)
	}
	return summaryJSON{Years: rows, Total: summaryRowView(r.Total)}
}

func summaryRowView(s application.YearSummary) summaryRowJSON {
	row := summaryRowJSON{
		Year:          s.Year,
		Policies:      s.Policies,
		Claims:        s.Claims,
		NilClaims:     s.NilClaims,
		Reopened:      s.Reopened,
		EarnedPremium: s.EarnedPremium,
		Paid:          s.Paid,
		Recovered:     s.Recovered,
	}
	if lr, ok := s.LossRatio(); ok {
		row.LossRatio = &lr
	}
	return row
}

// triangleView converts a triangle for JSON: NaN age-to-age factors (ages
// with no usable data) become null.
func triangleView(t triangle.Triangle) triangleJSON {
	factors := t.ATAFactors()
	ata := make([]*float64, len(factors))
	for i, f := range factors {
		if !math.IsNaN(f) {
			v := f
			ata[i] = &v
		}
	}
	return triangleJSON{StartYear: t.StartYear, Cells: t.Cells, ATA: ata}
}

func distributionsView(d application.Distributions) distributionsJSON {
	return distributionsJSON{
		Severity:      histogramView(d.Severity),
		ReportLagDays: histogramView(d.ReportLagDays),
		CloseLagDays:  histogramView(d.CloseLagDays),
	}
}

func histogramView(h application.Histogram) histogramJSON {
	bins := make([]binJSON, len(h.Bins))
	for i, b := range h.Bins {
		bins[i] = binJSON{Lo: b.Lo, Hi: b.Hi, Count: b.Count}
	}
	return histogramJSON{Bins: bins}
}

// finite replaces NaN/±Inf (reachable when no reference band exists) with 0 so
// the response stays valid JSON.
func finite(f float64) float64 {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return 0
	}
	return f
}

func realismView(r triangle.Report) realismJSON {
	return realismJSON{
		Pass:        r.Pass(),
		PaidATA:     ageChecksView(r.PaidATA),
		IncurredATA: ageChecksView(r.IncurredATA),
		LossRatio: checkJSON{
			Value:  finite(r.LossRatio.Value),
			Lo:     finite(r.LossRatio.Band.Lo),
			Hi:     finite(r.LossRatio.Band.Hi),
			Min:    finite(r.LossRatio.Band.Min),
			Max:    finite(r.LossRatio.Band.Max),
			Within: r.LossRatio.Within,
		},
		LossRatioDrift: checkJSON{
			Value:  finite(r.LossRatioDrift.Value),
			Lo:     finite(r.LossRatioDrift.Band.Lo),
			Hi:     finite(r.LossRatioDrift.Band.Hi),
			Min:    finite(r.LossRatioDrift.Band.Min),
			Max:    finite(r.LossRatioDrift.Band.Max),
			Within: r.LossRatioDrift.Within,
		},
	}
}

func ageChecksView(checks []triangle.AgeCheck) []ageCheckJSON {
	out := make([]ageCheckJSON, len(checks))
	for i, c := range checks {
		out[i] = ageCheckJSON{
			Age: c.Age, Value: finite(c.Value),
			Lo: finite(c.Band.Lo), Hi: finite(c.Band.Hi), Min: finite(c.Band.Min), Max: finite(c.Band.Max),
			Within: c.Within,
		}
	}
	return out
}
