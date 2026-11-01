package recovery

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"net/http"
)

// RuntimeHTTP repeats the live quarantine gate for every response write/flush,
// including already open SSE/download streams. It does not grant authentication.
// WSS hijacking still uses the transport's independent per-frame runtime gate.
func RuntimeHTTP(next http.Handler, gate func(context.Context) error) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if next == nil || gate == nil || gate(r.Context()) != nil {
			quarantineProblem(w)
			return
		}
		next.ServeHTTP(&quarantineResponse{ResponseWriter: w, ctx: r.Context(), gate: gate}, r)
	})
}
func quarantineProblem(w http.ResponseWriter) {
	// A handler may have prepared cookies/download headers before fence loss.
	// Keep only the correlation ID when replacing its uncommitted response.
	for name := range w.Header() {
		if name != "X-Request-Id" {
			w.Header().Del(name)
		}
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(http.StatusServiceUnavailable)
	_ = json.NewEncoder(w).Encode(map[string]any{"type": "about:blank", "title": "Service Unavailable", "status": 503, "code": "CONTROLLER_QUARANTINED"})
}

type quarantineResponse struct {
	http.ResponseWriter
	ctx             context.Context
	gate            func(context.Context) error
	denied, started bool
}

func (w *quarantineResponse) check() bool {
	if w.denied {
		return false
	}
	if w.gate(w.ctx) == nil {
		return true
	}
	w.denied = true
	if !w.started {
		w.started = true
		quarantineProblem(w.ResponseWriter)
	}
	return false
}
func (w *quarantineResponse) WriteHeader(status int) {
	if !w.check() {
		return
	}
	w.started = true
	w.ResponseWriter.WriteHeader(status)
}
func (w *quarantineResponse) Write(data []byte) (int, error) {
	if !w.check() {
		return 0, ErrQuarantined
	}
	w.started = true
	return w.ResponseWriter.Write(data)
}
func (w *quarantineResponse) FlushError() error {
	if !w.check() {
		return ErrQuarantined
	}
	w.started = true
	return http.NewResponseController(w.ResponseWriter).Flush()
}
func (w *quarantineResponse) Flush()                      { _ = w.FlushError() }
func (w *quarantineResponse) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func (w *quarantineResponse) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if !w.check() {
		return nil, nil, ErrQuarantined
	}
	w.started = true
	return http.NewResponseController(w.ResponseWriter).Hijack()
}
