package auth

import "net/http"

func (h *IdentityHTTP) credentialMetadata(w http.ResponseWriter, r *http.Request) {
	id, secret, ok := h.authenticated(w, r)
	if !ok {
		return
	}
	if len(r.URL.Query()) > 0 {
		h.problem(w, r, ErrInvalidCheck)
		return
	}
	items, err := h.credentials.List(r.Context(), secret)
	if err != nil {
		h.problem(w, r, err)
		return
	}
	h.send(w, r, 200, items, &id, secret)
}
func (h *IdentityHTTP) createCredential(w http.ResponseWriter, r *http.Request) {
	id, secret, ok := h.authenticated(w, r)
	if !ok {
		return
	}
	key, err := identityIdempotencyKey(r)
	if err != nil {
		h.problem(w, r, err)
		return
	}
	var input CredentialInput
	if err = decodeIdentityBody(w, r, &input); err != nil {
		h.problem(w, r, err)
		return
	}
	meta, err := h.credentials.CreateIdempotent(r.Context(), secret, r.Header.Get("X-CSRF-Token"), key, input)
	input.Secret = ""
	if err != nil {
		h.problem(w, r, err)
		return
	}
	h.send(w, r, 201, meta, &id, secret)
}
func (h *IdentityHTTP) credentialReferences(w http.ResponseWriter, r *http.Request) {
	id, secret, ok := h.authenticated(w, r)
	if !ok {
		return
	}
	items, err := h.credentials.References(r.Context(), secret, r.PathValue("credentialId"))
	if err != nil {
		h.problem(w, r, err)
		return
	}
	h.send(w, r, 200, map[string]any{"items": items}, &id, secret)
}
