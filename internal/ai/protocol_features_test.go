package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"serverfleet.local/serverfleet/internal/auth"
	"strings"
	"testing"
	"time"
)

func protocolJSON(t *testing.T, value any) string {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}
func protocolPrompt() *agentPrompt {
	return &agentPrompt{RunID: "protocol-run", Step: 1, Goal: "Inspect", Instructions: agentInstructions, Tools: []AgentTool{{Name: "self_check", Action: "tasks.command", NodeIDs: []string{"node-a"}}}, History: []json.RawMessage{}}
}
func protocolNative(t *testing.T, functions int) string {
	output := []any{}
	for index := 0; index < functions; index++ {
		output = append(output, map[string]any{"type": "function_call", "name": "self_check", "call_id": fmt.Sprintf("call-%d", index), "arguments": protocolJSON(t, map[string]any{"nodeIds": []string{"node-a"}}), "status": "completed"})
	}
	return protocolJSON(t, map[string]any{"id": "response-protocol", "object": "response", "model": "fixture-model", "store": false, "status": "completed", "output": output})
}
func TestEinoBatchExecutesAllCallsAcrossDurableResume(t *testing.T) {
	ctx := context.Background()
	loop, view, _, executions := agentLoopFixture(t, false, false)
	native := protocolNative(t, 2)
	modelCalls := 0
	loop.conversation.config.Generate = func(_ context.Context, r GenerationRequest, emit func(string) error) error {
		modelCalls++
		if modelCalls == 1 {
			decision, err := decodeAgentDecision([]byte(native), "fixture-model", r.Agent)
			if err != nil {
				return err
			}
			return emit(decision)
		}
		if len(r.Agent.History) != 4 {
			t.Fatalf("batch observations missing: %s", r.Agent.History)
		}
		return emit(protocolJSON(t, agentDecision{Text: "Both actual results received", Items: []json.RawMessage{}}))
	}
	run, err := loop.Create(ctx, view.ID, AgentRunInput{Goal: "check twice", ClientRunID: "batch-run", ScopeRevision: view.ScopeRevision, MaxSteps: 8})
	if err != nil {
		t.Fatal(err)
	}
	run, err = loop.Advance(ctx, view.ID, run.ID, run.Revision)
	if err != nil || run.State != "waiting_task" {
		t.Fatal(run, err)
	}
	run, err = loop.Advance(ctx, view.ID, run.ID, run.Revision)
	if err != nil || run.State != "queued" {
		t.Fatal(run, err)
	}
	// Reconstruct the real Eino-backed loop from the same durable store, then run
	// the remaining proposal without generating the batch again.
	resumed, err := NewAgentLoop(loop.sessions, loop.conversation, loop.config)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 4 && !agentTerminal(run.State); i++ {
		run, err = resumed.Advance(ctx, view.ID, run.ID, run.Revision)
		if err != nil {
			t.Fatal(err)
		}
	}
	if run.State != "completed" || *executions != 2 || modelCalls != 2 || len(run.Steps) != 3 {
		t.Fatal(run, *executions, modelCalls)
	}
}
func TestChatCompletionToolFormatsAndContinuation(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		t.Run(fmt.Sprint(legacy), func(t *testing.T) {
			message := map[string]any{"role": "assistant", "content": nil, "reasoning_content": "Provider-exposed reasoning"}
			function := map[string]string{"name": "self_check", "arguments": protocolJSON(t, map[string]any{"nodeIds": []string{"node-a"}})}
			if legacy {
				message["function_call"] = function
			} else {
				message["tool_calls"] = []any{map[string]any{"id": "chat-call", "type": "function", "function": function}}
			}
			raw := protocolJSON(t, map[string]any{"id": "chat-response", "object": "chat.completion", "model": "fixture-model", "choices": []any{map[string]any{"index": 0, "finish_reason": "stop", "message": message}}})
			converted, err := chatCompletionResponse([]byte(raw), "fixture-model")
			if err != nil {
				t.Fatal(err)
			}
			prompt := protocolPrompt()
			encoded, err := decodeAgentDecision(converted, "fixture-model", prompt)
			if err != nil {
				t.Fatal(err)
			}
			var decision agentDecision
			if json.Unmarshal([]byte(encoded), &decision) != nil || decision.Call == nil || decision.Reasoning != "Provider-exposed reasoning" {
				t.Fatal(encoded)
			}
			prompt.History = append(prompt.History, decision.Items...)
			result := protocolJSON(t, map[string]string{"type": "function_call_output", "call_id": decision.Call.CallID, "output": "actual result"})
			prompt.History = append(prompt.History, json.RawMessage(result))
			payload, err := chatCompletionPayload(GenerationRequest{Profile: ModelProfile{Model: "fixture-model"}, Agent: prompt}, true)
			if err != nil {
				t.Fatal(err)
			}
			body := protocolJSON(t, payload)
			if !strings.Contains(body, "tool_call_id") || !strings.Contains(body, "reasoning_content") || !strings.Contains(body, "actual result") {
				t.Fatal(body)
			}
		})
	}
}
func TestChatStreamAccumulatesReasoningAndSplitToolArguments(t *testing.T) {
	events := []any{
		map[string]any{"reasoning_content": "First "}, map[string]any{"reasoning_content": "then act"},
		map[string]any{"tool_calls": []any{map[string]any{"index": 0, "id": "split-call", "type": "function", "function": map[string]any{"name": "self_check", "arguments": "{\"nodeIds\":"}}}},
		map[string]any{"tool_calls": []any{map[string]any{"index": 0, "function": map[string]any{"arguments": "[\"node-a\"]}"}}}},
	}
	stream := ""
	for _, delta := range events {
		stream += "data: " + protocolJSON(t, map[string]any{"id": "chat-stream", "object": "chat.completion.chunk", "model": "fixture-model", "choices": []any{map[string]any{"index": 0, "delta": delta, "finish_reason": nil}}}) + "\n\n"
	}
	stream += "data: " + protocolJSON(t, map[string]any{"id": "chat-stream", "model": "fixture-model", "choices": []any{map[string]any{"index": 0, "delta": map[string]any{}, "finish_reason": "tool_calls"}}}) + "\n\ndata: [DONE]\n\n"
	var reasoning []string
	output, err := decodeChatStream(context.Background(), strings.NewReader(stream), "fixture-model", protocolPrompt(), func(kind, text string) error {
		if kind == "reasoning" {
			reasoning = append(reasoning, text)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	var decision agentDecision
	if json.Unmarshal([]byte(output), &decision) != nil || decision.Call == nil || decision.Reasoning != "First then act" || len(reasoning) != 2 {
		t.Fatal(output, reasoning)
	}
	if _, err := decodeChatStream(context.Background(), strings.NewReader(strings.TrimSuffix(stream, "data: [DONE]\n\n")), "fixture-model", protocolPrompt(), nil); err == nil {
		t.Fatal("truncated chat stream accepted")
	}
}
func TestResponsesStreamIncludesReadableReasoningAndToolDecision(t *testing.T) {
	stream := ""
	events := []map[string]any{{"type": "response.created"}, {"type": "response.reasoning_text.delta", "delta": "Detailed provider output"}, {"type": "response.reasoning_summary_text.delta", "delta": "Short summary"}, {"type": "response.output_item.added", "item": map[string]string{"type": "function_call"}}, {"type": "response.function_call_arguments.delta", "output_index": 0, "delta": "{\"nodeIds\":[\"node-a\"]}"}}
	for index, event := range events {
		event["sequence_number"] = index
		stream += "data: " + protocolJSON(t, event) + "\n\n"
	}
	stream += "data: " + protocolJSON(t, map[string]any{"type": "response.completed", "sequence_number": len(events), "response": json.RawMessage(protocolNative(t, 1))}) + "\n\n"
	got := map[string]string{}
	text, err := decodeResponseStreamObserved(context.Background(), strings.NewReader(stream), "fixture-model", protocolPrompt(), func(kind, text string) error { got[kind] = text; return nil })
	if err != nil {
		t.Fatal(err)
	}
	var decision agentDecision
	if json.Unmarshal([]byte(text), &decision) != nil || decision.Reasoning != "Detailed provider output" || decision.Summary != "Short summary" || decision.Call == nil || got["reasoning"] != decision.Reasoning {
		t.Fatal(text, got)
	}
}
func TestProfileFunctionAcceptsJSONArgumentsAndNumericTimeout(t *testing.T) {
	prompt := protocolPrompt()
	prompt.Tools = []AgentTool{{Name: "run_profile", Action: "tasks.exec.v1", NodeIDs: []string{"node-a"}, Profile: true}}
	arguments := protocolJSON(t, map[string]any{"nodeIds": []string{"node-a"}, "profileId": "profile-one", "policySha256": strings.Repeat("a", 64), "arguments": []string{"--status"}, "timeoutSeconds": 60})
	native := protocolJSON(t, map[string]any{"id": "profile-response", "object": "response", "model": "fixture-model", "output": []any{map[string]string{"type": "function_call", "name": "run_profile", "call_id": "profile-call", "arguments": arguments}}})
	encoded, err := decodeAgentDecision([]byte(native), "fixture-model", prompt)
	if err != nil {
		t.Fatal(err)
	}
	var decision agentDecision
	if json.Unmarshal([]byte(encoded), &decision) != nil || !strings.Contains(string(decision.Call.Parameters), "timeoutSeconds") || !strings.Contains(string(decision.Call.Parameters), "--status") {
		t.Fatal(encoded)
	}
}

func TestEinoRunPublishesReasoningBeforeFinalDecision(t *testing.T) {
	ctx := context.Background()
	loop, view, _, _ := agentLoopFixture(t, false, false)
	streamed, finish := make(chan struct{}), make(chan struct{})
	loop.conversation.config.Generate = func(ctx context.Context, _ GenerationRequest, emit func(string) error) error {
		if err := generationPreviewFrom(ctx)("reasoning", "Visible provider reasoning in progress"); err != nil {
			return err
		}
		close(streamed)
		<-finish
		return emit(protocolJSON(t, agentDecision{Text: "Final answer", Items: []json.RawMessage{}}))
	}
	run, err := loop.Create(ctx, view.ID, AgentRunInput{Goal: "inspect", ClientRunID: "reasoning-run", ScopeRevision: view.ScopeRevision})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := loop.Advance(ctx, view.ID, run.ID, run.Revision); done <- err }()
	select {
	case <-streamed:
	case <-time.After(3 * time.Second):
		t.Fatal("model did not stream")
	}
	current, err := loop.Get(ctx, view.ID, run.ID)
	if err != nil || current.State != "thinking" || current.Steps[0].ReasoningContent != "Visible provider reasoning in progress" {
		t.Fatal(current, err)
	}
	close(finish)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	current, err = loop.Get(ctx, view.ID, run.ID)
	if err != nil || current.StopReason != "no_successful_tool_receipt" || current.Steps[0].ReasoningContent != "Visible provider reasoning in progress" {
		t.Fatal(current, err)
	}
}

func TestChatProtocolThroughRealCredentialAndTLSChannel(t *testing.T) {
	for _, streaming := range []bool{false, true} {
		t.Run(fmt.Sprint(streaming), func(t *testing.T) {
			p, r, _, _ := providerFixture(t, func(w http.ResponseWriter, request *http.Request) {
				if !strings.HasSuffix(request.URL.Path, "/chat/completions") {
					t.Error("wrong endpoint", request.URL.Path)
				}
				var body map[string]json.RawMessage
				if json.NewDecoder(request.Body).Decode(&body) != nil || body["messages"] == nil || body["input"] != nil {
					t.Error("wrong protocol request")
				}
				reasoning := "Provider plaintext reasoning: inspect the latest observations before responding."
				if streaming {
					w.Header().Set("Content-Type", "text/event-stream")
					for _, entry := range []map[string]any{{"reasoning_content": reasoning}, {"content": "**Ready**"}} {
						raw := protocolJSON(t, map[string]any{"id": "chat-tls", "model": "fixture-model", "choices": []any{map[string]any{"index": 0, "delta": entry, "finish_reason": nil}}})
						fmt.Fprintf(w, "data: %s\n\n", raw)
					}
					fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", protocolJSON(t, map[string]any{"id": "chat-tls", "model": "fixture-model", "choices": []any{map[string]any{"index": 0, "delta": map[string]any{}, "finish_reason": "stop"}}}))
				} else {
					w.Header().Set("Content-Type", "application/json")
					_ = json.NewEncoder(w).Encode(map[string]any{"id": "chat-tls", "object": "chat.completion", "model": "fixture-model", "choices": []any{map[string]any{"index": 0, "finish_reason": "stop", "message": map[string]any{"role": "assistant", "content": "**Ready**", "reasoning_content": reasoning}}}})
				}
			})
			p.stream = streaming
			p.protocolFor = func(context.Context, GenerationRequest) (string, error) { return "openai-chat-completions", nil }
			got := ""
			thought := ""
			ctx := withGenerationPreview(context.Background(), func(kind, text string) error {
				if kind == "reasoning" {
					thought = text
				}
				return nil
			})
			if err := p.Generate(ctx, r, func(text string) error { got += text; return nil }); err != nil || got != "**Ready**" || !strings.Contains(thought, "latest observations") {
				t.Fatal(got, thought, err)
			}
		})
	}
}

func TestChatModelCapabilityProbeUsesSelectedProtocol(t *testing.T) {
	p, r, _, _ := providerFixture(t, func(w http.ResponseWriter, request *http.Request) {
		if !strings.HasSuffix(request.URL.Path, "/chat/completions") {
			t.Error("wrong probe endpoint")
		}
		var body map[string]json.RawMessage
		_ = json.NewDecoder(request.Body).Decode(&body)
		message := map[string]any{"role": "assistant", "content": "OK", "reasoning_content": "Provider-visible reasoning"}
		if body["tools"] != nil {
			message = map[string]any{"role": "assistant", "content": nil, "tool_calls": []any{map[string]any{"id": "probe-call", "type": "function", "function": map[string]any{"name": "serverfleet_capability_probe", "arguments": "{\"ok\":true}"}}}}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "chat-probe", "object": "chat.completion", "model": "fixture-model", "choices": []any{map[string]any{"index": 0, "finish_reason": "stop", "message": message}}})
	})
	runtime := &BYOKRuntime{provider: p}
	op := auth.CredentialOperation{SubjectID: r.SubjectID, CredentialID: r.Profile.CredentialRef, Module: "ai", Source: "ai", OperationID: "ai-models-probe-local", PlanDigest: strings.Repeat("a", 64), Action: "use", ExpectedRevision: 1}
	result, err := runtime.ProbeModel(context.Background(), op, "", r.Profile.Model, "openai-chat-completions")
	if err != nil || !result.ToolCalls || !result.Reasoning {
		t.Fatal(result, err)
	}
}

func TestResponsesCompletionPreservesPlaintextReasoningWithoutDeltas(t *testing.T) {
	output := []any{
		map[string]any{"type": "reasoning", "content": []any{map[string]string{"type": "reasoning_text", "text": "Full reasoning returned at completion"}}, "summary": []any{map[string]string{"type": "summary_text", "text": "Final summary"}}},
		map[string]any{"type": "message", "role": "assistant", "status": "completed", "content": []any{map[string]string{"type": "output_text", "text": "Done"}}},
	}
	response := map[string]any{"id": "completed-fields", "object": "response", "model": "fixture-model", "status": "completed", "store": false, "output": output}
	stream := "data: " + protocolJSON(t, map[string]any{"type": "response.output_text.delta", "sequence_number": 0, "delta": "Done"}) + "\n\n"
	stream += "data: " + protocolJSON(t, map[string]any{"type": "response.completed", "sequence_number": 1, "response": response}) + "\n\n"
	observed := map[string]string{}
	text, err := decodeProviderStreamObserved(context.Background(), strings.NewReader(stream), "fixture-model", func(kind, value string) error { observed[kind] = value; return nil })
	if err != nil || text != "Done" || observed["reasoning_completed"] != "Full reasoning returned at completion" || observed["summary_completed"] != "Final summary" {
		t.Fatal(text, observed, err)
	}
}
