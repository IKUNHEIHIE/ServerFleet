package ai

import (
	"context"
	"encoding/json"
	"net/http"
	"serverfleet.local/serverfleet/internal/auth"
	"strings"
	"sync/atomic"
	"testing"
)

func TestModelProbeUsesSyntheticFunctionAndNeverExecutes(t *testing.T) {
	var requests atomic.Int32
	provider, r, _, _ := providerFixture(t, func(w http.ResponseWriter, req *http.Request) {
		requests.Add(1)
		var body map[string]json.RawMessage
		if json.NewDecoder(req.Body).Decode(&body) != nil {
			t.Error("invalid probe body")
		}
		w.Header().Set("Content-Type", "application/json")
		reply := map[string]any{"id": "probe-result", "object": "response", "model": "fixture-model", "status": "completed", "store": false}
		if _, ok := body["tools"]; ok {
			reply["output"] = []any{map[string]any{"type": "function_call", "name": "serverfleet_capability_probe", "call_id": "probe-call", "arguments": `{"ok":true}`}}
		} else {
			reply["output"] = []any{map[string]any{"type": "reasoning", "summary": []any{map[string]string{"type": "summary_text", "text": "Check only"}}}}
		}
		_ = json.NewEncoder(w).Encode(reply)
	})
	runtime := &BYOKRuntime{provider: provider}
	op := auth.CredentialOperation{SubjectID: r.SubjectID, CredentialID: r.Profile.CredentialRef, Module: "ai", Source: "ai", OperationID: "ai-models-probe-local", PlanDigest: strings.Repeat("a", 64), Action: "use", ExpectedRevision: 1}
	result, err := runtime.ProbeModel(context.Background(), op, "", r.Profile.Model)
	if err != nil || !result.ToolCalls || !result.Reasoning || requests.Load() != 2 {
		t.Fatal(result, err, requests.Load())
	}
}
func TestCompactionKeepsRepeatedUserInstructionsAndOrder(t *testing.T) {
	text := "restart once\ncheck health\nrestart once"
	got := compactRecord("user", text)
	if !strings.HasSuffix(got, text) {
		t.Fatal("user instructions reordered or deduplicated")
	}
}
