package auth

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"
)

// IdentityHTTP is a candidate handler, not automatically mounted by Controller.
// It can be tested against the real persistent service before public integration
// review. Direct HTTPS is required; no forwarding header is trusted as TLS.
type IdentityHTTP struct {
	service     *IdentityService
	guard       *BrowserGuard
	mux         *http.ServeMux
	credentials *CredentialService
}
type IdentityHTTPConfig struct {
	Origin                 string
	RequireSecondFactor    bool
	Credentials            *CredentialService
	EnableRestrictedTokens bool
}

func NewIdentityHTTP(service *IdentityService, cfg IdentityHTTPConfig) (*IdentityHTTP, error) {
	if service == nil || cfg.RequireSecondFactor {
		return nil, ErrIdentityUnavailable
	}
	guard, err := NewBrowserGuard(cfg.Origin)
	if err != nil {
		return nil, err
	}
	if cfg.Credentials != nil && cfg.Credentials.identity != service {
		return nil, ErrIdentityUnavailable
	}
	h := &IdentityHTTP{service: service, guard: guard, mux: http.NewServeMux(), credentials: cfg.Credentials}
	h.mux.HandleFunc("GET /api/v1/auth/bootstrap-status", h.bootstrapStatus)
	h.mux.HandleFunc("POST /api/v1/auth/bootstrap", h.bootstrap)
	h.mux.HandleFunc("POST /api/v1/auth/login", h.login)
	h.mux.HandleFunc("POST /api/v1/auth/logout", h.logout)
	h.mux.HandleFunc("GET /api/v1/auth/me", h.me)
	h.mux.HandleFunc("GET /api/v1/auth/sessions", h.sessions)
	h.mux.HandleFunc("DELETE /api/v1/auth/sessions/{sessionId}", h.revokeSession)
	h.mux.HandleFunc("GET /api/v1/users", h.users)
	h.mux.HandleFunc("POST /api/v1/users", h.createUser)
	h.mux.HandleFunc("GET /api/v1/users/{userId}", h.getUser)
	h.mux.HandleFunc("PATCH /api/v1/users/{userId}", h.patchUser)
	h.mux.HandleFunc("GET /api/v1/roles", h.roles)
	h.mux.HandleFunc("GET /api/v1/permissions", h.permissions)
	h.mux.HandleFunc("GET /api/v1/resource-grants", h.grants)
	h.mux.HandleFunc("POST /api/v1/resource-grants", h.createGrant)
	h.mux.HandleFunc("DELETE /api/v1/resource-grants/{grantId}", h.revokeGrant)
	if cfg.EnableRestrictedTokens {
		h.mux.HandleFunc("GET /api/v1/auth/tokens", h.listTokens)
		h.mux.HandleFunc("POST /api/v1/auth/tokens", h.issueToken)
		h.mux.HandleFunc("DELETE /api/v1/auth/tokens/{tokenId}", h.revokeToken)
	}
	if h.credentials != nil {
		h.mux.HandleFunc("GET /api/v1/credentials", h.credentialMetadata)
		h.mux.HandleFunc("POST /api/v1/credentials", h.createCredential)
		h.mux.HandleFunc("GET /api/v1/credentials/{credentialId}/references", h.credentialReferences)
	}
	return h, nil
}
func (h *IdentityHTTP) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	id := w.Header().Get("X-Request-ID")
	var err error
	if id == "" {
		id, err = randomIdentity(rand.Reader)
	}
	if err != nil {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("X-Request-ID", id)
	r = r.WithContext(context.WithValue(r.Context(), identityAuditContextKey{}, id))
	if r.TLS == nil || h.guard.Check(r, r.Method != "GET" && r.Method != "HEAD") != nil {
		code := h.BrowserRejectionCode(r)
		if code == "" {
			code = "HTTPS_REQUIRED"
		}
		h.problemStatus(w, 403, code)
		return
	}
	h.mux.ServeHTTP(w, r)
}

func (h *IdentityHTTP) BrowserRejectionCode(r *http.Request) string {
	if h == nil || r == nil {
		return "HOST_MISMATCH"
	}
	return h.guard.RejectionCode(r, r.Method != "GET" && r.Method != "HEAD")
}

// AuthenticateRequest is the same Cookie/CSRF/Origin boundary used by all auth
// routes. Composition can adapt it to accepted DS principal ports after review.
// Every invocation reads current state; no connection-level principal is cached.
func (h *IdentityHTTP) AuthenticateRequest(r *http.Request) (Identity, error) {
	if h == nil || r == nil || r.TLS == nil {
		return Identity{}, ErrCredential
	}
	mutation := r.Method != "GET" && r.Method != "HEAD"
	if err := h.guard.Check(r, mutation); err != nil {
		return Identity{}, err
	}
	secret, err := SessionCredential(r)
	if err != nil {
		return Identity{}, err
	}
	csrf := r.Header.Values("X-CSRF-Token")
	if len(csrf) > 1 {
		return Identity{}, ErrDenied
	}
	return h.service.Authenticate(r.Context(), secret, r.Header.Get("X-CSRF-Token"), mutation)
}
func (h *IdentityHTTP) problem(w http.ResponseWriter, r *http.Request, err error) {
	if err == ErrDenied && r != nil {
		if actor, ok := r.Context().Value(identityActorContextKey{}).(Identity); ok {
			if auditErr := h.service.recordHTTPDenial(r.Context(), actor.SubjectID); auditErr != nil {
				err = ErrIdentityUnavailable
			}
		}
	}
	status := IdentityHTTPStatus(err)
	code := "IDENTITY_UNAVAILABLE"
	switch status {
	case 400:
		code = "INVALID_REQUEST"
	case 401:
		code = "CREDENTIAL_UNAVAILABLE"
	case 403:
		code = "NOT_AUTHORIZED"
	case 409:
		code = "CONFLICT"
	case 412:
		code = "REVISION_CONFLICT"
	case 429:
		code = "RATE_LIMITED"
		w.Header().Set("Retry-After", "300")
	}
	h.problemStatus(w, status, code)
}
func (h *IdentityHTTP) problemStatus(w http.ResponseWriter, status int, code string) {
	w.Header().Del("ETag")
	w.Header().Del("Set-Cookie")
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"type": "about:blank", "title": http.StatusText(status), "status": status, "code": code, "requestId": w.Header().Get("X-Request-ID")})
}
func decodeIdentityBody(w http.ResponseWriter, r *http.Request, value any) error {
	if len(r.Header.Values("Content-Type")) != 1 {
		return ErrInvalidCheck
	}
	media, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" || params["charset"] != "" && !strings.EqualFold(params["charset"], "utf-8") {
		return ErrInvalidCheck
	}
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 16384))
	if err != nil || !utf8.Valid(raw) {
		return ErrInvalidCheck
	}
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || raw[0] != '{' {
		return ErrInvalidCheck
	}
	check := json.NewDecoder(bytes.NewReader(raw))
	if err = checkIdentityJSON(check, 0); err != nil {
		return err
	}
	if _, err = check.Token(); err != io.EOF {
		return ErrInvalidCheck
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(value) != nil {
		return ErrInvalidCheck
	}
	var trailing any
	if decoder.Decode(&trailing) != io.EOF {
		return ErrInvalidCheck
	}
	return nil
}
func checkIdentityJSON(decoder *json.Decoder, depth int) error {
	if depth > 8 {
		return ErrInvalidCheck
	}
	token, err := decoder.Token()
	if err != nil {
		return ErrInvalidCheck
	}
	if token == nil {
		return ErrInvalidCheck
	}
	if delimiter, ok := token.(json.Delim); ok {
		switch delimiter {
		case '{':
			seen := map[string]bool{}
			for decoder.More() {
				name, err := decoder.Token()
				key, ok := name.(string)
				if err != nil || !ok || seen[key] {
					return ErrInvalidCheck
				}
				seen[key] = true
				if err = checkIdentityJSON(decoder, depth+1); err != nil {
					return err
				}
			}
			end, err := decoder.Token()
			if err != nil || end != json.Delim('}') {
				return ErrInvalidCheck
			}
		case '[':
			for decoder.More() {
				if err = checkIdentityJSON(decoder, depth+1); err != nil {
					return err
				}
			}
			end, err := decoder.Token()
			if err != nil || end != json.Delim(']') {
				return ErrInvalidCheck
			}
		default:
			return ErrInvalidCheck
		}
	}
	return nil
}
func (h *IdentityHTTP) authenticated(w http.ResponseWriter, r *http.Request) (Identity, string, bool) {
	id, err := h.AuthenticateRequest(r)
	if err != nil {
		h.problem(w, r, err)
		return Identity{}, "", false
	}
	secret, err := SessionCredential(r)
	if err != nil {
		h.problem(w, r, err)
		return Identity{}, "", false
	}
	*r = *r.WithContext(context.WithValue(r.Context(), identityActorContextKey{}, id))
	return id, secret, true
}
func (h *IdentityHTTP) send(w http.ResponseWriter, r *http.Request, status int, value any, identity *Identity, secret string, cookies ...*http.Cookie) {
	var body []byte
	var err error
	if value != nil {
		body, err = json.Marshal(value)
		if err != nil || len(body) > 1<<20 {
			h.problem(w, r, ErrIdentityUnavailable)
			return
		}
	}
	if identity != nil {
		if err = h.service.CommitGate(r.Context(), secret, *identity); err != nil {
			h.problem(w, r, err)
			return
		}
	}
	if r.Context().Err() != nil {
		h.problem(w, r, ErrIdentityUnavailable)
		return
	}
	for _, cookie := range cookies {
		http.SetCookie(w, cookie)
	}
	if value != nil {
		w.Header().Set("Content-Type", "application/json")
	}
	w.WriteHeader(status)
	if value != nil {
		_, _ = w.Write(append(body, '\n'))
	}
}
func (h *IdentityHTTP) bootstrapStatus(w http.ResponseWriter, r *http.Request) {
	state, err := h.service.BootstrapState(r.Context())
	if err != nil {
		h.problem(w, r, err)
		return
	}
	h.send(w, r, 200, state, nil, "")
}
func (h *IdentityHTTP) bootstrap(w http.ResponseWriter, r *http.Request) {
	prior, err := selectSessionCredential(r, true)
	if err != nil || prior != "" {
		h.problem(w, r, ErrCredential)
		return
	}
	key, err := identityIdempotencyKey(r)
	if err != nil {
		h.problem(w, r, err)
		return
	}
	var input struct {
		SetupToken string `json:"setupToken"`
		Username   string `json:"username"`
		Password   string `json:"password"`
	}
	if err = decodeIdentityBody(w, r, &input); err != nil {
		h.problem(w, r, err)
		return
	}
	if err = h.service.BootstrapIdempotent(r.Context(), input.SetupToken, input.Username, input.Password, key); err != nil {
		h.problem(w, r, err)
		return
	}
	h.send(w, r, 204, nil, nil, "")
}
func (h *IdentityHTTP) login(w http.ResponseWriter, r *http.Request) {
	prior, err := selectSessionCredential(r, true)
	if err != nil {
		h.problem(w, r, err)
		return
	}
	var input struct {
		Username         string `json:"username"`
		Password         string `json:"password"`
		SecondFactorCode string `json:"secondFactorCode"`
	}
	if err = decodeIdentityBody(w, r, &input); err != nil {
		h.problem(w, r, err)
		return
	}
	// A configured second factor cannot silently become password-only. Until
	// its verifier/policy is implemented this candidate explicitly fails closed.
	if input.SecondFactorCode != "" {
		h.problem(w, r, ErrIdentityUnavailable)
		return
	}
	if !validUsername(input.Username) || len(input.Password) == 0 || len(input.Password) > 1024 {
		h.problem(w, r, ErrInvalidCheck)
		return
	}
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		h.problem(w, r, ErrIdentityUnavailable)
		return
	}
	login, err := h.service.LoginReplacing(r.Context(), input.Username, input.Password, ip, prior)
	if err != nil {
		h.problem(w, r, err)
		return
	}
	if err = h.service.CommitGate(r.Context(), login.Secret, login.Identity); err != nil {
		h.problem(w, r, err)
		return
	}
	cookie, err := SessionCookie(login.Secret)
	if err != nil {
		h.problem(w, r, err)
		return
	}
	h.send(w, r, 200, map[string]any{"identity": login.Profile, "sessionExpiresAt": login.Identity.ExpiresAt, "csrfToken": login.CSRF}, &login.Identity, login.Secret, cookie)
}
func (h *IdentityHTTP) me(w http.ResponseWriter, r *http.Request) {
	id, secret, ok := h.authenticated(w, r)
	if !ok {
		return
	}
	profile, csrf, err := h.service.CurrentIdentity(r.Context(), secret)
	if err != nil {
		h.problem(w, r, err)
		return
	}
	view := struct {
		IdentityView
		CSRF string `json:"csrfToken"`
	}{profile, csrf}
	h.send(w, r, 200, view, &id, secret)
}
func (h *IdentityHTTP) logout(w http.ResponseWriter, r *http.Request) {
	id, secret, ok := h.authenticated(w, r)
	if !ok {
		return
	}
	if err := h.service.RevokeSession(r.Context(), secret, r.Header.Get("X-CSRF-Token"), id.SessionID); err != nil {
		h.problem(w, r, err)
		return
	}
	h.send(w, r, 204, nil, nil, "", ClearSessionCookie())
}
func (h *IdentityHTTP) revokeSession(w http.ResponseWriter, r *http.Request) {
	id, secret, ok := h.authenticated(w, r)
	if !ok {
		return
	}
	target := r.PathValue("sessionId")
	if err := h.service.RevokeSession(r.Context(), secret, r.Header.Get("X-CSRF-Token"), target); err != nil {
		h.problem(w, r, err)
		return
	}
	if target == id.SessionID {
		h.send(w, r, 204, nil, nil, "", ClearSessionCookie())
		return
	}
	h.send(w, r, 204, nil, nil, "")
}

type authPageMeta struct {
	NextCursor *string `json:"nextCursor,omitempty"`
	Limit      int     `json:"limit"`
	HasMore    bool    `json:"hasMore"`
}

func pageIdentityItems[T any](r *http.Request, items []T, key func(T) string) ([]T, authPageMeta, error) {
	values, err := parseIdentityQuery(r)
	if err != nil {
		return nil, authPageMeta{}, err
	}
	limit := 50
	if value := values["limit"]; value != "" {
		n, e := strconv.Atoi(value)
		if e != nil || n < 1 || n > 200 || strconv.Itoa(n) != value {
			return nil, authPageMeta{}, ErrInvalidCheck
		}
		limit = n
	}
	start := 0
	if cursor := values["cursor"]; cursor != "" {
		found := false
		for i, item := range items {
			if key(item) == cursor {
				start = i + 1
				found = true
				break
			}
		}
		if !found {
			return nil, authPageMeta{}, ErrInvalidCheck
		}
	}
	end := min(start+limit, len(items))
	out := append([]T{}, items[start:end]...)
	page := authPageMeta{Limit: limit, HasMore: end < len(items)}
	if page.HasMore && len(out) > 0 {
		next := key(out[len(out)-1])
		page.NextCursor = &next
	}
	return out, page, nil
}
func parseIdentityQuery(r *http.Request) (map[string]string, error) {
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		return nil, ErrInvalidCheck
	}
	out := map[string]string{}
	for key, values := range query {
		if (key != "limit" && key != "cursor") || len(values) != 1 || values[0] == "" {
			return nil, ErrInvalidCheck
		}
		out[key] = values[0]
	}
	return out, nil
}
func (h *IdentityHTTP) sessions(w http.ResponseWriter, r *http.Request) {
	id, secret, ok := h.authenticated(w, r)
	if !ok {
		return
	}
	items, err := h.service.ListSessions(r.Context(), secret)
	if err != nil {
		h.problem(w, r, err)
		return
	}
	pageItems, page, err := pageIdentityItems(r, items, func(v SessionView) string { return v.SessionID })
	if err != nil {
		h.problem(w, r, err)
		return
	}
	h.send(w, r, 200, map[string]any{"items": pageItems, "page": page}, &id, secret)
}
func (h *IdentityHTTP) users(w http.ResponseWriter, r *http.Request) {
	id, secret, ok := h.authenticated(w, r)
	if !ok {
		return
	}
	items, err := h.service.ListUsers(r.Context(), secret)
	if err != nil {
		h.problem(w, r, err)
		return
	}
	pageItems, page, err := pageIdentityItems(r, items, func(v ManagedUser) string { return v.UserID })
	if err != nil {
		h.problem(w, r, err)
		return
	}
	h.send(w, r, 200, map[string]any{"items": pageItems, "page": page}, &id, secret)
}
func (h *IdentityHTTP) getUser(w http.ResponseWriter, r *http.Request) {
	id, secret, ok := h.authenticated(w, r)
	if !ok {
		return
	}
	user, err := h.service.GetUser(r.Context(), secret, r.PathValue("userId"))
	if err != nil {
		h.problem(w, r, err)
		return
	}
	w.Header().Set("ETag", `"`+strconv.FormatUint(user.Revision, 10)+`"`)
	h.send(w, r, 200, user, &id, secret)
}
func identityIdempotencyKey(r *http.Request) (string, error) {
	values := r.Header.Values("Idempotency-Key")
	if len(values) != 1 || len(values[0]) < 16 || len(values[0]) > 128 || !validPolicyID(values[0]) {
		return "", ErrInvalidCheck
	}
	return values[0], nil
}
func (h *IdentityHTTP) createUser(w http.ResponseWriter, r *http.Request) {
	_, secret, ok := h.authenticated(w, r)
	if !ok {
		return
	}
	key, err := identityIdempotencyKey(r)
	if err != nil {
		h.problem(w, r, err)
		return
	}
	var input struct {
		Username    string   `json:"username"`
		DisplayName string   `json:"displayName"`
		Password    string   `json:"password"`
		RoleIDs     []string `json:"roleIds"`
		Enabled     *bool    `json:"enabled"`
	}
	if err = decodeIdentityBody(w, r, &input); err != nil {
		h.problem(w, r, err)
		return
	}
	if input.RoleIDs == nil {
		h.problem(w, r, ErrInvalidCheck)
		return
	}
	enabled := true
	if input.Enabled != nil {
		enabled = *input.Enabled
	}
	user, err := h.service.CreateUserIdempotent(r.Context(), secret, r.Header.Get("X-CSRF-Token"), UserCreation{input.Username, input.DisplayName, input.Password, input.RoleIDs, enabled}, key)
	if err != nil {
		h.problem(w, r, err)
		return
	}
	w.Header().Set("ETag", `"`+strconv.FormatUint(user.Revision, 10)+`"`)
	h.send(w, r, 200, user, nil, "")
}
func (h *IdentityHTTP) patchUser(w http.ResponseWriter, r *http.Request) {
	_, secret, ok := h.authenticated(w, r)
	if !ok {
		return
	}
	values := r.Header.Values("If-Match")
	if len(values) == 0 {
		h.problemStatus(w, 428, "PRECONDITION_REQUIRED")
		return
	}
	if len(values) != 1 || len(values[0]) < 3 || values[0][0] != '"' || values[0][len(values[0])-1] != '"' {
		h.problem(w, r, ErrInvalidCheck)
		return
	}
	number := values[0][1 : len(values[0])-1]
	revision, err := strconv.ParseUint(number, 10, 64)
	if err != nil || revision == 0 || strconv.FormatUint(revision, 10) != number {
		h.problem(w, r, ErrInvalidCheck)
		return
	}
	var input struct {
		DisplayName *string   `json:"displayName"`
		RoleIDs     *[]string `json:"roleIds"`
		Enabled     *bool     `json:"enabled"`
	}
	if err = decodeIdentityBody(w, r, &input); err != nil {
		h.problem(w, r, err)
		return
	}
	user, err := h.service.PatchUser(r.Context(), secret, r.Header.Get("X-CSRF-Token"), r.PathValue("userId"), revision, UserChange{input.DisplayName, input.RoleIDs, input.Enabled})
	if err != nil {
		h.problem(w, r, err)
		return
	}
	w.Header().Set("ETag", `"`+strconv.FormatUint(user.Revision, 10)+`"`)
	h.send(w, r, 200, user, nil, "")
}
func (h *IdentityHTTP) roles(w http.ResponseWriter, r *http.Request) {
	id, secret, ok := h.authenticated(w, r)
	if ok {
		h.send(w, r, 200, map[string]any{"items": h.service.RoleCatalog()}, &id, secret)
	}
}
func (h *IdentityHTTP) permissions(w http.ResponseWriter, r *http.Request) {
	id, secret, ok := h.authenticated(w, r)
	if ok {
		h.send(w, r, 200, map[string]any{"items": h.service.PermissionCatalog()}, &id, secret)
	}
}
func (h *IdentityHTTP) grants(w http.ResponseWriter, r *http.Request) {
	id, secret, ok := h.authenticated(w, r)
	if !ok {
		return
	}
	items, err := h.service.ListResourceGrants(r.Context(), secret)
	if err != nil {
		h.problem(w, r, err)
		return
	}
	pageItems, page, err := pageIdentityItems(r, items, func(v ResourceGrant) string { return v.GrantID })
	if err != nil {
		h.problem(w, r, err)
		return
	}
	h.send(w, r, 200, map[string]any{"items": pageItems, "page": page}, &id, secret)
}
func (h *IdentityHTTP) createGrant(w http.ResponseWriter, r *http.Request) {
	_, secret, ok := h.authenticated(w, r)
	if !ok {
		return
	}
	key, err := identityIdempotencyKey(r)
	if err != nil {
		h.problem(w, r, err)
		return
	}
	var input GrantRequest
	if err = decodeIdentityBody(w, r, &input); err != nil {
		h.problem(w, r, err)
		return
	}
	grant, err := h.service.CreateResourceGrantIdempotent(r.Context(), secret, r.Header.Get("X-CSRF-Token"), input, key)
	if err != nil {
		h.problem(w, r, err)
		return
	}
	h.send(w, r, 200, grant, nil, "")
}
func (h *IdentityHTTP) revokeGrant(w http.ResponseWriter, r *http.Request) {
	_, secret, ok := h.authenticated(w, r)
	if !ok {
		return
	}
	if err := h.service.RevokeResourceGrant(r.Context(), secret, r.Header.Get("X-CSRF-Token"), r.PathValue("grantId")); err != nil {
		h.problem(w, r, err)
		return
	}
	h.send(w, r, 204, nil, nil, "")
}
