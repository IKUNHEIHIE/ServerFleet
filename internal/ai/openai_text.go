package ai

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"io"
	"mime"
	"net"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"serverfleet.local/serverfleet/internal/auth"
)

// CredentialResolver must check the CURRENT enabled owner profile and durable
// generating request. The credential service's independent gate must verify the
// same operation. No request-supplied operation, model default, key environment
// fallback, general decrypt API, arbitrary base URL or provider tool execution.
type CredentialResolver func(context.Context, GenerationRequest, string) (auth.CredentialOperation, error)

type OpenAITextProvider struct {
	credentials *auth.CredentialService
	resolve     CredentialResolver
	client      *http.Client
	endpoint    string
	stream      bool
}

func NewOpenAITextProvider(credentials *auth.CredentialService, resolve CredentialResolver) (*OpenAITextProvider, error) {
	if credentials == nil || resolve == nil {
		return nil, ErrModelUnavailable
	}
	transport := &http.Transport{
		Proxy:               nil,
		DialContext:         (&net.Dialer{Timeout: 5 * time.Second}).DialContext,
		TLSClientConfig:     &tls.Config{MinVersion: tls.VersionTLS13},
		TLSHandshakeTimeout: 5 * time.Second, ResponseHeaderTimeout: 15 * time.Second,
		MaxResponseHeaderBytes: 32 << 10, MaxConnsPerHost: 4, DisableKeepAlives: true,
		// One fresh HTTP/1 connection per operation prevents transport replay of
		// a POST on a pooled connection. No SDK retry policy is involved.
		TLSNextProto: map[string]func(string, *tls.Conn) http.RoundTripper{},
	}
	return &OpenAITextProvider{credentials: credentials, resolve: resolve, client: &http.Client{Transport: transport, Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, endpoint: "https://api.openai.com/v1/responses"}, nil
}

// NewOpenAIStreamingTextProvider consumes actual upstream SSE. Deltas remain
// private until terminal-response, secret and post-borrow checks succeed; partial
// upstream text is never disclosed on a revoked/failed/unknown operation.
func NewOpenAIStreamingTextProvider(credentials *auth.CredentialService, resolve CredentialResolver) (*OpenAITextProvider, error) {
	p, err := NewOpenAITextProvider(credentials, resolve)
	if err == nil {
		p.stream = true
	}
	return p, err
}
func (p *OpenAITextProvider) Close() {
	if p != nil && p.client != nil {
		p.client.CloseIdleConnections()
	}
}

func generationDigest(r GenerationRequest) (string, error) {
	if !exactID(r.SubjectID) || !exactID(r.SessionID) || !exactID(r.MessageID) || r.OperationID != "ai-generation-"+r.MessageID || len(r.OperationID) > 128 || r.ScopeRevision == 0 || !validModelProfile(r.Profile) || len(r.Messages) == 0 || len(r.Messages) > 64 {
		return "", ErrToolDenied
	}
	size := 0
	for i, m := range r.Messages {
		if m.SessionID != r.SessionID || !exactID(m.MessageID) || m.CreatedAt.IsZero() || !validMessageContent(m.RedactedContent) || len(m.TaskIDs) != 0 || i%2 == 0 && m.Role != "user" || i%2 == 1 && m.Role != "assistant" {
			return "", ErrToolDenied
		}
		size += len(m.RedactedContent)
	}
	last := r.Messages[len(r.Messages)-1]
	if len(r.Messages)%2 != 1 || last.Role != "user" || last.MessageID != r.MessageID || size > 256000 {
		return "", ErrToolDenied
	}
	if r.Agent != nil {
		b, e := json.Marshal(r.Agent)
		if e != nil || validateAgentPrompt(r.Agent) != nil || size+len(b) > 256000 {
			return "", ErrToolDenied
		}
	}
	raw, err := json.Marshal(r)
	if err != nil {
		return "", ErrToolDenied
	}
	defer clear(raw)
	h := sha256.Sum256(append([]byte("serverfleet.ai.generation.v1\x00"), raw...))
	return hex.EncodeToString(h[:]), nil
}

func validateProviderJSON(d *json.Decoder, depth int, budget *int) error {
	*budget--
	if depth > 16 || *budget < 0 {
		return ErrGenerationUnknown
	}
	token, err := d.Token()
	if err != nil {
		return ErrGenerationUnknown
	}
	if delim, ok := token.(json.Delim); ok {
		switch delim {
		case '{':
			seen := map[string]bool{}
			for d.More() {
				token, err = d.Token()
				key, ok := token.(string)
				if err != nil || !ok || seen[strings.ToLower(key)] {
					return ErrGenerationUnknown
				}
				switch strings.ToLower(key) {
				case "id", "object", "status", "model", "store", "error", "incomplete_details", "output", "type", "role", "phase", "content", "text", "sequence_number", "delta", "response":
					if key != strings.ToLower(key) {
						return ErrGenerationUnknown
					}
				}
				seen[strings.ToLower(key)] = true
				if err = validateProviderJSON(d, depth+1, budget); err != nil {
					return err
				}
			}
			token, err = d.Token()
			if err != nil || token != json.Delim('}') {
				return ErrGenerationUnknown
			}
		case '[':
			for d.More() {
				if err = validateProviderJSON(d, depth+1, budget); err != nil {
					return err
				}
			}
			token, err = d.Token()
			if err != nil || token != json.Delim(']') {
				return ErrGenerationUnknown
			}
		default:
			return ErrGenerationUnknown
		}
	}
	return nil
}

func decodeProviderText(raw []byte, model string) (string, error) {
	if len(raw) == 0 || len(raw) > 1<<20 || !utf8.Valid(raw) {
		return "", ErrGenerationUnknown
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	budget := 100000
	if validateProviderJSON(d, 0, &budget) != nil {
		return "", ErrGenerationUnknown
	}
	if _, err := d.Token(); err != io.EOF {
		return "", ErrGenerationUnknown
	}
	var result struct {
		ID, Object, Status, Model string
		Store                     *bool
		Output                    []struct {
			Type, Role, Status, Phase string
			Content                   []struct{ Type, Text string }
		}
	}
	// Unknown upstream metadata is additive, but ambiguous field aliases are
	// rejected above. Check critical field spelling explicitly before decoding.
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return "", ErrGenerationUnknown
	}
	for key := range fields {
		switch strings.ToLower(key) {
		case "id", "object", "status", "model", "store", "error", "incomplete_details", "output":
			if key != strings.ToLower(key) {
				return "", ErrGenerationUnknown
			}
		}
	}
	if json.Unmarshal(raw, &result) != nil || result.ID == "" || len(result.ID) > 256 || result.Object != "response" || result.Status != "completed" || result.Model != model || result.Store == nil || *result.Store || len(result.Output) == 0 || len(result.Output) > 64 {
		return "", ErrGenerationUnknown
	}
	for _, key := range []string{"error", "incomplete_details"} {
		if value := fields[key]; len(value) > 0 && !bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return "", ErrGenerationUnknown
		}
	}
	var output strings.Builder
	for _, item := range result.Output {
		if item.Type == "reasoning" {
			continue
		} // Never disclose chain-of-thought.
		if item.Type != "message" || item.Role != "assistant" || item.Status != "completed" || item.Phase != "" && item.Phase != "final_answer" || len(item.Content) == 0 || len(item.Content) > 64 {
			return "", ErrGenerationUnknown
		}
		for _, part := range item.Content {
			if part.Type != "output_text" || !utf8.ValidString(part.Text) || output.Len()+len(part.Text) > 128000 {
				return "", ErrGenerationUnknown
			}
			output.WriteString(part.Text)
		}
	}
	text := output.String()
	if !validMessageContent(text) {
		return "", ErrGenerationUnknown
	}
	return text, nil
}

// Generate borrows BYOK through the persistent credential use fence. Provider
// I/O returns ordinary text or a privately bound typed Agent proposal. Output
// is emitted ONCE after post-use authorization and credential/audit checks.
// Model output never grants execution.
func (p *OpenAITextProvider) Generate(ctx context.Context, r GenerationRequest, emit func(string) error) (result error) {
	if p == nil || p.credentials == nil || p.resolve == nil || p.client == nil || ctx == nil || ctx.Err() != nil || emit == nil {
		return ErrModelUnavailable
	}
	defer func() {
		if recover() != nil {
			result = ErrGenerationUnknown
		}
	}()
	r.Messages = append([]Message{}, r.Messages...)
	digest, err := generationDigest(r)
	if err != nil {
		return err
	}
	bounded, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	resolverRequest := r
	resolverRequest.Messages = append([]Message{}, r.Messages...)
	op, err := p.resolve(bounded, resolverRequest, digest)
	if err != nil || op.SubjectID != r.SubjectID || op.CredentialID != r.Profile.CredentialRef || op.OperationID != r.OperationID || op.Module != "ai" || op.Source != "ai" || op.Action != "use" || op.ChangeID != "" || op.ExpectedRevision == 0 || op.PlanDigest != digest {
		return ErrToolDenied
	}
	input := []map[string]string{}
	for _, m := range r.Messages {
		item := map[string]string{"type": "message", "role": m.Role, "content": m.RedactedContent}
		if m.Role == "assistant" {
			item["phase"] = "final_answer"
		}
		input = append(input, item)
	}
	stream := p.stream && r.Agent == nil
	payload := map[string]any{"model": r.Profile.Model, "input": input, "store": false, "stream": stream, "background": false, "truncation": "disabled", "tools": []any{}, "tool_choice": "none", "parallel_tool_calls": false, "max_output_tokens": ProviderMaxOutputTokens}
	if r.Agent != nil {
		if err := agentProviderPayload(payload, r.Agent); err != nil {
			return err
		}
	}
	body, err := json.Marshal(payload)
	if err != nil || len(body) > 512<<10 {
		return ErrToolDenied
	}
	defer clear(body)
	var text string
	err = p.credentials.UseForService(bounded, op, func(ctx context.Context, secret []byte) error {
		if len(secret) < 8 || len(secret) > 4096 || bytes.ContainsAny(secret, " \t\r\n\x00") {
			return ErrGenerationUnknown
		}
		for _, c := range secret {
			if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("-._~+/=", rune(c))) {
				return ErrGenerationUnknown
			}
		}
		request, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endpoint, bytes.NewReader(body))
		if err != nil {
			return ErrGenerationUnknown
		}
		request.Header.Set("Authorization", "Bearer "+string(secret))
		defer request.Header.Del("Authorization")
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Accept", "application/json")
		if stream {
			request.Header.Set("Accept", "text/event-stream")
		}
		response, err := p.client.Do(request)
		if err != nil {
			return ErrGenerationUnknown
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusOK {
			return ErrGenerationUnknown
		}
		media, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
		if err != nil || (!stream && media != "application/json") || (stream && media != "text/event-stream") || response.ContentLength > 1<<20 {
			return ErrGenerationUnknown
		}
		if stream {
			text, err = decodeProviderStream(ctx, response.Body, r.Profile.Model)
		} else {
			raw, readErr := io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
			defer clear(raw)
			if readErr != nil {
				return ErrGenerationUnknown
			}
			if r.Agent != nil {
				text, err = decodeAgentDecision(raw, r.Profile.Model, r.Agent)
			} else {
				text, err = decodeProviderText(raw, r.Profile.Model)
			}
		}
		if err != nil || strings.Contains(text, string(secret)) {
			text = ""
			return ErrGenerationUnknown
		}
		return nil
	})
	if err != nil || bounded.Err() != nil || text == "" {
		return ErrGenerationUnknown
	}
	if emit(text) != nil || bounded.Err() != nil {
		return ErrGenerationUnknown
	}
	return nil
}
