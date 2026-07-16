package application_test

import (
	"math"
	"reflect"
	"testing"

	"github.com/le-marais/claimsgen/internal/application"
)

func TestLinearHistogram(t *testing.T) {
	h := application.LinearHistogram([]float64{0, 1, 2, 3, 4, 5, 6, 7, 8, 9}, 5)
	if len(h.Bins) != 5 {
		t.Fatalf("bins = %d, want 5", len(h.Bins))
	}
	for i, b := range h.Bins {
		if b.Count != 2 {
			t.Fatalf("bin %d count = %d, want 2", i, b.Count)
		}
	}
	if h.Bins[0].Lo != 0 || h.Bins[4].Hi != 9 {
		t.Fatalf("range = [%v, %v], want [0, 9]", h.Bins[0].Lo, h.Bins[4].Hi)
	}
}

func TestLinearHistogramSingleValue(t *testing.T) {
	h := application.LinearHistogram([]float64{5, 5, 5}, 4)
	total := 0
	for _, b := range h.Bins {
		total += b.Count
	}
	if total != 3 {
		t.Fatalf("total count = %d, want 3", total)
	}
}

func TestLinearHistogramEmpty(t *testing.T) {
	if h := application.LinearHistogram(nil, 5); len(h.Bins) != 0 {
		t.Fatalf("bins = %d, want 0", len(h.Bins))
	}
}

func TestLogHistogram(t *testing.T) {
	h := application.LogHistogram([]float64{1, 10, 100, 0, -5}, 2)
	if len(h.Bins) != 2 {
		t.Fatalf("bins = %d, want 2", len(h.Bins))
	}
	if got := []int{h.Bins[0].Count, h.Bins[1].Count}; !reflect.DeepEqual(got, []int{1, 2}) {
		t.Fatalf("counts = %v, want [1 2] (non-positive values ignored)", got)
	}
	if math.Abs(h.Bins[0].Lo-1) > 1e-9 || math.Abs(h.Bins[0].Hi-10) > 1e-9 || math.Abs(h.Bins[1].Hi-100) > 1e-9 {
		t.Fatalf("edges = [%v %v][%v %v], want [1 10][10 100]", h.Bins[0].Lo, h.Bins[0].Hi, h.Bins[1].Lo, h.Bins[1].Hi)
	}
}

func TestComputeDistributions(t *testing.T) {
	d := application.ComputeDistributions(tinyDataset())
	for name, h := range map[string]application.Histogram{
		"severity": d.Severity, "report lag": d.ReportLagDays, "close lag": d.CloseLagDays,
	} {
		if len(h.Bins) != 20 {
			t.Fatalf("%s: bins = %d, want 20", name, len(h.Bins))
		}
		total := 0
		for _, b := range h.Bins {
			total += b.Count
		}
		if total != 2 {
			t.Fatalf("%s: total count = %d, want 2 (one per claim)", name, total)
		}
	}
	// Severities are the per-claim ultimates 1000 and 500, so the log bins
	// span [500, 1000].
	if math.Abs(d.Severity.Bins[0].Lo-500) > 1e-9 || math.Abs(d.Severity.Bins[19].Hi-1000) > 1e-9 {
		t.Fatalf("severity range = [%v, %v], want [500, 1000]", d.Severity.Bins[0].Lo, d.Severity.Bins[19].Hi)
	}
	// Report lags are 10 and 30 days.
	if d.ReportLagDays.Bins[0].Lo != 10 || d.ReportLagDays.Bins[19].Hi != 30 {
		t.Fatalf("report lag range = [%v, %v], want [10, 30]", d.ReportLagDays.Bins[0].Lo, d.ReportLagDays.Bins[19].Hi)
	}
}
