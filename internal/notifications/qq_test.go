package notifications_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"serverfleet.local/serverfleet/internal/notifications"
)

type qqRecorder struct {
	mu       sync.Mutex
	requests []qqRequest
}

type qqRequest struct {
	path          string
	authorization string
	body          map[string]any
}

func (r *qqRecorder) record(t *testing.T, request *http.Request) map[string]any {
	t.Helper()
	payload, _ := io.ReadAll(request.Body)
	var decoded map[string]any
	_ = json.Unmarshal(payload, &decoded)
	r.mu.Lock()
	r.requests = append(r.requests, qqRequest{path: request.URL.Path, authorization: request.Header.Get("Authorization"), body: decoded})
	r.mu.Unlock()
	return decoded
}

func (r *qqRecorder) all() []qqRequest {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]qqRequest(nil), r.requests...)
}

func newQQSender(t *testing.T, server *httptest.Server, secret string) *notifications.QQBotSender {
	t.Helper()
	sender, err := notifications.NewQQBotSender(notifications.QQConfig{
		Client: server.Client(), TokenURL: server.URL + "/token", APIBaseURL: server.URL + "/api",
		Secrets: notifications.SecretBorrowerFunc(func(ctx context.Context, _ notifications.SecretUse, fn func(context.Context, string) error) error {
			return fn(ctx, secret)
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	return sender
}

func TestQQBotSenderPrivateAndGroup(t *testing.T) {
	recorder := &qqRecorder{}
	var messages int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/token":
			body := recorder.record(t, r)
			if body["appId"] != "app-1" || body["clientSecret"] != "qq-secret" {
				t.Errorf("token body = %+v", body)
			}
			_, _ = io.WriteString(w, `{"access_token":"qq-token-xyz","expires_in":7200}`)
		case "/api/v2/users/openid-1/messages", "/api/v2/groups/group-1/messages":
			atomic.AddInt32(&messages, 1)
			if r.Method != http.MethodPost {
				t.Errorf("method = %s", r.Method)
			}
			if r.Header.Get("Authorization") != "QQBot qq-token-xyz" {
				t.Errorf("authorization = %q", r.Header.Get("Authorization"))
			}
			body := recorder.record(t, r)
			if body["msg_type"] != float64(0) || body["msg_seq"] != float64(1) {
				t.Errorf("message body = %+v", body)
			}
			_, _ = io.WriteString(w, `{"id":"msg-1","timestamp":"1700000000"}`)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	sender := newQQSender(t, server, "qq-secret")

	result, err := sender.Send(context.Background(), notifications.QQSettings{
		AppID: "app-1", TargetType: notifications.QQTargetPrivate, TargetID: "openid-1",
	}, "secret/qq", notifications.Message{Subject: "告警", Body: "cpu high"})
	if err != nil || result.Outcome != notifications.ProviderAccepted {
		t.Fatalf("private result = %+v, err = %v", result, err)
	}
	result, err = sender.Send(context.Background(), notifications.QQSettings{
		AppID: "app-1", TargetType: notifications.QQTargetGroup, TargetID: "group-1",
	}, "secret/qq", notifications.Message{Subject: "告警", Body: "cpu high"})
	if err != nil || result.Outcome != notifications.ProviderAccepted {
		t.Fatalf("group result = %+v, err = %v", result, err)
	}
	requests := recorder.all()
	if len(requests) != 4 || messages != 2 {
		t.Fatalf("requests = %+v messages = %d", requests, messages)
	}
	if requests[1].body["content"] != "告警\ncpu high" {
		t.Fatalf("content = %v", requests[1].body["content"])
	}
	for _, request := range requests {
		if request.path == "/token" && request.authorization != "" {
			t.Fatalf("token request carried authorization")
		}
	}
}

func TestQQBotSenderCredentialAndRateLimitAndUnknown(t *testing.T) {
	cases := []struct {
		name        string
		tokenCode   int
		sendCode    int
		wantOutcome notifications.ProviderOutcome
		wantCred    bool
	}{
		{"token unauthorized", 401, 0, notifications.ProviderRejected, true},
		{"send unauthorized", 200, 401, notifications.ProviderRejected, true},
		{"send forbidden", 200, 403, notifications.ProviderRejected, true},
		{"send rate limited", 200, 429, notifications.ProviderRateLimited, false},
		{"send server error", 200, 503, notifications.ProviderUnknown, false},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.URL.Path == "/token" {
					if test.tokenCode != 200 {
						w.WriteHeader(test.tokenCode)
						_, _ = io.WriteString(w, `{"code":401,"message":"bad secret"}`)
						return
					}
					_, _ = io.WriteString(w, `{"access_token":"qq-token-xyz","expires_in":7200}`)
					return
				}
				w.WriteHeader(test.sendCode)
				_, _ = io.WriteString(w, `{"code":500,"message":"upstream says `+"qq-secret"+`"}`)
			}))
			defer server.Close()
			sender := newQQSender(t, server, "qq-secret")
			result, err := sender.Send(context.Background(), notifications.QQSettings{
				AppID: "app-1", TargetType: notifications.QQTargetPrivate, TargetID: "openid-1",
			}, "secret/qq", notifications.Message{Subject: "alert", Body: "body"})
			if test.wantOutcome == notifications.ProviderRejected || test.wantOutcome == notifications.ProviderRateLimited || test.wantOutcome == notifications.ProviderUnknown {
				if err == nil {
					t.Fatalf("expected error for %s", test.name)
				}
			}
			if result.Outcome != test.wantOutcome || result.CredentialInvalid != test.wantCred {
				t.Fatalf("result = %+v", result)
			}
			if err != nil && strings.Contains(err.Error(), "qq-secret") {
				t.Fatalf("secret leaked: %v", err)
			}
		})
	}
}

func TestQQBotSenderTimeoutIsUnknown(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/token" {
			_, _ = io.WriteString(w, `{"access_token":"qq-token-xyz","expires_in":7200}`)
			return
		}
		time.Sleep(400 * time.Millisecond)
		_, _ = io.WriteString(w, `{"id":"msg-late"}`)
	}))
	defer server.Close()
	sender := newQQSender(t, server, "qq-secret")
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	result, err := sender.Send(ctx, notifications.QQSettings{
		AppID: "app-1", TargetType: notifications.QQTargetGroup, TargetID: "group-1",
	}, "secret/qq", notifications.Message{Subject: "alert", Body: "slow"})
	if err == nil || result.Outcome != notifications.ProviderUnknown {
		t.Fatalf("result = %+v, err = %v", result, err)
	}
}

func TestQQBotSenderRejectsOversizeAndRedirects(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/token" {
			_, _ = io.WriteString(w, `{"access_token":"qq-token-xyz","expires_in":7200}`)
			return
		}
		_, _ = io.WriteString(w, `{"id":"msg-1"}`)
	}))
	defer server.Close()
	sender := newQQSender(t, server, "qq-secret")
	result, err := sender.Send(context.Background(), notifications.QQSettings{
		AppID: "app-1", TargetType: notifications.QQTargetPrivate, TargetID: "openid-1",
	}, "secret/qq", notifications.Message{Subject: "alert", Body: strings.Repeat("x", 3000)})
	if err == nil || result.Outcome != notifications.ProviderRejected || result.Code != "message_too_large" {
		t.Fatalf("result = %+v, err = %v", result, err)
	}

	var redirected int32
	target := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&redirected, 1)
	}))
	defer target.Close()
	redirecting := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusFound)
	}))
	defer redirecting.Close()
	sender, err = notifications.NewQQBotSender(notifications.QQConfig{
		Client: redirecting.Client(), TokenURL: redirecting.URL, APIBaseURL: redirecting.URL,
		Secrets: notifications.SecretBorrowerFunc(func(ctx context.Context, _ notifications.SecretUse, fn func(context.Context, string) error) error {
			return fn(ctx, "qq-secret")
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sender.Send(context.Background(), notifications.QQSettings{
		AppID: "app-1", TargetType: notifications.QQTargetPrivate, TargetID: "openid-1",
	}, "secret/qq", notifications.Message{Subject: "alert", Body: "body"}); err == nil {
		t.Fatal("redirected request succeeded")
	}
	if redirected != 0 {
		t.Fatalf("redirect target requests = %d", redirected)
	}
}

func TestQQBotSenderConfigAndSecretFailures(t *testing.T) {
	for _, config := range []notifications.QQConfig{
		{},
		{TokenURL: "http://insecure.example/token", Secrets: notifications.SecretBorrowerFunc(func(ctx context.Context, _ notifications.SecretUse, fn func(context.Context, string) error) error {
			return fn(ctx, "x")
		})},
		{APIBaseURL: "https://user:pass@example.com", Secrets: notifications.SecretBorrowerFunc(func(ctx context.Context, _ notifications.SecretUse, fn func(context.Context, string) error) error {
			return fn(ctx, "x")
		})},
	} {
		if _, err := notifications.NewQQBotSender(config); !errors.Is(err, notifications.ErrQQConfig) {
			t.Fatalf("config %+v err = %v", config, err)
		}
	}
	sender, err := notifications.NewQQBotSender(notifications.QQConfig{
		TokenURL: "https://example.com/token", APIBaseURL: "https://example.com",
		Secrets: notifications.SecretBorrowerFunc(func(context.Context, notifications.SecretUse, func(context.Context, string) error) error {
			return errors.New("store offline")
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sender.Send(context.Background(), notifications.QQSettings{
		AppID: "app-1", TargetType: notifications.QQTargetPrivate, TargetID: "openid-1",
	}, "secret/qq", notifications.Message{Subject: "alert", Body: "body"}); !errors.Is(err, notifications.ErrQQSecret) {
		t.Fatalf("secret err = %v", err)
	}
}

func TestQQBotSenderRejectsNonSuccessTokenStatusEvenWithTokenBody(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/token" {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = io.WriteString(w, `{"access_token":"must-not-be-used","expires_in":7200}`)
			return
		}
		t.Error("message endpoint was called after a failed token response")
	}))
	defer server.Close()
	sender := newQQSender(t, server, "qq-secret")
	result, err := sender.Send(context.Background(), notifications.QQSettings{
		AppID: "app-1", TargetType: notifications.QQTargetPrivate, TargetID: "openid-1",
	}, "secret/qq", notifications.Message{Subject: "alert", Body: "body"})
	if err == nil || result.Outcome == notifications.ProviderAccepted {
		t.Fatalf("result = %+v, err = %v", result, err)
	}
}

func TestQQBotSenderBorrowBoundaries(t *testing.T) {
	var requests int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requests, 1)
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/token" {
			_, _ = io.WriteString(w, `{"access_token":"qq-token-xyz","expires_in":7200}`)
			return
		}
		_, _ = io.WriteString(w, `{"id":"msg-1"}`)
	}))
	defer server.Close()

	pre, err := notifications.NewQQBotSender(notifications.QQConfig{
		Client: server.Client(), TokenURL: server.URL + "/token", APIBaseURL: server.URL + "/api",
		Secrets: notifications.SecretBorrowerFunc(func(context.Context, notifications.SecretUse, func(context.Context, string) error) error {
			return errors.New("revoked before use")
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pre.Send(context.Background(), notifications.QQSettings{
		AppID: "app-1", TargetType: notifications.QQTargetPrivate, TargetID: "openid-1",
	}, "secret/qq", notifications.Message{Subject: "alert", Body: "body"}); !errors.Is(err, notifications.ErrQQSecret) {
		t.Fatalf("pre err = %v", err)
	}
	if requests != 0 {
		t.Fatalf("provider I/O ran after the borrow pre-check failed: %d", requests)
	}

	post, err := notifications.NewQQBotSender(notifications.QQConfig{
		Client: server.Client(), TokenURL: server.URL + "/token", APIBaseURL: server.URL + "/api",
		Secrets: notifications.SecretBorrowerFunc(func(ctx context.Context, _ notifications.SecretUse, fn func(context.Context, string) error) error {
			if err := fn(ctx, "qq-secret"); err != nil {
				return err
			}
			return errors.New("revoked after use")
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := post.Send(context.Background(), notifications.QQSettings{
		AppID: "app-1", TargetType: notifications.QQTargetPrivate, TargetID: "openid-1",
	}, "secret/qq", notifications.Message{Subject: "alert", Body: "body"})
	if err == nil || result.Outcome != notifications.ProviderUnknown {
		t.Fatalf("post result = %+v, err = %v", result, err)
	}
	if requests == 0 {
		t.Fatal("provider I/O did not run inside the borrow callback")
	}
}
