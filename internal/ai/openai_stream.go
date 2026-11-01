package ai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
)

// Only text response events are supported. Unknown/tool/refusal/error events
// reject the entire result. Event, total-byte, output and sequence bounds apply
// independently. A disconnected stream without response.completed is UNKNOWN.
func decodeProviderStream(ctx context.Context, reader io.Reader, model string) (string, error) {
	limited := &io.LimitedReader{R: reader, N: (1 << 20) + 1}
	scanner := bufio.NewScanner(limited)
	scanner.Buffer(make([]byte, 4096), 256<<10)
	var data, delta strings.Builder
	eventName, completed, answer := "", false, ""
	sequence, events := int64(-1), 0
	process := func() error {
		if data.Len() == 0 {
			eventName = ""
			return nil
		}
		if completed || ctx.Err() != nil || events >= 2048 {
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
			Type     string          `json:"type"`
			Sequence *int64          `json:"sequence_number"`
			Delta    string          `json:"delta"`
			Response json.RawMessage `json:"response"`
			Item     struct {
				Type string `json:"type"`
			} `json:"item"`
		}
		if json.Unmarshal(raw, &e) != nil || e.Sequence == nil || *e.Sequence != sequence+1 || eventName != "" && eventName != e.Type {
			return ErrGenerationUnknown
		}
		sequence = *e.Sequence
		switch e.Type {
		case "response.created", "response.in_progress", "response.content_part.added", "response.content_part.done", "response.output_text.done":
		case "response.output_item.added", "response.output_item.done":
			if e.Item.Type != "message" && e.Item.Type != "reasoning" {
				return ErrGenerationUnknown
			}
		case "response.output_text.delta":
			if delta.Len()+len(e.Delta) > 128000 {
				return ErrGenerationUnknown
			}
			delta.WriteString(e.Delta)
		case "response.completed":
			var err error
			answer, err = decodeProviderText(e.Response, model)
			if err != nil || answer != delta.String() {
				return ErrGenerationUnknown
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
