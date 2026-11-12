package server

import (
	"bufio"
	"log/slog"
	"net"
	"net/http"
	"time"
)

type accessResponse struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (w *accessResponse) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func (w *accessResponse) WriteHeader(status int) {
	if status >= 100 && status < 200 && status != 101 {
		w.ResponseWriter.WriteHeader(status)
		return
	}
	if w.status == 0 {
		w.status = status
		w.ResponseWriter.WriteHeader(status)
	}
}
func (w *accessResponse) Write(raw []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(200)
	}
	n, e := w.ResponseWriter.Write(raw)
	w.bytes += n
	return n, e
}
func (w *accessResponse) Flush() {
	_ = w.FlushError()
}
func (w *accessResponse) FlushError() error {
	if w.status == 0 {
		w.WriteHeader(200)
	}
	return http.NewResponseController(w.ResponseWriter).Flush()
}
func (w *accessResponse) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	c, b, e := http.NewResponseController(w.ResponseWriter).Hijack()
	if e == nil {
		w.status = 101
	}
	return c, b, e
}
func logAccess(start time.Time, w *accessResponse, r *http.Request) {
	status := w.status
	if status == 0 {
		status = 200
	}
	route := r.Pattern
	if route == "" {
		route = "unmatched_or_console"
	}
	// Never log request bodies, cookies, headers, raw URLs/queries, or remote IPs.
	if r.URL.Path == "/healthz" || r.URL.Path == "/readyz" {
		if status < 400 {
			return
		}
	}
	slog.Info("http request", "request_id", w.Header().Get("X-Request-ID"), "method", r.Method, "route", route, "status", status, "bytes", w.bytes, "duration_ms", time.Since(start).Milliseconds(), "diagnostic_code", w.Header().Get("X-SF-Diagnostic-Code"))
}
