package server

import (
	"context"
	"errors"
	agentv1 "serverfleet.local/serverfleet/gen/go/serverfleet/agent/v1"
	"serverfleet.local/serverfleet/internal/nodes"
	"serverfleet.local/serverfleet/internal/storage/testkit"
	"testing"
	"time"

	"serverfleet.local/serverfleet/internal/gateway"
	"serverfleet.local/serverfleet/internal/transport"
)

// The protocol whitelist must admit the two typed gateway action ids, or a
// gateway-enabled link is rejected between the dynamic handshake grant and the
// static semantic re-check in ControllerLinks.Serve. Actual grant stays
// dynamic: the bridge only offers them once the trusted gateway chain exists.
func TestAgentValidationLimitsAdmitGatewayActions(t *testing.T) {
	limits := agentValidationLimits()
	if limits.AllowedActions[gateway.NginxActionApply] != "1" {
		t.Fatalf("protocol whitelist rejects %s", gateway.NginxActionApply)
	}
	if limits.AllowedActions[gateway.NginxActionRemove] != "1" {
		t.Fatalf("protocol whitelist rejects %s", gateway.NginxActionRemove)
	}
	for _, action := range []string{"tasks.command", transport.FileCapability, "host.patch", "host.root"} {
		if limits.AllowedActions[action] != "1" {
			t.Fatalf("protocol whitelist dropped %s", action)
		}
	}
}

// Without the trusted gateway chain the bridge still never offers the typed
// gateway actions, so a non-gateway controller grants nothing new.
func TestBridgeAllowedActionsStayClosedWithoutGatewayChain(t *testing.T) {
	bridge := &agentLinkBridge{}
	allowed := bridge.allowedActions()
	if _, ok := allowed[gateway.NginxActionApply]; ok {
		t.Fatalf("gateway apply offered without the trusted gateway chain")
	}
	if _, ok := allowed[gateway.NginxActionRemove]; ok {
		t.Fatalf("gateway remove offered without the trusted gateway chain")
	}
	if allowed["tasks.command"] != "1" || allowed[transport.FileCapability] != "1" {
		t.Fatalf("base actions dropped: %v", allowed)
	}
}

// A gateway-enabled bridge offers the typed actions so the handshake grant and
// the static whitelist agree.
func TestBridgeAllowedActionsAdmitGatewayChain(t *testing.T) {
	bridge := &agentLinkBridge{gatewayChild: &GatewayArtifactObserver{}}
	allowed := bridge.allowedActions()
	if allowed[gateway.NginxActionApply] != "1" || allowed[gateway.NginxActionRemove] != "1" {
		t.Fatalf("gateway actions missing with the trusted chain: %v", allowed)
	}
}

func TestBridgeRejectsRevokedNodeBeforeProcessingEvents(t *testing.T) {
	ctx := context.Background()
	db := testkit.NewDB(t, nodes.Migrations())
	repo, err := nodes.NewRepo(db)
	if err != nil {
		t.Fatal(err)
	}
	_, err = repo.Create(ctx, db, nodes.NodeCreateInput{ID: "revoked-node", Name: "Synthetic", Status: nodes.StatusOnline})
	if err != nil {
		t.Fatal(err)
	}
	bridge := &agentLinkBridge{nodesRepo: repo}
	observation := transport.LinkObservation{Identity: transport.AgentIdentity{NodeID: "revoked-node"}}
	hello := &agentv1.Envelope{NodeId: "revoked-node", Message: &agentv1.Envelope_AgentHello{AgentHello: &agentv1.AgentHello{}}}
	if err := bridge.event(ctx, observation, hello); err != nil {
		t.Fatalf("online control rejected: %v", err)
	}
	if _, err := repo.UpdateStatus(ctx, db, "revoked-node", nodes.StatusRevoked, time.Now()); err != nil {
		t.Fatal(err)
	}
	for _, envelope := range []*agentv1.Envelope{
		hello,
		{NodeId: "revoked-node", Message: &agentv1.Envelope_TaskAck{TaskAck: &agentv1.TaskAck{TaskId: "old-task", ExecutionId: "old-execution", Accepted: true}}},
		{NodeId: "revoked-node", Message: &agentv1.Envelope_TaskOutput{TaskOutput: &agentv1.TaskOutput{TaskId: "old-task"}}},
		{NodeId: "revoked-node", Message: &agentv1.Envelope_TaskFinished{TaskFinished: &agentv1.TaskFinished{TaskId: "old-task"}}},
	} {
		if err := bridge.event(ctx, observation, envelope); !errors.Is(err, transport.ErrLinkRejected) {
			t.Fatalf("revoked event accepted: %v", err)
		}
	}
}
