package ai

import (
	"errors"
	"testing"
	"time"
)

func TestToolScopeZeroSnapshotRemovalAndCurrentRBAC(t *testing.T) {
	now := time.Now()
	scope, _ := NewScope("session-a", "user-a")
	scope.Mode = LowRisk
	call := ToolCall{"nodes.read", []string{"node-a"}, []byte(`{}`), 1}
	tool := ToolDefinition{ID: "nodes.read", ReadOnly: true, AutoAllowed: true}
	current := CurrentAuthority{SubjectID: "user-a", AllowedNodes: []string{"node-a", "node-b", "new-node"}, AllowedTools: []string{"nodes.read"}, GroupMembers: map[string][]string{"group-a": {"node-a", "node-b", "new-node"}}}
	if e := EvaluateTool(scope, call, tool, current, nil, now); e != ErrToolDenied {
		t.Fatal("new scope not empty", e)
	}
	scope.Groups["group-a"] = []string{"node-a", "node-b"}
	scope.GroupActions["group-a"] = []string{"nodes.read"}
	if e := EvaluateTool(scope, call, tool, current, nil, now); e != nil {
		t.Fatal(e)
	}
	call.NodeIDs = []string{"new-node"}
	if e := EvaluateTool(scope, call, tool, current, nil, now); e != ErrToolDenied {
		t.Fatal("new group member acquired permission", e)
	}
	call.NodeIDs = []string{"node-a"}
	current.GroupMembers["group-a"] = []string{"node-b"}
	if e := EvaluateTool(scope, call, tool, current, nil, now); e != ErrToolDenied {
		t.Fatal("removed member retained permission", e)
	}
	scope.Nodes = []string{"node-a"}
	scope.NodeActions["node-a"] = []string{"nodes.read"}
	current.AllowedNodes = nil
	if e := EvaluateTool(scope, call, tool, current, nil, now); e != ErrToolDenied {
		t.Fatal("scope bypassed RBAC", e)
	}
}

func TestToolRootRequiresRealAdminApprovalInFullAccess(t *testing.T) {
	now := time.Now()
	scope, _ := NewScope("session-a", "user-a")
	scope.Nodes = []string{"node-a"}
	scope.NodeActions["node-a"] = []string{"shell.root"}
	scope.Mode = FullAccess
	scope.FullUntil = now.Add(15 * time.Minute)
	scope.FullSince = now
	call := ToolCall{"shell.root", []string{"node-a"}, []byte(`{"command":"test-only"}`), 1}
	tool := ToolDefinition{ID: "shell.root", RootShell: true}
	current := CurrentAuthority{SubjectID: "user-a", AllowedNodes: []string{"node-a"}, AllowedTools: []string{"shell.root"}}
	if e := EvaluateTool(scope, call, tool, current, nil, now); e != ErrApprovalRequired {
		t.Fatal(e)
	}
	digest, _ := BindCall(scope, call)
	approval := Approval{"approval-1", scope.SessionID, scope.SubjectID, digest, 1, now.Add(time.Minute), false, true, true}
	if e := EvaluateTool(scope, call, tool, current, &approval, now); e != nil {
		t.Fatal(e)
	}
	for _, change := range []func(*Approval){func(a *Approval) { a.Administrator = false }, func(a *Approval) { a.HumanWeb = false }, func(a *Approval) { a.Consumed = true }, func(a *Approval) { a.Expires = now }, func(a *Approval) { a.ScopeRevision = 2 }, func(a *Approval) { a.SessionID = "other" }} {
		copy := approval
		change(&copy)
		if e := EvaluateTool(scope, call, tool, current, &copy, now); !errors.Is(e, ErrApprovalRequired) {
			t.Fatal(e)
		}
	}
	call.Parameters = []byte(`{"command":"changed"}`)
	if e := EvaluateTool(scope, call, tool, current, &approval, now); e != ErrApprovalRequired {
		t.Fatal("modified plan approved", e)
	}
}

func TestToolFullExpiryPluginDisableAndSecretExport(t *testing.T) {
	now := time.Now()
	scope, _ := NewScope("s", "u")
	scope.Nodes = []string{"n"}
	scope.NodeActions["n"] = []string{"xray.read"}
	scope.Mode = FullAccess
	scope.FullUntil = now
	call := ToolCall{"xray.read", []string{"n"}, []byte(`{}`), 1}
	tool := ToolDefinition{ID: "xray.read", PluginID: "xray-manager", ReadOnly: true, AutoAllowed: true}
	current := CurrentAuthority{SubjectID: "u", AllowedNodes: []string{"n"}, AllowedTools: []string{"xray.read"}, EnabledPlugins: []string{"xray-manager"}}
	if e := EvaluateTool(scope, call, tool, current, nil, now); e != ErrApprovalRequired {
		t.Fatal(e)
	}
	scope.Mode = LowRisk
	current.EnabledPlugins = nil
	if e := EvaluateTool(scope, call, tool, current, nil, now); e != ErrToolDenied {
		t.Fatal(e)
	}
	tool.PluginID = ""
	tool.SecretExport = true
	if e := EvaluateTool(scope, call, tool, current, nil, now); e != ErrToolDenied {
		t.Fatal(e)
	}
	tool.SecretExport = false
	call.ScopeRevision = 2
	if e := EvaluateTool(scope, call, tool, current, nil, now); e != ErrToolDenied {
		t.Fatal(e)
	}
}
