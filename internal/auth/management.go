package auth

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"errors"
	"slices"
	"sort"
	"time"
)

var ErrRevisionConflict = errors.New("auth: revision conflict")

// ResourceResolver is trusted composition code. It must return CURRENT exact
// node IDs for the resource (and reject nonexistent/inaccessible resources).
// Group grants freeze these IDs and later intersect current membership, so a
// new member never silently inherits an earlier grant. No resolver fails closed.
type ResourceResolver func(context.Context, string, string) ([]string, error)
type PermissionDefinition struct {
	Key          string `json:"key"`
	Description  string `json:"description"`
	ResourceType string `json:"resourceType,omitempty"`
	ReadOnly     bool   `json:"-"`
	AdminOnly    bool   `json:"-"`
}
type RoleDefinition struct {
	RoleID      string   `json:"roleId"`
	Label       string   `json:"label"`
	Permissions []string `json:"permissions"`
	BuiltIn     bool     `json:"builtIn"`
}
type ManagedUser struct {
	UserID      string   `json:"userId"`
	Username    string   `json:"username"`
	DisplayName string   `json:"displayName"`
	RoleIDs     []string `json:"roleIds"`
	Enabled     bool     `json:"enabled"`
	Revision    uint64   `json:"revision"`
}
type UserCreation struct {
	Username, DisplayName, Password string
	RoleIDs                         []string
	Enabled                         bool
}
type UserChange struct {
	DisplayName *string
	RoleIDs     *[]string
	Enabled     *bool
}
type GrantRequest struct {
	PrincipalID  string     `json:"principalId"`
	ResourceType string     `json:"resourceType"`
	ResourceID   string     `json:"resourceId"`
	Permissions  []string   `json:"permissions"`
	ExpiresAt    *time.Time `json:"expiresAt,omitempty"`
}
type ResourceGrant struct {
	GrantID      string     `json:"grantId"`
	PrincipalID  string     `json:"principalId"`
	ResourceType string     `json:"resourceType"`
	ResourceID   string     `json:"resourceId"`
	Permissions  []string   `json:"permissions"`
	CreatedAt    time.Time  `json:"createdAt"`
	ExpiresAt    *time.Time `json:"expiresAt,omitempty"`
}
type identityGrant struct {
	Value ResourceGrant
	Nodes []string
}

func NewIdentityServiceWithResources(store IdentityStore, resolver ResourceResolver, permissions []PermissionDefinition) (*IdentityService, error) {
	return newIdentityService(store, resolver, permissions)
}
func newIdentityService(store IdentityStore, resolver ResourceResolver, extra []PermissionDefinition) (*IdentityService, error) {
	if store == nil {
		return nil, ErrIdentityUnavailable
	}
	s := &IdentityService{store: store, now: time.Now, random: rand.Reader, resources: resolver, permissions: map[string]PermissionDefinition{}}
	for _, p := range []PermissionDefinition{{"nodes.read", "Read explicitly granted nodes", "node", true, false}, {"tasks.submit", "Submit controlled tasks", "node", false, false}, {"tasks.read", "Read granted-node tasks", "node", true, false}, {"audit.read", "Read permitted audit events", "node", true, false}} {
		s.permissions[p.Key] = p
	}
	for _, p := range extra {
		if !validPolicyID(p.Key) || p.Description == "" || len(p.Description) > 256 || p.ResourceType != "node" {
			return nil, ErrInvalidCheck
		}
		if _, exists := s.permissions[p.Key]; exists {
			return nil, ErrInvalidCheck
		}
		s.permissions[p.Key] = p
	}
	return s, nil
}
func validRoleIDs(ids []string) bool {
	if !validExactIDs(ids) || len(ids) > 3 {
		return false
	}
	for _, id := range ids {
		if id != "administrator" && id != "operator" && id != "reader" {
			return false
		}
	}
	return true
}

func containsAdministrator(ids []string) bool { return slices.Contains(ids, "administrator") }
func validExactIDs(ids []string) bool {
	if len(ids) > 1000 {
		return false
	}
	seen := map[string]bool{}
	for _, id := range ids {
		if !validPolicyID(id) || seen[id] {
			return false
		}
		seen[id] = true
	}
	return true
}
func validResourceType(t string) bool {
	return t == "node" || t == "node_group" || t == "service_group"
}
func userView(u identityUser) ManagedUser {
	return ManagedUser{u.ID, u.Username, u.DisplayName, append([]string{}, u.RoleIDs...), !u.Disabled, u.Revision}
}
func managementActor(st *identityState, secret, csrf string, mutation bool, now time.Time) (identityUser, error) {
	u, se, err := findIdentity(st, secret, now)
	if err != nil {
		return identityUser{}, err
	}
	if mutation && (!validSecret(csrf) || subtle.ConstantTimeCompare([]byte(csrf), []byte(se.CSRF)) != 1) {
		return identityUser{}, ErrDenied
	}
	if !u.Administrator {
		return identityUser{}, ErrDenied
	}
	return u, nil
}
func (s *IdentityService) PermissionCatalog() []PermissionDefinition {
	if s == nil {
		return nil
	}
	out := make([]PermissionDefinition, 0, len(s.permissions))
	for _, p := range s.permissions {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}
func (s *IdentityService) RoleCatalog() []RoleDefinition {
	out := []RoleDefinition{{"administrator", "Administrator", []string{}, true}, {"operator", "Operator", []string{}, true}, {"reader", "Read only", []string{}, true}}
	for _, p := range s.PermissionCatalog() {
		out[0].Permissions = append(out[0].Permissions, p.Key)
		if !p.AdminOnly {
			out[1].Permissions = append(out[1].Permissions, p.Key)
			if p.ReadOnly {
				out[2].Permissions = append(out[2].Permissions, p.Key)
			}
		}
	}
	return out
}
func (s *IdentityService) roleAllows(u identityUser, action string) bool {
	for _, role := range s.RoleCatalog() {
		if slices.Contains(u.RoleIDs, role.RoleID) && slices.Contains(role.Permissions, action) {
			return true
		}
	}
	return false
}

func (s *IdentityService) CreateUser(ctx context.Context, secret, csrf string, input UserCreation) (ManagedUser, error) {
	return s.createUser(ctx, secret, csrf, input, "")
}
func (s *IdentityService) CreateUserIdempotent(ctx context.Context, secret, csrf string, input UserCreation, key string) (ManagedUser, error) {
	return s.createUser(ctx, secret, csrf, input, key)
}
func (s *IdentityService) createUser(ctx context.Context, secret, csrf string, input UserCreation, key string) (ManagedUser, error) {
	var result ManagedUser
	input.RoleIDs = append([]string{}, input.RoleIDs...)
	if !validUsername(input.Username) || len(input.DisplayName) > 128 || !validRoleIDs(input.RoleIDs) {
		return result, ErrInvalidCheck
	}
	err := s.update(ctx, func(st *identityState, now time.Time) error {
		actor, err := managementActor(st, secret, csrf, true, now)
		if err != nil {
			return err
		}
		// Never store a fast digest of a password-containing request. Replay
		// checks retain the original salted KDF and hash only non-secret metadata.
		metadata := input
		metadata.Password = ""
		digest := requestDigest(metadata)
		receipt, replay, err := loadReceipt(st, actor.ID, "auth.user.create", key, digest, now)
		if err != nil {
			return err
		}
		if replay {
			if !checkPassword(input.Password, receipt.Password) {
				return ErrIdempotencyConflict
			}
			u, ok := st.Users[receipt.EntityID]
			if !ok {
				return ErrIdentityUnavailable
			}
			result = userView(u)
			return nil
		}
		if len(st.Users) >= 10000 {
			return ErrIdentityUnavailable
		}
		for _, u := range st.Users {
			if u.Username == input.Username {
				return ErrRevisionConflict
			}
		}
		pw, err := makePassword(input.Password, s.random)
		if err != nil {
			return err
		}
		id, err := randomIdentity(s.random)
		if err != nil {
			return err
		}
		name := input.DisplayName
		if name == "" {
			name = input.Username
		}
		u := identityUser{ID: id, Username: input.Username, Password: pw, DisplayName: name, RoleIDs: input.RoleIDs, Administrator: slices.Contains(input.RoleIDs, "administrator"), Disabled: !input.Enabled, Epoch: 1, Revision: 1}
		st.Users[id] = u
		saveReceipt(st, actor.ID, "auth.user.create", key, digest, id, pw, now)
		st.PolicyRevision++
		result = userView(u)
		return appendIdentityAudit(st, actor.ID, "auth.user.create", "succeeded", now)
	})
	if err != nil {
		return ManagedUser{}, err
	}
	return result, nil
}
func (s *IdentityService) ListUsers(ctx context.Context, secret string) ([]ManagedUser, error) {
	var result []ManagedUser
	err := s.read(ctx, func(st *identityState, now time.Time) error {
		if _, err := managementActor(st, secret, "", false, now); err != nil {
			return err
		}
		result = make([]ManagedUser, 0, len(st.Users))
		for _, u := range st.Users {
			result = append(result, userView(u))
		}
		sort.Slice(result, func(i, j int) bool { return result[i].UserID < result[j].UserID })
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}
func (s *IdentityService) PatchUser(ctx context.Context, secret, csrf, userID string, expected uint64, change UserChange) (ManagedUser, error) {
	var result ManagedUser
	if expected == 0 {
		return result, ErrRevisionConflict
	}
	if change.RoleIDs != nil {
		copy := append([]string{}, (*change.RoleIDs)...)
		change.RoleIDs = &copy
		if !validRoleIDs(copy) {
			return result, ErrInvalidCheck
		}
	}
	if change.DisplayName != nil && len(*change.DisplayName) > 128 {
		return result, ErrInvalidCheck
	}
	err := s.update(ctx, func(st *identityState, now time.Time) error {
		actor, err := managementActor(st, secret, csrf, true, now)
		if err != nil {
			return err
		}
		u, exists := st.Users[userID]
		if !exists {
			return ErrDenied
		}
		if u.Revision != expected {
			return ErrRevisionConflict
		}
		if change.DisplayName == nil && change.RoleIDs == nil && change.Enabled == nil {
			result = userView(u)
			return nil
		}
		if change.DisplayName != nil {
			u.DisplayName = *change.DisplayName
			if u.DisplayName == "" {
				u.DisplayName = u.Username
			}
		}
		if change.RoleIDs != nil {
			u.RoleIDs = *change.RoleIDs
			u.Administrator = slices.Contains(u.RoleIDs, "administrator")
		}
		if change.Enabled != nil {
			u.Disabled = !*change.Enabled
		}
		if st.Users[userID].Administrator && !st.Users[userID].Disabled && (!u.Administrator || u.Disabled) {
			count := 0
			for _, other := range st.Users {
				if other.Administrator && !other.Disabled {
					count++
				}
			}
			if count <= 1 {
				return ErrDenied
			}
		}
		u.Epoch++
		u.Revision++
		st.Users[userID] = u
		st.PolicyRevision++
		result = userView(u)
		return appendIdentityAudit(st, actor.ID, "auth.user.patch", "succeeded", now)
	})
	if err != nil {
		return ManagedUser{}, err
	}
	return result, nil
}

func (s *IdentityService) CreateResourceGrant(ctx context.Context, secret, csrf string, input GrantRequest) (ResourceGrant, error) {
	return s.createResourceGrant(ctx, secret, csrf, input, "")
}
func (s *IdentityService) CreateResourceGrantIdempotent(ctx context.Context, secret, csrf string, input GrantRequest, key string) (ResourceGrant, error) {
	return s.createResourceGrant(ctx, secret, csrf, input, key)
}
func (s *IdentityService) createResourceGrant(ctx context.Context, secret, csrf string, input GrantRequest, key string) (ResourceGrant, error) {
	var result ResourceGrant
	input.Permissions = append([]string{}, input.Permissions...)
	if input.ExpiresAt != nil {
		expires := input.ExpiresAt.UTC()
		input.ExpiresAt = &expires
	}
	if !validPolicyID(input.PrincipalID) || !validResourceType(input.ResourceType) || !validPolicyID(input.ResourceID) || len(input.Permissions) == 0 || !validExactIDs(input.Permissions) {
		return result, ErrInvalidCheck
	}
	for _, permission := range input.Permissions {
		if _, ok := s.permissions[permission]; !ok {
			return result, ErrInvalidCheck
		}
	}
	err := s.update(ctx, func(st *identityState, now time.Time) error {
		actor, err := managementActor(st, secret, csrf, true, now)
		if err != nil {
			return err
		}
		digest := requestDigest(input)
		receipt, replay, err := loadReceipt(st, actor.ID, "auth.grant.create", key, digest, now)
		if err != nil {
			return err
		}
		if replay {
			grant, ok := st.Grants[receipt.EntityID]
			if !ok {
				return ErrDenied
			}
			if grant.Value.ExpiresAt != nil && !now.Before(*grant.Value.ExpiresAt) {
				return ErrDenied
			}
			result = grant.Value
			return nil
		}
		if _, ok := st.Users[input.PrincipalID]; !ok {
			return ErrDenied
		}
		if input.ExpiresAt != nil && !now.Before(*input.ExpiresAt) {
			return ErrInvalidCheck
		}
		if len(st.Grants) >= 10000 || s.resources == nil {
			return ErrIdentityUnavailable
		}
		nodes, err := s.resources(ctx, input.ResourceType, input.ResourceID)
		if err != nil {
			return ErrPolicyUnavailable
		}
		if !validExactIDs(nodes) || (input.ResourceType == "node" && (len(nodes) != 1 || nodes[0] != input.ResourceID)) {
			return ErrInvalidCheck
		}
		id, err := randomIdentity(s.random)
		if err != nil {
			return err
		}
		result = ResourceGrant{id, input.PrincipalID, input.ResourceType, input.ResourceID, input.Permissions, now, input.ExpiresAt}
		st.Grants[id] = identityGrant{result, append([]string{}, nodes...)}
		saveReceipt(st, actor.ID, "auth.grant.create", key, digest, id, passwordRecord{}, now)
		st.PolicyRevision++
		return appendIdentityAudit(st, actor.ID, "auth.grant.create", "succeeded", now, nodes...)
	})
	if err != nil {
		return ResourceGrant{}, err
	}
	return result, nil
}
func (s *IdentityService) RevokeResourceGrant(ctx context.Context, secret, csrf, grantID string) error {
	return s.update(ctx, func(st *identityState, now time.Time) error {
		actor, err := managementActor(st, secret, csrf, true, now)
		if err != nil {
			return err
		}
		removed, ok := st.Grants[grantID]
		if !ok {
			return ErrDenied
		}
		delete(st.Grants, grantID)
		st.PolicyRevision++
		return appendIdentityAudit(st, actor.ID, "auth.grant.revoke", "succeeded", now, removed.Nodes...)
	})
}
func (s *IdentityService) ListResourceGrants(ctx context.Context, secret string) ([]ResourceGrant, error) {
	var result []ResourceGrant
	err := s.read(ctx, func(st *identityState, now time.Time) error {
		actor, _, err := findIdentity(st, secret, now)
		if err != nil {
			return err
		}
		result = []ResourceGrant{}
		for _, grant := range st.Grants {
			if actor.Administrator || grant.Value.PrincipalID == actor.ID {
				result = append(result, grant.Value)
			}
		}
		sort.Slice(result, func(i, j int) bool { return result[i].GrantID < result[j].GrantID })
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}
func (s *IdentityService) grantedNodes(ctx context.Context, st *identityState, subject, action string, now time.Time) (map[string]struct{}, error) {
	u, ok := st.Users[subject]
	if !ok || u.Disabled || !s.roleAllows(u, action) {
		return nil, ErrDenied
	}
	out := map[string]struct{}{}
	for _, grant := range st.Grants {
		g := grant.Value
		if g.PrincipalID != subject || !slices.Contains(g.Permissions, action) || g.ExpiresAt != nil && !now.Before(*g.ExpiresAt) {
			continue
		}
		if s.resources == nil {
			return nil, ErrPolicyUnavailable
		}
		current, err := s.resources(ctx, g.ResourceType, g.ResourceID)
		if err != nil || !validExactIDs(current) {
			return nil, ErrPolicyUnavailable
		}
		for _, node := range grant.Nodes {
			if slices.Contains(current, node) {
				out[node] = struct{}{}
			}
		}
	}
	return out, nil
}

// Authorize implements the existing port for ALREADY authenticated subjects.
// It does not turn a SubjectID or role name supplied by a client into identity.
// Every target must be explicitly granted; even administrators get no wildcard.
func (s *IdentityService) Authorize(ctx context.Context, check Check) error {
	check.NodeIDs = append([]string(nil), check.NodeIDs...)
	if !validPolicyID(check.SubjectID) || !validPolicyID(check.Action) || len(check.NodeIDs) == 0 || !validExactIDs(check.NodeIDs) {
		return ErrInvalidCheck
	}
	return s.read(ctx, func(st *identityState, now time.Time) error {
		allowed, err := s.grantedNodes(ctx, st, check.SubjectID, check.Action, now)
		if err != nil {
			return err
		}
		for _, id := range check.NodeIDs {
			if _, ok := allowed[id]; !ok {
				return ErrDenied
			}
		}
		return nil
	})
}

var _ Authorizer = (*IdentityService)(nil)
