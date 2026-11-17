package ai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
)

// Responses streams expose answer text, API-readable reasoning and function
// proposals independently. A disconnected stream without completion is UNKNOWN.
func decodeProviderStream(ctx context.Context, reader io.Reader, model string) (string, error) {
	return decodeProviderStreamObserved(ctx, reader, model, nil)
}
func decodeProviderStreamObserved(ctx context.Context, reader io.Reader, model string, observe generationPreview) (string, error) {
	return decodeResponseStreamObserved(ctx, reader, model, nil, observe)
}
func decodeResponseStreamObserved(ctx context.Context, reader io.Reader, model string, agent *agentPrompt, observe generationPreview) (string, error) {
	limited := &io.LimitedReader{R: reader, N: (1 << 20) + 1}
	scanner := bufio.NewScanner(limited)
	scanner.Buffer(make([]byte, 4096), 256<<10)
	var data, delta, summary, reasoning strings.Builder
	arguments := map[int64]int{}
	eventName, completed, answer := "", false, ""
	sequence, events := int64(-1), 0
	process := func() error {
		if data.Len() == 0 {
			eventName = ""
			return nil
		}
		if strings.TrimSpace(data.String()) == "[DONE]" {
			if !completed {
				return ErrGenerationUnknown
			}
			data.Reset()
			eventName = ""
			return nil
		}
		if completed || ctx.Err() != nil || events >= 8192 {
			return ErrGenerationUnknown
		}
		events++
		raw := []byte(data.String())
		defer clear(raw)
		d := json.NewDecoder(bytes.NewReader(raw))
		d.UseNumber()
		budget := 10000
		if validateProviderJSON(d, 0, &budget) != nil {
			return ErrGenerationUnknown
		}
		if _, err := d.Token(); err != io.EOF {
			return ErrGenerationUnknown
		}
		var e struct {
			Type        string          `json:"type"`
			Sequence    *int64          `json:"sequence_number"`
			Delta       string          `json:"delta"`
			Response    json.RawMessage `json:"response"`
			OutputIndex int64           `json:"output_index"`
			Item        struct {
				Type string `json:"type"`
			} `json:"item"`
		}
		if json.Unmarshal(raw, &e) != nil || e.Sequence != nil && *e.Sequence != sequence+1 || eventName != "" && eventName != e.Type {
			return ErrGenerationUnknown
		}
		if e.Sequence != nil {
			sequence = *e.Sequence
		}
		switch e.Type {
		case "response.created", "response.in_progress", "response.content_part.added", "response.content_part.done", "response.output_text.done":
		case "response.reasoning_summary_text.delta":
			if summary.Len()+len(e.Delta) > 8192 {
				return ErrGenerationUnknown
			}
			summary.WriteString(e.Delta)
			if observe != nil {
				if err := observe("summary", summary.String()); err != nil {
					return err
				}
			}
		case "response.reasoning_text.delta":
			if reasoning.Len()+len(e.Delta) > 64<<10 {
				return ErrGenerationUnknown
			}
			reasoning.WriteString(e.Delta)
			if observe != nil {
				if err := observe("reasoning", reasoning.String()); err != nil {
					return err
				}
			}
		case "response.reasoning_text.done", "response.reasoning_summary_text.done",
			"response.reasoning_summary_part.added", "response.reasoning_summary_part.done",
			"response.reasoning_part.added", "response.reasoning_part.done":
		case "response.function_call_arguments.delta", "response.function_call_arguments.done":
			if agent == nil {
				return ErrGenerationUnknown
			}
			arguments[e.OutputIndex] += len(e.Delta)
			if arguments[e.OutputIndex] > 8<<10 || len(arguments) > 8 {
				return ErrGenerationUnknown
			}
		case "response.output_item.added", "response.output_item.done":
			if e.Item.Type != "message" && e.Item.Type != "reasoning" && (agent == nil || e.Item.Type != "function_call") {
				return ErrGenerationUnknown
			}
		case "response.output_text.delta":
			if delta.Len()+len(e.Delta) > 128000 {
				return ErrGenerationUnknown
			}
			delta.WriteString(e.Delta)
			if observe != nil {
				if err := observe("text", delta.String()); err != nil {
					return err
				}
			}
		case "response.completed":
			var err error
			visible := ""
			if agent != nil {
				answer, err = decodeAgentDecision(e.Response, model, agent)
				if err == nil {
					var decision agentDecision
					if json.Unmarshal([]byte(answer), &decision) != nil {
						return ErrGenerationUnknown
					}
					visible = decision.Text
					if reasoning.Len() > 0 {
						decision.Reasoning = reasoning.String()
					}
					if summary.Len() > 0 {
						decision.Summary = summary.String()
					}
					encoded, _ := json.Marshal(decision)
					answer = string(encoded)
				}
			} else {
				answer, err = decodeProviderText(e.Response, model)
				visible = answer
			}
			if err != nil || visible != delta.String() {
				return ErrGenerationUnknown
			}
			if observe != nil {
				if summary.Len() > 0 {
					if err := observe("summary_completed", summary.String()); err != nil {
						return err
					}
				}
				if reasoning.Len() > 0 {
					if err := observe("reasoning_completed", reasoning.String()); err != nil {
						return err
					}
				}
			}
			completed = true
		default:
			return ErrGenerationUnknown
		}
		data.Reset()
		eventName = ""
		return nil
	}
	for scanner.Scan() {
		line := scanner.Text()
		if ctx.Err() != nil || limited.N <= 0 {
			return "", ErrGenerationUnknown
		}
		if line == "" {
			if process() != nil {
				return "", ErrGenerationUnknown
			}
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		field, value, _ := strings.Cut(line, ":")
		value = strings.TrimPrefix(value, " ")
		switch field {
		case "event":
			if eventName != "" {
				return "", ErrGenerationUnknown
			}
			eventName = value
		case "data":
			if data.Len()+len(value) > 256<<10 {
				return "", ErrGenerationUnknown
			}
			if data.Len() > 0 {
				data.WriteByte('\n')
			}
			data.WriteString(value)
		default:
			return "", ErrGenerationUnknown
		}
	}
	if scanner.Err() != nil || limited.N <= 0 || data.Len() != 0 || !completed || ctx.Err() != nil {
		return "", ErrGenerationUnknown
	}
	return answer, nil
}
