package server

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"google.golang.org/protobuf/proto"
	agentv1 "serverfleet.local/serverfleet/gen/go/serverfleet/agent/v1"
	"serverfleet.local/serverfleet/internal/audit"
	"serverfleet.local/serverfleet/internal/storage/testkit"
	"serverfleet.local/serverfleet/internal/tasks"
	"serverfleet.local/serverfleet/internal/transport"
)

func writeGatewayDeploymentFile(t *testing.T, document string, mode os.FileMode) string {
	t.Helper()
	directory := t.TempDir()
	if err := os.Chmod(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "gateway-deployment.json")
	if err := os.WriteFile(path, []byte(document), mode); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadGatewayDeploymentReadsExactPrivatePaths(t *testing.T) {
	path := writeGatewayDeploymentFile(t, `{
		"version": 1,
		"gateways": [{"gatewayId":"gw-one","siteDir":"/srv/fleet/nginx/conf","artifactDir":"/srv/fleet/nginx/artifacts"}],
		"certificates": [{"certificateId":"cert-one","certificatePath":"/srv/fleet/tls/site.crt","privateKeyPath":"/srv/fleet/tls/site.key"}]
	}`, 0o600)
	deployment, err := LoadGatewayDeployment(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := deployment.Gateways["gw-one"]; got.SiteDir != "/srv/fleet/nginx/conf" || got.ArtifactDir != "/srv/fleet/nginx/artifacts" {
		t.Fatalf("gateway paths = %+v", got)
	}
	if got := deployment.Certificates["cert-one"]; got.CertificatePath != "/srv/fleet/tls/site.crt" || got.PrivateKeyPath != "/srv/fleet/tls/site.key" {
		t.Fatalf("certificate paths = %+v", got)
	}
}

func TestLoadGatewayDeploymentRejectsInsecureOrAmbiguousConfiguration(t *testing.T) {
	valid := `{"version":1,"gateways":[{"gatewayId":"gw-one","siteDir":"/srv/a","artifactDir":"/srv/b"}],"certificates":[]}`
	cases := []struct {
		name     string
		document string
		mode     os.FileMode
	}{
		{"readable", valid, 0o644},
		{"unknown field", `{"version":1,"gateways":[{"gatewayId":"gw-one","siteDir":"/srv/a","artifactDir":"/srv/b"}],"certificates":[],"extra":true}`, 0o600},
		{"duplicate gateway", `{"version":1,"gateways":[{"gatewayId":"gw-one","siteDir":"/srv/a","artifactDir":"/srv/b"},{"gatewayId":"gw-one","siteDir":"/srv/c","artifactDir":"/srv/d"}],"certificates":[]}`, 0o600},
		{"relative path", `{"version":1,"gateways":[{"gatewayId":"gw-one","siteDir":"srv/a","artifactDir":"/srv/b"}],"certificates":[]}`, 0o600},
		{"traversal path", `{"version":1,"gateways":[{"gatewayId":"gw-one","siteDir":"/srv/../a","artifactDir":"/srv/b"}],"certificates":[]}`, 0o600},
		{"no gateways", `{"version":1,"gateways":[],"certificates":[]}`, 0o600},
		{"wrong version", `{"version":2,"gateways":[{"gatewayId":"gw-one","siteDir":"/srv/a","artifactDir":"/srv/b"}],"certificates":[]}`, 0o600},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			path := writeGatewayDeploymentFile(t, testCase.document, testCase.mode)
			if deployment, err := LoadGatewayDeployment(path); !errors.Is(err, ErrGatewayDeployment) || deployment != nil {
				t.Fatalf("deployment=%v err=%v", deployment, err)
			}
		})
	}
}

func TestLoadGatewayDeploymentRejectsPublicDirectory(t *testing.T) {
	path := writeGatewayDeploymentFile(t, `{"version":1,"gateways":[{"gatewayId":"gw-one","siteDir":"/srv/a","artifactDir":"/srv/b"}],"certificates":[]}`, 0o600)
	if err := os.Chmod(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadGatewayDeployment(path); !errors.Is(err, ErrGatewayDeployment) {
		t.Fatalf("err=%v", err)
	}
}

type wiringStageStub struct{}

func (wiringStageStub) CheckArtifactStage(context.Context, string, string, string, uint64, string, string, string, string, string) error {
	return nil
}
func (wiringStageStub) CheckArtifactChild(context.Context, string, string, string, uint64, string, string, string, string, string) error {
	return nil
}

func gatewayWiringBinding(t *testing.T) (GatewayArtifactBinding, *agentv1.TaskOffer) {
	t.Helper()
	content := []byte("server { listen 127.0.0.1:18080; }\n")
	digest := gatewayContentDigest(content)
	planDigest := gatewayContentDigest([]byte("plan"))
	params := transport.FileParameters{
		TransferID: "transfer-one", RelativePath: "site.conf", Direction: "write",
		Size: uint64(len(content)), SHA256: digest, ConflictPolicy: "skip",
	}
	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatal(err)
	}
	binding := GatewayArtifactBinding{
		TaskID: "task-one", NodeID: "node-one", ExecutionID: "gwa-one", TransferID: "transfer-one",
		PlanDigest: planDigest, GatewayID: "gw-one", PlanID: "plan-one", TargetRevision: 1,
		AbsolutePath: "/srv/fleet/nginx/artifacts/site.conf", RelativePath: "site.conf",
		SitePath: "/srv/fleet/nginx/conf/site.conf", Size: int64(len(content)), Content: content, SHA256: digest,
	}
	offer := &agentv1.TaskOffer{
		TaskId: binding.TaskID, ExecutionId: binding.ExecutionID, ActionId: transport.FileCapability,
		CanonicalParametersJson: raw, PlanDigestSha256: planDigest, ActionSchemaVersion: "1",
		Privilege: agentv1.Privilege_PRIVILEGE_NORMAL,
	}
	return binding, offer
}

func TestComposeGatewayFileGateRoutesOnlyPersistedReservations(t *testing.T) {
	ctx := context.Background()
	db := testkit.NewDB(t, tasks.Migrations(), audit.Migrations())
	if err := EnsureGatewayArtifactBindings(ctx, db); err != nil {
		t.Fatal(err)
	}
	binding, offer := gatewayWiringBinding(t)
	if err := saveGatewayArtifactBinding(ctx, db, binding); err != nil {
		t.Fatal(err)
	}
	gate, err := NewGatewayArtifactGate(db, wiringStageStub{})
	if err != nil {
		t.Fatal(err)
	}
	delegated := 0
	composed := composeGatewayFileGate(gate, func(context.Context, transport.LinkObservation, *agentv1.TaskOffer, *agentv1.Envelope) error {
		delegated++
		return nil
	})
	observation := transport.LinkObservation{Identity: transport.AgentIdentity{NodeID: binding.NodeID}}
	if err := composed(ctx, observation, offer, &agentv1.Envelope{Message: &agentv1.Envelope_TaskOffer{TaskOffer: offer}}); err != nil {
		t.Fatal(err)
	}
	if delegated != 0 {
		t.Fatalf("gateway reservation reached the file worker gate: %d", delegated)
	}
	foreign := proto.Clone(offer).(*agentv1.TaskOffer)
	foreign.ExecutionId = "gwa-two"
	if err := composed(ctx, observation, foreign, &agentv1.Envelope{Message: &agentv1.Envelope_TaskOffer{TaskOffer: foreign}}); err != nil {
		t.Fatal(err)
	}
	if delegated != 1 {
		t.Fatalf("unrelated file offer bypassed the file gate: %d", delegated)
	}
	mismatched := proto.Clone(offer).(*agentv1.TaskOffer)
	mismatched.PlanDigestSha256 = gatewayContentDigest([]byte("other-plan"))
	if err := composed(ctx, observation, mismatched, &agentv1.Envelope{Message: &agentv1.Envelope_TaskOffer{TaskOffer: mismatched}}); err == nil {
		t.Fatal("mismatched plan digest was accepted by the gateway gate")
	}
}

func TestGatewayArtifactMigrationIsRegisteredExactlyOnce(t *testing.T) {
	total := 0
	seen := 0
	for _, group := range Migrations() {
		for _, migration := range group {
			total++
			if migration.Version == "cx/0010_gateway_artifact_bindings" {
				seen++
			}
		}
	}
	if seen != 1 {
		t.Fatalf("cx/0010 registrations = %d, want 1", seen)
	}
	if total != 49 {
		t.Fatalf("complete Controller migrations = %d, want 49", total)
	}
}

func TestServerRejectsGatewayDeploymentWithoutTrustedFileRoots(t *testing.T) {
	directory := t.TempDir()
	if err := os.Chmod(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	_, err := New(context.Background(), Config{
		DBPath:     filepath.Join(directory, "fleet.db"),
		Origin:     "https://controller.example",
		StagingDir: filepath.Join(directory, "staging"),
		GatewayDeployment: &GatewayDeployment{Gateways: map[string]GatewayInstallPaths{
			"gw-one": {SiteDir: "/srv/fleet/nginx/conf", ArtifactDir: "/srv/fleet/nginx/artifacts"},
		}},
	})
	if !errors.Is(err, ErrGatewayDeployment) {
		t.Fatalf("err=%v, want ErrGatewayDeployment", err)
	}
}
