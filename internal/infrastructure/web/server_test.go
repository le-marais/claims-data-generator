package web_test

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	refdata "github.com/le-marais/claimsgen/data/reference"
	"github.com/le-marais/claimsgen/internal/application"
	"github.com/le-marais/claimsgen/internal/domain/triangle"
	"github.com/le-marais/claimsgen/internal/infrastructure/config"
	csvout "github.com/le-marais/claimsgen/internal/infrastructure/csv"
	"github.com/le-marais/claimsgen/internal/infrastructure/random"
	"github.com/le-marais/claimsgen/internal/infrastructure/schedulep"
	"github.com/le-marais/claimsgen/internal/infrastructure/web"
)

func newTestServer(t *testing.T) *web.Server {
	t.Helper()
	refs, err := schedulep.LoadFS(refdata.Files, refdata.PersonalMotorFile)
	if err != nil {
		t.Fatal(err)
	}
	return web.NewServer(triangle.SelectReferences(refs, application.PersonalMotorCriteria()))
}

func do(t *testing.T, srv http.Handler, method, target string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var reader *bytes.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		reader = bytes.NewReader(b)
	} else {
		reader = bytes.NewReader(nil)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(method, target, reader)
	req.Host = "127.0.0.1" // httptest.NewRequest defaults to "example.com"; tests exercise a local client
	srv.ServeHTTP(rec, req)
	return rec
}

func TestLOBList(t *testing.T) {
	rec := do(t, newTestServer(t), "GET", "/api/lobs", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var lobs []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &lobs); err != nil {
		t.Fatal(err)
	}
	if len(lobs) != 1 || lobs[0].ID != "motor-personal" || lobs[0].Name != "Motor personal" {
		t.Fatalf("lobs = %+v", lobs)
	}
}

func TestPresetEndpoint(t *testing.T) {
	rec := do(t, newTestServer(t), "GET", "/api/lobs/motor-personal/preset", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var params config.LOBParams
	if err := json.Unmarshal(rec.Body.Bytes(), &params); err != nil {
		t.Fatal(err)
	}
	want, err := config.MotorPersonal()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(params.ToDomain(), want) {
		t.Fatal("preset JSON does not round-trip to the embedded preset")
	}
}

func TestPresetUnknown(t *testing.T) {
	rec := do(t, newTestServer(t), "GET", "/api/lobs/marine-cargo/preset", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "error") {
		t.Fatalf("body = %s, want JSON error", rec.Body.String())
	}
}

func generateBody(t *testing.T) map[string]any {
	t.Helper()
	params, err := config.PresetParams("motor-personal")
	if err != nil {
		t.Fatal(err)
	}
	return map[string]any{
		"seed":              "7",
		"start_year":        1998,
		"years":             2,
		"initial_book_size": 300,
		"params":            params,
	}
}

func TestGenerateRoundTrip(t *testing.T) {
	rec := do(t, newTestServer(t), "POST", "/api/generate", generateBody(t))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Run struct {
			LOB          string `json:"lob"`
			Policies     int    `json:"policies"`
			Claims       int    `json:"claims"`
			Transactions int    `json:"transactions"`
		} `json:"run"`
		Summary struct {
			Years []struct {
				Year     int `json:"year"`
				Policies int `json:"policies"`
			} `json:"years"`
		} `json:"summary"`
		Triangles struct {
			Paid struct {
				StartYear int         `json:"start_year"`
				Cells     [][]float64 `json:"cells"`
				ATA       []*float64  `json:"ata"`
			} `json:"paid"`
		} `json:"triangles"`
		Distributions struct {
			Severity struct {
				Bins []struct {
					Count int `json:"count"`
				} `json:"bins"`
			} `json:"severity"`
		} `json:"distributions"`
		Realism struct {
			Sections []string `json:"sections"`
			PaidATA  []struct {
				Age    int     `json:"age"`
				Value  float64 `json:"value"`
				Min    float64 `json:"min"`
				Max    float64 `json:"max"`
				Within bool    `json:"within"`
			} `json:"paid_ata"`
			PaidShares []struct {
				Age int `json:"age"`
			} `json:"paid_shares"`
			LossRatio struct {
				Value float64 `json:"value"`
			} `json:"loss_ratio"`
			LossRatioDrift struct {
				Value float64 `json:"value"`
			} `json:"loss_ratio_drift"`
		} `json:"realism"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Run.LOB != "motor-personal" || resp.Run.Policies == 0 || resp.Run.Claims == 0 || resp.Run.Transactions == 0 {
		t.Fatalf("run = %+v", resp.Run)
	}
	if len(resp.Summary.Years) != 2 || resp.Summary.Years[0].Year != 1998 {
		t.Fatalf("summary years = %+v", resp.Summary.Years)
	}
	if len(resp.Triangles.Paid.Cells) != 2 || len(resp.Triangles.Paid.Cells[0]) != 10 {
		t.Fatalf("paid triangle shape = %d x %d, want 2 x 10", len(resp.Triangles.Paid.Cells), len(resp.Triangles.Paid.Cells[0]))
	}
	if len(resp.Distributions.Severity.Bins) != 20 {
		t.Fatalf("severity bins = %d, want 20", len(resp.Distributions.Severity.Bins))
	}
	if len(resp.Realism.PaidATA) == 0 || resp.Realism.LossRatio.Value <= 0 {
		t.Fatalf("realism = %+v", resp.Realism)
	}
	if len(resp.Realism.PaidShares) != 9 || resp.Realism.PaidShares[0].Age != 1 {
		t.Fatalf("realism.paid_shares = %+v, want ages 1-9", resp.Realism.PaidShares)
	}
	if resp.Realism.LossRatioDrift.Value <= 0 {
		t.Fatalf("realism.loss_ratio_drift = %+v", resp.Realism.LossRatioDrift)
	}
	if !reflect.DeepEqual(resp.Realism.Sections, []string{"third_party_property", "third_party_injury"}) {
		t.Fatalf("realism.sections = %v, want [third_party_property third_party_injury]", resp.Realism.Sections)
	}
}

// The download is the CLI's five CSVs, byte for byte, as one zip named after
// the line of business and seed.
func TestDownloadMatchesTheCLI(t *testing.T) {
	rec := do(t, newTestServer(t), "POST", "/api/download", generateBody(t))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != "application/zip" {
		t.Errorf("Content-Type = %q, want application/zip", got)
	}
	if got := rec.Header().Get("Content-Disposition"); got != `attachment; filename="motor-personal-seed-7.zip"` {
		t.Errorf("Content-Disposition = %q", got)
	}

	params, err := config.PresetParams("motor-personal")
	if err != nil {
		t.Fatal(err)
	}
	ds, err := application.GenerateDataset(t.Context(), random.NewSource(7), application.GenerateRequest{
		LOB: params.ToDomain(), StartYear: 1998, Years: 2, InitialBookSize: 300,
	})
	if err != nil {
		t.Fatal(err)
	}
	ag, err := application.Aggregate(ds, 1998, 2, triangle.AccidentMonth)
	if err != nil {
		t.Fatal(err)
	}
	wantDir := t.TempDir()
	if err := csvout.WriteDataset(wantDir, ds); err != nil {
		t.Fatal(err)
	}
	if err := csvout.WriteAggregates(wantDir, ag); err != nil {
		t.Fatal(err)
	}
	z, err := zip.NewReader(bytes.NewReader(rec.Body.Bytes()), int64(rec.Body.Len()))
	if err != nil {
		t.Fatal(err)
	}
	if len(z.File) != 5 {
		t.Fatalf("zip holds %d files, want 5", len(z.File))
	}
	for _, f := range z.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		got, err := io.ReadAll(rc)
		if cerr := rc.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			t.Fatal(err)
		}
		want, err := os.ReadFile(filepath.Join(wantDir, f.Name))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, want) {
			t.Errorf("%s differs between the download and the CLI path", f.Name)
		}
	}
}

func TestDownloadRejectsAnInvalidRun(t *testing.T) {
	body := generateBody(t)
	body["years"] = 0
	rec := do(t, newTestServer(t), "POST", "/api/download", body)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "years") {
		t.Fatalf("status = %d, body = %s; want a 400 naming years", rec.Code, rec.Body.String())
	}
}

// The browser no longer sends an output directory, and an old client that
// still does is told so rather than silently ignored.
func TestGenerateRejectsAnOutputDirectory(t *testing.T) {
	body := generateBody(t)
	body["out_dir"] = t.TempDir()
	rec := do(t, newTestServer(t), "POST", "/api/generate", body)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "out_dir") {
		t.Fatalf("status = %d, body = %s; want a 400 naming out_dir", rec.Code, rec.Body.String())
	}
}

func TestGenerateDefaultsToTheAccidentBasis(t *testing.T) {
	// generateBody carries no origin_basis, so the response must report the
	// accident default rather than an empty string.
	rec := do(t, newTestServer(t), "POST", "/api/generate", generateBody(t))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Run struct {
			OriginBasis string `json:"origin_basis"`
		} `json:"run"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Run.OriginBasis != "accident" {
		t.Errorf("origin_basis = %q, want \"accident\"", resp.Run.OriginBasis)
	}
}

func TestGenerateAcceptsTheUnderwritingBasis(t *testing.T) {
	body := generateBody(t)
	body["origin_basis"] = "underwriting"
	rec := do(t, newTestServer(t), "POST", "/api/generate", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Run struct {
			OriginBasis string `json:"origin_basis"`
		} `json:"run"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Run.OriginBasis != "underwriting" {
		t.Errorf("origin_basis = %q, want \"underwriting\"", resp.Run.OriginBasis)
	}
}

func TestGenerateRejectsAnUnknownOriginBasis(t *testing.T) {
	body := generateBody(t)
	body["origin_basis"] = "policy"
	rec := do(t, newTestServer(t), "POST", "/api/generate", body)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "origin basis") {
		t.Errorf("body %q should name the origin basis problem", rec.Body.String())
	}
}

func TestGenerateResponseIncludesNilCount(t *testing.T) {
	rec := do(t, newTestServer(t), "POST", "/api/generate", generateBody(t))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Summary struct {
			Years []struct {
				Claims    int `json:"claims"`
				NilClaims int `json:"nil_claims"`
			} `json:"years"`
			Total struct {
				NilClaims int     `json:"nil_claims"`
				Recovered float64 `json:"recovered"`
				Reopened  int     `json:"reopened"`
			} `json:"total"`
		} `json:"summary"`
		Triangles struct {
			NetPaid struct {
				Cells [][]float64 `json:"cells"`
			} `json:"net_paid"`
		} `json:"triangles"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Summary.Years) == 0 {
		t.Fatal("no summary years")
	}
	// The default preset generates nils at ~8%, so the total should be positive
	// and never exceed total claims.
	if resp.Summary.Total.NilClaims <= 0 {
		t.Fatalf("total nil claims = %d, want positive with the default preset", resp.Summary.Total.NilClaims)
	}
	if resp.Summary.Total.Recovered <= 0 {
		t.Fatalf("total recovered = %v, want positive with the default preset", resp.Summary.Total.Recovered)
	}
	if resp.Summary.Total.Reopened <= 0 {
		t.Fatalf("total reopened = %d, want positive with the default preset", resp.Summary.Total.Reopened)
	}
	if len(resp.Triangles.NetPaid.Cells) == 0 {
		t.Fatal("net paid triangle missing from the generate response")
	}
}

func TestGenerateValidationError(t *testing.T) {
	body := generateBody(t)
	body["years"] = 0
	rec := do(t, newTestServer(t), "POST", "/api/generate", body)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "years") {
		t.Fatalf("body = %s, want mention of years", rec.Body.String())
	}
}

func TestGenerateBadParam(t *testing.T) {
	body := generateBody(t)
	params := body["params"].(config.LOBParams)
	params.Book.GrowthFactor = 0
	body["params"] = params
	rec := do(t, newTestServer(t), "POST", "/api/generate", body)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "growth_factor") {
		t.Fatalf("body = %s, want mention of growth_factor", rec.Body.String())
	}
}

func TestServesUI(t *testing.T) {
	rec := do(t, newTestServer(t), "GET", "/", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("content type = %q, want text/html", ct)
	}
	if !strings.Contains(rec.Body.String(), "claimsgen") {
		t.Fatal("page body does not mention claimsgen")
	}
}

func TestRejectsForeignHost(t *testing.T) {
	srv := newTestServer(t)
	req := httptest.NewRequest("GET", "/api/lobs", nil)
	req.Host = "evil.example.com"
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
}

func TestRejectsForeignOrigin(t *testing.T) {
	srv := newTestServer(t)
	req := httptest.NewRequest("POST", "/api/generate", strings.NewReader("{}"))
	req.Host = "127.0.0.1:8080"
	req.Header.Set("Origin", "https://evil.example.com")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
}

func TestAllowsLocalOrigin(t *testing.T) {
	srv := newTestServer(t)
	req := httptest.NewRequest("GET", "/api/lobs", nil)
	req.Host = "localhost:8080"
	req.Header.Set("Origin", "http://localhost:8080")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

func TestServesStaticAssets(t *testing.T) {
	srv := newTestServer(t)
	for _, target := range []string{"/app.js", "/style.css"} {
		rec := do(t, srv, "GET", target, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s: status = %d, want 200", target, rec.Code)
		}
	}
}

func TestLimitsEndpoint(t *testing.T) {
	rec := do(t, newTestServer(t), "GET", "/api/limits", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var limits struct {
		MaxYears           int `json:"max_years"`
		MaxInitialBookSize int `json:"max_initial_book_size"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &limits); err != nil {
		t.Fatal(err)
	}
	if limits.MaxYears <= 0 || limits.MaxInitialBookSize <= 0 {
		t.Fatalf("limits = %+v, want positive caps for the form to mirror", limits)
	}
}

func TestFieldsEndpoint(t *testing.T) {
	rec := do(t, newTestServer(t), "GET", "/api/fields", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var groups []struct {
		Label  string `json:"label"`
		Fields []struct {
			Path  []string `json:"path"`
			Label string   `json:"label"`
			Tip   string   `json:"tip"`
		} `json:"fields"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &groups); err != nil {
		t.Fatal(err)
	}
	if len(groups) == 0 || len(groups[0].Fields) == 0 || len(groups[0].Fields[0].Path) == 0 {
		t.Fatalf("fields = %+v, want groups of fields with paths", groups)
	}
}

func TestGenerateRejectsOversizedRuns(t *testing.T) {
	cases := []struct {
		name string
		edit func(map[string]any)
		want string
	}{
		{"years above the cap", func(b map[string]any) { b["years"] = 1000 }, "years"},
		{"book above the cap", func(b map[string]any) { b["initial_book_size"] = 50_000_000 }, "initial book size"},
		{
			// Neither scalar is over its own cap; the compounding is.
			"compounding growth", func(b map[string]any) {
				b["years"] = 100
				params := b["params"].(config.LOBParams)
				params.Book.GrowthFactor = 1.5
				b["params"] = params
			},
			"run too large",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := generateBody(t)
			tc.edit(body)
			rec := do(t, newTestServer(t), "POST", "/api/generate", body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body = %s", rec.Code, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), tc.want) {
				t.Fatalf("body = %s, want mention of %q", rec.Body.String(), tc.want)
			}
		})
	}
}

func TestGenerateReportsACancelledRun(t *testing.T) {
	b, err := json.Marshal(generateBody(t))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	req := httptest.NewRequest("POST", "/api/generate", bytes.NewReader(b)).WithContext(ctx)
	req.Host = "127.0.0.1"
	rec := httptest.NewRecorder()
	newTestServer(t).ServeHTTP(rec, req)

	if rec.Code != 499 {
		t.Fatalf("status = %d, want 499 (client closed request); body = %s", rec.Code, rec.Body.String())
	}
}
