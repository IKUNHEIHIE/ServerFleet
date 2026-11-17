package ai

import "encoding/json"

// Only explicit plaintext API output is displayable. Opaque encrypted state is
// retained separately for model continuation and is never decoded for the UI.
func responseReasoning(raw []byte) (string, string) {
	var response struct {
		Output []struct {
			Type             string
			Content, Summary []struct{ Type, Text string }
		}
	}
	if json.Unmarshal(raw, &response) != nil {
		return "", ""
	}
	var reasoning, summary string
	for _, item := range response.Output {
		if item.Type != "reasoning" {
			continue
		}
		for _, part := range item.Content {
			if part.Type == "reasoning_text" && len(reasoning)+len(part.Text) <= 64<<10 {
				reasoning += part.Text
			}
		}
		for _, part := range item.Summary {
			if part.Type == "summary_text" && len(summary)+len(part.Text) <= 8192 {
				summary += part.Text
			}
		}
	}
	return reasoning, summary
}
