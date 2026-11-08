package server

import (
	"testing"

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
