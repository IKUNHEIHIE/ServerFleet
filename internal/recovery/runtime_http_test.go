package recovery

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRuntimeHTTPStopsAlreadyOpenStreamAfterFenceLoss(t *testing.T) {
	allowed := true
	gate := func(context.Context) error {
		if !allowed {
			return ErrQuarantined
		}
		return nil
	}
	handler := RuntimeHTTP(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		if _, err := w.Write([]byte("data: first\n\n")); err != nil {
			t.Fatal(err)
		}
		w.(http.Flusher).Flush()
		allowed = false
		if n, err := w.Write([]byte("data: must-not-leak\n\n")); n != 0 || err != ErrQuarantined {
			t.Fatal("stream continued after fence loss", n, err)
		}
		if err := http.NewResponseController(w).Flush(); err != ErrQuarantined {
			t.Fatal("flush bypassed quarantine", err)
		}
	}), gate)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest("GET", "https://localhost/events", nil))
	if response.Body.String() != "data: first\n\n" {
		t.Fatal(response.Body.String())
	}
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest("GET", "https://localhost/events", nil))
	if response.Code != 503 {
		t.Fatal(response.Code)
	}
}

func TestRuntimeHTTPDropsPreparedCookieOnFenceLoss(t *testing.T) {
	allowed := true
	response := httptest.NewRecorder()
	handler := RuntimeHTTP(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Set-Cookie", "must-not-be-issued=synthetic")
		w.Header().Set("Content-Length", "9999")
		allowed = false
		w.WriteHeader(200)
		if _, err := w.Write([]byte("must not leak")); err != ErrQuarantined {
			t.Fatal(err)
		}
	}), func(context.Context) error {
		if !allowed {
			return ErrQuarantined
		}
		return nil
	})
	handler.ServeHTTP(response, httptest.NewRequest("GET", "https://localhost/auth", nil))
	if response.Code != 503 || response.Header().Get("Set-Cookie") != "" || response.Header().Get("Content-Length") != "" {
		t.Fatal(response.Code, response.Header())
	}
}
