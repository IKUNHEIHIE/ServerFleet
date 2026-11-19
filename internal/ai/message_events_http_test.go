package ai

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"serverfleet.local/serverfleet/internal/auth"
)

func TestMessageEventCursorBindingAndCanonicalSequence(t *testing.T) {
	for _, cursor := range []string{"other-session.1", "session.01", "session.-1", "session.18446744073709551616", "session.1\n", "session."} {
		if _, err := parseMessageEventCursor("session", cursor); err != ErrScopeConflict {
			t.Fatal("invalid/cross-session event cursor accepted", err)
		}
	}
	for _, sequence := range []uint64{0, 1, 99} {
		if n, err := parseMessageEventCursor("session", messageEventCursor("session", sequence)); err != nil || n != sequence {
			t.Fatal(n, err)
		}
	}
}

func TestMessageStreamRejectsBeforeAnyEventDisclosure(t *testing.T) {
	ctx := context.Background()
	s, _, _, _, _ := sessionFixture(t)
	view, err := s.Create(ctx)
	if err != nil {
		t.Fatal(err)
	}
	chat, _ := conversationFixture(t, s, func(context.Context, GenerationRequest, func(string) error) error { return ErrModelUnavailable })
	if _, err := chat.Submit(ctx, view.ID, "request-one", MessageInput{"question", "client-one", view.ScopeRevision}); err != nil {
		t.Fatal(err)
	}
	if _, err := NewMessageEventsHTTP(chat, nil, nil); err != ErrSessionUnavailable {
		t.Fatal("missing security gates accepted")
	}
	for _, test := range []struct {
		name             string
		status           int
		modify           func(*http.Request)
		authErr, gateErr error
	}{
		{"plain-http", 403, func(r *http.Request) { r.TLS = nil }, nil, nil},
		{"method", 405, func(r *http.Request) { r.Method = http.MethodHead }, nil, nil},
		{"query", 400, func(r *http.Request) { r.URL.RawQuery = "token=not-accepted" }, nil, nil},
		{"duplicate-cursor", 400, func(r *http.Request) { r.Header["Last-Event-Id"] = []string{"a", "b"} }, nil, nil},
		{"cross-cursor", 409, func(r *http.Request) { r.Header.Set("Last-Event-ID", "another-session.1") }, nil, nil},
		{"future-cursor", 409, func(r *http.Request) { r.Header.Set("Last-Event-ID", messageEventCursor(view.ID, 99)) }, nil, nil},
		{"missing-credential", 401, func(*http.Request) {}, auth.ErrCredential, nil},
		{"source-down", 503, func(*http.Request) {}, auth.ErrIdentityUnavailable, nil},
		{"cross-origin", 403, func(*http.Request) {}, auth.ErrDenied, nil},
		{"revoked-before-flush", 403, func(*http.Request) {}, nil, auth.ErrDenied},
	} {
		t.Run(test.name, func(t *testing.T) {
			h, err := NewMessageEventsHTTP(chat, func(r *http.Request) (context.Context, error) { return r.Context(), test.authErr }, func(context.Context) error { return test.gateErr })
			if err != nil {
				t.Fatal(err)
			}
			r := httptest.NewRequest(http.MethodGet, "https://fleet.test/api/v1/ai/sessions/"+view.ID+"/events", nil)
			test.modify(r)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != test.status || strings.Contains(w.Body.String(), "ai.message.created") || strings.Contains(w.Body.String(), "question") || w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal(w.Code, w.Body.String())
			}
		})
	}
}

type messageStreamRecorder struct {
	*httptest.ResponseRecorder
}

func (*messageStreamRecorder) SetWriteDeadline(time.Time) error { return nil }

func TestMessageStreamAuthenticatesOnceAndRechecksEachDelivery(t *testing.T) {
	ctx := context.Background()
	sessions, _, _, _, _ := sessionFixture(t)
	view, err := sessions.Create(ctx)
	if err != nil {
		t.Fatal(err)
	}
	chat, _ := conversationFixture(t, sessions, func(context.Context, GenerationRequest, func(string) error) error {
		return ErrModelUnavailable
	})
	for _, client := range []string{"client-one", "client-two"} {
		if _, err := chat.Submit(ctx, view.ID, client, MessageInput{"question", client, view.ScopeRevision}); err != nil {
			t.Fatal(err)
		}
	}
	authentications, observations := 0, 0
	handler, err := NewMessageEventsHTTP(chat, func(r *http.Request) (context.Context, error) {
		authentications++
		return r.Context(), nil
	}, func(context.Context) error {
		observations++
		if observations == 3 {
			return auth.ErrDenied
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	w := &messageStreamRecorder{httptest.NewRecorder()}
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "https://fleet.test/api/v1/ai/sessions/"+view.ID+"/events", nil))
	if authentications != 1 || observations != 3 {
		t.Fatalf("stream authentications = %d, authorization observations = %d", authentications, observations)
	}
	out := w.Body.String()
	if !strings.Contains(out, "id: "+messageEventCursor(view.ID, 1)) || strings.Contains(out, "id: "+messageEventCursor(view.ID, 2)) {
		t.Fatalf("stream must withhold the event that lost authorization: %q", out)
	}
}
