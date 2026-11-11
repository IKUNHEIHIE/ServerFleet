// Controller is the modular monolith entrypoint. Without -db/-origin/-tls-*
// flags it runs the M0-P1 local health scaffold; with full configuration it
// mounts the composed SQLite WAL management API over direct TLS and opens the
// local Agent PKI beside the database.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"serverfleet.local/serverfleet/apps/controller/bootstrap"
	"serverfleet.local/serverfleet/apps/controller/health"
)

// Set only by the controlled core release build.
var controllerBuildVersion = "0.9.0-dev"

func main() {
	if len(os.Args) > 1 && (os.Args[1] == "install" || os.Args[1] == "provision" || os.Args[1] == "bootstrap-token" || os.Args[1] == "release") {
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		var err error
		switch os.Args[1] {
		case "install":
			err = bootstrap.RunInstallCommand(ctx, os.Args[2:], os.Stdout, os.Stderr)
		case "provision":
			err = bootstrap.RunProvisionCommand(ctx, os.Args[2:], os.Stdout, os.Stderr)
		case "bootstrap-token":
			err = bootstrap.RunBootstrapTokenCommand(ctx, os.Args[2:], os.Stdout, os.Stderr)
		case "release":
			err = bootstrap.RunReleaseCommand(ctx, os.Args[2:], os.Stdout, os.Stderr)
		}
		if err != nil {
			log.Print(err)
			os.Exit(2)
		}
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "supervise" {
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		if err := bootstrap.RunSuperviseCommand(ctx, os.Args[2:], os.Stdout, os.Stderr); err != nil {
			log.Print(err)
			os.Exit(2)
		}
		return
	}

	if len(os.Args) > 1 && os.Args[1] == "recovery" {
		if err := bootstrap.RunRecoveryCommand(context.Background(), os.Args[2:], os.Stdout, os.Stderr); err != nil {
			log.Print(err)
			os.Exit(2)
		}
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "pki" {
		if err := bootstrap.RunPKICommand(context.Background(), os.Args[2:], os.Stdout, os.Stderr); err != nil {
			log.Print(err)
			os.Exit(2)
		}
		return
	}
	options, err := bootstrap.ParseOptions(os.Args[1:], os.Stderr)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return
		}
		os.Exit(2)
	}

	if options.ShowVersion {
		fmt.Fprintln(os.Stdout, controllerBuildVersion)
		return
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	options.BuildVersion = controllerBuildVersion
	composed, material, err := bootstrap.Compose(ctx, options)
	if err != nil {
		log.Fatalf("initialize controller: %v", err)
	}
	if composed != nil {
		defer func() { _ = composed.Close() }()
		log.Printf("local agent CA: %s (root pin sha256 %s)", material.CertificatePath, material.RootSPKI)
	}

	if composed != nil {
		go func() {
			if err := composed.RunResidentHealth(ctx, controllerBuildVersion); err != nil && ctx.Err() == nil {
				log.Printf("resident readiness unavailable: %v", err)
			}
		}()
	}
	var handler http.Handler = health.Handler()
	if composed != nil {
		handler = composed
	}
	httpServer := &http.Server{
		Addr:              options.Listen,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		// Explicit AI generation is bounded at 35s, including provider and
		// post-borrow checks. Allow its verified response to reach the browser.
		WriteTimeout: 40 * time.Second,
		IdleTimeout:  30 * time.Second,
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
		tlsConfig, err := bootstrap.TLSConfig(options, material)
		if err != nil {
			log.Fatal(err)
		}
		httpServer.TLSConfig = tlsConfig
		log.Printf("Server Fleet Controller: https://%s (SQLite WAL management API and Agent enrollment active)", options.Listen)
		if err := httpServer.ListenAndServeTLS("", ""); err != nil && err != http.ErrServerClosed {
			log.Fatal(err)
		}
		return
	}
	log.Printf("Server Fleet scaffold: http://%s/healthz (management API unavailable)", options.Listen)
	if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
