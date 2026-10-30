package auth

import (
	"context"
	"crypto/pbkdf2"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"slices"
	"strings"
	"time"
)

var (
	ErrCredential          = errors.New("auth: credential unavailable")
	ErrIdentityUnavailable = errors.New("auth: identity source unavailable")
	ErrRateLimited         = errors.New("auth: login rate limited")
	ErrAlreadyInitialized  = errors.New("auth: already initialized")
)

// IdentityStore is a candidate auth-owned persistence seam, pending owner review.
// View and Update supply detached committed bytes. Update serializes writers,
// invokes fn ONCE and atomically commits its output only if fn and ctx succeed.
// Empty bytes mean a never-initialized store, NOT a missing table/read failure.
// Production adapters must preserve the initialization marker across restarts.
// There is deliberately no in-memory production fallback.
type IdentityStore interface {
	View(context.Context, func([]byte) error) error
	Update(context.Context, func([]byte) ([]byte, error)) error
}

// Identity is an authenticated observation, never accepted as a client claim.
// It is NOT resource authorization. Administrators start with zero node grants.
type Identity struct {
	SubjectID      string    `json:"subjectId"`
	Username       string    `json:"username"`
	Administrator  bool      `json:"administrator"`
	SessionID      string    `json:"sessionId"`
	Epoch          uint64    `json:"-"`
	PolicyRevision uint64    `json:"-"`
	ExpiresAt      time.Time `json:"sessionExpiresAt"`
}

type identityUser struct {
	ID            string
	Username      string
	Password      passwordRecord
	Administrator bool
	Bootstrap     bool
	Disabled      bool
	Epoch         uint64
	DisplayName   string
	RoleIDs       []string
	Revision      uint64
}
type passwordRecord struct {
	Salt       []byte
	Hash       []byte
	Iterations int
}
type identitySession struct {
	ID      string
	Subject string
	Digest  string
	CSRF    string
	Epoch   uint64
	Created time.Time
	Seen    time.Time
	Expires time.Time
	Revoked bool
}
type loginWindow struct {
	Start time.Time
	Count int
}
type identityAudit struct {
	SubjectID, Action, Result string
	At                        time.Time
	RequestID                 string
	Source                    string
	NodeIDs                   []string
}
type identityState struct {
	Version           int
	Initialized       bool
	SetupDigest       string
	SetupExpires      time.Time
	ClockFloor        time.Time
	PolicyRevision    uint64
	Users             map[string]identityUser
	Sessions          map[string]identitySession
	Failures          map[string]loginWindow
	Outbox            []identityAudit
	Grants            map[string]identityGrant
	Receipts          map[string]identityReceipt
	AuditSequence     uint64
	Credentials       map[string]storedCredential
	CredentialChanges map[string]credentialChange
	CredentialKeyUses map[string]uint64
	CredentialUses    map[string]credentialUse
	Tokens            map[string]restrictedToken
	Approvals         map[string]approvalRecord
	requestID         string
	requestSource     string
}

// IdentityService implements auth-local lifecycle logic. The public contract and
// adapter are candidates; no Controller route consumes this before review.
type IdentityService struct {
	store       IdentityStore
	now         func() time.Time
	random      io.Reader
	resources   ResourceResolver
	permissions map[string]PermissionDefinition
}

func NewIdentityService(store IdentityStore) (*IdentityService, error) {
	if store == nil {
		return nil, ErrIdentityUnavailable
	}
	return newIdentityService(store, nil, nil)
}

func decodeIdentity(data []byte) (*identityState, error) {
	s := &identityState{}
	if len(data) > 16<<20 {
		return nil, ErrIdentityUnavailable
	}
	if len(data) > 0 && (json.Unmarshal(data, s) != nil || s.Version != 1) {
		return nil, ErrIdentityUnavailable
	}
	if len(data) > 0 && (!s.Initialized && len(s.Users) > 0 || s.Initialized && len(s.Users) == 0) {
		return nil, ErrIdentityUnavailable
	}
	s.Version = 1
	if s.Users == nil {
		s.Users = map[string]identityUser{}
	}
	if s.Sessions == nil {
		s.Sessions = map[string]identitySession{}
	}
	if s.Failures == nil {
		s.Failures = map[string]loginWindow{}
	}
	if s.Grants == nil {
		s.Grants = map[string]identityGrant{}
	}
	if s.Receipts == nil {
		s.Receipts = map[string]identityReceipt{}
	}
	if s.Credentials == nil {
		s.Credentials = map[string]storedCredential{}
	}
	if s.CredentialChanges == nil {
		s.CredentialChanges = map[string]credentialChange{}
	}
	if s.CredentialKeyUses == nil {
		s.CredentialKeyUses = map[string]uint64{}
	}
	if s.CredentialUses == nil {
		s.CredentialUses = map[string]credentialUse{}
	}
	if s.Tokens == nil {
		s.Tokens = map[string]restrictedToken{}
	}
	if s.Approvals == nil {
		s.Approvals = map[string]approvalRecord{}
	}
	if len(s.Approvals) > 10000 {
		return nil, ErrIdentityUnavailable
	}
	for id, record := range s.Approvals {
		if id != record.View.ID || !validSecret(id) || record.View.Revision == 0 || record.SubjectEpoch == 0 || record.PolicyRevision == 0 || s.Users[record.Plan.SubjectID].ID == "" || record.Plan.SubjectID != record.View.SubjectID || record.Plan.Digest != record.View.Digest || record.Plan.ID != record.View.PlanID || record.Plan.ActionID != record.View.ActionID || record.Plan.Source != record.View.Source || !slices.Equal(record.Plan.NodeIDs, record.View.NodeIDs) || record.View.ExpiresAt.IsZero() || record.View.ExpiresAt.After(record.Plan.ExpiresAt) {
			return nil, ErrIdentityUnavailable
		}
		switch record.View.State {
		case "pending", "expired":
		case "approved", "denied", "consumed":
			if record.ApproverID == "" || s.Users[record.ApproverID].ID == "" || record.ApproverEpoch == 0 || !record.HumanWeb {
				return nil, ErrIdentityUnavailable
			}
		default:
			return nil, ErrIdentityUnavailable
		}
		if record.View.State == "consumed" && (!validPolicyID(record.ConsumedTaskID) || len(record.ConsumedTaskID) > 128) {
			return nil, ErrIdentityUnavailable
		}
	}
	if len(s.Tokens) > 10000 {
		return nil, ErrIdentityUnavailable
	}
	for digest, token := range s.Tokens {
		if digest != token.Digest || !validSecret(digest) || !validSecret(token.ID) || s.Users[token.SubjectID].ID == "" || token.Epoch == 0 || !validTokenCaps(token.Actions, token.NodeIDs) || token.CreatedAt.IsZero() || !token.ExpiresAt.After(token.CreatedAt) || token.ExpiresAt.Sub(token.CreatedAt) > time.Hour {
			return nil, ErrIdentityUnavailable
		}
	}
	if len(s.Credentials) > 5000 || len(s.CredentialChanges) > 1000 || len(s.CredentialKeyUses) > 64 || len(s.CredentialUses) > 10000 {
		return nil, ErrIdentityUnavailable
	}
	if len(s.Receipts) > 5000 {
		return nil, ErrIdentityUnavailable
	}
	if len(s.Grants) > 10000 {
		return nil, ErrIdentityUnavailable
	}
	if len(s.Users) > 10000 || len(s.Sessions) > 10000 || len(s.Failures) > 20002 || len(s.Outbox) > 10000 {
		return nil, ErrIdentityUnavailable
	}
	names := map[string]bool{}
	for id, u := range s.Users {
		if id != u.ID || !validSecret(id) || !validUsername(u.Username) || names[u.Username] || u.Epoch == 0 || u.Password.Iterations != 600000 || len(u.Password.Salt) != 32 || len(u.Password.Hash) != 32 {
			return nil, ErrIdentityUnavailable
		}
		names[u.Username] = true
		if u.Revision == 0 {
			u.Revision = 1
		}
		if u.DisplayName == "" {
			u.DisplayName = u.Username
		}
		if u.RoleIDs == nil && u.Administrator {
			u.RoleIDs = []string{"administrator"}
		}
		if !validRoleIDs(u.RoleIDs) {
			return nil, ErrIdentityUnavailable
		}
		if u.Administrator != containsAdministrator(u.RoleIDs) {
			return nil, ErrIdentityUnavailable
		}
		s.Users[id] = u
	}
	for id, grant := range s.Grants {
		if id != grant.Value.GrantID || !validSecret(id) || s.Users[grant.Value.PrincipalID].ID == "" || !validResourceType(grant.Value.ResourceType) || !validPolicyID(grant.Value.ResourceID) || !validExactIDs(grant.Nodes) || !validExactIDs(grant.Value.Permissions) {
			return nil, ErrIdentityUnavailable
		}
	}
	for digest, se := range s.Sessions {
		if digest != se.Digest || !validSecret(digest) || !validSecret(se.ID) || !validSecret(se.CSRF) || s.Users[se.Subject].ID == "" || se.Epoch == 0 || se.Created.IsZero() || se.Seen.Before(se.Created) || !se.Expires.After(se.Created) {
			return nil, ErrIdentityUnavailable
		}
	}
	return s, nil
}

func (s *IdentityService) update(ctx context.Context, fn func(*identityState, time.Time) error) error {
	if ctx == nil || s == nil || s.store == nil || s.now == nil || s.random == nil {
		return ErrIdentityUnavailable
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	err := s.store.Update(ctx, func(data []byte) ([]byte, error) {
		st, err := decodeIdentity(data)
		if err != nil {
			return nil, err
		}
		now := s.now().UTC()
		if now.IsZero() || now.Before(st.ClockFloor) {
			return nil, ErrIdentityUnavailable
		}
		st.ClockFloor = now
		st.requestID, _ = ctx.Value(identityAuditContextKey{}).(string)
		st.requestSource = "system"
		if st.requestID != "" {
			st.requestSource = "web"
		}
		if err = fn(st, now); err != nil {
			return nil, err
		}
		if err = ctx.Err(); err != nil {
			return nil, err
		}
		encoded, err := json.Marshal(st)
		if err != nil || len(encoded) > 16<<20 {
			return nil, ErrIdentityUnavailable
		}
		return encoded, nil
	})
	return identityStoreError(ctx, err)
}

func identityStoreError(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	for _, category := range []error{ErrCredential, ErrIdentityUnavailable, ErrRateLimited, ErrAlreadyInitialized, ErrDenied, ErrInvalidCheck, ErrRevisionConflict, ErrPolicyUnavailable, ErrIdempotencyConflict, ErrCredentialUseUnknown, ErrCredentialReplay} {
		if errors.Is(err, category) {
			return category
		}
	}
	if err != nil {
		return ErrIdentityUnavailable
	}
	return nil
}

func (s *IdentityService) read(ctx context.Context, fn func(*identityState, time.Time) error) error {
	if ctx == nil || s == nil || s.store == nil || s.now == nil {
		return ErrIdentityUnavailable
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return identityStoreError(ctx, s.store.View(ctx, func(data []byte) error {
		st, err := decodeIdentity(data)
		if err != nil {
			return err
		}
		now := s.now().UTC()
		if now.IsZero() || now.Before(st.ClockFloor) {
			return ErrIdentityUnavailable
		}
		return fn(st, now)
	}))
}

func randomIdentity(r io.Reader) (string, error) {
	b := make([]byte, 32)
	if _, err := io.ReadFull(r, b); err != nil {
		return "", ErrIdentityUnavailable
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
func credentialDigest(secret string) string {
	h := sha256.Sum256([]byte(secret))
	return base64.RawURLEncoding.EncodeToString(h[:])
}
func validSecret(secret string) bool {
	b, e := base64.RawURLEncoding.DecodeString(secret)
	return e == nil && len(b) == 32 && base64.RawURLEncoding.EncodeToString(b) == secret
}
func matchSecret(secret, digest string) bool {
	return validSecret(secret) && subtle.ConstantTimeCompare([]byte(credentialDigest(secret)), []byte(digest)) == 1
}

func makePassword(password string, r io.Reader) (passwordRecord, error) {
	if len(password) < 12 || len(password) > 1024 {
		return passwordRecord{}, ErrInvalidCheck
	}
	salt := make([]byte, 32)
	if _, err := io.ReadFull(r, salt); err != nil {
		return passwordRecord{}, ErrIdentityUnavailable
	}
	h, err := pbkdf2.Key(sha256.New, password, salt, 600000, 32)
	if err != nil {
		return passwordRecord{}, ErrIdentityUnavailable
	}
	return passwordRecord{salt, h, 600000}, nil
}
func checkPassword(password string, record passwordRecord) bool {
	if len(password) > 1024 || record.Iterations != 600000 || len(record.Salt) != 32 || len(record.Hash) != 32 {
		return false
	}
	h, err := pbkdf2.Key(sha256.New, password, record.Salt, record.Iterations, 32)
	return err == nil && subtle.ConstantTimeCompare(h, record.Hash) == 1
}
func validUsername(name string) bool {
	return len(name) >= 1 && len(name) <= 64 && !strings.ContainsFunc(name, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-' || r == '.')
	})
}

func appendIdentityAudit(st *identityState, subject, action, result string, now time.Time, nodes ...string) error {
	// A durable outbox allows the DS audit adapter to deliver committed events.
	// Capacity exhaustion fails closed until the reviewed adapter drains it.
	if len(st.Outbox) >= 10000 {
		return ErrIdentityUnavailable
	}
	st.AuditSequence++
	if st.AuditSequence == 0 {
		return ErrIdentityUnavailable
	}
	requestID := st.requestID
	if requestID == "" {
		requestID = requestDigest(struct {
			Subject, Action string
			At              time.Time
			Sequence        uint64
		}{subject, action, now, st.AuditSequence})
	}
	st.Outbox = append(st.Outbox, identityAudit{SubjectID: subject, Action: action, Result: result, At: now, RequestID: requestID, Source: st.requestSource, NodeIDs: append([]string{}, nodes...)})
	return nil
}

func (s *IdentityService) Initialized(ctx context.Context) (bool, error) {
	var result bool
	err := s.read(ctx, func(st *identityState, _ time.Time) error { result = st.Initialized; return nil })
	return result, err
}

// PrepareSetup is for a separately protected local installer only. Do not expose
// this through HTTP, logs, process arguments, or automatic empty-DB startup.
func (s *IdentityService) PrepareSetup(ctx context.Context) (string, error) {
	var secret string
	err := s.update(ctx, func(st *identityState, now time.Time) error {
		if st.Initialized {
			return ErrAlreadyInitialized
		}
		var err error
		secret, err = randomIdentity(s.random)
		if err != nil {
			return err
		}
		st.SetupDigest = credentialDigest(secret)
		st.SetupExpires = now.Add(10 * time.Minute)
		return appendIdentityAudit(st, "installer", "auth.setup", "prepared", now)
	})
	if err != nil {
		return "", err
	}
	return secret, nil
}

func (s *IdentityService) Bootstrap(ctx context.Context, setup, username, password string) error {
	return s.bootstrap(ctx, setup, username, password, "")
}
func (s *IdentityService) BootstrapIdempotent(ctx context.Context, setup, username, password, key string) error {
	return s.bootstrap(ctx, setup, username, password, key)
}
func (s *IdentityService) bootstrap(ctx context.Context, setup, username, password, key string) error {
	if !validUsername(username) {
		return ErrInvalidCheck
	}
	var outcome error
	err := s.update(ctx, func(st *identityState, now time.Time) error {
		digest := requestDigest(struct{ Username, SetupDigest string }{username, credentialDigest(setup)})
		receipt, replay, err := loadReceipt(st, "installer", "auth.bootstrap", key, digest, now)
		if err != nil {
			return err
		}
		if replay {
			if !st.Initialized || !checkPassword(password, receipt.Password) {
				return ErrCredential
			}
			return nil
		}
		if st.Initialized {
			return ErrAlreadyInitialized
		}
		if !now.Before(st.SetupExpires) || !matchSecret(setup, st.SetupDigest) {
			outcome = ErrCredential
			return nil
		}
		pw, err := makePassword(password, s.random)
		if err != nil {
			return err
		}
		id, err := randomIdentity(s.random)
		if err != nil {
			return err
		}
		st.Users[id] = identityUser{ID: id, Username: username, Password: pw, Administrator: true, Bootstrap: true, Epoch: 1}
		saveReceipt(st, "installer", "auth.bootstrap", key, digest, id, pw, now)
		if key != "" {
			cacheKey := receiptKey("installer", "auth.bootstrap", key)
			cached := st.Receipts[cacheKey]
			cached.Expires = st.SetupExpires
			st.Receipts[cacheKey] = cached
		}
		st.Initialized = true
		st.SetupDigest = ""
		st.SetupExpires = time.Time{}
		st.PolicyRevision = 1
		return appendIdentityAudit(st, id, "auth.bootstrap", "succeeded", now)
	})
	if err != nil {
		return err
	}
	return outcome
}

type LoginSession struct {
	Identity Identity     `json:"-"`
	Secret   string       `json:"-"`
	CSRF     string       `json:"csrfToken"`
	Profile  IdentityView `json:"identity"`
}

// Login requires the canonical source IP from the trusted listener, never an
// untrusted forwarding header. Rate limits are persisted with identity state.
func (s *IdentityService) Login(ctx context.Context, username, password, source string) (LoginSession, error) {
	return s.login(ctx, username, password, source, "")
}

// LoginReplacing atomically revokes the browser's old session when login
// succeeds, including switching subjects. It never reuses a supplied secret.
func (s *IdentityService) LoginReplacing(ctx context.Context, username, password, source, oldSecret string) (LoginSession, error) {
	if oldSecret != "" && !validSecret(oldSecret) {
		return LoginSession{}, ErrCredential
	}
	return s.login(ctx, username, password, source, oldSecret)
}
func (s *IdentityService) login(ctx context.Context, username, password, source, oldSecret string) (LoginSession, error) {
	var result LoginSession
	var outcome error
	if len(username) > 64 || len(password) > 1024 || source == "" || len(source) > 256 {
		return result, ErrCredential
	}
	err := s.update(ctx, func(st *identityState, now time.Time) error {
		if !st.Initialized {
			return ErrCredential
		}
		for k, w := range st.Failures {
			if !now.Before(w.Start.Add(5 * time.Minute)) {
				delete(st.Failures, k)
			}
		}
		accountKey := "account:" + credentialDigest(username)
		sourceKey := "source:" + credentialDigest(source)
		for _, k := range []string{accountKey, sourceKey} {
			if st.Failures[k].Count >= 5 {
				outcome = ErrRateLimited
				return nil
			}
		}
		if len(st.Failures) > 20000 {
			outcome = ErrRateLimited
			return nil
		}
		var user identityUser
		for _, u := range st.Users {
			if u.Username == username {
				user = u
				break
			}
		}
		pw := user.Password
		if user.ID == "" {
			pw = passwordRecord{Salt: make([]byte, 32), Hash: make([]byte, 32), Iterations: 600000}
		}
		valid := checkPassword(password, pw)
		if !valid || user.Disabled || user.ID == "" {
			for _, k := range []string{accountKey, sourceKey} {
				w := st.Failures[k]
				if w.Count == 0 {
					w.Start = now
				}
				w.Count++
				st.Failures[k] = w
			}
			outcome = ErrCredential
			return appendIdentityAudit(st, "anonymous", "auth.login", "denied", now)
		}
		for k, se := range st.Sessions {
			if se.Revoked || !now.Before(se.Expires) || !now.Before(se.Seen.Add(30*time.Minute)) {
				delete(st.Sessions, k)
			}
		}
		if len(st.Sessions) >= 10000 {
			return ErrIdentityUnavailable
		}
		secret, err := randomIdentity(s.random)
		if err != nil {
			return err
		}
		id, err := randomIdentity(s.random)
		if err != nil {
			return err
		}
		csrf, err := randomIdentity(s.random)
		if err != nil {
			return err
		}
		se := identitySession{ID: id, Subject: user.ID, Digest: credentialDigest(secret), CSRF: csrf, Epoch: user.Epoch, Created: now, Seen: now, Expires: now.Add(8 * time.Hour)}
		st.Sessions[se.Digest] = se
		if prior, ok := st.Sessions[credentialDigest(oldSecret)]; oldSecret != "" && ok {
			prior.Revoked = true
			st.Sessions[prior.Digest] = prior
		}
		delete(st.Failures, accountKey) // Source failures are not cleared by another account's success.
		result = LoginSession{identityFor(user, se, st.PolicyRevision), secret, csrf, s.identityProfile(user)}
		return appendIdentityAudit(st, user.ID, "auth.login", "succeeded", now)
	})
	if err != nil {
		return LoginSession{}, err
	}
	if outcome != nil {
		return LoginSession{}, outcome
	}
	return result, nil
}

func identityFor(u identityUser, se identitySession, revision uint64) Identity {
	return Identity{u.ID, u.Username, u.Administrator, se.ID, u.Epoch, revision, se.Expires}
}
func findIdentity(st *identityState, secret string, now time.Time) (identityUser, identitySession, error) {
	if !validSecret(secret) {
		return identityUser{}, identitySession{}, ErrCredential
	}
	se, ok := st.Sessions[credentialDigest(secret)]
	u, userOK := st.Users[se.Subject]
	if !st.Initialized || !ok || !userOK || u.Disabled || se.Revoked || se.Epoch != u.Epoch || !now.Before(se.Expires) || !now.Before(se.Seen.Add(30*time.Minute)) {
		return identityUser{}, identitySession{}, ErrCredential
	}
	return u, se, nil
}

// Authenticate validates the current store on each request. For mutations csrf
// must match. Call only after Origin checking. Reads use an empty csrf argument.
func (s *IdentityService) Authenticate(ctx context.Context, secret, csrf string, mutation bool) (Identity, error) {
	var identity Identity
	var outcome error
	err := s.update(ctx, func(st *identityState, now time.Time) error {
		u, se, err := findIdentity(st, secret, now)
		if err != nil {
			outcome = err
			return nil
		}
		if mutation && (!validSecret(csrf) || subtle.ConstantTimeCompare([]byte(csrf), []byte(se.CSRF)) != 1) {
			outcome = ErrDenied
			return nil
		}
		se.Seen = now
		st.Sessions[se.Digest] = se
		identity = identityFor(u, se, st.PolicyRevision)
		return nil
	})
	if err != nil {
		return Identity{}, err
	}
	if outcome != nil {
		return Identity{}, outcome
	}
	return identity, nil
}

func (s *IdentityService) CommitGate(ctx context.Context, secret string, observation Identity) error {
	var outcome error
	err := s.update(ctx, func(st *identityState, now time.Time) error {
		u, se, err := findIdentity(st, secret, now)
		if err != nil {
			outcome = err
			return nil
		}
		if observation.SubjectID != u.ID || observation.SessionID != se.ID || observation.Epoch != u.Epoch || observation.PolicyRevision != st.PolicyRevision {
			outcome = ErrDenied
		}
		return nil
	})
	if err != nil {
		return err
	}
	return outcome
}

// CheckObservationReadOnly is a candidate composition seam for a domain already
// holding a DS writer transaction. It never opens a nested writer, refreshes
// idle lifetime or persists a clock observation. Authenticate (Origin/CSRF) must
// have run before entering that transaction; use a transaction-bound auth reader
// for a shared snapshot. Final HTTP output still uses CommitGate outside it.
func (s *IdentityService) CheckObservationReadOnly(ctx context.Context, secret string, observation Identity) error {
	return s.read(ctx, func(st *identityState, now time.Time) error {
		u, se, err := findIdentity(st, secret, now)
		if err != nil {
			return err
		}
		if observation.SubjectID != u.ID || observation.SessionID != se.ID || observation.Epoch != u.Epoch || observation.PolicyRevision != st.PolicyRevision {
			return ErrDenied
		}
		return nil
	})
}

func (s *IdentityService) RevokeSession(ctx context.Context, secret, csrf, sessionID string) error {
	return s.update(ctx, func(st *identityState, now time.Time) error {
		u, current, err := findIdentity(st, secret, now)
		if err != nil {
			return err
		}
		if !validSecret(csrf) || subtle.ConstantTimeCompare([]byte(csrf), []byte(current.CSRF)) != 1 {
			return ErrDenied
		}
		for k, se := range st.Sessions {
			if se.ID == sessionID {
				if se.Subject != u.ID {
					return ErrDenied
				}
				se.Revoked = true
				st.Sessions[k] = se
				return appendIdentityAudit(st, u.ID, "auth.session.revoke", "succeeded", now)
			}
		}
		return ErrDenied
	})
}

func (s *IdentityService) ChangePassword(ctx context.Context, secret, csrf, oldPassword, newPassword string) error {
	return s.update(ctx, func(st *identityState, now time.Time) error {
		u, se, err := findIdentity(st, secret, now)
		if err != nil {
			return err
		}
		if !validSecret(csrf) || subtle.ConstantTimeCompare([]byte(csrf), []byte(se.CSRF)) != 1 || !checkPassword(oldPassword, u.Password) {
			return ErrDenied
		}
		pw, err := makePassword(newPassword, s.random)
		if err != nil {
			return err
		}
		u.Password = pw
		u.Epoch++
		st.Users[u.ID] = u
		return appendIdentityAudit(st, u.ID, "auth.password.change", "succeeded", now)
	})
}
