// Controller is the modular monolith entrypoint. Without -db/-origin/-tls-*
// flags it runs the M0-P1 local health scaffold; with full configuration it
// mounts the composed SQLite WAL management API over direct TLS.
package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"serverfleet.local/serverfleet/apps/controller/health"
	"serverfleet.local/serverfleet/apps/controller/server"
)

func main() {
	listen := flag.String("listen", "127.0.0.1:8080", "local listen address")
	dbPath := flag.String("db", "", "SQLite WAL database path (enables management API)")
	origin := flag.String("origin", "", "trusted https:// browser Origin (required with -db)")
	tlsCert := flag.String("tls-cert", "", "TLS server certificate path (required with -db)")
	tlsKey := flag.String("tls-key", "", "TLS server private key path (required with -db)")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var handler http.Handler = health.Handler()
	var composed *server.Server
	if *dbPath != "" {
		if *origin == "" || *tlsCert == "" || *tlsKey == "" {
			log.Fatal("-db requires -origin, -tls-cert and -tls-key (direct HTTPS is mandatory)")
		}
		var err error
		composed, err = server.New(ctx, server.Config{
			DBPath: *dbPath,
			Origin: *origin,
		})
		if err != nil {
			log.Fatalf("initialize controller: %v", err)
		}
		defer func() { _ = composed.Close() }()
		handler = composed
	}

	httpServer := &http.Server{
		Addr:              *listen,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       30 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			log.Printf("shutdown: %v", err)
		}
	}()
	if composed != nil {
		log.Printf("Server Fleet Controller: https://%s (SQLite WAL management API active)", *listen)
		if err := httpServer.ListenAndServeTLS(*tlsCert, *tlsKey); err != nil && err != http.ErrServerClosed {
			log.Fatal(err)
		}
		return
	}
	log.Printf("Server Fleet scaffold: http://%s/healthz (management API unavailable)", *listen)
	if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
