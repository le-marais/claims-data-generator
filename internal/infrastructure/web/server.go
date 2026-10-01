// Package web serves the claimsgen browser UI: an embedded static page plus
// a small JSON API over the existing use cases.
package web

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"sync/atomic"

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
// scalar bound alone catches compounding. About 240k policies take a second
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

// maxRunsInFlight is how many generate requests may be running or queued
// before the rest are turned away. One local user needs one run; the slack is
// for a stray double-click or a second tab, not for throughput.
const maxRunsInFlight = 4

// Server handles the UI's HTTP API. Apart from the loaded reference sets and
// the run slot it is stateless: the latest run lives in the browser.
type Server struct {
	refs []triangle.ReferenceSet
	mux  *http.ServeMux
	// runSlot is a one-deep semaphore holding the right to generate. Two runs
	// pointed at the same out_dir would interleave writes to the same three
	// CSVs and both report success, so runs are serialized. Waiting for the
	// slot is cancellable, unlike a mutex: a cancelled run keeps working until
	// the next stage boundary, and the retry that usually follows should queue
	// behind it rather than be rejected.
	runSlot  chan struct{}
	inFlight atomic.Int32
}

func NewServer(refs []triangle.ReferenceSet) *Server {
	s := &Server{refs: refs, mux: http.NewServeMux(), runSlot: make(chan struct{}, 1)}
	s.mux.HandleFunc("GET /api/lobs", s.handleLOBs)
	s.mux.HandleFunc("GET /api/lobs/{id}/preset", s.handlePreset)
	s.mux.HandleFunc("GET /api/limits", s.handleLimits)
	s.mux.HandleFunc("GET /api/fields", s.handleFields)
	s.mux.HandleFunc("POST /api/generate", s.handleGenerate)

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

type generateRequest struct {
	Seed            string           `json:"seed"`
	StartYear       int              `json:"start_year"`
	Years           int              `json:"years"`
	InitialBookSize int              `json:"initial_book_size"`
	OutDir          string           `json:"out_dir"`
	OriginBasis     string           `json:"origin_basis"`
	Params          config.LOBParams `json:"params"`
}

func (s *Server) handleGenerate(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	var req generateRequest
	if err := dec.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("parsing request: %v", err))
		return
	}
	seed, err := strconv.ParseUint(req.Seed, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "seed: must be a base-10 unsigned integer")
		return
	}
	if req.OutDir == "" {
		writeError(w, http.StatusBadRequest, "out_dir: must not be empty")
		return
	}
	absOut, err := filepath.Abs(req.OutDir)
	if err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("out_dir: %v", err))
		return
	}
	req.OutDir = absOut
	if req.OriginBasis == "" {
		req.OriginBasis = string(triangle.AccidentMonth)
	}
	basis := triangle.OriginBasis(req.OriginBasis)
	if err := basis.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	line := req.Params.ToDomain()
	if err := checkRunSize(line, req.Years, req.InitialBookSize); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.acquireRun(r.Context()); err != nil {
		if errors.Is(err, errTooManyRuns) {
			writeError(w, http.StatusConflict, err.Error())
		} else {
			writeError(w, statusClientClosedRequest, "run cancelled")
		}
		return
	}
	defer s.releaseRun()

	ds, err := application.GenerateDataset(r.Context(), random.NewSource(seed), application.GenerateRequest{
		LOB:             line,
		StartYear:       req.StartYear,
		Years:           req.Years,
		InitialBookSize: req.InitialBookSize,
	})
	if errors.Is(err, context.Canceled) {
		writeError(w, statusClientClosedRequest, "run cancelled")
		return
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := csvout.WriteDataset(req.OutDir, ds); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	ag, err := application.Aggregate(ds, req.StartYear, req.Years, basis)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := csvout.WriteAggregates(req.OutDir, ag); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	realism, err := application.EvaluateRealism(ds, req.StartYear, req.Years, line.Claims.ScoredSection(), s.refs)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, buildResponse(req, ds, ag, realism))
}

var errTooManyRuns = errors.New("too many generation runs in flight; wait for one to finish")

// acquireRun blocks until this request owns the single run slot, the client
// goes away, or too many requests are already stacked up behind it.
func (s *Server) acquireRun(ctx context.Context) error {
	if s.inFlight.Add(1) > maxRunsInFlight {
		s.inFlight.Add(-1)
		return errTooManyRuns
	}
	select {
	case s.runSlot <- struct{}{}:
		return nil
	case <-ctx.Done():
		s.inFlight.Add(-1)
		return ctx.Err()
	}
}

func (s *Server) releaseRun() {
	<-s.runSlot
	s.inFlight.Add(-1)
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
		return fmt.Errorf("run too large: %d policies over %d years at growth %g projects to about %.0f policies, more than the %d limit",
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
