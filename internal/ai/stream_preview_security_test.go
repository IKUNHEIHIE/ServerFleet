package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestCredentialSplitAcrossProviderDeltasNeverReachesPreview(t *testing.T) {
	answer := strings.Repeat("safe content. ", 100) + testBYOK + " end"
	parts := []string{strings.Repeat("safe content. ", 100) + testBYOK[:8], testBYOK[8:] + " end"}
	provider, r, _, _ := providerFixture(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "event: response.created\ndata: {\"type\":\"response.created\",\"sequence_number\":0}\n\n")
		for i, part := range parts {
			raw, _ := json.Marshal(map[string]any{"type": "response.output_text.delta", "sequence_number": i + 1, "delta": part})
			_, _ = fmt.Fprintf(w, "event: response.output_text.delta\ndata: %s\n\n", raw)
		}
		_, _ = fmt.Fprintf(w, "event: response.completed\ndata: {\"type\":\"response.completed\",\"sequence_number\":3,\"response\":%s}\n\n", providerResponse(answer))
	})
	provider.stream = true
	var previews []string
	ctx := withGenerationPreview(context.Background(), func(_, text string) error { previews = append(previews, text); return nil })
	err := provider.Generate(ctx, r, func(string) error { t.Error("secret-bearing final response emitted"); return nil })
	if err == nil {
		t.Fatal("credential echo accepted")
	}
	if len(previews) == 0 {
		t.Fatal("test did not exercise incremental previews")
	}
	for _, text := range previews {
		if strings.Contains(text, testBYOK[:8]) {
			t.Fatal("split credential leaked")
		}
	}
}
