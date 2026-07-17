package web_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	refdata "github.com/le-marais/claimsgen/data/reference"
	"github.com/le-marais/claimsgen/internal/application"
	"github.com/le-marais/claimsgen/internal/infrastructure/config"
	csvout "github.com/le-marais/claimsgen/internal/infrastructure/csv"
	"github.com/le-marais/claimsgen/internal/infrastructure/random"
	"github.com/le-marais/claimsgen/internal/infrastructure/schedulep"
	"github.com/le-marais/claimsgen/internal/infrastructure/web"
)

func newTestServer(t *testing.T) *web.Server {
	t.Helper()
	refs, err := schedulep.LoadFS(refdata.Files, refdata.PersonalMotorDirs...)
	if err != nil {
		t.Fatal(err)
	}
	return web.NewServer(refs)
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

func generateBody(t *testing.T, outDir string) map[string]any {
	t.Helper()
	params, err := config.PresetParams("motor-personal")
	if err != nil {
		t.Fatal(err)
	}
	return map[string]any{
		"lob_id":            "motor-personal",
		"seed":              7,
		"start_year":        1998,
		"years":             2,
		"initial_book_size": 300,
		"out_dir":           outDir,
		"params":            params,
	}
}

func TestGenerateRoundTrip(t *testing.T) {
	outDir := t.TempDir()
	rec := do(t, newTestServer(t), "POST", "/api/generate", generateBody(t, outDir))
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
			PaidATA []struct {
				Age    int     `json:"age"`
				Value  float64 `json:"value"`
				Min    float64 `json:"min"`
				Max    float64 `json:"max"`
				Within bool    `json:"within"`
			} `json:"paid_ata"`
			LossRatio struct {
				Value float64 `json:"value"`
			} `json:"loss_ratio"`
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

	// The UI path must write byte-identical CSVs to the CLI path.
	params, err := config.PresetParams("motor-personal")
	if err != nil {
		t.Fatal(err)
	}
	ds, err := application.GenerateDataset(random.NewSource(7), application.GenerateRequest{
		LOB: params.ToDomain(), StartYear: 1998, Years: 2, InitialBookSize: 300,
	})
	if err != nil {
		t.Fatal(err)
	}
	wantDir := t.TempDir()
	if err := csvout.WriteDataset(wantDir, ds); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"policies.csv", "claims.csv", "transactions.csv"} {
		got, err := os.ReadFile(filepath.Join(outDir, name))
		if err != nil {
			t.Fatal(err)
		}
		want, err := os.ReadFile(filepath.Join(wantDir, name))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("%s differs between UI and CLI path", name)
		}
	}
}

func TestGenerateResponseIncludesNilCount(t *testing.T) {
	outDir := t.TempDir()
	rec := do(t, newTestServer(t), "POST", "/api/generate", generateBody(t, outDir))
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
	body := generateBody(t, t.TempDir())
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
	body := generateBody(t, t.TempDir())
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
