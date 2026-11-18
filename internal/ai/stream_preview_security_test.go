package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
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

func TestProviderSmallDeltasArriveBeforeCompletion(t *testing.T) {
	first, second := make(chan struct{}), make(chan struct{})
	provider, r, _, _ := providerFixture(t, func(w http.ResponseWriter, request *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for i, part := range []string{"开", "始"} {
			_, _ = fmt.Fprintf(w, "event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"sequence_number\":%d,\"delta\":%q}\n\n", i, part)
			w.(http.Flusher).Flush()
			seen := first
			if i == 1 {
				seen = second
			}
			select {
			case <-seen:
			case <-request.Context().Done():
				return
			}
			time.Sleep(35 * time.Millisecond)
		}
		_, _ = fmt.Fprintf(w, "event: response.completed\ndata: {\"type\":\"response.completed\",\"sequence_number\":2,\"response\":%s}\n\n", providerResponse("开始"))
	})
	provider.stream = true
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	ctx = withGenerationPreview(ctx, func(kind, value string) error {
		switch value {
		case "开":
			close(first)
		case "开始":
			close(second)
		}
		return nil
	})
	var answer string
	if err := provider.Generate(ctx, r, func(value string) error { answer += value; return nil }); err != nil || answer != "开始" {
		t.Fatalf("small deltas did not arrive during generation: %q %v", answer, err)
	}
}
