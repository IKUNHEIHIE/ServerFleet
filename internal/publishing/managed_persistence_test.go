package publishing_test

import (
	"context"
	"encoding/json"
	"serverfleet.local/serverfleet/internal/publishing"
	"strings"
	"testing"
)

func TestManagedGatewayCompensationReceiptPersistsExactPublicProbe(t *testing.T) {
	f := newPublicationFixture(t)
	f.createPlan(t, "managed-plan", "app.example.com", "")
	receipt := publishing.GatewayReceipt{RouteID: "route", GatewayID: "gateway", NodeID: "node", SitePath: "/private/" + strings.Repeat("a", 230) + "/nginx.conf", ConfigSHA256: strings.Repeat("a", 64), TaskID: "gateway-task", RouteRevision: 2, RouteCreated: true, PlanID: "managed-plan", PlanTargetRevision: 1, ProbeToken: strings.Repeat("b", 64), HTTPStatus: 200, ResponseSHA256: strings.Repeat("c", 64), ResponseBytes: 123}
	raw, e := json.Marshal(receipt)
	if e != nil {
		t.Fatal(e)
	}
	f.ports.gateway.set(publishing.StageResult{Summary: string(raw)}, nil)
	if _, e = f.service.Run(context.Background(), "admin", "managed-run", "managed-plan"); e != nil {
		t.Fatal("public compensation receipt rejected", e)
	}
	_, stages := f.plan(t, "managed-plan")
	stored := stageByKind(t, stages, publishing.StageGateway)
	if stored.ResultSummary != string(raw) {
		t.Fatal("compensation bytes were truncated or redacted", stored.ResultSummary)
	}
}
