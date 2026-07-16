// Package web serves the claimsgen browser UI: an embedded static page plus
// a small JSON API over the existing use cases.
package web

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"net/url"

	"github.com/le-marais/claimsgen/internal/application"
	"github.com/le-marais/claimsgen/internal/domain/triangle"
	"github.com/le-marais/claimsgen/internal/infrastructure/config"
	csvout "github.com/le-marais/claimsgen/internal/infrastructure/csv"
	"github.com/le-marais/claimsgen/internal/infrastructure/random"
)

//go:embed static
var staticFS embed.FS

// Server handles the UI's HTTP API. It is stateless apart from the loaded
// reference sets: the latest run lives in the browser.
type Server struct {
	refs []triangle.ReferenceSet
	mux  *http.ServeMux
}

func NewServer(refs []triangle.ReferenceSet) *Server {
	s := &Server{refs: refs, mux: http.NewServeMux()}
	s.mux.HandleFunc("GET /api/lobs", s.handleLOBs)
	s.mux.HandleFunc("GET /api/lobs/{id}/preset", s.handlePreset)
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

type generateRequest struct {
	LOBID           string           `json:"lob_id"`
	Seed            uint64           `json:"seed"`
	StartYear       int              `json:"start_year"`
	Years           int              `json:"years"`
	InitialBookSize int              `json:"initial_book_size"`
	OutDir          string           `json:"out_dir"`
	Params          config.LOBParams `json:"params"`
}

func (s *Server) handleGenerate(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var req generateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("parsing request: %v", err))
		return
	}
	if req.OutDir == "" {
		writeError(w, http.StatusBadRequest, "out_dir: must not be empty")
		return
	}
	ds, err := application.GenerateDataset(random.NewSource(req.Seed), application.GenerateRequest{
		LOB:             req.Params.ToDomain(),
		StartYear:       req.StartYear,
		Years:           req.Years,
		InitialBookSize: req.InitialBookSize,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := csvout.WriteDataset(req.OutDir, ds); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, buildResponse(req, ds, s.refs))
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
