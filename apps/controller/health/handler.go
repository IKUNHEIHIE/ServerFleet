// Package health exposes process liveness only; it grants no management access.
package health

import "net/http"

func Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("{\"status\":\"scaffold\",\"stage\":\"M0-P1\"}\n"))
	})
	return mux
}
