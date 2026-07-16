package application

import (
	"math"

	"github.com/le-marais/claimsgen/internal/domain/shared"
	"github.com/le-marais/claimsgen/internal/domain/transaction"
)

// HistogramBin is one bin: [Lo, Hi) except the last bin, which includes Hi.
type HistogramBin struct {
	Lo, Hi float64
	Count  int
}

// Histogram is a binned view of a sample, ready to render.
type Histogram struct {
	Bins []HistogramBin
}

// LinearHistogram bins values into equal-width bins spanning [min, max].
func LinearHistogram(values []float64, bins int) Histogram {
	if len(values) == 0 || bins < 1 {
		return Histogram{}
	}
	lo, hi := minMax(values)
	if hi == lo {
		hi = lo + 1
	}
	width := (hi - lo) / float64(bins)
	h := Histogram{Bins: make([]HistogramBin, bins)}
	for i := range h.Bins {
		h.Bins[i] = HistogramBin{Lo: lo + float64(i)*width, Hi: lo + float64(i+1)*width}
	}
	h.Bins[bins-1].Hi = hi
	for _, v := range values {
		i := int((v - lo) / width)
		if i >= bins {
			i = bins - 1
		}
		h.Bins[i].Count++
	}
	return h
}

// LogHistogram bins positive values into log10-spaced bins; values at or
// below zero are ignored.
func LogHistogram(values []float64, bins int) Histogram {
	logs := make([]float64, 0, len(values))
	for _, v := range values {
		if v > 0 {
			logs = append(logs, math.Log10(v))
		}
	}
	h := LinearHistogram(logs, bins)
	for i, b := range h.Bins {
		h.Bins[i].Lo = math.Pow(10, b.Lo)
		h.Bins[i].Hi = math.Pow(10, b.Hi)
	}
	return h
}

func minMax(values []float64) (float64, float64) {
	lo, hi := values[0], values[0]
	for _, v := range values[1:] {
		lo = math.Min(lo, v)
		hi = math.Max(hi, v)
	}
	return lo, hi
}

// Distributions are the histograms behind the UI's distributions tab.
type Distributions struct {
	// Severity bins each claim's ultimate (total paid) on a log scale.
	Severity      Histogram
	ReportLagDays Histogram
	CloseLagDays  Histogram
}

const histogramBins = 20

// ComputeDistributions bins claim severities and lags for display.
func ComputeDistributions(ds Dataset) Distributions {
	paid := make(map[int]float64, len(ds.Claims))
	for _, tx := range ds.Transactions {
		if tx.Type == transaction.Payment {
			paid[tx.ClaimID] += tx.Amount.Dollars()
		}
	}
	severities := make([]float64, 0, len(ds.Claims))
	reportLags := make([]float64, 0, len(ds.Claims))
	closeLags := make([]float64, 0, len(ds.Claims))
	for _, c := range ds.Claims {
		severities = append(severities, paid[c.ID])
		reportLags = append(reportLags, float64(shared.DaysBetween(c.OccurrenceDate, c.ReportDate)))
		closeLags = append(closeLags, float64(shared.DaysBetween(c.ReportDate, c.CloseDate)))
	}
	return Distributions{
		Severity:      LogHistogram(severities, histogramBins),
		ReportLagDays: LinearHistogram(reportLags, histogramBins),
		CloseLagDays:  LinearHistogram(closeLags, histogramBins),
	}
}
