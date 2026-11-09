package dns

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
	DefaultCloudflareBaseURL = "https://api.cloudflare.com/client/v4"
	maxCloudflareBodyBytes   = 1 << 20
	maxCloudflareZonePages   = 100
	cloudflarePerPage        = 50
)

var (
	ErrCloudflareConfig    = errors.New("dns: cloudflare provider configuration is invalid")
	ErrCloudflareSecret    = errors.New("dns: cloudflare credential cannot be resolved")
	ErrCloudflareResponse  = errors.New("dns: cloudflare response is invalid")
	ErrCloudflareTransport = errors.New("dns: cloudflare request failed")
	ErrCloudflareRedirect  = errors.New("dns: cloudflare redirect is not allowed")
)

// SecretUse is the borrow intent. The controller adapter maps Ref to the CX
// credential id/revision/scope and enforces before/after checks inside
// WithSecret; the provider adapter never receives a plaintext resolver.
type SecretUse struct {
	Ref        string
	Operation  string
	SubjectID  string
	RequestID  string
	PlanDigest string
	Module     string
	Purpose    string
}

// SecretBorrower lends a credential only inside a callback so provider I/O and
// the post-use authority check happen in the same borrow window.
type SecretBorrower interface {
	WithSecret(ctx context.Context, use SecretUse, fn func(context.Context, string) error) error
}

// SecretBorrowerFunc adapts a function to SecretBorrower.
type SecretBorrowerFunc func(context.Context, SecretUse, func(context.Context, string) error) error

func (f SecretBorrowerFunc) WithSecret(ctx context.Context, use SecretUse, fn func(context.Context, string) error) error {
	return f(ctx, use, fn)
}

type secretScopeKey struct{}

// SecretScope carries the acting subject/request/plan into a borrow intent
// without adding them to Provider method signatures.
type SecretScope struct {
	SubjectID  string
	RequestID  string
	PlanDigest string
}

func WithSecretScope(ctx context.Context, scope SecretScope) context.Context {
	if previous := secretScopeFrom(ctx); scope.PlanDigest == "" && scope.SubjectID == previous.SubjectID && scope.RequestID == previous.RequestID {
		scope.PlanDigest = previous.PlanDigest
	}
	return context.WithValue(ctx, secretScopeKey{}, scope)
}

func secretScopeFrom(ctx context.Context) SecretScope {
	if ctx == nil {
		return SecretScope{}
	}
	scope, _ := ctx.Value(secretScopeKey{}).(SecretScope)
	return scope
}

// CloudflareConfig configures the real Cloudflare API adapter. BaseURL is
// overridable for a local TLS test server; the production value is never derived
// from an account record.
type CloudflareConfig struct {
	Client    *http.Client
	BaseURL   string
	Secrets   SecretBorrower
	UserAgent string
	Timeout   time.Duration
}

// CloudflareProvider implements Provider against the Cloudflare v4 API. It
// resolves the bearer token per request and never stores it on the struct.
type CloudflareProvider struct {
	client    *http.Client
	baseURL   string
	secrets   SecretBorrower
	userAgent string
}

var _ Provider = (*CloudflareProvider)(nil)

func NewCloudflareProvider(config CloudflareConfig) (*CloudflareProvider, error) {
	if config.Secrets == nil {
		return nil, ErrCloudflareConfig
	}
	base := config.BaseURL
	if base == "" {
		base = DefaultCloudflareBaseURL
	}
	parsed, err := url.Parse(base)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil ||
		parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, fmt.Errorf("%w: base url", ErrCloudflareConfig)
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
		return ErrCloudflareRedirect
	}
	client = &cloned
	agent := config.UserAgent
	if agent == "" {
		agent = "serverfleet-controller/0.9"
	}
	return &CloudflareProvider{
		client: client, baseURL: strings.TrimSuffix(base, "/"),
		secrets: config.Secrets, userAgent: boundedHeader(agent),
	}, nil
}

type cloudflareEnvelope struct {
	Success bool            `json:"success"`
	Errors  []cloudflareErr `json:"errors"`
	Result  json.RawMessage `json:"result"`
}

type cloudflareErr struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type cloudflarePageInfo struct {
	Page       int `json:"page"`
	TotalPages int `json:"total_pages"`
}

type cloudflareZonesEnvelope struct {
	Success    bool               `json:"success"`
	Errors     []cloudflareErr    `json:"errors"`
	Result     []cloudflareZone   `json:"result"`
	ResultInfo cloudflarePageInfo `json:"result_info"`
}

type cloudflareZone struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Status string `json:"status"`
}

type cloudflareRecordEnvelope struct {
	Success bool             `json:"success"`
	Errors  []cloudflareErr  `json:"errors"`
	Result  cloudflareRecord `json:"result"`
}

type cloudflareRecord struct {
	ID string `json:"id"`
}

type cloudflareTokenVerify struct {
	Status string `json:"status"`
}

type cloudflareAPIError struct {
	Operation  string
	StatusCode int
	Code       int
	Credential CredentialStatus
}

func (e *cloudflareAPIError) Error() string {
	if e == nil {
		return "cloudflare: request failed"
	}
	if e.StatusCode > 0 {
		return fmt.Sprintf("cloudflare: %s failed (status=%d code=%d)", e.Operation, e.StatusCode, e.Code)
	}
	return "cloudflare: " + e.Operation + " failed"
}

// CredentialStatusFromCloudflare maps a typed adapter error to a credential
// status without exposing provider text. The bool reports whether the error
// carried a credential verdict.
func CredentialStatusFromCloudflare(err error) (CredentialStatus, bool) {
	var apiErr *cloudflareAPIError
	if !errors.As(err, &apiErr) || apiErr.Credential == "" {
		return "", false
	}
	return apiErr.Credential, true
}

func (p *CloudflareProvider) Test(ctx context.Context, credential CredentialRef) (ConnectionTest, error) {
	if err := credential.Validate(); err != nil {
		return ConnectionTest{}, fmt.Errorf("%w: credential ref", ErrCloudflareSecret)
	}
	envelope, err := p.do(ctx, http.MethodGet, "/user/tokens/verify", credential, nil, "verify token")
	if err != nil {
		if status, ok := CredentialStatusFromCloudflare(err); ok {
			return ConnectionTest{OK: false, Status: status}, nil
		}
		return ConnectionTest{}, err
	}
	var verify cloudflareTokenVerify
	if len(envelope.Result) == 0 || json.Unmarshal(envelope.Result, &verify) != nil {
		return ConnectionTest{}, ErrCloudflareResponse
	}
	if verify.Status == "active" {
		return ConnectionTest{OK: true, Status: CredentialValid}, nil
	}
	return ConnectionTest{OK: false, Status: CredentialInvalid}, nil
}

func (p *CloudflareProvider) ListZones(ctx context.Context, credential CredentialRef) ([]ZoneInfo, error) {
	if err := credential.Validate(); err != nil {
		return nil, fmt.Errorf("%w: credential ref", ErrCloudflareSecret)
	}
	zones := make([]ZoneInfo, 0, cloudflarePerPage)
	seen := make(map[string]struct{})
	for page := 1; page <= maxCloudflareZonePages; page++ {
		query := fmt.Sprintf("?page=%d&per_page=%d&order=name&direction=asc", page, cloudflarePerPage)
		envelope, err := p.doZones(ctx, http.MethodGet, "/zones"+query, credential, nil, "list zones")
		if err != nil {
			return nil, err
		}
		for _, zone := range envelope.Result {
			if zone.ID == "" || zone.Name == "" {
				return nil, ErrCloudflareResponse
			}
			if _, duplicate := seen[zone.ID]; duplicate {
				continue
			}
			seen[zone.ID] = struct{}{}
			zones = append(zones, ZoneInfo{ProviderZoneID: zone.ID, Name: zone.Name, Status: zone.Status})
		}
		if envelope.ResultInfo.TotalPages <= page || len(envelope.Result) == 0 {
			return zones, nil
		}
	}
	return nil, fmt.Errorf("%w: zone page ceiling", ErrCloudflareResponse)
}

func (p *CloudflareProvider) CreateRecord(ctx context.Context, credential CredentialRef, zoneProviderID string, input RecordInput) (string, error) {
	body, err := cloudflareRecordBody(input)
	if err != nil {
		return "", err
	}
	if !validProviderID(zoneProviderID) {
		return "", ErrCloudflareResponse
	}
	envelope, err := p.doRecord(ctx, http.MethodPost, "/zones/"+url.PathEscape(zoneProviderID)+"/dns_records", credential, body, "create record")
	if err != nil {
		return "", err
	}
	if !validProviderID(envelope.Result.ID) {
		// A successful remote mutation without a usable identity cannot be
		// reconciled or safely retried. This is different from local validation
		// rejecting an input before the request was sent.
		return "", cloudflareOutcomeUnknown(ErrCloudflareResponse)
	}
	return envelope.Result.ID, nil
}

func (p *CloudflareProvider) UpdateRecord(ctx context.Context, credential CredentialRef, zoneProviderID, recordProviderID string, input RecordInput) error {
	body, err := cloudflareRecordBody(input)
	if err != nil {
		return err
	}
	if !validProviderID(zoneProviderID) || !validProviderID(recordProviderID) {
		return ErrCloudflareResponse
	}
	_, err = p.doRecord(ctx, http.MethodPatch, "/zones/"+url.PathEscape(zoneProviderID)+"/dns_records/"+url.PathEscape(recordProviderID), credential, body, "update record")
	return err
}

func (p *CloudflareProvider) DeleteRecord(ctx context.Context, credential CredentialRef, zoneProviderID, recordProviderID string) error {
	if !validProviderID(zoneProviderID) || !validProviderID(recordProviderID) {
		return ErrCloudflareResponse
	}
	_, err := p.doRecord(ctx, http.MethodDelete, "/zones/"+url.PathEscape(zoneProviderID)+"/dns_records/"+url.PathEscape(recordProviderID), credential, nil, "delete record")
	return err
}

func cloudflareRecordBody(input RecordInput) ([]byte, error) {
	if !input.Type.valid() || input.Name == "" || input.Content == "" || input.TTL < 0 || input.TTL > 86400 {
		return nil, ErrInvalidInput
	}
	if input.Proxied && !input.Type.proxiable() {
		return nil, ErrInvalidInput
	}
	payload := map[string]any{
		"type":    string(input.Type),
		"name":    input.Name,
		"content": input.Content,
		"ttl":     input.TTL,
	}
	if input.Proxied {
		payload["proxied"] = true
	}
	if input.Type == RecordMX {
		if input.Priority == nil || *input.Priority < 0 || *input.Priority > 65535 {
			return nil, ErrInvalidInput
		}
		payload["priority"] = *input.Priority
	} else if input.Priority != nil {
		return nil, ErrInvalidInput
	}
	return json.Marshal(payload)
}

func (p *CloudflareProvider) borrow(ctx context.Context, credential CredentialRef, operation string, fn func(context.Context, string) error) error {
	if err := credential.Validate(); err != nil {
		return fmt.Errorf("%w: credential ref", ErrCloudflareSecret)
	}
	use := SecretUse{Ref: string(credential), Operation: operation, Module: "dns", Purpose: "dns-provider"}
	scope := secretScopeFrom(ctx)
	use.SubjectID, use.RequestID, use.PlanDigest = scope.SubjectID, scope.RequestID, scope.PlanDigest
	ran := false
	err := p.secrets.WithSecret(ctx, use, func(borrowCtx context.Context, token string) error {
		ran = true
		if strings.TrimSpace(token) == "" || len(token) > 4096 || strings.ContainsAny(token, "\r\n\x00") {
			return fmt.Errorf("%w: %s", ErrCloudflareSecret, operation)
		}
		return fn(borrowCtx, token)
	})
	if err != nil && !ran {
		return fmt.Errorf("%w: %s: %v", ErrCloudflareSecret, operation, err)
	}
	return err
}

func (p *CloudflareProvider) newRequest(ctx context.Context, method, path, token string, body io.Reader, operation string) (*http.Request, error) {
	request, err := http.NewRequestWithContext(ctx, method, p.baseURL+path, body)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrCloudflareConfig, operation)
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("User-Agent", p.userAgent)
	request.Header.Set("Accept", "application/json")
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	return request, nil
}

// cloudflareOutcomeUnknown marks a failure whose remote effect cannot be
// proven. The protocol-level error identity is preserved for diagnostics while
// errors.Is(err, ErrProviderOutcomeUnknown) tells the service and cleanup state
// machine to refuse an automatic replay.
func cloudflareOutcomeUnknown(err error) error {
	return fmt.Errorf("%w: %w", ErrProviderOutcomeUnknown, err)
}

// rawRequest performs one HTTP call inside a borrow callback. The token is never
// stored outside the callback.
func (p *CloudflareProvider) rawRequest(ctx context.Context, method, path, token string, body io.Reader, operation string) (int, http.Header, []byte, error) {
	request, err := p.newRequest(ctx, method, path, token, body, operation)
	if err != nil {
		return 0, nil, nil, err
	}
	response, err := p.client.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return 0, nil, nil, ctx.Err()
		}
		return 0, nil, nil, fmt.Errorf("%w: %w: %s: %w", ErrProviderOutcomeUnknown, ErrCloudflareTransport, operation, err)
	}
	defer response.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(response.Body, maxCloudflareBodyBytes+1))
	if err != nil || len(payload) > maxCloudflareBodyBytes {
		// The request reached the provider but the response could not be
		// confirmed, so a create/delete must not be replayed automatically.
		return 0, nil, nil, cloudflareOutcomeUnknown(fmt.Errorf("%w: %s body", ErrCloudflareResponse, operation))
	}
	return response.StatusCode, response.Header, payload, nil
}

func (p *CloudflareProvider) do(ctx context.Context, method, path string, credential CredentialRef, body io.Reader, operation string) (cloudflareEnvelope, error) {
	var envelope cloudflareEnvelope
	err := p.borrow(ctx, credential, operation, func(borrowCtx context.Context, token string) error {
		status, _, payload, err := p.rawRequest(borrowCtx, method, path, token, body, operation)
		if err != nil {
			return err
		}
		decodeErr := json.Unmarshal(payload, &envelope)
		if credentialStatus := cloudflareCredentialStatus(status, envelope.Errors); credentialStatus != "" {
			return &cloudflareAPIError{Operation: operation, StatusCode: status, Code: firstCloudflareCode(envelope.Errors), Credential: credentialStatus}
		}
		if status >= 500 || decodeErr != nil {
			return cloudflareOutcomeUnknown(&cloudflareAPIError{Operation: operation, StatusCode: status, Code: firstCloudflareCode(envelope.Errors)})
		}
		if status < 200 || status > 299 || !envelope.Success {
			return &cloudflareAPIError{Operation: operation, StatusCode: status, Code: firstCloudflareCode(envelope.Errors)}
		}
		return nil
	})
	return envelope, err
}

func (p *CloudflareProvider) doZones(ctx context.Context, method, path string, credential CredentialRef, body io.Reader, operation string) (cloudflareZonesEnvelope, error) {
	var envelope cloudflareZonesEnvelope
	err := p.borrow(ctx, credential, operation, func(borrowCtx context.Context, token string) error {
		status, _, payload, err := p.rawRequest(borrowCtx, method, path, token, body, operation)
		if err != nil {
			return err
		}
		decodeErr := json.Unmarshal(payload, &envelope)
		if credentialStatus := cloudflareCredentialStatus(status, envelope.Errors); credentialStatus != "" {
			return &cloudflareAPIError{Operation: operation, StatusCode: status, Code: firstCloudflareCode(envelope.Errors), Credential: credentialStatus}
		}
		if status >= 500 || decodeErr != nil {
			return cloudflareOutcomeUnknown(&cloudflareAPIError{Operation: operation, StatusCode: status, Code: firstCloudflareCode(envelope.Errors)})
		}
		if status < 200 || status > 299 || !envelope.Success {
			return &cloudflareAPIError{Operation: operation, StatusCode: status, Code: firstCloudflareCode(envelope.Errors)}
		}
		return nil
	})
	return envelope, err
}

func (p *CloudflareProvider) doRecord(ctx context.Context, method, path string, credential CredentialRef, body []byte, operation string) (cloudflareRecordEnvelope, error) {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	var envelope cloudflareEnvelope
	var record cloudflareRecordEnvelope
	err := p.borrow(ctx, credential, operation, func(borrowCtx context.Context, token string) error {
		status, _, payload, err := p.rawRequest(borrowCtx, method, path, token, reader, operation)
		if err != nil {
			return err
		}
		decodeErr := json.Unmarshal(payload, &envelope)
		// Only a complete provider verdict can establish a definite rejection.
		// The bool zero value also represents a missing/null success field, and
		// an auth-looking error embedded in a 5xx is not proof of no effect.
		if status >= 500 {
			return cloudflareOutcomeUnknown(&cloudflareAPIError{Operation: operation, StatusCode: status, Code: firstCloudflareCode(envelope.Errors)})
		}
		var verdict struct {
			Success *bool `json:"success"`
		}
		if decodeErr != nil || json.Unmarshal(payload, &verdict) != nil || verdict.Success == nil {
			return cloudflareOutcomeUnknown(ErrCloudflareResponse)
		}
		if credentialStatus := cloudflareCredentialStatus(status, envelope.Errors); credentialStatus != "" {
			return &cloudflareAPIError{Operation: operation, StatusCode: status, Code: firstCloudflareCode(envelope.Errors), Credential: credentialStatus}
		}
		if status == http.StatusNotFound {
			return ErrProviderNotFound
		}
		if status < 200 || status > 299 || !envelope.Success {
			return &cloudflareAPIError{Operation: operation, StatusCode: status, Code: firstCloudflareCode(envelope.Errors)}
		}
		if len(envelope.Result) > 0 {
			if err := json.Unmarshal(envelope.Result, &record.Result); err != nil {
				return cloudflareOutcomeUnknown(fmt.Errorf("%w: %s result", ErrCloudflareResponse, operation))
			}
		}
		return nil
	})
	record.Success = envelope.Success
	record.Errors = envelope.Errors
	return record, err
}

func cloudflareCredentialStatus(status int, errors []cloudflareErr) CredentialStatus {
	switch status {
	case http.StatusUnauthorized:
		return CredentialInvalid
	case http.StatusForbidden:
		return CredentialMissingPermissions
	}
	for _, item := range errors {
		if item.Code == 1000 {
			return CredentialInvalid
		}
		if item.Code == 9109 {
			return CredentialMissingPermissions
		}
	}
	return ""
}

func firstCloudflareCode(errors []cloudflareErr) int {
	if len(errors) == 0 {
		return 0
	}
	return errors[0].Code
}

func boundedHeader(value string) string {
	cleaned := strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, value)
	if len(cleaned) > 200 {
		cleaned = cleaned[:200]
	}
	if cleaned == "" {
		return "serverfleet-controller/0.9"
	}
	return cleaned
}
