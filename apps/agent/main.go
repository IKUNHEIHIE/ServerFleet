// Agent is an offline M0-P1 bootstrap entry. Without arguments it keeps the
// historical scaffold output; explicit flags run a local status self-check, one
// enrollment attempt or one controlled WSS+mTLS connection.
package main

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"syscall"
	"time"

	agentruntime "serverfleet.local/serverfleet/apps/agent/runtime"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	os.Exit(agentruntime.Run(ctx, os.Args[1:], agentRuntimeDeps(), os.Stdout, os.Stderr))
}

func agentRuntimeDeps() agentruntime.Deps {
	return agentruntime.Deps{Status: agentStatus, Enroll: agentEnrollOnce, Connect: agentConnectOnce, Serve: agentServe}
}

func agentStatus(ctx context.Context, stateDir, rootPin string) (agentruntime.Status, error) {
	store, err := openAgentIdentity(stateDir, rootPin, 1)
	if err != nil {
		return agentruntime.Status{}, errAgentIdentity
	}
	defer store.Close()
	facts, enrolled, err := store.Status(ctx)
	if err != nil {
		return agentruntime.Status{}, errAgentIdentity
	}
	status := agentruntime.Status{StateDir: stateDir, Enrolled: enrolled}
	if enrolled {
		expires := facts.ExpiresAt
		status.NodeID = facts.NodeID
		status.Fingerprint = facts.Fingerprint
		status.TrustGeneration = facts.TrustGeneration
		status.ExpiresAt = &expires
	}
	return status, nil
}

func agentEnrollOnce(ctx context.Context, request agentruntime.EnrollRequest) (agentruntime.EnrollResult, error) {
	rootPEM, err := os.ReadFile(request.RootCertificate)
	if err != nil || len(rootPEM) == 0 || len(rootPEM) > 16<<10 {
		return agentruntime.EnrollResult{}, errAgentEnrollment
	}
	if err := os.MkdirAll(request.StateDir, 0o700); err != nil {
		return agentruntime.EnrollResult{}, errAgentEnrollment
	}
	store, err := openAgentIdentity(request.StateDir, request.RootPin, 1)
	if err != nil {
		return agentruntime.EnrollResult{}, errAgentEnrollment
	}
	defer store.Close()
	if _, err := store.Prepare(ctx, request.AgentVersion); err != nil {
		return agentruntime.EnrollResult{}, errAgentEnrollment
	}
	client, err := newAgentEnrollmentClient(store, agentEnrollmentConfig{
		Origin:             request.ControllerURL,
		RootCertificatePEM: string(rootPEM),
		Current:            func(context.Context) error { return nil },
	})
	if err != nil {
		return agentruntime.EnrollResult{}, errAgentEnrollment
	}
	observation, err := client.EnrollOnce(ctx, request.EnrollToken)
	result := agentruntime.EnrollResult{State: observation.State, ExpiresAt: observation.ExpiresAt}
	if err != nil {
		if errors.Is(err, errAgentEnrollmentDenied) {
			result.State = "denied"
		}
		return result, err
	}
	if observation.State == "issued" {
		if facts, enrolled, statusErr := store.Status(ctx); statusErr == nil && enrolled {
			expires := facts.ExpiresAt
			result.NodeID = facts.NodeID
			result.ExpiresAt = expires.In(time.UTC)
		}
	}
	return result, nil
}
