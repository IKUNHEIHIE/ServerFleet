package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"serverfleet.local/serverfleet/internal/auth"
)

func messageHTTPFixture(t *testing.T) (*MessagesHTTP, *ConversationService, SessionView, *SessionActor, *bool, *int) {
	t.Helper()
	s, _, actor, _, _ := sessionFixture(t)
	v, err := s.Create(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	chat, enabled := conversationFixture(t, s, func(context.Context, GenerationRequest, func(string) error) error {
		calls++
		return ErrModelUnavailable
	})
	h, err := NewMessagesHTTP(chat, func(r *http.Request) (context.Context, error) { return r.Context(), nil }, func(context.Context) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	return h, chat, v, actor, enabled, &calls
}

func messageHTTPRequest(h *MessagesHTTP, method, target, body string, alter func(*http.Request)) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "https://fleet.test"+target, strings.NewReader(body))
	if method == http.MethodPost {
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Idempotency-Key", "request-one")
	}
	if alter != nil {
		alter(r)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestMessageHTTPSubmitReplayPaginationAndNoProviderInvocation(t *testing.T) {
	h, _, v, _, _, calls := messageHTTPFixture(t)
	path := "/api/v1/ai/sessions/" + v.ID + "/messages"
	body := `{"content":"question synthetic-secret","clientMessageId":"client-one","scopeRevision":1}`
	w := messageHTTPRequest(h, "POST", path, body, nil)
	var first Message
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &first) != nil || first.RedactedContent != "question [redacted]" || first.Role != "user" || len(first.TaskIDs) != 0 || *calls != 0 {
		t.Fatal(w.Code, w.Body.String())
	}
	for _, header := range []string{"Cache-Control", "X-Content-Type-Options", "X-Request-ID"} {
		if w.Header().Get(header) == "" {
			t.Fatal("missing security header", header)
		}
	}
	replayed := messageHTTPRequest(h, "POST", path, body, nil)
	if replayed.Code != 200 || !bytes.Equal(w.Body.Bytes(), replayed.Body.Bytes()) {
		t.Fatal("idempotent POST changed result", replayed.Code, replayed.Body.String())
	}
	conflict := messageHTTPRequest(h, "POST", path, strings.Replace(body, "question", "changed", 1), nil)
	if conflict.Code != 409 {
		t.Fatal("changed replay accepted", conflict.Code)
	}
	second := strings.Replace(body, "client-one", "client-two", 1)
	if got := messageHTTPRequest(h, "POST", path, second, func(r *http.Request) { r.Header.Set("Idempotency-Key", "request-two") }); got.Code != 200 {
		t.Fatal(got.Code, got.Body.String())
	}
	w = messageHTTPRequest(h, "GET", path+"?limit=1", "", nil)
	var page messagePage
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &page) != nil || len(page.Items) != 1 || page.Items[0].MessageID != first.MessageID || !page.Page.HasMore || page.Page.Limit != 1 || page.Page.NextCursor == "" {
		t.Fatal(w.Code, w.Body.String())
	}
	next := messageHTTPRequest(h, "GET", path+"?limit=1&cursor="+url.QueryEscape(page.Page.NextCursor), "", nil)
	if next.Code != 200 || json.Unmarshal(next.Body.Bytes(), &page) != nil || len(page.Items) != 1 || page.Items[0].MessageID == first.MessageID || page.Page.HasMore {
		t.Fatal(next.Code, next.Body.String())
	}
	if *calls != 0 {
		t.Fatal("HTTP submission invoked a model", *calls)
	}
}

func TestMessageHTTPInvalidInputIsStrictAndNeverPersists(t *testing.T) {
	h, chat, v, _, _, _ := messageHTTPFixture(t)
	path := "/api/v1/ai/sessions/" + v.ID + "/messages"
	valid := `{"content":"text","clientMessageId":"client-one","scopeRevision":1}`
	for _, body := range []string{
		`[]`, `null`, `{}`, valid + `{}`, `{"content":"a","content":"b","clientMessageId":"id","scopeRevision":1}`,
		`{"Content":"a","clientMessageId":"id","scopeRevision":1}`,
		`{"content":null,"clientMessageId":"id","scopeRevision":1}`,
		`{"content":"a","clientMessageId":"id","scopeRevision":null}`,
		`{"content":"a","clientMessageId":"id","scopeRevision":1.5}`,
		`{"content":"a","clientMessageId":"id","scopeRevision":-1}`,
		`{"content":"a","clientMessageId":"id","scopeRevision":18446744073709551616}`,
		`{"content":"a","clientMessageId":"id","scopeRevision":1,"taskIds":["forged"]}`,
		strings.Replace(valid, "text", strings.Repeat("界", 32001), 1), string([]byte{'{', '"', 'x', '"', ':', '"', 0xff, '"', '}'}), strings.Repeat(" ", 513<<10),
	} {
		if w := messageHTTPRequest(h, "POST", path, body, nil); w.Code != 400 {
			t.Fatal("invalid body accepted", w.Code)
		}
	}
	for _, alter := range []func(*http.Request){
		func(r *http.Request) { r.Header.Del("Idempotency-Key") },
		func(r *http.Request) { r.Header["Idempotency-Key"] = []string{"one-key-here", "two-key-here"} },
		func(r *http.Request) { r.Header.Set("Idempotency-Key", "short") },
		func(r *http.Request) { r.Header.Set("Content-Type", "text/plain") },
		func(r *http.Request) { r.Header.Set("Content-Type", "application/json; unknown=1") },
		func(r *http.Request) { r.Header.Set("Content-Encoding", "gzip") },
	} {
		if w := messageHTTPRequest(h, "POST", path, valid, alter); w.Code != 400 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if items, err := chat.Messages(context.Background(), v.ID, 0, 100); err != nil || len(items) != 0 {
		t.Fatal("malformed input persisted", err, items)
	}
}

func TestMessageHTTPCurrentAuthorityFinalGateAndModelDisable(t *testing.T) {
	h, _, v, actor, enabled, _ := messageHTTPFixture(t)
	path := "/api/v1/ai/sessions/" + v.ID + "/messages"
	body := `{"content":"text","clientMessageId":"client-one","scopeRevision":1}`
	if _, err := NewMessagesHTTP(h.conversation, nil, nil); err != ErrSessionUnavailable {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name             string
		status           int
		authErr, gateErr error
	}{
		{"credential", 401, auth.ErrCredential, nil}, {"cross-origin", 403, auth.ErrDenied, nil}, {"source", 503, auth.ErrIdentityUnavailable, nil}, {"revoked-at-disclosure", 403, nil, auth.ErrDenied},
	} {
		t.Run(test.name, func(t *testing.T) {
			candidate, _ := NewMessagesHTTP(h.conversation, func(r *http.Request) (context.Context, error) { return r.Context(), test.authErr }, func(context.Context) error { return test.gateErr })
			w := messageHTTPRequest(candidate, "POST", path, body, nil)
			if w.Code != test.status || strings.Contains(w.Body.String(), "redactedContent") {
				t.Fatal(w.Code, w.Body.String())
			}
		})
	}
	*enabled = false
	if w := messageHTTPRequest(h, "POST", path, body, nil); w.Code != 503 {
		t.Fatal("disabled provider accepted", w.Code)
	}
	*enabled = true
	actor.SubjectID = "another-owner"
	for _, method := range []string{"GET", "POST"} {
		data := body
		if method == "GET" {
			data = ""
		}
		if w := messageHTTPRequest(h, method, path, data, nil); w.Code != 403 {
			t.Fatal("cross-owner request accepted", w.Code)
		}
	}
}

func TestMessageHTTPQueryCursorAndTransportBoundaries(t *testing.T) {
	h, _, v, _, _, _ := messageHTTPFixture(t)
	path := "/api/v1/ai/sessions/" + v.ID + "/messages"
	for _, query := range []string{"?token=abc", "?limit=0", "?limit=201", "?limit=01", "?limit=", "?limit=1&limit=2", "?cursor=a&cursor=b", "?limit=%FF", "?cursor=%"} {
		if w := messageHTTPRequest(h, "GET", path+query, "", nil); w.Code != 400 {
			t.Fatal(query, w.Code)
		}
	}
	for _, cursor := range []string{"invalid", encodeMessageCursor("other", 1, 1), encodeMessageCursor(v.ID, 2, 1), encodeMessageCursor(v.ID, 1, 99)} {
		if w := messageHTTPRequest(h, "GET", path+"?cursor="+url.QueryEscape(cursor), "", nil); w.Code != 409 {
			t.Fatal("invalid cursor accepted", w.Code)
		}
	}
	if w := messageHTTPRequest(h, "HEAD", path, "", nil); w.Code != 405 {
		t.Fatal(w.Code)
	}
	if w := messageHTTPRequest(h, "GET", path, "", func(r *http.Request) { r.TLS = nil }); w.Code != 403 {
		t.Fatal(w.Code)
	}
}

func TestMessageHTTPByteBoundedPageDoesNotLoseMessages(t *testing.T) {
	h, chat, v, _, _, _ := messageHTTPFixture(t)
	for i := 0; i < 8; i++ {
		id := "id-" + strconv.Itoa(i)
		if _, err := chat.Submit(context.Background(), v.ID, id, MessageInput{Content: strings.Repeat("界", 32000), ClientMessageID: id, ScopeRevision: 1}); err != nil {
			t.Fatal(err)
		}
	}
	path := "/api/v1/ai/sessions/" + v.ID + "/messages?limit=200"
	count, cursor := 0, ""
	for {
		w := messageHTTPRequest(h, "GET", path+cursor, "", nil)
		var page messagePage
		if w.Code != 200 || w.Body.Len() > 512<<10 || json.Unmarshal(w.Body.Bytes(), &page) != nil || len(page.Items) == 0 {
			t.Fatal(w.Code, w.Body.Len())
		}
		count += len(page.Items)
		if !page.Page.HasMore {
			break
		}
		cursor = "&cursor=" + url.QueryEscape(page.Page.NextCursor)
	}
	if count != 8 {
		t.Fatal("byte pagination lost/duplicated messages", count)
	}
}

func TestMessageHTTPRevokedAfterCommitWithholdsResponseAndReplayIsSafe(t *testing.T) {
	h, chat, v, _, _, calls := messageHTTPFixture(t)
	gates := 0
	h.commitGate = func(context.Context) error {
		gates++
		if gates == 2 {
			return auth.ErrDenied
		}
		return nil
	}
	path := "/api/v1/ai/sessions/" + v.ID + "/messages"
	body := `{"content":"saved question","clientMessageId":"client-one","scopeRevision":1}`
	w := messageHTTPRequest(h, "POST", path, body, nil)
	if w.Code != 403 || strings.Contains(w.Body.String(), "saved question") {
		t.Fatal("revoked response leaked", w.Code, w.Body.String())
	}
	items, err := chat.Messages(context.Background(), v.ID, 0, 100)
	if err != nil || len(items) != 1 || *calls != 0 {
		t.Fatal("lost committed message or started model", err)
	}
	h.commitGate = func(context.Context) error { return nil }
	w = messageHTTPRequest(h, "POST", path, body, nil)
	var replay Message
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &replay) != nil || replay.MessageID != items[0].MessageID {
		t.Fatal("retry duplicated committed message", w.Code)
	}
}
