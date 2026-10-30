package auth

import (
	"context"
	"time"
)

type identityAuditContextKey struct{}
type identityActorContextKey struct{}

func (s *IdentityService) recordHTTPDenial(ctx context.Context, subject string) error {
	return s.update(ctx, func(st *identityState, now time.Time) error {
		if _, ok := st.Users[subject]; !ok {
			return ErrIdentityUnavailable
		}
		return appendIdentityAudit(st, subject, "auth.http.denied", "denied", now)
	})
}

// IdentityAuditEvent is redacted structured data for the same-transaction sink.
// It deliberately has no user text, request body, credential, cookie or token.
type IdentityAuditEvent struct {
	SubjectID string
	Action    string
	Result    string
	RequestID string
	Source    string
	At        time.Time
	NodeIDs   []string
}

func identityAuditEvents(records []identityAudit) ([]IdentityAuditEvent, error) {
	events := make([]IdentityAuditEvent, 0, len(records))
	for i, r := range records {
		result := ""
		switch r.Result {
		case "succeeded", "prepared":
			result = "success"
		case "denied":
			result = "denied"
		case "failed":
			result = "failure"
		default:
			return nil, ErrIdentityUnavailable
		}
		source := r.Source
		if source == "" {
			source = "system"
		}
		if source != "web" && source != "system" {
			return nil, ErrIdentityUnavailable
		}
		requestID := r.RequestID
		if requestID == "" {
			requestID = requestDigest(struct {
				Record identityAudit
				Index  int
			}{r, i})
		}
		events = append(events, IdentityAuditEvent{SubjectID: r.SubjectID, Action: r.Action, Result: result, RequestID: requestID, Source: source, At: r.At, NodeIDs: append([]string{}, r.NodeIDs...)})
	}
	return events, nil
}
