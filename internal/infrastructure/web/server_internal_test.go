package web

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/le-marais/claimsgen/internal/infrastructure/config"
)

// generatePost builds a valid generate request. The body only has to survive
// decoding and the size check: these tests never reach the simulation.
func generatePost(t *testing.T, edit func(map[string]any)) *http.Request {
	t.Helper()
	params, err := config.PresetParams("motor-personal")
	if err != nil {
		t.Fatal(err)
	}
	fields := map[string]any{
		"seed":              "7",
		"start_year":        1998,
		"years":             2,
		"initial_book_size": 300,
		"out_dir":           t.TempDir(),
		"params":            params,
	}
	if edit != nil {
		edit(fields)
	}
	body, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("POST", "/api/generate", bytes.NewReader(body))
	req.Host = "127.0.0.1"
	return req
}

// occupyRunSlot stands in for a run already under way, which is otherwise
// only reachable by racing two slow requests.
func occupyRunSlot(t *testing.T, s *Server) {
	t.Helper()
	s.runSlot <- struct{}{}
	s.inFlight.Add(1)
	t.Cleanup(s.releaseRun)
}

func TestGenerateRejectsRunsBeyondTheInFlightCap(t *testing.T) {
	srv := NewServer(nil)
	occupyRunSlot(t, srv)
	srv.inFlight.Store(maxRunsInFlight) // the queue behind that run is full

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, generatePost(t, nil))
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409; body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "in flight") {
		t.Fatalf("body = %s, want mention of runs in flight", rec.Body.String())
	}
	srv.inFlight.Store(1) // leave the cleanup release balanced
}

// A queued request must answer its own cancel rather than waiting out the run
// ahead of it - that wait is the whole reason the slot is not a mutex.
func TestGenerateCancelWhileQueued(t *testing.T) {
	srv := NewServer(nil)
	occupyRunSlot(t, srv)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, generatePost(t, nil).WithContext(ctx))
	if rec.Code != statusClientClosedRequest {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, statusClientClosedRequest, rec.Body.String())
	}
	if got := srv.inFlight.Load(); got != 1 {
		t.Fatalf("in-flight count = %d after an abandoned wait, want 1 (the occupying run)", got)
	}
}

// The slot must be given back whichever way a run ends, or the first failure
// wedges the server for its lifetime.
func TestGenerateReleasesTheSlotAfterAFailedRun(t *testing.T) {
	srv := NewServer(nil)
	// years: 0 clears the size check and fails inside the use case, so the run
	// holds the slot before it fails.
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, generatePost(t, func(f map[string]any) { f["years"] = 0 }))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if len(srv.runSlot) != 0 || srv.inFlight.Load() != 0 {
		t.Fatalf("run slot still held after a rejected run: slot = %d, in flight = %d", len(srv.runSlot), srv.inFlight.Load())
	}
}
