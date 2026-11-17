package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"serverfleet.local/serverfleet/internal/auth"
	"strings"
	"time"
)

type ModelCapabilities struct {
	Model     string    `json:"model"`
	ToolCalls bool      `json:"toolCalls"`
	Reasoning bool      `json:"reasoning"`
	CheckedAt time.Time `json:"checkedAt"`
}

func (r *BYOKRuntime) ProbeModel(ctx context.Context, op auth.CredentialOperation, endpoint, model string) (ModelCapabilities, error) {
	result := ModelCapabilities{Model: model, CheckedAt: time.Now().UTC()}
	if !exactID(model) || len(model) > 128 || strings.HasPrefix(model, "sk-") || r == nil || r.provider == nil {
		return result, ErrModelUnavailable
	}
	url, err := ModelResponsesURL(endpoint)
	if endpoint == "" {
		url = r.provider.endpoint
		err = nil
	}
	if err != nil {
		return result, err
	}
	bounded, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	p := r.provider
	err = p.credentials.UseForService(bounded, op, func(ctx context.Context, secret []byte) error {
		probe := func(reasoning bool) bool {
			body := map[string]any{"model": model, "store": false, "stream": false, "input": "Capability check only. Call serverfleet_capability_probe with ok=true. Do not run commands.", "max_output_tokens": 256, "parallel_tool_calls": false, "background": false, "truncation": "disabled", "include": []string{"reasoning.encrypted_content"}}
			if reasoning {
				body["input"] = "Reply OK."
				body["reasoning"] = map[string]any{"effort": "low", "summary": "auto"}
			} else {
				body["tools"] = []any{map[string]any{"type": "function", "name": "serverfleet_capability_probe", "description": "Protocol check, no external action", "parameters": map[string]any{"type": "object", "properties": map[string]any{"ok": map[string]string{"type": "boolean"}}, "required": []string{"ok"}, "additionalProperties": false}, "strict": true}}
				body["tool_choice"] = map[string]string{"type": "function", "name": "serverfleet_capability_probe"}
			}
			raw, _ := json.Marshal(body)
			defer clear(raw)
			req, e := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(raw))
			if e != nil {
				return false
			}
			req.Header.Set("Authorization", "Bearer "+string(secret))
			req.Header.Set("Content-Type", "application/json")
			response, e := p.client.Do(req)
			if e != nil {
				return false
			}
			defer response.Body.Close()
			if response.StatusCode != 200 {
				return false
			}
			data, e := io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
			defer clear(data)
			if e != nil || len(data) > 1<<20 || bytes.Contains(data, secret) {
				return false
			}
			var value struct {
				ID, Object, Status, Model string
				Store                     *bool
				Error                     json.RawMessage
				Output                    []struct {
					Type, Name, Arguments string
					CallID                string `json:"call_id"`
					Summary               []struct{ Type, Text string }
				}
			}
			if json.Unmarshal(data, &value) != nil || value.ID == "" || value.Object != "response" || value.Model != model || value.Status != "" && value.Status != "completed" || value.Store != nil && *value.Store || len(value.Output) > 32 || len(value.Error) > 0 && string(value.Error) != "null" {
				return false
			}
			functionCalls, validFunction := 0, false
			for _, item := range value.Output {
				if reasoning && item.Type == "reasoning" {
					for _, part := range item.Summary {
						if part.Type == "summary_text" && strings.TrimSpace(part.Text) != "" {
							return true
						}
					}
				}
				if item.Type == "function_call" {
					functionCalls++
				}
				if !reasoning && item.Type == "function_call" && item.Name == "serverfleet_capability_probe" && exactID(item.CallID) {
					var args struct {
						OK bool `json:"ok"`
					}
					if decodeAgentJSON([]byte(item.Arguments), &args) == nil && args.OK {
						validFunction = true
					}
				}
			}
			return !reasoning && functionCalls == 1 && validFunction
		}
		result.ToolCalls = probe(false)
		result.Reasoning = probe(true)
		return nil
	})
	return result, err
}
