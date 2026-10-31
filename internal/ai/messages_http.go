package ai

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"serverfleet.local/serverfleet/internal/auth"
)

// MessagesHTTP adapts existing candidate paths; it is not mounted by main.
// Authenticate must enforce CURRENT Cookie/CSRF/Origin and derive its context
// from this request. CommitGate must recheck that observation before disclosure.
// Submission persists a queued user message; no model/tool worker starts here.
type MessagesHTTP struct {
	conversation *ConversationService
	authenticate func(*http.Request) (context.Context, error)
	commitGate   func(context.Context) error
	mux          *http.ServeMux
}

type messagePageMeta struct {
	NextCursor string `json:"nextCursor,omitempty"`
	Limit      int    `json:"limit"`
	HasMore    bool   `json:"hasMore"`
}
type messagePage struct {
	Items []Message       `json:"items"`
	Page  messagePageMeta `json:"page"`
}
type messageCursor struct {
	Version  int    `json:"v"`
	Session  string `json:"s"`
	Revision uint64 `json:"r"`
	After    int    `json:"a"`
}

func NewMessagesHTTP(conversation *ConversationService, authenticate func(*http.Request) (context.Context, error), commitGate func(context.Context) error) (*MessagesHTTP, error) {
	if conversation == nil || authenticate == nil || commitGate == nil {
		return nil, ErrSessionUnavailable
	}
	h := &MessagesHTTP{conversation, authenticate, commitGate, http.NewServeMux()}
	h.mux.HandleFunc("GET /api/v1/ai/sessions/{sessionId}/messages", h.list)
	h.mux.HandleFunc("POST /api/v1/ai/sessions/{sessionId}/messages", h.submit)
	return h, nil
}

func (h *MessagesHTTP) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	id, err := sessionID()
	if err != nil {
		messageStreamProblem(w, 503, "MESSAGE_SOURCE_UNAVAILABLE")
		return
	}
	w.Header().Set("X-Request-ID", id)
	if r.TLS == nil {
		messageStreamProblem(w, 403, "NOT_AUTHORIZED")
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		w.Header().Set("Allow", "GET, POST")
		messageStreamProblem(w, 405, "METHOD_NOT_ALLOWED")
		return
	}
	bounded, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	h.mux.ServeHTTP(w, r.WithContext(bounded))
}

func (h *MessagesHTTP) observed(w http.ResponseWriter, r *http.Request) (context.Context, bool) {
	if !exactID(r.PathValue("sessionId")) {
		messageStreamProblem(w, 400, "INVALID_REQUEST")
		return nil, false
	}
	ctx, err := h.authenticate(r)
	if err != nil || ctx == nil || ctx.Err() != nil {
		messageStreamProblem(w, auth.IdentityHTTPStatus(err), "AUTHENTICATION_REQUIRED")
		return nil, false
	}
	return ctx, true
}

func messageProblem(w http.ResponseWriter, err error) {
	status, code := 503, "MESSAGE_SOURCE_UNAVAILABLE"
	if errors.Is(err, ErrToolDenied) {
		status, code = 403, "NOT_AUTHORIZED"
	} else if errors.Is(err, ErrScopeConflict) {
		status, code = 409, "STALE_SCOPE_OR_REQUEST"
	}
	messageStreamProblem(w, status, code)
}

func (h *MessagesHTTP) send(w http.ResponseWriter, ctx context.Context, value any) {
	raw, err := json.Marshal(value)
	if err != nil || len(raw) > 512<<10 {
		messageStreamProblem(w, 503, "MESSAGE_SOURCE_UNAVAILABLE")
		return
	}
	if err = h.commitGate(ctx); err != nil || ctx.Err() != nil {
		messageStreamProblem(w, auth.IdentityHTTPStatus(err), "AUTHENTICATION_REQUIRED")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(raw)
}

// A cursor is an opaque positional hint, not an authorization credential. All
// ownership/resource/policy checks run anew; clients may choose any valid offset
// in their OWN current scope. Binding prevents accidental cross-session reuse.
func decodeMessageCursor(raw, session string, revision uint64) (int, error) {
	if raw == "" {
		return 0, nil
	}
	if len(raw) > 1024 {
		return 0, ErrScopeConflict
	}
	b, err := base64.RawURLEncoding.DecodeString(raw)
	var c messageCursor
	if err != nil || base64.RawURLEncoding.EncodeToString(b) != raw || json.Unmarshal(b, &c) != nil {
		return 0, ErrScopeConflict
	}
	canonical, _ := json.Marshal(c)
	if !bytes.Equal(b, canonical) || c.Version != 1 || c.Session != session || c.Revision != revision || c.After < 1 || c.After > 512 {
		return 0, ErrScopeConflict
	}
	return c.After, nil
}
func encodeMessageCursor(session string, revision uint64, after int) string {
	raw, _ := json.Marshal(messageCursor{1, session, revision, after})
	return base64.RawURLEncoding.EncodeToString(raw)
}

func (h *MessagesHTTP) list(w http.ResponseWriter, r *http.Request) {
	ctx, ok := h.observed(w, r)
	if !ok {
		return
	}
	if r.ContentLength > 0 || len(r.TransferEncoding) != 0 {
		messageStreamProblem(w, 400, "INVALID_REQUEST")
		return
	}
	q, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		messageStreamProblem(w, 400, "INVALID_REQUEST")
		return
	}
	for k, v := range q {
		if k != "limit" && k != "cursor" || len(v) != 1 {
			messageStreamProblem(w, 400, "INVALID_REQUEST")
			return
		}
	}
	limit := 50
	if values, exists := q["limit"]; exists {
		limit, err = strconv.Atoi(values[0])
		if err != nil || strconv.Itoa(limit) != values[0] || limit < 1 || limit > 200 {
			messageStreamProblem(w, 400, "INVALID_REQUEST")
			return
		}
	}
	id := r.PathValue("sessionId")
	view, err := h.conversation.sessions.Get(ctx, id)
	if err != nil {
		messageProblem(w, err)
		return
	}
	after, err := decodeMessageCursor(q.Get("cursor"), id, view.ScopeRevision)
	if err != nil {
		messageProblem(w, err)
		return
	}
	messages := []Message{}
	// Domain reads are capped at 100; gather at most 201, with one lookahead.
	for len(messages) < limit+1 {
		n := min(100, limit+1-len(messages))
		batch, err := h.conversation.Messages(ctx, id, after+len(messages), n)
		if err != nil {
			messageProblem(w, err)
			return
		}
		messages = append(messages, batch...)
		if len(batch) < n {
			break
		}
	}
	page := messagePage{Items: []Message{}, Page: messagePageMeta{Limit: limit}}
	size := 1024 // page/cursor overhead; each message is independently bounded
	for _, m := range messages {
		raw, err := json.Marshal(m)
		if err != nil {
			messageProblem(w, ErrSessionUnavailable)
			return
		}
		if len(page.Items) == limit || size+len(raw)+1 > 512<<10 {
			break
		}
		size += len(raw) + 1
		page.Items = append(page.Items, m)
	}
	page.Page.HasMore = len(page.Items) < len(messages)
	if page.Page.HasMore {
		page.Page.NextCursor = encodeMessageCursor(id, view.ScopeRevision, after+len(page.Items))
	}
	fresh, err := h.conversation.sessions.Get(ctx, id)
	if err != nil || fresh.ScopeRevision != view.ScopeRevision || fresh.CreatedBy != view.CreatedBy || fresh.Status != view.Status || !slices.Equal(fresh.AllowedNodeIDs, view.AllowedNodeIDs) {
		messageProblem(w, ErrToolDenied)
		return
	}
	h.send(w, ctx, page)
}

func decodeMessageInput(w http.ResponseWriter, r *http.Request) (MessageInput, error) {
	var out MessageInput
	if len(r.Header.Values("Content-Type")) != 1 || r.Header.Get("Content-Encoding") != "" {
		return out, ErrToolDenied
	}
	media, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" || len(params) > 1 || params["charset"] != "" && !strings.EqualFold(params["charset"], "utf-8") {
		return out, ErrToolDenied
	}
	for key := range params {
		if key != "charset" {
			return out, ErrToolDenied
		}
	}
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 512<<10))
	if err != nil || !utf8.Valid(raw) {
		return out, ErrToolDenied
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	token, err := d.Token()
	if err != nil || token != json.Delim('{') {
		return out, ErrToolDenied
	}
	seen := map[string]bool{}
	for d.More() {
		token, err = d.Token()
		key, ok := token.(string)
		if err != nil || !ok || seen[key] || key != "content" && key != "clientMessageId" && key != "scopeRevision" {
			return out, ErrToolDenied
		}
		seen[key] = true
		var value json.RawMessage
		if d.Decode(&value) != nil || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return out, ErrToolDenied
		}
		switch key {
		case "content":
			err = json.Unmarshal(value, &out.Content)
		case "clientMessageId":
			err = json.Unmarshal(value, &out.ClientMessageID)
		case "scopeRevision":
			err = json.Unmarshal(value, &out.ScopeRevision)
		}
		if err != nil {
			return out, ErrToolDenied
		}
	}
	if token, err = d.Token(); err != nil || token != json.Delim('}') {
		return out, ErrToolDenied
	}
	if _, err = d.Token(); err != io.EOF || len(seen) != 3 || !validMessageContent(out.Content) || !exactID(out.ClientMessageID) {
		return out, ErrToolDenied
	}
	return out, nil
}

func (h *MessagesHTTP) submit(w http.ResponseWriter, r *http.Request) {
	ctx, ok := h.observed(w, r)
	if !ok {
		return
	}
	keys := r.Header.Values("Idempotency-Key")
	if r.URL.RawQuery != "" || len(keys) != 1 || len(keys[0]) < 8 || !exactID(keys[0]) {
		messageStreamProblem(w, 400, "INVALID_REQUEST")
		return
	}
	input, err := decodeMessageInput(w, r)
	if err != nil {
		messageStreamProblem(w, 400, "INVALID_REQUEST")
		return
	}
	if input.ScopeRevision == 0 {
		messageProblem(w, ErrScopeConflict)
		return
	}
	if err = h.commitGate(ctx); err != nil || ctx.Err() != nil {
		messageStreamProblem(w, auth.IdentityHTTPStatus(err), "AUTHENTICATION_REQUIRED")
		return
	}
	m, err := h.conversation.Submit(ctx, r.PathValue("sessionId"), keys[0], input)
	if err != nil {
		messageProblem(w, err)
		return
	}
	h.send(w, ctx, m)
}
