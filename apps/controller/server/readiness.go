package server

import (
	"context"
	"encoding/json"
	"net/http"
	"time"
)

// Readiness reports only fixed subsystem categories. No node inventory, private
// paths, fingerprints, policy details or downstream credentials are exposed.
func (s *Server) readiness(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	checks := map[string]string{"database": "ready", "authority": "ready", "scheduler": "ready", "agentTransport": "not_configured", "pki": "not_configured"}
	ready := true
	var one int
	if s.DB == nil || s.DB.QueryRow(ctx, "SELECT 1").Scan(&one) != nil {
		checks["database"] = "unavailable"
		ready = false
	}
	if s.runtimeGate == nil || s.runtimeGate(ctx) != nil {
		checks["authority"] = "unavailable"
		ready = false
	}
	if !s.schedulerReady.Load() {
		checks["scheduler"] = "unavailable"
		ready = false
	}
	if s.Enrollment != nil {
		checks["agentTransport"] = "ready"
		checks["pki"] = "ready"
		if s.Links == nil || !s.Links.Ready() {
			checks["agentTransport"] = "unavailable"
			ready = false
		}
		if s.Enrollment.CheckIdentityIssuer(ctx) != nil {
			checks["pki"] = "unavailable"
			ready = false
		}
	}
	state := "ready"
	if !ready {
		state = "not_ready"
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	if !ready {
		w.WriteHeader(http.StatusServiceUnavailable)
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"status": state, "checks": checks})
}
