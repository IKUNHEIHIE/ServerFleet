package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// Transaction double ONLY. This does not establish SQLite/restart durability.
type identityTestStore struct {
	mu         sync.Mutex
	data       []byte
	fail       bool
	failCommit bool
}

func (m *identityTestStore) View(ctx context.Context, fn func([]byte) error) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail {
		return errors.New("secret database path")
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return fn(bytes.Clone(m.data))
}
func (m *identityTestStore) Update(ctx context.Context, fn func([]byte) ([]byte, error)) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail {
		return errors.New("secret database path")
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	data, err := fn(bytes.Clone(m.data))
	if err != nil {
		return err
	}
	if m.failCommit {
		return errors.New("commit failed")
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	m.data = bytes.Clone(data)
	return nil
}

const identityTestPassword = "a-long-test-only-password"

func identityFixture(t *testing.T) (*IdentityService, *identityTestStore, *time.Time) {
	t.Helper()
	m := &identityTestStore{}
	s, err := NewIdentityService(m)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 30, 0, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return now }
	setup, err := s.PrepareSetup(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Bootstrap(context.Background(), setup, "admin", identityTestPassword); err != nil {
		t.Fatal(err)
	}
	return s, m, &now
}
func identityLogin(t *testing.T, s *IdentityService) LoginSession {
	t.Helper()
	r, e := s.Login(context.Background(), "admin", identityTestPassword, "127.0.0.1")
	if e != nil {
		t.Fatal(e)
	}
	return r
}

func TestIdentityBootstrapSingleUseAndPermanentMarker(t *testing.T) {
	ctx := context.Background()
	m := &identityTestStore{}
	s, _ := NewIdentityService(m)
	token, e := s.PrepareSetup(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Bootstrap(ctx, token, "admin", identityTestPassword); e != nil {
		t.Fatal(e)
	}
	if e = s.Bootstrap(ctx, token, "other", identityTestPassword); !errors.Is(e, ErrAlreadyInitialized) {
		t.Fatal(e)
	}
	s2, _ := NewIdentityService(m)
	if _, e = s2.PrepareSetup(ctx); !errors.Is(e, ErrAlreadyInitialized) {
		t.Fatal(e)
	}
	if bytes.Contains(m.data, []byte(token)) || bytes.Contains(m.data, []byte(identityTestPassword)) {
		t.Fatal("persisted plaintext credential")
	}
	var st identityState
	_ = json.Unmarshal(m.data, &st)
	st.Users = nil
	m.data, _ = json.Marshal(st)
	if _, e = s2.Initialized(ctx); !errors.Is(e, ErrIdentityUnavailable) {
		t.Fatal("lost users reopened setup", e)
	}
}

func TestIdentityExpiredSetupAndCommitFailure(t *testing.T) {
	ctx := context.Background()
	m := &identityTestStore{}
	s, _ := NewIdentityService(m)
	now := time.Now()
	s.now = func() time.Time { return now }
	token, _ := s.PrepareSetup(ctx)
	now = now.Add(10 * time.Minute)
	if e := s.Bootstrap(ctx, token, "admin", identityTestPassword); !errors.Is(e, ErrCredential) {
		t.Fatal(e)
	}
	m.failCommit = true
	if token, e := s.PrepareSetup(ctx); token != "" || !errors.Is(e, ErrIdentityUnavailable) {
		t.Fatal(token, e)
	}
}

func TestIdentityAuthenticationRevocationAndOwnership(t *testing.T) {
	s, m, _ := identityFixture(t)
	ctx := context.Background()
	login := identityLogin(t, s)
	if login.Identity.SubjectID == login.Secret || login.Identity.SessionID == login.Secret {
		t.Fatal("management ID is secret")
	}
	if bytes.Contains(m.data, []byte(login.Secret)) {
		t.Fatal("persisted raw session secret")
	}
	if _, e := s.Authenticate(ctx, login.Identity.SessionID, "", false); !errors.Is(e, ErrCredential) {
		t.Fatal(e)
	}
	observation, e := s.Authenticate(ctx, login.Secret, login.CSRF, true)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Authenticate(ctx, login.Secret, "wrong", true); !errors.Is(e, ErrDenied) {
		t.Fatal(e)
	}
	if e = s.RevokeSession(ctx, login.Secret, login.CSRF, "other-subject-session"); !errors.Is(e, ErrDenied) {
		t.Fatal(e)
	}
	if e = s.RevokeSession(ctx, login.Secret, login.CSRF, login.Identity.SessionID); e != nil {
		t.Fatal(e)
	}
	if e = s.CommitGate(ctx, login.Secret, observation); !errors.Is(e, ErrCredential) {
		t.Fatal(e)
	}
	s2, _ := NewIdentityService(m)
	s2.now = s.now
	if _, e = s2.Authenticate(ctx, login.Secret, "", false); !errors.Is(e, ErrCredential) {
		t.Fatal("recreated service revived session", e)
	}
}

func TestIdentityExpirationEpochPolicyAndClockRollback(t *testing.T) {
	for _, scenario := range []string{"idle", "absolute", "epoch", "policy", "disabled", "clock"} {
		t.Run(scenario, func(t *testing.T) {
			s, m, now := identityFixture(t)
			login := identityLogin(t, s)
			ctx := context.Background()
			id, e := s.Authenticate(ctx, login.Secret, "", false)
			if e != nil {
				t.Fatal(e)
			}
			var st identityState
			_ = json.Unmarshal(m.data, &st)
			switch scenario {
			case "idle":
				*now = now.Add(30 * time.Minute)
			case "absolute":
				*now = now.Add(8 * time.Hour)
				se := st.Sessions[credentialDigest(login.Secret)]
				se.Seen = *now
				st.Sessions[se.Digest] = se
			case "epoch":
				u := st.Users[id.SubjectID]
				u.Epoch++
				st.Users[u.ID] = u
			case "policy":
				st.PolicyRevision++
			case "disabled":
				u := st.Users[id.SubjectID]
				u.Disabled = true
				st.Users[u.ID] = u
			case "clock":
				*now = now.Add(-time.Second)
			}
			m.data, _ = json.Marshal(st)
			if e = s.CommitGate(ctx, login.Secret, id); e == nil {
				t.Fatal("accepted invalid observation")
			}
		})
	}
}

func TestIdentityRateLimitPersistsAndExpires(t *testing.T) {
	s, m, now := identityFixture(t)
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		if _, e := s.Login(ctx, "unknown", "bad", "ip"); !errors.Is(e, ErrCredential) {
			t.Fatal(e)
		}
	}
	s2, _ := NewIdentityService(m)
	s2.now = s.now
	if _, e := s2.Login(ctx, "admin", identityTestPassword, "ip"); !errors.Is(e, ErrRateLimited) {
		t.Fatal(e)
	}
	*now = now.Add(5 * time.Minute)
	if _, e := s2.Login(ctx, "admin", identityTestPassword, "ip"); e != nil {
		t.Fatal(e)
	}
}

func TestIdentityObservedExpirationCannotReviveAfterClockRollback(t *testing.T) {
	s, _, now := identityFixture(t)
	login := identityLogin(t, s)
	*now = now.Add(31 * time.Minute)
	if _, err := s.Authenticate(context.Background(), login.Secret, "", false); err != ErrCredential {
		t.Fatal(err)
	}
	*now = now.Add(-2 * time.Minute)
	if _, err := s.Authenticate(context.Background(), login.Secret, "", false); err != ErrIdentityUnavailable {
		t.Fatal("clock rollback revived expired credential", err)
	}
	b, _ := json.Marshal(login)
	if bytes.Contains(b, []byte(login.Secret)) {
		t.Fatal("secret included in accidental JSON response")
	}
}

func TestIdentityFailClosedAndNoSecretDeliveryOnCommitFailure(t *testing.T) {
	s, m, _ := identityFixture(t)
	ctx := context.Background()
	m.fail = true
	if _, e := s.Initialized(ctx); e != ErrIdentityUnavailable {
		t.Fatal(e)
	}
	m.fail = false
	m.failCommit = true
	if r, e := s.Login(ctx, "admin", identityTestPassword, "ip"); r.Secret != "" || e != ErrIdentityUnavailable {
		t.Fatal(r, e)
	}
	m.failCommit = false
	s.random = io.LimitReader(strings.NewReader(""), 0)
	if r, e := s.Login(ctx, "admin", identityTestPassword, "ip"); r.Secret != "" || e != ErrIdentityUnavailable {
		t.Fatal(r, e)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, e := s.Initialized(canceled); e != context.Canceled {
		t.Fatal(e)
	}
}

func TestIdentityPasswordChangeRevokesEverySession(t *testing.T) {
	s, _, _ := identityFixture(t)
	a := identityLogin(t, s)
	b := identityLogin(t, s)
	ctx := context.Background()
	if e := s.ChangePassword(ctx, a.Secret, a.CSRF, identityTestPassword, "different-long-test-password"); e != nil {
		t.Fatal(e)
	}
	for _, login := range []LoginSession{a, b} {
		if _, e := s.Authenticate(ctx, login.Secret, "", false); e != ErrCredential {
			t.Fatal(e)
		}
	}
	if _, e := s.Login(ctx, "admin", identityTestPassword, "ip"); e != ErrCredential {
		t.Fatal(e)
	}
	if _, e := s.Login(ctx, "admin", "different-long-test-password", "ip"); e != nil {
		t.Fatal(e)
	}
}

func TestIdentityConcurrentBootstrap(t *testing.T) {
	m := &identityTestStore{}
	s, _ := NewIdentityService(m)
	token, _ := s.PrepareSetup(context.Background())
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Go(func() { results <- s.Bootstrap(context.Background(), token, "admin", identityTestPassword) })
	}
	wg.Wait()
	close(results)
	success := 0
	for e := range results {
		if e == nil {
			success++
		} else if e != ErrAlreadyInitialized {
			t.Fatal(e)
		}
	}
	if success != 1 {
		t.Fatal(success)
	}
}

func TestBrowserOriginCredentialAndCookieBoundaries(t *testing.T) {
	g, e := NewBrowserGuard("https://fleet.example")
	if e != nil {
		t.Fatal(e)
	}
	secret := base64TestSecret()
	cookie, _ := SessionCookie(secret)
	if !cookie.Secure || !cookie.HttpOnly || cookie.Domain != "" || cookie.Path != "/" || cookie.MaxAge != 0 {
		t.Fatal(cookie)
	}
	for _, test := range []struct {
		name, cookies, auth, origin, site string
		denied                            bool
	}{
		{"valid", cookie.String(), "", "https://fleet.example", "same-origin", false},
		{"foreign", cookie.String(), "", "https://evil.example", "cross-site", true},
		{"missing-origin", cookie.String(), "", "", "", true},
		{"bearer-and-cookie", cookie.String(), "Bearer fake", "https://fleet.example", "", true},
		{"duplicate", cookie.String() + "; " + cookie.String(), "", "https://fleet.example", "", true},
		{"legacy", cookie.String() + "; sf_session=old", "", "https://fleet.example", "", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := httptest.NewRequest("POST", "https://fleet.example/action", nil)
			r.Header.Set("Cookie", test.cookies)
			if test.auth != "" {
				r.Header.Set("Authorization", test.auth)
			}
			if test.origin != "" {
				r.Header.Set("Origin", test.origin)
			}
			if test.site != "" {
				r.Header.Set("Sec-Fetch-Site", test.site)
			}
			err := g.Check(r, true)
			if err == nil {
				_, err = SessionCredential(r)
			}
			if (err != nil) != test.denied {
				t.Fatal(err)
			}
		})
	}
	r := httptest.NewRequest("GET", "https://fleet.example/", nil)
	r.Header.Set("Cookie", cookie.String())
	r.Header["Authorization"] = []string{""}
	if _, e = SessionCredential(r); e == nil {
		t.Fatal("empty Authorization accepted")
	}
	if _, e = NewBrowserGuard("http://fleet.example"); e == nil {
		t.Fatal("insecure origin")
	}
}
func base64TestSecret() string { return strings.Repeat("A", 43) }
