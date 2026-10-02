// Package web serves the claimsgen browser UI: an embedded static page plus
// a small JSON API over the existing use cases.
package web

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/le-marais/claimsgen/internal/application"
	"github.com/le-marais/claimsgen/internal/domain/lob"
	"github.com/le-marais/claimsgen/internal/domain/policy"
	"github.com/le-marais/claimsgen/internal/domain/triangle"
	"github.com/le-marais/claimsgen/internal/infrastructure/config"
	csvout "github.com/le-marais/claimsgen/internal/infrastructure/csv"
	"github.com/le-marais/claimsgen/internal/infrastructure/random"
)

//go:embed static
var staticFS embed.FS

// Run limits for the browser UI. They are a guard against a mistyped form -
// book size compounds by the growth factor every year, so a slip in either
// field can ask for billions of policies - not a security boundary; the CLI
// stays unlimited. maxProjectedPolicies is the one that bites, since neither
// scalar bound alone catches compounding, and on a fleet book it counts the
// vehicles, the policies that cost run time. About 240k policies take a second
// on a laptop, so the cap is roughly half a minute of work.
const (
	maxYears             = 100
	maxInitialBookSize   = 1_000_000
	maxProjectedPolicies = 2_000_000
)

// statusClientClosedRequest is nginx's 499: the client went away before the
// response was ready. Go has no constant for it, and no 4xx in the standard
// set says "you cancelled this yourself".
const statusClientClosedRequest = 499

// Server handles the UI's HTTP API. Apart from the loaded reference pools it
// is stateless: the latest run lives in the browser, and a download
// regenerates the run from its seed and parameters, which reproduce it byte
// for byte. The server writes no files.
type Server struct {
	pools map[string]application.ReferencePool
	mux   *http.ServeMux
}

// NewServer serves the UI, scoring each run against the reference pool of
// its preset's realism line; pools are keyed by reference line ID.
func NewServer(pools map[string]application.ReferencePool) *Server {
	s := &Server{pools: pools, mux: http.NewServeMux()}
	s.mux.HandleFunc("GET /api/lobs", s.handleLOBs)
	s.mux.HandleFunc("GET /api/lobs/{id}/preset", s.handlePreset)
	s.mux.HandleFunc("GET /api/limits", s.handleLimits)
	s.mux.HandleFunc("GET /api/fields", s.handleFields)
	s.mux.HandleFunc("POST /api/generate", s.handleGenerate)
	s.mux.HandleFunc("POST /api/download", s.handleDownload)

	staticRoot, err := fs.Sub(staticFS, "static")
	if err != nil {
		panic(err) // the embedded tree is fixed at compile time
	}
	s.mux.Handle("GET /", http.FileServerFS(staticRoot))

	return s
}

// ServeHTTP guards against DNS rebinding (foreign Host) and cross-site
// requests (foreign Origin) before dispatching: the server is loopback-only
// and the browser must not be usable as a bridge to it.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !localHost(r.Host) {
		writeError(w, http.StatusForbidden, "forbidden host")
		return
	}
	if origin := r.Header.Get("Origin"); origin != "" && !localOrigin(origin) {
		writeError(w, http.StatusForbidden, "forbidden origin")
		return
	}
	s.mux.ServeHTTP(w, r)
}

// localHost reports whether a request Host (with optional port) is loopback.
func localHost(host string) bool {
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	return host == "127.0.0.1" || host == "localhost" || host == "::1"
}

// localOrigin reports whether an Origin header points at a loopback origin.
func localOrigin(origin string) bool {
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	return localHost(u.Host)
}

func (s *Server) handleLOBs(w http.ResponseWriter, r *http.Request) {
	infos := config.Presets()
	out := make([]lobInfoJSON, len(infos))
	for i, p := range infos {
		out[i] = lobInfoJSON{ID: p.ID, Name: p.Name}
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handlePreset(w http.ResponseWriter, r *http.Request) {
	params, err := config.PresetParams(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, params)
}

// handleLimits serves the run caps so the form can mirror them as input
// bounds instead of restating the numbers in JavaScript.
func (s *Server) handleLimits(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]int{
		"max_years":              maxYears,
		"max_initial_book_size":  maxInitialBookSize,
		"max_projected_policies": maxProjectedPolicies,
	})
}

// handleFields serves the parameter form's field metadata (see formFields).
func (s *Server) handleFields(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, formFields)
}

// generateRequest is one run. Preset names the registered preset the
// parameters were edited from, which decides how the run is scored; the
// parameters alone decide what is generated.
type generateRequest struct {
	Seed            string           `json:"seed"`
	StartYear       int              `json:"start_year"`
	Years           int              `json:"years"`
	InitialBookSize int              `json:"initial_book_size"`
	OriginBasis     string           `json:"origin_basis"`
	Preset          string           `json:"preset"`
	Params          config.LOBParams `json:"params"`
}

// run is one generated run: the request it answers, the line of business it
// was generated for, the dataset and its aggregates.
type run struct {
	req  generateRequest
	line lob.LineOfBusiness
	ds   application.Dataset
	ag   application.Aggregates
}

// runError is a failed run with the HTTP status that reports it.
type runError struct {
	status int
	msg    string
}

// generate decodes a run request, checks it, and generates and aggregates the
// run. Request, validation and domain errors and an oversized run are a 400,
// a run cancelled by the client a 499, and an aggregation failure a 500.
func (s *Server) generate(w http.ResponseWriter, r *http.Request) (run, *runError) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	var req generateRequest
	if err := dec.Decode(&req); err != nil {
		return run{}, &runError{http.StatusBadRequest, fmt.Sprintf("parsing request: %v", err)}
	}
	seed, err := strconv.ParseUint(req.Seed, 10, 64)
	if err != nil {
		return run{}, &runError{http.StatusBadRequest, "seed: must be a base-10 unsigned integer"}
	}
	if req.OriginBasis == "" {
		req.OriginBasis = string(triangle.AccidentMonth)
	}
	basis := triangle.OriginBasis(req.OriginBasis)
	if err := basis.Validate(); err != nil {
		return run{}, &runError{http.StatusBadRequest, err.Error()}
	}
	line := req.Params.ToDomain()
	if err := checkRunSize(line, req.Years, req.InitialBookSize); err != nil {
		return run{}, &runError{http.StatusBadRequest, err.Error()}
	}
	ds, err := application.GenerateDataset(r.Context(), random.NewSource(seed), application.GenerateRequest{
		LOB:             line,
		StartYear:       req.StartYear,
		Years:           req.Years,
		InitialBookSize: req.InitialBookSize,
	})
	if errors.Is(err, context.Canceled) {
		return run{}, &runError{statusClientClosedRequest, "run cancelled"}
	}
	if err != nil {
		return run{}, &runError{http.StatusBadRequest, err.Error()}
	}
	ag, err := application.Aggregate(ds, req.StartYear, req.Years, basis)
	if err != nil {
		return run{}, &runError{http.StatusInternalServerError, err.Error()}
	}
	return run{req: req, line: line, ds: ds, ag: ag}, nil
}

// handleGenerate runs a request and returns its analytics for the browser.
func (s *Server) handleGenerate(w http.ResponseWriter, r *http.Request) {
	res, rerr := s.generate(w, r)
	if rerr != nil {
		writeError(w, rerr.status, rerr.msg)
		return
	}
	realism, err := s.score(res)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, buildResponse(res.req, res.ds, res.ag, realism))
}

// score scores a run against the reference pool of the preset it names. A
// run that names no registered preset, whose preset's line has no pool, or
// whose parameters lack a section the preset scores is not scored, and the
// view says why.
func (s *Server) score(res run) (realismJSON, error) {
	if res.req.Preset == "" {
		return notScored("the run names no preset to score against"), nil
	}
	info, ok := config.PresetInfoFor(res.req.Preset)
	if !ok {
		return notScored(fmt.Sprintf("no preset %q to score against", res.req.Preset)), nil
	}
	pool, ok := s.pools[info.Realism.Line]
	if !ok {
		return notScored(fmt.Sprintf("no reference data for %s", info.Realism.Line)), nil
	}
	sections, err := info.Realism.SectionIndices(res.line)
	if err != nil {
		return notScored(err.Error()), nil
	}
	report, err := application.EvaluateRealism(res.ds, res.req.StartYear, res.req.Years, sections, pool.Refs)
	if err != nil {
		return realismJSON{}, err
	}
	return realismView(report, info.Realism.Sections, pool), nil
}

// handleDownload runs a request and returns its five CSVs as one zip archive.
// The browser sends the request of the run it shows, and the same seed and
// parameters reproduce that run byte for byte.
func (s *Server) handleDownload(w http.ResponseWriter, r *http.Request) {
	res, rerr := s.generate(w, r)
	if rerr != nil {
		writeError(w, rerr.status, rerr.msg)
		return
	}
	// The archive is built in memory first, so a failure is still a clean 500
	// rather than a truncated download.
	var buf bytes.Buffer
	if err := csvout.WriteZip(&buf, res.ds, res.ag); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, downloadName(res.req)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(buf.Bytes()) // a failed write means the client has gone; there is no one left to tell
}

// downloadName names a run's archive after its line of business and seed,
// keeping only characters that are safe in a file name and a header.
func downloadName(req generateRequest) string {
	name := strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
			return r
		}
		return -1
	}, req.Params.Name)
	if name == "" {
		name = "claimsgen"
	}
	return fmt.Sprintf("%s-seed-%s.zip", name, req.Seed)
}

// checkRunSize rejects a run that would be too large to be a deliberate ask.
// Lower bounds stay with the use case; these upper bounds are a property of
// this interface, which is why the CLI does not share them.
func checkRunSize(l lob.LineOfBusiness, years, initialBookSize int) error {
	if years > maxYears {
		return fmt.Errorf("years: must be at most %d, got %d", maxYears, years)
	}
	if initialBookSize > maxInitialBookSize {
		return fmt.Errorf("initial book size: must be at most %d, got %d", maxInitialBookSize, initialBookSize)
	}
	if projected := policy.ProjectedSize(l.Book, years, initialBookSize); projected > maxProjectedPolicies {
		return fmt.Errorf("run too large: an initial book of %d over %d years at growth %g projects to about %.0f policies, more than the %d limit",
			initialBookSize, years, l.Book.GrowthFactor, projected, maxProjectedPolicies)
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	buf, err := json.Marshal(v)
	if err != nil {
		http.Error(w, `{"error":"encoding response"}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(buf) // a failed write means the client has gone; there is no one left to tell
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
