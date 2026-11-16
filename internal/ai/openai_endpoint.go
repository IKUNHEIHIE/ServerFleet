package ai

import (
	"context"
	"net/url"
	"strings"

	"serverfleet.local/serverfleet/internal/auth"
)

// OpenAICompatibleEndpoint is a deployment-level OpenAI Responses endpoint
// (for example an OpenAI-compatible MaaS gateway). It is never supplied by a
// browser request or model profile, so account owners cannot redirect secrets.
type OpenAICompatibleEndpoint struct {
	URL string
}

func NewOpenAIEndpointStreamingTextProvider(credentials *auth.CredentialService, resolve CredentialResolver, endpoint OpenAICompatibleEndpoint) (*OpenAITextProvider, error) {
	value := strings.TrimSpace(endpoint.URL)
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil ||
		parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" || parsed.Opaque != "" ||
		!strings.HasSuffix(parsed.Path, "/responses") {
		return nil, ErrModelUnavailable
	}
	provider, err := NewOpenAIStreamingTextProvider(credentials, resolve)
	if err != nil {
		return nil, err
	}
	provider.endpoint = value
	return provider, nil
}

func NewOpenAIEndpointBYOKRuntime(sessions *SessionService, credentials *auth.CredentialService, filter func(context.Context, SessionView, string, string) (string, error), quota GenerationQuota, endpoint OpenAICompatibleEndpoint) (*BYOKRuntime, error) {
	return newBYOKRuntime(sessions, credentials, filter, quota, func(v *auth.CredentialService, r CredentialResolver) (*OpenAITextProvider, error) {
		return NewOpenAIEndpointStreamingTextProvider(v, r, endpoint)
	})
}