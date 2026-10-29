package auth

import "net/http"

func (h *IdentityHTTP) listTokens(w http.ResponseWriter, r *http.Request) {
	id, secret, ok := h.authenticated(w, r)
	if !ok {
		return
	}
	items, err := h.service.ListRestrictedTokens(r.Context(), secret)
	if err != nil {
		h.problem(w, r, err)
		return
	}
	h.send(w, r, 200, map[string]any{"items": items}, &id, secret)
}
func (h *IdentityHTTP) issueToken(w http.ResponseWriter, r *http.Request) {
	id, secret, ok := h.authenticated(w, r)
	if !ok {
		return
	}
	var input RestrictedTokenInput
	if err := decodeIdentityBody(w, r, &input); err != nil {
		h.problem(w, r, err)
		return
	}
	issued, err := h.service.IssueRestrictedToken(r.Context(), secret, r.Header.Get("X-CSRF-Token"), input)
	if err != nil {
		h.problem(w, r, err)
		return
	}
	// One delivery only; list never returns this secret. Lost delivery requires
	// revoke/reissue. No token secret can authenticate this browser endpoint.
	h.send(w, r, 201, issued, &id, secret)
}
func (h *IdentityHTTP) revokeToken(w http.ResponseWriter, r *http.Request) {
	_, secret, ok := h.authenticated(w, r)
	if !ok {
		return
	}
	if err := h.service.RevokeRestrictedToken(r.Context(), secret, r.Header.Get("X-CSRF-Token"), r.PathValue("tokenId")); err != nil {
		h.problem(w, r, err)
		return
	}
	h.send(w, r, 204, nil, nil, "")
}
