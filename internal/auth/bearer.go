package auth

import (
	"context"
	"crypto/subtle"
	"net/http"
	"slices"
	"sort"
	"strings"
	"time"
)

// The initial migration allows only node/task reads. Tokens cannot authenticate
// the browser management APIs, AI, plugin Host, enrollment or execution methods.
type RestrictedTokenInput struct {
	Name       string   `json:"name"`
	Purpose    string   `json:"purpose"`
	Actions    []string `json:"actions"`
	NodeIDs    []string `json:"nodeIds"`
	TTLSeconds int      `json:"ttlSeconds"`
}
type RestrictedTokenMetadata struct {
	ID        string    `json:"tokenId"`
	Name      string    `json:"name"`
	Purpose   string    `json:"purpose"`
	Actions   []string  `json:"actions"`
	NodeIDs   []string  `json:"nodeIds"`
	CreatedAt time.Time `json:"createdAt"`
	ExpiresAt time.Time `json:"expiresAt"`
	Revoked   bool      `json:"revoked"`
}
type restrictedToken struct {
	RestrictedTokenMetadata
	SubjectID, Digest string
	Epoch             uint64
}
type IssuedRestrictedToken struct {
	Metadata RestrictedTokenMetadata `json:"metadata"`
	Secret   string                  `json:"token"`
}

func (t IssuedRestrictedToken) String() string   { return "auth.IssuedRestrictedToken{secret:redacted}" }
func (t IssuedRestrictedToken) GoString() string { return t.String() }

// BearerIdentity is intentionally distinct from a browser session Identity.
// A caller must not synthesize a session or pass SubjectID as authentication.
type BearerIdentity struct {
	SubjectID, TokenID    string
	Epoch, PolicyRevision uint64
	ExpiresAt             time.Time
}

func validTokenCaps(actions, nodes []string) bool {
	if len(actions) == 0 || len(actions) > 2 || len(nodes) == 0 || len(nodes) > 200 || !validExactIDs(actions) || !validExactIDs(nodes) {
		return false
	}
	for _, a := range actions {
		if a != "nodes.read" && a != "tasks.read" {
			return false
		}
	}
	for _, n := range nodes {
		if len(n) > 128 {
			return false
		}
	}
	return true
}
func tokenView(t restrictedToken) RestrictedTokenMetadata {
	m := t.RestrictedTokenMetadata
	m.Actions = slices.Clone(m.Actions)
	m.NodeIDs = slices.Clone(m.NodeIDs)
	return m
}
func (s *IdentityService) IssueRestrictedToken(ctx context.Context, secret, csrf string, input RestrictedTokenInput) (IssuedRestrictedToken, error) {
	input.Actions = slices.Clone(input.Actions)
	input.NodeIDs = slices.Clone(input.NodeIDs)
	if input.Name == "" || len(input.Name) > 128 || input.Purpose != "script-read" || input.TTLSeconds < 60 || input.TTLSeconds > 3600 || !validTokenCaps(input.Actions, input.NodeIDs) {
		return IssuedRestrictedToken{}, ErrInvalidCheck
	}
	var out IssuedRestrictedToken
	err := s.update(ctx, func(st *identityState, now time.Time) error {
		u, se, err := findIdentity(st, secret, now)
		if err != nil {
			return err
		}
		if !validSecret(csrf) || subtle.ConstantTimeCompare([]byte(csrf), []byte(se.CSRF)) != 1 {
			return ErrDenied
		}
		for _, action := range input.Actions {
			allowed, err := s.grantedNodes(ctx, st, u.ID, action, now)
			if err != nil {
				return err
			}
			for _, node := range input.NodeIDs {
				if _, ok := allowed[node]; !ok {
					return ErrDenied
				}
			}
		}
		// Expired/revoked tokens do not become valid again on clock rollback: update
		// persists ClockFloor. Old plaintexts are never reissued.
		for digest, t := range st.Tokens {
			if t.Revoked || !now.Before(t.ExpiresAt) {
				delete(st.Tokens, digest)
			}
		}
		if len(st.Tokens) >= 10000 {
			return ErrIdentityUnavailable
		}
		token, err := randomIdentity(s.random)
		if err != nil {
			return err
		}
		id, err := randomIdentity(s.random)
		if err != nil {
			return err
		}
		m := RestrictedTokenMetadata{id, input.Name, input.Purpose, input.Actions, input.NodeIDs, now, now.Add(time.Duration(input.TTLSeconds) * time.Second), false}
		st.Tokens[credentialDigest(token)] = restrictedToken{m, u.ID, credentialDigest(token), u.Epoch}
		out = IssuedRestrictedToken{m, token}
		return appendIdentityAudit(st, u.ID, "auth.token.issue", "succeeded", now, input.NodeIDs...)
	})
	if err != nil {
		return IssuedRestrictedToken{}, err
	}
	return out, nil
}
func (s *IdentityService) ListRestrictedTokens(ctx context.Context, secret string) ([]RestrictedTokenMetadata, error) {
	var out []RestrictedTokenMetadata
	err := s.read(ctx, func(st *identityState, now time.Time) error {
		u, _, err := findIdentity(st, secret, now)
		if err != nil {
			return err
		}
		out = []RestrictedTokenMetadata{}
		for _, t := range st.Tokens {
			if t.SubjectID == u.ID {
				out = append(out, tokenView(t))
			}
		}
		sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
func (s *IdentityService) RevokeRestrictedToken(ctx context.Context, secret, csrf, id string) error {
	return s.update(ctx, func(st *identityState, now time.Time) error {
		u, se, err := findIdentity(st, secret, now)
		if err != nil {
			return err
		}
		if !validSecret(csrf) || subtle.ConstantTimeCompare([]byte(csrf), []byte(se.CSRF)) != 1 {
			return ErrDenied
		}
		for digest, t := range st.Tokens {
			if t.ID == id && t.SubjectID == u.ID {
				t.Revoked = true
				st.Tokens[digest] = t
				return appendIdentityAudit(st, u.ID, "auth.token.revoke", "succeeded", now, t.NodeIDs...)
			}
		}
		return ErrDenied
	})
}
func findRestrictedToken(st *identityState, secret string, now time.Time) (restrictedToken, error) {
	t, ok := st.Tokens[credentialDigest(secret)]
	u := st.Users[t.SubjectID]
	if !validSecret(secret) || !ok || t.Revoked || !now.Before(t.ExpiresAt) || u.ID == "" || u.Disabled || t.Epoch != u.Epoch {
		return restrictedToken{}, ErrCredential
	}
	return t, nil
}

// AuthorizeRestrictedToken authenticates AND intersects the stored read caps
// with current role/grants/current group membership. Every target must match.
// Invoke again immediately before buffered output; do not cache subject-only.
func (s *IdentityService) AuthorizeRestrictedToken(ctx context.Context, secret, action string, nodes []string) (BearerIdentity, error) {
	nodes = slices.Clone(nodes)
	if !validTokenCaps([]string{action}, nodes) {
		return BearerIdentity{}, ErrDenied
	}
	var out BearerIdentity
	var decision error
	err := s.update(ctx, func(st *identityState, now time.Time) error {
		decision = func() error {
			t, err := findRestrictedToken(st, secret, now)
			if err != nil {
				return err
			}
			if !slices.Contains(t.Actions, action) {
				return ErrDenied
			}
			allowed, err := s.grantedNodes(ctx, st, t.SubjectID, action, now)
			if err != nil {
				return err
			}
			for _, n := range nodes {
				if !slices.Contains(t.NodeIDs, n) {
					return ErrDenied
				}
				if _, ok := allowed[n]; !ok {
					return ErrDenied
				}
			}
			out = BearerIdentity{t.SubjectID, t.ID, t.Epoch, st.PolicyRevision, t.ExpiresAt}
			return nil
		}()
		return nil
	})
	if err != nil {
		return BearerIdentity{}, err
	}
	if decision != nil {
		return BearerIdentity{}, decision
	}
	return out, nil
}
func (s *IdentityService) BearerCommitGate(ctx context.Context, secret, action string, nodes []string, observed BearerIdentity) error {
	now, err := s.AuthorizeRestrictedToken(ctx, secret, action, nodes)
	if err != nil {
		return err
	}
	if now != observed {
		return ErrDenied
	}
	return nil
}

// RestrictedBearerCredential is an opt-in non-browser boundary for accepted
// read routes only. No forwarded TLS/Origin claims, mixed credentials or JWTs.
func RestrictedBearerCredential(r *http.Request) (string, error) {
	if r == nil || r.TLS == nil || r.Method != "GET" || len(r.Header.Values("Authorization")) != 1 || len(r.Header.Values("Cookie")) != 0 {
		return "", ErrCredential
	}
	for _, h := range []string{"Origin", "Sec-Fetch-Site", "Sec-Fetch-Mode", "Sec-Fetch-Dest", "X-CSRF-Token"} {
		if len(r.Header.Values(h)) != 0 {
			return "", ErrDenied
		}
	}
	value := r.Header.Get("Authorization")
	secret, ok := strings.CutPrefix(value, "Bearer ")
	if !ok || !validSecret(secret) {
		return "", ErrCredential
	}
	return secret, nil
}
