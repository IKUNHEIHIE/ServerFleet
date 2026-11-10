package ai

import (
	"bytes"
	"encoding/json"
	"io"
	"slices"
	"strings"
	"unicode/utf8"
)

// Native Responses function calls are proposals, never execution tickets.
// No hosted tools, arbitrary URLs, shell tools or provider-side execution.
func agentProviderPayload(body map[string]any, p *agentPrompt) error {
	if validateAgentPrompt(p) != nil {
		return ErrToolDenied
	}
	input := []any{map[string]string{"role": "user", "content": p.Goal}}
	for _, item := range p.History {
		input = append(input, json.RawMessage(bytes.Clone(item)))
	}
	tools := []any{}
	for _, t := range p.Tools {
		maxNodes := 200
		if t.Name == "inspect_nodes" {
			maxNodes = 50
		}
		properties := map[string]any{"nodeIds": map[string]any{"type": "array", "items": map[string]any{"type": "string", "enum": t.NodeIDs}, "minItems": 1, "maxItems": maxNodes}}
		required := []string{"nodeIds"}
		if t.Profile {
			for _, key := range []string{"profileId", "policySha256", "arguments", "timeoutSeconds"} {
				properties[key] = map[string]string{"type": "string"}
				required = append(required, key)
			}
		}
		tools = append(tools, map[string]any{"type": "function", "name": t.Name, "description": t.Description, "strict": true, "parameters": map[string]any{"type": "object", "properties": properties, "required": required, "additionalProperties": false}})
	}
	body["input"], body["instructions"], body["tools"], body["include"] = input, agentInstructions, tools, []string{"reasoning.encrypted_content"}
	if len(tools) > 0 {
		body["tool_choice"] = "auto"
	}
	return nil
}

func decodeAgentDecision(raw []byte, model string, p *agentPrompt) (string, error) {
	if len(raw) == 0 || len(raw) > 1<<20 || !utf8.Valid(raw) {
		return "", ErrGenerationUnknown
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	budget := 100000
	if validateProviderJSON(d, 0, &budget) != nil {
		return "", ErrGenerationUnknown
	}
	if _, e := d.Token(); e != io.EOF {
		return "", ErrGenerationUnknown
	}
	var response struct {
		ID, Object, Status, Model string
		Store                     *bool
		Output                    []json.RawMessage
	}
	if json.Unmarshal(raw, &response) != nil || response.ID == "" || response.Object != "response" || response.Status != "completed" || response.Model != model || response.Store == nil || *response.Store || len(response.Output) == 0 || len(response.Output) > 32 {
		return "", ErrGenerationUnknown
	}
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(raw, &fields)
	for _, name := range []string{"error", "incomplete_details"} {
		if value := fields[name]; len(value) > 0 && !bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return "", ErrGenerationUnknown
		}
	}
	out := agentDecision{Items: []json.RawMessage{}}
	for _, item := range response.Output {
		var v struct {
			Type, ID, Name, Arguments, CallID, Role, Status, EncryptedContent string
			Content                                                           []struct{ Type, Text string }
		}
		// Critical snake_case names are decoded explicitly; aliases are rejected
		// by the case-insensitive duplicate checker before structural parsing.
		var f map[string]json.RawMessage
		if json.Unmarshal(item, &f) != nil {
			return "", ErrGenerationUnknown
		}
		if json.Unmarshal(item, &v) != nil {
			return "", ErrGenerationUnknown
		}
		_ = json.Unmarshal(f["call_id"], &v.CallID)
		_ = json.Unmarshal(f["encrypted_content"], &v.EncryptedContent)
		switch v.Type {
		case "reasoning":
			if v.EncryptedContent != "" {
				if len(v.EncryptedContent) > 64<<10 {
					return "", ErrGenerationUnknown
				}
				b, _ := json.Marshal(map[string]any{"type": "reasoning", "id": v.ID, "encrypted_content": v.EncryptedContent, "summary": []any{}})
				out.Items = append(out.Items, b)
			}
		case "function_call":
			if out.Call != nil || !exactID(v.CallID) || v.Status != "completed" || len(v.Arguments) > 8<<10 {
				return "", ErrGenerationUnknown
			}
			var tool *AgentTool
			for i := range p.Tools {
				if p.Tools[i].Name == v.Name {
					tool = &p.Tools[i]
				}
			}
			if tool == nil {
				return "", ErrToolDenied
			}
			var a struct {
				NodeIDs   []string `json:"nodeIds"`
				ProfileID string   `json:"profileId"`
				PolicySHA string   `json:"policySha256"`
				Arguments string   `json:"arguments"`
				Timeout   string   `json:"timeoutSeconds"`
			}
			if decodeAgentJSON([]byte(v.Arguments), &a) != nil || len(a.NodeIDs) == 0 || len(a.NodeIDs) > 200 || !validIDs(a.NodeIDs) {
				return "", ErrToolDenied
			}
			if tool.Name == "inspect_nodes" && len(a.NodeIDs) > 50 {
				return "", ErrToolDenied
			}
			for _, node := range a.NodeIDs {
				if !slices.Contains(tool.NodeIDs, node) {
					return "", ErrToolDenied
				}
			}
			params := map[string]string{}
			if tool.Profile {
				params = map[string]string{"profileId": a.ProfileID, "policySha256": a.PolicySHA, "arguments": a.Arguments, "timeoutSeconds": a.Timeout}
			} else if a.ProfileID != "" || a.PolicySHA != "" || a.Arguments != "" || a.Timeout != "" {
				return "", ErrToolDenied
			}
			b, _ := json.Marshal(params)
			out.Call = &AgentCall{tool.Action, slices.Clone(a.NodeIDs), b, v.CallID}
			b, _ = json.Marshal(map[string]string{"type": "function_call", "id": v.ID, "name": v.Name, "call_id": v.CallID, "arguments": v.Arguments, "status": "completed"})
			out.Items = append(out.Items, b)
		case "message":
			if v.Role != "assistant" || v.Status != "completed" {
				return "", ErrGenerationUnknown
			}
			for _, part := range v.Content {
				if part.Type != "output_text" || !utf8.ValidString(part.Text) || len(out.Text)+len(part.Text) > 24000 {
					return "", ErrGenerationUnknown
				}
				out.Text += part.Text
			}
		default:
			return "", ErrGenerationUnknown
		}
	}
	if out.Call == nil && strings.TrimSpace(out.Text) == "" && len(out.Items) == 0 {
		return "", ErrGenerationUnknown
	}
	b, e := json.Marshal(out)
	if e != nil || len(b) > 120000 {
		return "", ErrGenerationUnknown
	}
	return string(b), nil
}
