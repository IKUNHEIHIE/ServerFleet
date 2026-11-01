package notifications

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	DefaultQQTokenURL   = "https://bots.qq.com/app/getAppAccessToken"
	DefaultQQAPIBaseURL = "https://api.sgroup.qq.com"
	maxQQBodyBytes      = 64 << 10
	maxQQContentBytes   = 2000
)

var (
	ErrQQConfig    = errors.New("notifications: qq bot configuration is invalid")
	ErrQQSecret    = errors.New("notifications: qq bot secret is invalid")
	ErrQQMessage   = errors.New("notifications: qq bot message is invalid or too large")
	ErrQQTransport = errors.New("notifications: qq bot request failed")
)

// QQConfig configures the real QQ Bot OpenAPI adapter. Endpoints are injectable
// for a local TLS test server and must be https in both cases.
type QQConfig struct {
	Client     *http.Client
	TokenURL   string
	APIBaseURL string
	Secrets    SecretBorrower
	Timeout    time.Duration
}

// QQBotSender implements QQSender. A token is fetched for each send; no token or
// app secret is cached on the struct.
type QQBotSender struct {
	client     *http.Client
	tokenURL   string
	apiBaseURL string
	secrets    SecretBorrower
}

var _ QQSender = (*QQBotSender)(nil)

func NewQQBotSender(config QQConfig) (*QQBotSender, error) {
	if config.Secrets == nil {
		return nil, ErrQQConfig
	}
	tokenURL := config.TokenURL
	if tokenURL == "" {
		tokenURL = DefaultQQTokenURL
	}
	apiBase := config.APIBaseURL
	if apiBase == "" {
		apiBase = DefaultQQAPIBaseURL
	}
	for _, endpoint := range []string{tokenURL, apiBase} {
		parsed, err := url.Parse(endpoint)
		if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil ||
			parsed.RawQuery != "" || parsed.Fragment != "" {
			return nil, fmt.Errorf("%w: endpoint", ErrQQConfig)
		}
	}
	client := config.Client
	if client == nil {
		timeout := config.Timeout
		if timeout <= 0 {
			timeout = 20 * time.Second
		}
		client = &http.Client{
			Timeout: timeout,
			Transport: &http.Transport{
				Proxy:                 nil,
				TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12},
				TLSHandshakeTimeout:   10 * time.Second,
				ResponseHeaderTimeout: 15 * time.Second,
				MaxIdleConns:          4,
				IdleConnTimeout:       30 * time.Second,
			},
		}
	}
	cloned := *client
	cloned.CheckRedirect = func(*http.Request, []*http.Request) error {
		return errors.New("notifications: qq redirect is not allowed")
	}
	return &QQBotSender{
		client: &cloned, tokenURL: strings.TrimSuffix(tokenURL, "/"),
		apiBaseURL: strings.TrimSuffix(apiBase, "/"), secrets: config.Secrets,
	}, nil
}

type qqTokenResponse struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"`
	Code        int    `json:"code"`
	Message     string `json:"message"`
}

type qqMessageResponse struct {
	ID        string `json:"id"`
	Timestamp string `json:"timestamp"`
	Code      int    `json:"code"`
	Message   string `json:"message"`
}

// Send delivers one text message through the QQ Bot OpenAPI. Token fetch and
// message send both run inside the borrow callback. A failed send after the
// request reached the API is delivery_unknown and is never retried automatically.
func (s *QQBotSender) Send(ctx context.Context, settings QQSettings, secretRef string, message Message) (ProviderResult, error) {
	if s == nil || s.secrets == nil {
		return ProviderResult{}, ErrQQConfig
	}
	if err := validateQQSettings(settings); err != nil {
		return ProviderResult{Outcome: ProviderRejected, Code: "invalid_config"}, err
	}
	if err := validateMessage(message); err != nil {
		return ProviderResult{Outcome: ProviderRejected, Code: "invalid_message"}, err
	}
	content, err := qqContent(message)
	if err != nil {
		return ProviderResult{Outcome: ProviderRejected, Code: "message_too_large"}, err
	}
	use := SecretUse{Ref: secretRef, Operation: "qqbot.send"}
	scope := secretScopeFrom(ctx)
	use.SubjectID, use.RequestID, use.PlanDigest = scope.SubjectID, scope.RequestID, scope.PlanDigest
	ran := false
	var result ProviderResult
	var sendErr error
	err = s.secrets.WithSecret(ctx, use, func(borrowCtx context.Context, secret string) error {
		ran = true
		result, sendErr = s.sendWithSecret(borrowCtx, settings, secret, content)
		return sendErr
	})
	if err != nil && !ran {
		return ProviderResult{}, ErrQQSecret
	}
	if err != nil {
		if sendErr == nil {
			result = ProviderResult{Outcome: ProviderUnknown, Code: "qq_use_unknown"}
		}
		return result, err
	}
	return result, sendErr
}

func (s *QQBotSender) sendWithSecret(ctx context.Context, settings QQSettings, clientSecret, content string) (ProviderResult, error) {
	if strings.TrimSpace(clientSecret) == "" || len(clientSecret) > 4096 ||
		strings.ContainsAny(clientSecret, "\x00\r\n") {
		return ProviderResult{}, ErrQQSecret
	}
	token, result, err := s.fetchToken(ctx, settings.AppID, clientSecret)
	if err != nil {
		return result, err
	}
	path := "/v2/users/" + url.PathEscape(settings.TargetID) + "/messages"
	if settings.TargetType == QQTargetGroup {
		path = "/v2/groups/" + url.PathEscape(settings.TargetID) + "/messages"
	}
	body, err := json.Marshal(map[string]any{
		"content": content, "msg_type": 0, "msg_seq": 1,
	})
	if err != nil {
		return ProviderResult{Outcome: ProviderRejected, Code: "invalid_message"}, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, s.apiBaseURL+path, bytes.NewReader(body))
	if err != nil {
		return ProviderResult{}, fmt.Errorf("%w: request", ErrQQConfig)
	}
	request.Header.Set("Authorization", "QQBot "+token)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	response, err := s.client.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return ProviderResult{Outcome: ProviderUnknown, Code: "qq_unknown"}, ctx.Err()
		}
		return ProviderResult{Outcome: ProviderUnknown, Code: "qq_unknown"}, fmt.Errorf("%w: send", ErrQQTransport)
	}
	defer response.Body.Close()
	payload, readErr := io.ReadAll(io.LimitReader(response.Body, maxQQBodyBytes+1))
	if readErr != nil || len(payload) > maxQQBodyBytes {
		return ProviderResult{Outcome: ProviderUnknown, Code: "qq_unknown"}, fmt.Errorf("%w: send body", ErrQQTransport)
	}
	var decoded qqMessageResponse
	_ = json.Unmarshal(payload, &decoded)
	switch response.StatusCode {
	case http.StatusOK, http.StatusCreated, http.StatusNoContent:
		return ProviderResult{Outcome: ProviderAccepted, Code: "qq_200"}, nil
	case http.StatusUnauthorized:
		return ProviderResult{Outcome: ProviderRejected, Code: "qq_401", CredentialInvalid: true},
			fmt.Errorf("%w: unauthorized", ErrQQSecret)
	case http.StatusForbidden:
		return ProviderResult{Outcome: ProviderRejected, Code: "qq_403", CredentialInvalid: true},
			fmt.Errorf("%w: forbidden", ErrQQSecret)
	case http.StatusTooManyRequests:
		return ProviderResult{Outcome: ProviderRateLimited, Code: "qq_429"}, fmt.Errorf("%w: rate limited", ErrQQTransport)
	}
	if response.StatusCode >= 500 {
		return ProviderResult{Outcome: ProviderUnknown, Code: "qq_unknown"}, fmt.Errorf("%w: server error", ErrQQTransport)
	}
	if decoded.Code == 401 || decoded.Code == 403 {
		return ProviderResult{Outcome: ProviderRejected, Code: "qq_auth", CredentialInvalid: true}, fmt.Errorf("%w: unauthorized", ErrQQSecret)
	}
	return ProviderResult{Outcome: ProviderRejected, Code: "qq_rejected"}, fmt.Errorf("%w: rejected", ErrQQTransport)
}

func (s *QQBotSender) fetchToken(ctx context.Context, appID, clientSecret string) (string, ProviderResult, error) {
	body, err := json.Marshal(map[string]string{"appId": appID, "clientSecret": clientSecret})
	if err != nil {
		return "", ProviderResult{}, ErrQQConfig
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, s.tokenURL, bytes.NewReader(body))
	if err != nil {
		return "", ProviderResult{}, fmt.Errorf("%w: token request", ErrQQConfig)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	response, err := s.client.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return "", ProviderResult{}, ctx.Err()
		}
		return "", ProviderResult{}, fmt.Errorf("%w: token", ErrQQTransport)
	}
	defer response.Body.Close()
	payload, readErr := io.ReadAll(io.LimitReader(response.Body, maxQQBodyBytes+1))
	if readErr != nil || len(payload) > maxQQBodyBytes {
		return "", ProviderResult{}, fmt.Errorf("%w: token body", ErrQQTransport)
	}
	var decoded qqTokenResponse
	_ = json.Unmarshal(payload, &decoded)
	switch response.StatusCode {
	case http.StatusOK:
	case http.StatusUnauthorized, http.StatusForbidden:
		return "", ProviderResult{Outcome: ProviderRejected, Code: "qq_token_auth", CredentialInvalid: true},
			fmt.Errorf("%w: token unauthorized", ErrQQSecret)
	case http.StatusTooManyRequests:
		return "", ProviderResult{Outcome: ProviderRateLimited, Code: "qq_token_429"}, fmt.Errorf("%w: token rate limited", ErrQQTransport)
	default:
		return "", ProviderResult{}, fmt.Errorf("%w: token status %d", ErrQQTransport, response.StatusCode)
	}
	if decoded.AccessToken == "" {
		if decoded.Code == 401 || decoded.Code == 403 {
			return "", ProviderResult{Outcome: ProviderRejected, Code: "qq_token_auth", CredentialInvalid: true},
				fmt.Errorf("%w: token unauthorized", ErrQQSecret)
		}
		return "", ProviderResult{}, fmt.Errorf("%w: token missing", ErrQQTransport)
	}
	if strings.ContainsAny(decoded.AccessToken, " \r\n\x00") {
		return "", ProviderResult{}, fmt.Errorf("%w: token format", ErrQQTransport)
	}
	return decoded.AccessToken, ProviderResult{}, nil
}

func qqContent(message Message) (string, error) {
	content := strings.TrimSpace(message.Subject + "\n" + message.Body)
	if content == "" || len(content) > maxQQContentBytes {
		return "", ErrQQMessage
	}
	return content, nil
}
