package auth

import (
	"context"
	"sort"
	"time"
)

// IdentityView is the existing HTTP identity shape. It contains no credential,
// and is separate from the internal generation observation in Identity.
type IdentityView struct {
	UserID           string   `json:"userId"`
	DisplayName      string   `json:"displayName"`
	Roles            []string `json:"roles"`
	Permissions      []string `json:"permissions"`
	IsBootstrapAdmin bool     `json:"isBootstrapAdmin"`
}
type SessionView struct {
	SessionID  string    `json:"sessionId"`
	CreatedAt  time.Time `json:"createdAt"`
	ExpiresAt  time.Time `json:"expiresAt"`
	LastSeenAt time.Time `json:"lastSeenAt"`
	Current    bool      `json:"current"`
}

type BootstrapView struct {
	Initialized bool   `json:"initialized"`
	Available   bool   `json:"bootstrapAvailable"`
	Method      string `json:"setupMethod"`
}

func (s *IdentityService) BootstrapState(ctx context.Context) (BootstrapView, error) {
	var result BootstrapView
	err := s.update(ctx, func(st *identityState, now time.Time) error {
		result = BootstrapView{st.Initialized, !st.Initialized && st.SetupDigest != "" && now.Before(st.SetupExpires), "local-console-token"}
		return nil
	})
	if err != nil {
		return BootstrapView{}, err
	}
	return result, nil
}

func (s *IdentityService) CurrentIdentity(ctx context.Context, secret string) (IdentityView, string, error) {
	var result IdentityView
	var csrf string
	err := s.read(ctx, func(st *identityState, now time.Time) error {
		u, se, err := findIdentity(st, secret, now)
		if err != nil {
			return err
		}
		result = s.identityProfile(u)
		csrf = se.CSRF
		return nil
	})
	if err != nil {
		return IdentityView{}, "", err
	}
	return result, csrf, nil
}

func (s *IdentityService) identityProfile(u identityUser) IdentityView {
	permissions := []string{}
	for _, p := range s.PermissionCatalog() {
		if s.roleAllows(u, p.Key) {
			permissions = append(permissions, p.Key)
		}
	}
	return IdentityView{u.ID, u.DisplayName, append([]string{}, u.RoleIDs...), permissions, u.Bootstrap}
}

func (s *IdentityService) GetUser(ctx context.Context, secret, id string) (ManagedUser, error) {
	var result ManagedUser
	err := s.read(ctx, func(st *identityState, now time.Time) error {
		actor, _, err := findIdentity(st, secret, now)
		if err != nil {
			return err
		}
		if !actor.Administrator && actor.ID != id {
			return ErrDenied
		}
		u, ok := st.Users[id]
		if !ok {
			return ErrDenied
		}
		result = userView(u)
		return nil
	})
	if err != nil {
		return ManagedUser{}, err
	}
	return result, nil
}
func (s *IdentityService) ListSessions(ctx context.Context, secret string) ([]SessionView, error) {
	var result []SessionView
	err := s.read(ctx, func(st *identityState, now time.Time) error {
		u, current, err := findIdentity(st, secret, now)
		if err != nil {
			return err
		}
		result = []SessionView{}
		for _, se := range st.Sessions {
			if se.Subject == u.ID && !se.Revoked && se.Epoch == u.Epoch && now.Before(se.Expires) && now.Before(se.Seen.Add(30*time.Minute)) {
				result = append(result, SessionView{se.ID, se.Created, se.Expires, se.Seen, se.ID == current.ID})
			}
		}
		sort.Slice(result, func(i, j int) bool { return result[i].SessionID < result[j].SessionID })
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// NodeReadPolicy supplies a fresh subject-specific snapshot to the existing
// PrepareNodeReadScope path. It never calls inventory or reuses an old grant.
func (s *IdentityService) NodeReadPolicy(ctx context.Context, subject string) (*NodePolicy, error) {
	policy := &NodePolicy{KnownNodes: map[string]struct{}{}, ReadGrants: map[string]map[string]struct{}{}}
	err := s.read(ctx, func(st *identityState, now time.Time) error {
		nodes, err := s.grantedNodes(ctx, st, subject, NodeReadAction, now)
		if err == ErrDenied {
			return nil
		}
		if err != nil {
			return err
		}
		policy.ReadGrants[subject] = nodes
		for id := range nodes {
			policy.KnownNodes[id] = struct{}{}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return policy, nil
}

// NodeReadPolicySnapshot builds a fresh NodePolicy snapshot across all active
// subjects whose role permits nodes.read. It satisfies auth.PolicyReader for
// Controller composition without caching or granting implicit node access.
func (s *IdentityService) NodeReadPolicySnapshot(ctx context.Context) (*NodePolicy, error) {
	policy := &NodePolicy{KnownNodes: map[string]struct{}{}, ReadGrants: map[string]map[string]struct{}{}}
	err := s.read(ctx, func(st *identityState, now time.Time) error {
		for subject, u := range st.Users {
			if u.Disabled || !s.roleAllows(u, NodeReadAction) {
				continue
			}
			nodes, err := s.grantedNodes(ctx, st, subject, NodeReadAction, now)
			if err == ErrDenied {
				continue
			}
			if err != nil {
				return err
			}
			policy.ReadGrants[subject] = nodes
			for id := range nodes {
				policy.KnownNodes[id] = struct{}{}
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return policy, nil
}

// AuthorizeSubjectAction checks whether an active subject's role permits action
// for non-node-scoped catalog operations (such as audit list, task list, DNS
// account/zone catalog and notification settings). Node-targeted actions must
// still use Authorize with explicit NodeIDs.
func (s *IdentityService) AuthorizeSubjectAction(ctx context.Context, subject, action string) error {
	if !validPolicyID(subject) || !validPolicyID(action) {
		return ErrInvalidCheck
	}
	return s.read(ctx, func(st *identityState, _ time.Time) error {
		u, ok := st.Users[subject]
		if !ok || u.Disabled || !s.roleAllows(u, action) {
			return ErrDenied
		}
		return nil
	})
}
