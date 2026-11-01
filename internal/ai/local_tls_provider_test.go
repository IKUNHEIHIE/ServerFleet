package ai

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"serverfleet.local/serverfleet/internal/auth"
)

func TestLocalTLSProviderInjectsRootsWithoutExternalEndpointOrRedirect(t *testing.T) {
	for _, mode := range []string{"success", "cancel", "revoked", "redirect"} {
		t.Run(mode, func(t *testing.T) {
			var calls atomic.Int32
			var revoked atomic.Bool
			p, r, vault, _ := providerFixture(t, func(w http.ResponseWriter, request *http.Request) {
				io.Copy(io.Discard, request.Body)
				calls.Add(1)
				if request.Header.Get("Authorization") != "Bearer "+testBYOK || request.TLS == nil {
					t.Error("wrong borrowed TLS credential")
				}
				if mode == "redirect" {
					w.Header().Set("Location", "https://example.invalid")
					w.WriteHeader(307)
					return
				}
				w.Header().Set("Content-Type", "text/event-stream")
				if mode == "cancel" {
					w.Write([]byte("event: response.created\ndata: {\"type\":\"response.created\",\"sequence_number\":0}\n\n"))
					w.(http.Flusher).Flush()
					select {
					case <-request.Context().Done():
					case <-time.After(time.Second):
						t.Error("provider did not observe request cancellation")
					}
					return
				}
				if mode == "revoked" {
					revoked.Store(true)
				}
				w.Write([]byte(streamFixture("verified local response")))
			}, func(context.Context, auth.CredentialOperation) error {
				if revoked.Load() {
					return ErrToolDenied
				}
				return nil
			})
			roots := p.client.Transport.(*http.Transport).TLSClientConfig.RootCAs
			cfg := LocalTLSProviderConfig{Origin: strings.TrimSuffix(p.endpoint, "/v1/responses"), Roots: roots}
			local, err := NewLocalTLSOpenAITextProvider(vault, p.resolve, cfg)
			if err != nil {
				t.Fatal(err)
			}
			defer local.Close()
			for _, origin := range []string{"http://localhost:1234", "https://example.invalid:443", "https://localhost:1234/path", "https://user@localhost:1234", "https://localhost:1234?bad"} {
				bad := cfg
				bad.Origin = origin
				if _, err = NewLocalTLSOpenAITextProvider(vault, p.resolve, bad); err == nil {
					t.Fatal("unsafe local provider origin")
				}
			}
			ctx := context.Background()
			var cancel context.CancelFunc
			if mode == "cancel" {
				ctx, cancel = context.WithTimeout(ctx, 100*time.Millisecond)
				defer cancel()
			}
			output := ""
			err = local.Generate(ctx, r, func(text string) error { output += text; return nil })
			if mode == "success" {
				if err != nil || output != "verified local response" {
					t.Fatal(err)
				}
			} else if err == nil || output != "" {
				t.Fatal("partial/revoked response disclosed")
			}
			if calls.Load() != 1 {
				t.Fatal("provider request replayed")
			}
		})
	}
}
