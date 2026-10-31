package publishing_test

import (
	"context"
	"errors"
	"testing"

	"serverfleet.local/serverfleet/internal/publishing"
)

func TestLocalBridgeDelegatesToConfiguredFunctions(t *testing.T) {
	t.Parallel()
	var (
		dnsRequest   publishing.DNSPublishRequest
		certRequest  publishing.CertificateRequest
		gwRequest    publishing.GatewayRequest
		rollbackPlan string
		rollbackSeen publishing.StageResult
	)
	bridge := publishing.NewLocalBridge(publishing.LocalBridgeConfig{
		DNSPublish: func(_ context.Context, request publishing.DNSPublishRequest) (publishing.StageResult, error) {
			dnsRequest = request
			return publishing.StageResult{Summary: "dns ok"}, nil
		},
		DNSRollback: func(_ context.Context, planID string, result publishing.StageResult) error {
			rollbackPlan, rollbackSeen = planID, result
			return nil
		},
		CertificatePublish: func(_ context.Context, request publishing.CertificateRequest) (publishing.StageResult, error) {
			certRequest = request
			return publishing.StageResult{Summary: "cert ok"}, nil
		},
		GatewayPublish: func(_ context.Context, request publishing.GatewayRequest) (publishing.StageResult, error) {
			gwRequest = request
			return publishing.StageResult{Summary: "gateway ok"}, nil
		},
	})
	if bridge.DNS == nil || bridge.Certificates == nil || bridge.Gateway == nil {
		t.Fatalf("bridge ports = %+v", bridge)
	}
	dnsResult, err := bridge.DNS.Publish(context.Background(), publishing.DNSPublishRequest{PlanID: "p1", Domain: "app.example.com"})
	if err != nil || dnsResult.Summary != "dns ok" || dnsRequest.PlanID != "p1" {
		t.Fatalf("dns = %+v %+v err=%v", dnsResult, dnsRequest, err)
	}
	certResult, err := bridge.Certificates.Publish(context.Background(), publishing.CertificateRequest{PlanID: "p2"})
	if err != nil || certResult.Summary != "cert ok" || certRequest.PlanID != "p2" {
		t.Fatalf("certificate = %+v %+v err=%v", certResult, certRequest, err)
	}
	gwResult, err := bridge.Gateway.Publish(context.Background(), publishing.GatewayRequest{PlanID: "p3"})
	if err != nil || gwResult.Summary != "gateway ok" || gwRequest.PlanID != "p3" {
		t.Fatalf("gateway = %+v %+v err=%v", gwResult, gwRequest, err)
	}
	if err := bridge.DNS.Rollback(context.Background(), "p1", publishing.StageResult{Summary: "dns ok"}); err != nil {
		t.Fatal(err)
	}
	if rollbackPlan != "p1" || rollbackSeen.Summary != "dns ok" {
		t.Fatalf("rollback = %q %+v", rollbackPlan, rollbackSeen)
	}
}

func TestLocalBridgeFailsClosedWithoutFunctions(t *testing.T) {
	t.Parallel()
	bridge := publishing.NewLocalBridge(publishing.LocalBridgeConfig{})
	if _, err := bridge.DNS.Publish(context.Background(), publishing.DNSPublishRequest{}); !errors.Is(err, publishing.ErrPublisherUnavailable) {
		t.Fatalf("nil dns publish err = %v", err)
	}
	if err := bridge.Certificates.Rollback(context.Background(), "p", publishing.StageResult{}); !errors.Is(err, publishing.ErrPublisherUnavailable) {
		t.Fatalf("nil certificate rollback err = %v", err)
	}
	if _, err := bridge.Gateway.Publish(context.Background(), publishing.GatewayRequest{}); !errors.Is(err, publishing.ErrPublisherUnavailable) {
		t.Fatalf("nil gateway publish err = %v", err)
	}
}
