package transport

import (
	"context"
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"
)

// EnrollmentPrincipal is trusted Web composition data for enrollment management.
// The service gate reads it from the SAME request context; it is not a credential
// and never comes from a request body.
type EnrollmentPrincipal struct {
	SubjectID     string
	RequestID     string
	Administrator bool
}

type enrollmentPrincipalKey struct{}

func WithEnrollmentPrincipal(ctx context.Context, principal EnrollmentPrincipal) context.Context {
	return context.WithValue(ctx, enrollmentPrincipalKey{}, principal)
}

func EnrollmentPrincipalFromContext(ctx context.Context) (EnrollmentPrincipal, bool) {
	if ctx == nil {
		return EnrollmentPrincipal{}, false
	}
	principal, ok := ctx.Value(enrollmentPrincipalKey{}).(EnrollmentPrincipal)
	return principal, ok
}

// EnrollmentManagementHTTP exposes one-time token issuance, the pending request
// list and human decisions. It never lists token secrets and never approves an
// enrollment without the service's transaction-bound gate.
type EnrollmentManagementHTTP struct {
	service        *EnrollmentService
	authenticate   func(*http.Request) (EnrollmentPrincipal, error)
	ensureEnrolled func(context.Context, EnrollmentPrincipal, EnrollmentView) error
	mux            *http.ServeMux
}

func NewEnrollmentManagementHTTP(service *EnrollmentService, authenticate func(*http.Request) (EnrollmentPrincipal, error), ensureEnrolled func(context.Context, EnrollmentPrincipal, EnrollmentView) error) (*EnrollmentManagementHTTP, error) {
	if service == nil || authenticate == nil {
		return nil, ErrEnrollmentUnavailable
	}
	h := &EnrollmentManagementHTTP{service: service, authenticate: authenticate, ensureEnrolled: ensureEnrolled, mux: http.NewServeMux()}
	for _, path := range []string{"/api/v1/enrollment-tokens", "/api/v1/node-enrollment-tokens"} {
		h.mux.HandleFunc("POST "+path, h.issueToken)
	}
	for _, path := range []string{"/api/v1/enrollments", "/api/v1/node-enrollments"} {
		h.mux.HandleFunc("GET "+path, h.list)
	}
	for _, path := range []string{"/api/v1/enrollments/{enrollmentId}/decision", "/api/v1/node-enrollments/{enrollmentId}/decision"} {
		h.mux.HandleFunc("POST "+path, h.decide)
	}
	return h, nil
}

func (h *EnrollmentManagementHTTP) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if r.TLS == nil {
		writeTransportProblem(w, http.StatusForbidden, "NOT_AUTHORIZED")
		return
	}
	h.mux.ServeHTTP(w, r)
}

func (h *EnrollmentManagementHTTP) principal(w http.ResponseWriter, r *http.Request) (EnrollmentPrincipal, bool) {
	principal, err := h.authenticate(r)
	if err != nil || !principal.Administrator || !validBindingID(principal.SubjectID) || len(principal.SubjectID) > 128 {
		writeTransportProblem(w, http.StatusForbidden, "NOT_AUTHORIZED")
		return EnrollmentPrincipal{}, false
	}
	return principal, true
}

func decodeManagementJSON(w http.ResponseWriter, r *http.Request, value any) bool {
	if len(r.Header.Values("Content-Type")) != 1 || r.Header.Get("Content-Encoding") != "" || r.URL.RawQuery != "" {
		writeTransportProblem(w, http.StatusBadRequest, "INVALID_REQUEST")
		return false
	}
	media, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" || len(params) > 1 || params["charset"] != "" && !strings.EqualFold(params["charset"], "utf-8") {
		writeTransportProblem(w, http.StatusBadRequest, "INVALID_REQUEST")
		return false
	}
	for key := range params {
		if key != "charset" {
			writeTransportProblem(w, http.StatusBadRequest, "INVALID_REQUEST")
			return false
		}
	}
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 64<<10))
	if err != nil || !utf8.Valid(raw) || len(strings.TrimSpace(string(raw))) == 0 || strings.TrimSpace(string(raw))[0] != '{' {
		writeTransportProblem(w, http.StatusBadRequest, "INVALID_REQUEST")
		return false
	}
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if decoder.Decode(value) != nil {
		writeTransportProblem(w, http.StatusBadRequest, "INVALID_REQUEST")
		return false
	}
	if _, err := decoder.Token(); err != io.EOF {
		writeTransportProblem(w, http.StatusBadRequest, "INVALID_REQUEST")
		return false
	}
	return true
}

func writeManagementJSON(w http.ResponseWriter, status int, value any) {
	raw, err := json.Marshal(value)
	if err != nil || len(raw) > 512<<10 {
		writeTransportProblem(w, http.StatusServiceUnavailable, "ENROLLMENT_SOURCE_UNAVAILABLE")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(raw)
}

type enrollmentTokenResponse struct {
	ID        string    `json:"id"`
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expiresAt"`
}

func (h *EnrollmentManagementHTTP) issueToken(w http.ResponseWriter, r *http.Request) {
	principal, ok := h.principal(w, r)
	if !ok {
		return
	}
	var input struct {
		Name          string `json:"name"`
		ExpiryMinutes int    `json:"expiryMinutes"`
		MaxUses       int    `json:"maxUses"`
	}
	if !decodeManagementJSON(w, r, &input) {
		return
	}
	if input.ExpiryMinutes == 0 {
		input.ExpiryMinutes = 10
	}
	if input.MaxUses == 0 {
		input.MaxUses = 1
	}
	if input.MaxUses != 1 || input.ExpiryMinutes < 1 || input.ExpiryMinutes > 10 {
		writeTransportProblem(w, http.StatusBadRequest, "ENROLLMENT_TOKEN_POLICY")
		return
	}
	if input.Name == "" {
		input.Name = "enrollment-token"
	}
	if len(input.Name) > 128 {
		writeTransportProblem(w, http.StatusBadRequest, "INVALID_REQUEST")
		return
	}
	ctx := WithEnrollmentPrincipal(r.Context(), principal)
	token, err := h.service.IssueToken(ctx, EnrollmentOperation{
		ActorID: principal.SubjectID,
		Action:  "enrollment.issue",
	}, EnrollmentTokenInput{Name: input.Name, TTLSeconds: input.ExpiryMinutes * 60})
	if err != nil {
		writeEnrollmentError(w, err)
		return
	}
	writeManagementJSON(w, http.StatusCreated, enrollmentTokenResponse{ID: token.ID, Token: token.Token, ExpiresAt: token.ExpiresAt})
}

func (h *EnrollmentManagementHTTP) list(w http.ResponseWriter, r *http.Request) {
	principal, ok := h.principal(w, r)
	if !ok {
		return
	}
	if r.ContentLength > 0 || len(r.TransferEncoding) != 0 || r.URL.RawQuery != "" {
		writeTransportProblem(w, http.StatusBadRequest, "INVALID_REQUEST")
		return
	}
	ctx := WithEnrollmentPrincipal(r.Context(), principal)
	views, err := h.service.List(ctx, EnrollmentOperation{ActorID: principal.SubjectID, Action: "enrollment.list"})
	if err != nil {
		writeEnrollmentError(w, err)
		return
	}
	writeManagementJSON(w, http.StatusOK, map[string]any{"items": views})
}

func (h *EnrollmentManagementHTTP) decide(w http.ResponseWriter, r *http.Request) {
	principal, ok := h.principal(w, r)
	if !ok {
		return
	}
	enrollmentID := r.PathValue("enrollmentId")
	if !validBindingID(enrollmentID) {
		writeTransportProblem(w, http.StatusBadRequest, "INVALID_REQUEST")
		return
	}
	var input struct {
		Decision string `json:"decision"`
		Reason   string `json:"reason"`
	}
	if !decodeManagementJSON(w, r, &input) {
		return
	}
	if input.Decision != "approve" && input.Decision != "reject" || len(input.Reason) > 1024 {
		writeTransportProblem(w, http.StatusBadRequest, "INVALID_REQUEST")
		return
	}
	ctx := WithEnrollmentPrincipal(r.Context(), principal)
	views, err := h.service.List(ctx, EnrollmentOperation{ActorID: principal.SubjectID, Action: "enrollment.list"})
	if err != nil {
		writeEnrollmentError(w, err)
		return
	}
	var current EnrollmentView
	found := false
	for _, view := range views {
		if view.ID == enrollmentID {
			current = view
			found = true
			break
		}
	}
	if !found {
		writeTransportProblem(w, http.StatusNotFound, "ENROLLMENT_NOT_FOUND")
		return
	}
	if current.Status != "pending" {
		writeTransportProblem(w, http.StatusConflict, "ENROLLMENT_NOT_PENDING")
		return
	}
	view, err := h.service.Decide(ctx, EnrollmentOperation{
		ActorID:          principal.SubjectID,
		Action:           "enrollment.decide",
		EnrollmentID:     enrollmentID,
		NodeID:           current.NodeID,
		ExpectedRevision: current.Revision,
	}, input.Decision == "approve")
	if err != nil {
		writeEnrollmentError(w, err)
		return
	}
	if view.Status == "approved" && h.ensureEnrolled != nil {
		if err := h.ensureEnrolled(r.Context(), principal, view); err != nil {
			writeTransportProblem(w, http.StatusServiceUnavailable, "ENROLLMENT_NODE_UNAVAILABLE")
			return
		}
	}
	writeManagementJSON(w, http.StatusOK, view)
}

func writeEnrollmentError(w http.ResponseWriter, err error) {
	switch {
	case err == ErrEnrollmentRejected:
		writeTransportProblem(w, http.StatusForbidden, "ENROLLMENT_REJECTED")
	case err == ErrEnrollmentUnavailable:
		writeTransportProblem(w, http.StatusServiceUnavailable, "ENROLLMENT_SOURCE_UNAVAILABLE")
	default:
		writeTransportProblem(w, http.StatusServiceUnavailable, "ENROLLMENT_SOURCE_UNAVAILABLE")
	}
}
