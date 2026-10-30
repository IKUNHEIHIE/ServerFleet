package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"serverfleet.local/serverfleet/internal/auth"
)

// MessageEventsHTTP is an unmounted candidate for the existing AI SSE path.
// Authenticate MUST run the real TLS/Origin/Cookie boundary on each call and
// attach the current trusted actor for SessionAccess. CommitGate MUST recheck
// that observation immediately before flush. Neither has a default success.
type MessageEventsHTTP struct {
	conversation *ConversationService
	authenticate func(*http.Request) (context.Context, error)
	commitGate   func(context.Context) error
	mux          *http.ServeMux
}

func NewMessageEventsHTTP(conversation *ConversationService, authenticate func(*http.Request) (context.Context, error), commitGate func(context.Context) error) (*MessageEventsHTTP, error) {
	if conversation == nil || authenticate == nil || commitGate == nil {
		return nil, ErrSessionUnavailable
	}
	h := &MessageEventsHTTP{conversation, authenticate, commitGate, http.NewServeMux()}
	h.mux.HandleFunc("GET /api/v1/ai/sessions/{sessionId}/events", h.events)
	return h, nil
}
func (h *MessageEventsHTTP) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	requestID, err := sessionID()
	if err != nil {
		messageStreamProblem(w, http.StatusServiceUnavailable, "EVENT_SOURCE_UNAVAILABLE")
		return
	}
	w.Header().Set("X-Request-ID", requestID)
	if r.TLS == nil {
		messageStreamProblem(w, http.StatusForbidden, "NOT_AUTHORIZED")
		return
	}
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		messageStreamProblem(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED")
		return
	}
	h.mux.ServeHTTP(w, r)
}
func messageStreamProblem(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"type": "about:blank", "title": http.StatusText(status), "status": status, "code": code, "requestId": w.Header().Get("X-Request-ID")})
}
func messageEventCursor(session string, sequence uint64) string {
	return session + "." + strconv.FormatUint(sequence, 10)
}
func parseMessageEventCursor(session, cursor string) (uint64, error) {
	if cursor == "" {
		return 0, nil
	}
	prefix := session + "."
	if len(cursor) > 256 || !strings.HasPrefix(cursor, prefix) {
		return 0, ErrScopeConflict
	}
	value := strings.TrimPrefix(cursor, prefix)
	sequence, err := strconv.ParseUint(value, 10, 64)
	if err != nil || strconv.FormatUint(sequence, 10) != value {
		return 0, ErrScopeConflict
	}
	return sequence, nil
}
func (h *MessageEventsHTTP) events(w http.ResponseWriter, r *http.Request) {
	if r.ContentLength > 0 || len(r.TransferEncoding) != 0 || len(r.Header.Values("Last-Event-ID")) > 1 || r.URL.RawQuery != "" {
		messageStreamProblem(w, http.StatusBadRequest, "INVALID_REQUEST")
		return
	}
	id := r.PathValue("sessionId")
	if !exactID(id) {
		messageStreamProblem(w, http.StatusBadRequest, "INVALID_REQUEST")
		return
	}
	after, err := parseMessageEventCursor(id, r.Header.Get("Last-Event-ID"))
	if err != nil {
		messageStreamProblem(w, http.StatusConflict, "INVALID_CURSOR")
		return
	}
	bounded, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	r = r.WithContext(bounded)
	controller := http.NewResponseController(w)
	started := false
	for {
		observed, err := h.authenticate(r)
		if err != nil || observed == nil || observed.Err() != nil {
			if !started {
				messageStreamProblem(w, auth.IdentityHTTPStatus(err), "AUTHENTICATION_REQUIRED")
			}
			return
		}
		events, err := h.conversation.Events(observed, id, after, 100)
		gateErr := h.commitGate(observed)
		if err != nil || gateErr != nil {
			if !started {
				status := http.StatusServiceUnavailable
				if err == ErrToolDenied {
					status = http.StatusForbidden
				} else if err == ErrScopeConflict {
					status = http.StatusConflict
				} else if gateErr != nil {
					status = auth.IdentityHTTPStatus(gateErr)
				}
				messageStreamProblem(w, status, "EVENT_SOURCE_UNAVAILABLE")
			}
			return
		}
		if !started {
			if controller.SetWriteDeadline(time.Now().Add(5*time.Second)) != nil {
				messageStreamProblem(w, http.StatusServiceUnavailable, "STREAM_UNAVAILABLE")
				return
			}
			w.Header().Set("Content-Type", "text/event-stream")
			w.Header().Set("X-Accel-Buffering", "no")
			w.WriteHeader(http.StatusOK)
			if controller.Flush() != nil {
				return
			}
			started = true
		}
		for _, event := range events {
			// Do not cache one poll's principal across a batch of writes.
			fresh, err := h.authenticate(r)
			if err != nil || fresh == nil || fresh.Err() != nil {
				return
			}
			// Session ownership/action is rechecked after reauthentication too.
			if _, err = h.conversation.Events(fresh, id, event.Sequence-1, 1); err != nil {
				return
			}
			payload, err := json.Marshal(event)
			if err != nil || bounded.Err() != nil || controller.SetWriteDeadline(time.Now().Add(5*time.Second)) != nil || h.commitGate(fresh) != nil {
				return
			}
			if _, err = fmt.Fprintf(w, "id: %s\nevent: %s\ndata: %s\n\n", messageEventCursor(id, event.Sequence), event.Kind, payload); err != nil || controller.Flush() != nil {
				return
			}
			after = event.Sequence
		}
		timer := time.NewTimer(500 * time.Millisecond)
		select {
		case <-bounded.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}
