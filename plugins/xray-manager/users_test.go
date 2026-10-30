package xraymanager

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

type managedUserTestSource struct{}
type managedUserTestTx struct{}

// In-memory transaction/audit double; actual SQLite combination is separate.
type managedUserTestStore struct {
	mu                sync.Mutex
	raw               []byte
	events            []ManagedUserAudit
	fail, skip, twice bool
}

func (s *managedUserTestStore) View(ctx context.Context, fn func(context.Context, []byte) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return fn(context.WithValue(ctx, managedUserTestTx{}, true), bytes.Clone(s.raw))
}
func (s *managedUserTestStore) Update(ctx context.Context, fn func(context.Context, []byte) ([]byte, []ManagedUserAudit, error)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.skip {
		return nil
	}
	txCtx := context.WithValue(ctx, managedUserTestTx{}, true)
	next, events, err := fn(txCtx, bytes.Clone(s.raw))
	if err == nil && s.twice {
		_, _, err = fn(txCtx, bytes.Clone(s.raw))
	}
	if err != nil {
		return err
	}
	if s.fail || ctx.Err() != nil {
		return ErrManagedUser
	}
	s.raw = bytes.Clone(next)
	s.events = append(s.events, events...)
	return nil
}

type managedUserFixture struct {
	service *ManagedUserService
	store   *managedUserTestStore
	ctx     context.Context
	scope   ManagedUserScope
	denied  string
	rotated bool
}

func newManagedUserFixture(t *testing.T) *managedUserFixture {
	t.Helper()
	f := &managedUserFixture{scope: ManagedUserScope{"node-a", "instance-a"}, ctx: context.WithValue(context.Background(), managedUserTestSource{}, true)}
	raw, err := ManagedUserSeed(f.scope)
	if err != nil {
		t.Fatal(err)
	}
	f.store = &managedUserTestStore{raw: raw}
	f.service, err = NewManagedUserService(f.scope, f.store, func(ctx context.Context, check ManagedUserCheck) error {
		if ctx.Value(managedUserTestSource{}) != true || check.Operation.SubjectID != "human-a" || check.Stage == f.denied || strings.HasPrefix(check.Stage, "commit") && ctx.Value(managedUserTestTx{}) != true {
			return ErrManagedUser
		}
		return nil // explicit task/profile/permission double
	}, func(_ context.Context, _ ManagedUserScope, u ManagedUser) error {
		if u.IdentityRef != "central-ref-"+u.ID || u.CredentialRevision != 1 || f.rotated {
			return ErrManagedUser
		}
		return nil // central metadata double; no UUID enters this store
	})
	if err != nil {
		t.Fatal(err)
	}
	return f
}
func (f *managedUserFixture) op(action, id string, rev uint64) ManagedUserOperation {
	op := ManagedUserOperation{Scope: f.scope, SubjectID: "human-a", OperationID: id, TaskID: "task-" + id, Source: "web", Action: action, UserID: "user-a", PlanDigest: strings.Repeat("a", 64), ExpectedRevision: rev, ExpiresAt: time.Now().Add(time.Minute)}
	if action == "read" || action == "export" {
		op.TaskID = ""
	}
	return op
}
func (f *managedUserFixture) create(t *testing.T) ManagedUser {
	t.Helper()
	u, err := f.service.Change(f.ctx, f.op("create", "create-a", 0), ManagedUserChange{InboundID: "inbound-a", DisplayName: "A", IdentityRef: "central-ref-user-a", CredentialRevision: 1, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	return u
}
func (f *managedUserFixture) applied(t *testing.T, u ManagedUser, id string) ManagedUser {
	t.Helper()
	op := f.op("application", id, u.Revision)
	op.TaskID, op.PlanDigest = u.PendingTaskID, u.PendingPlanDigest
	u, err := f.service.RecordApplication(f.ctx, op, ManagedUserApplication{Result: "applied", ConfigRevision: u.AppliedConfigRevision + 1})
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func TestManagedUsersKeepImmutableAttributionAndSeparateDesiredFromApplied(t *testing.T) {
	f := newManagedUserFixture(t)
	u := f.create(t)
	if u.Revision != 1 || u.Application != "pending" || u.AppliedRevision != 0 {
		t.Fatal("queued change was reported applied")
	}
	if _, err := f.service.Read(f.ctx, f.op("export", "pending-export", 1)); err == nil {
		t.Fatal("unapplied user exported")
	}
	u = f.applied(t, u, "confirm-create")
	if _, err := f.service.Read(f.ctx, f.op("export", "applied-export", 1)); err != nil {
		t.Fatal(err)
	}
	renamed, err := f.service.Change(f.ctx, f.op("rename", "rename-a", 1), ManagedUserChange{DisplayName: "New display name"})
	if err != nil || renamed.StatsEmail != u.StatsEmail || renamed.IdentityRef != u.IdentityRef || renamed.InboundID != u.InboundID || renamed.Application != "pending" {
		t.Fatal("rename lost identity, attribution or pending state", err)
	}
	f.applied(t, renamed, "confirm-rename")
	// New service on the same persisted bytes keeps identity and attribution.
	reopened, err := NewManagedUserService(f.scope, f.store, f.service.gate, f.service.material)
	if err != nil {
		t.Fatal(err)
	}
	f.service = reopened
	disabled, err := f.service.Change(f.ctx, f.op("set-enabled", "disable-a", 2), ManagedUserChange{Enabled: false})
	if err != nil || disabled.Enabled || disabled.Application != "pending" {
		t.Fatal("disable was inferred from a frontend toggle", err)
	}
	if _, err := f.service.Read(f.ctx, f.op("export", "disable-pending-export", 3)); err == nil {
		t.Fatal("disable pending did not close export")
	}
	f.applied(t, disabled, "confirm-disable")
	if _, err := f.service.Read(f.ctx, f.op("export", "disabled-export", 3)); err == nil {
		t.Fatal("disabled user exported")
	}
	deleted, err := f.service.Change(f.ctx, f.op("remove", "remove-a", 3), ManagedUserChange{})
	if err != nil || !deleted.Deleted || deleted.Enabled || deleted.StatsEmail != u.StatsEmail {
		t.Fatal("tombstone lost identity", err)
	}
	if _, err = f.service.Change(f.ctx, f.op("create", "reuse-id", 0), ManagedUserChange{InboundID: "inbound-a", DisplayName: "Other", IdentityRef: "central-ref-user-a", CredentialRevision: 1}); err != ErrManagedUserConflict {
		t.Fatal("deleted user identity reused", err)
	}
}

func TestManagedUserCASReceiptsAuditRollbackAndUnknownApplication(t *testing.T) {
	f := newManagedUserFixture(t)
	u := f.create(t)
	op := f.op("rename", "rename-once", 1)
	change := ManagedUserChange{DisplayName: "Changed"}
	before := bytes.Clone(f.store.raw)
	eventsBefore := len(f.store.events)
	f.store.fail = true
	if got, err := f.service.Change(f.ctx, op, change); err == nil || got.ID != "" || !bytes.Equal(before, f.store.raw) || len(f.store.events) != eventsBefore {
		t.Fatal("audit/commit failure changed state or returned success", err)
	}
	f.store.fail = false
	renamed, err := f.service.Change(f.ctx, op, change)
	if err != nil {
		t.Fatal(err)
	}
	eventsBefore = len(f.store.events)
	if same, err := f.service.Change(f.ctx, op, change); err != nil || same.Revision != renamed.Revision || len(f.store.events) != eventsBefore {
		t.Fatal("idempotent metadata replay duplicated mutation", err)
	}
	if _, err = f.service.Change(f.ctx, op, ManagedUserChange{DisplayName: "Tampered"}); err != ErrManagedUserConflict {
		t.Fatal("same operation changed its planned content", err)
	}
	if _, err = f.service.Change(f.ctx, f.op("rename", "stale", u.Revision), change); err != ErrManagedUserConflict {
		t.Fatal("stale revision overwrote user", err)
	}
	confirm := f.op("application", "confirm-unknown", renamed.Revision)
	confirm.TaskID = renamed.PendingTaskID
	if _, err = f.service.RecordApplication(f.ctx, confirm, ManagedUserApplication{Result: "unknown"}); err != nil {
		t.Fatal(err)
	}
	confirm.OperationID = "late-success"
	if _, err = f.service.RecordApplication(f.ctx, confirm, ManagedUserApplication{Result: "applied", ConfigRevision: 7}); err != ErrManagedUserConflict {
		t.Fatal("unknown task became successful by a late callback", err)
	}
	if _, err = f.service.Read(f.ctx, f.op("export", "unknown-export", renamed.Revision)); err == nil {
		t.Fatal("unknown user exported")
	}
}

func TestManagedUsersCurrentSourcePayloadAndMaterialChecks(t *testing.T) {
	for _, mode := range []string{"source", "commit", "material", "material-panic", "skip", "twice", "cross-instance", "expired", "wrong-ref"} {
		t.Run(mode, func(t *testing.T) {
			f := newManagedUserFixture(t)
			op := f.op("create", "guarded-create", 0)
			change := ManagedUserChange{InboundID: "inbound-a", DisplayName: "A", IdentityRef: "central-ref-user-a", CredentialRevision: 1}
			ctx := f.ctx
			switch mode {
			case "source":
				ctx = context.Background()
			case "commit":
				f.denied = "commit"
			case "material":
				f.rotated = true
			case "material-panic":
				f.service.material = func(context.Context, ManagedUserScope, ManagedUser) error { panic("unavailable") }
			case "skip":
				f.store.skip = true
			case "twice":
				f.store.twice = true
			case "cross-instance":
				op.Scope.InstanceID = "instance-b"
			case "expired":
				op.ExpiresAt = time.Now().Add(-time.Second)
			case "wrong-ref":
				change.IdentityRef = "other-central-ref"
			}
			before := bytes.Clone(f.store.raw)
			if got, err := f.service.Change(ctx, op, change); err == nil || got.ID != "" || !bytes.Equal(before, f.store.raw) || len(f.store.events) != 0 {
				t.Fatal("invalid source/transaction/material changed user", err)
			}
		})
	}
	f := newManagedUserFixture(t)
	u := f.create(t)
	originalGate := f.service.gate
	f.service.gate = func(ctx context.Context, check ManagedUserCheck) error {
		if check.Operation.Action == "rename" && check.Change.DisplayName != "Approved" || check.Operation.Action == "application" && check.Application.ConfigRevision != 7 {
			return errors.New("task evidence differs from frozen plan")
		}
		return originalGate(ctx, check)
	}
	if _, err := f.service.Change(f.ctx, f.op("rename", "changed-payload", 1), ManagedUserChange{DisplayName: "Not approved"}); err == nil {
		t.Fatal("gate did not receive actual change payload")
	}
	op := f.op("application", "wrong-evidence", u.Revision)
	op.TaskID = u.PendingTaskID
	if _, err := f.service.RecordApplication(f.ctx, op, ManagedUserApplication{Result: "applied", ConfigRevision: 9}); err == nil {
		t.Fatal("gate did not receive actual task result evidence")
	}
}

func TestManagedUsersConcurrentCASAndCorruptStoreNeverReseed(t *testing.T) {
	f := newManagedUserFixture(t)
	f.create(t)
	results := make(chan error, 2)
	for _, id := range []string{"rename-one", "rename-two"} {
		go func() {
			_, err := f.service.Change(f.ctx, f.op("rename", id, 1), ManagedUserChange{DisplayName: id})
			results <- err
		}()
	}
	wins := 0
	for range 2 {
		if err := <-results; err == nil {
			wins++
		} else if err != ErrManagedUserConflict {
			t.Fatal(err)
		}
	}
	if wins != 1 {
		t.Fatal("concurrent stale writes both won", wins)
	}
	for _, mutate := range []func(*managedUserState){
		func(st *managedUserState) { st.Format = 2 },
		func(st *managedUserState) { st.Scope.InstanceID = "other" },
		func(st *managedUserState) { u := st.Users["user-a"]; u.StatsEmail = "new-label"; st.Users[u.ID] = u },
		func(st *managedUserState) {
			u := st.Users["user-a"]
			u.AppliedRevision = u.Revision
			st.Users[u.ID] = u
		},
	} {
		var st managedUserState
		if json.Unmarshal(f.store.raw, &st) != nil {
			t.Fatal("fixture decode")
		}
		mutate(&st)
		raw, _ := json.Marshal(st)
		if _, err := decodeManagedUsers(raw, f.scope); err == nil {
			t.Fatal("corrupt/foreign user state accepted")
		}
	}
	f.store.raw = nil
	if _, err := f.service.Change(f.ctx, f.op("create", "missing-store", 0), ManagedUserChange{InboundID: "inbound-a", DisplayName: "A", IdentityRef: "central-ref-user-a", CredentialRevision: 1}); err == nil || len(f.store.raw) != 0 {
		t.Fatal("missing permanent user state silently reseeded")
	}
}

func TestManagedUsersRevokedMaterialClosesExportAndReenableButAllowsDisable(t *testing.T) {
	f := newManagedUserFixture(t)
	u := f.applied(t, f.create(t), "confirm-active")
	f.rotated = true
	if _, err := f.service.Read(f.ctx, f.op("export", "revoked-material-export", u.Revision)); err == nil {
		t.Fatal("stale central reference exported")
	}
	disabled, err := f.service.Change(f.ctx, f.op("set-enabled", "disable-with-revoked-key", u.Revision), ManagedUserChange{})
	if err != nil || disabled.Enabled {
		t.Fatal("revoked material prevented disabling user", err)
	}
	if _, err = f.service.Change(f.ctx, f.op("set-enabled", "reenable-with-revoked-key", disabled.Revision), ManagedUserChange{Enabled: true}); err == nil {
		t.Fatal("revoked central material re-enabled")
	}
	ack := f.op("application", "disable-old-config", disabled.Revision)
	ack.TaskID, ack.PlanDigest = disabled.PendingTaskID, disabled.PendingPlanDigest
	if _, err = f.service.RecordApplication(f.ctx, ack, ManagedUserApplication{Result: "applied", ConfigRevision: disabled.AppliedConfigRevision}); err != ErrManagedUserConflict {
		t.Fatal("unchanged config revision falsely confirmed a disable", err)
	}
	if removed, err := f.service.Change(f.ctx, f.op("remove", "remove-with-revoked-key", disabled.Revision), ManagedUserChange{}); err != nil || !removed.Deleted {
		t.Fatal("revoked material prevented removing user", err)
	}
	other := newManagedUserFixture(t)
	pending := other.create(t)
	other.rotated = true
	ack = other.op("application", "revoked-before-application", pending.Revision)
	ack.TaskID, ack.PlanDigest = pending.PendingTaskID, pending.PendingPlanDigest
	if _, err = other.service.RecordApplication(other.ctx, ack, ManagedUserApplication{Result: "applied", ConfigRevision: 1}); err == nil {
		t.Fatal("application of revoked credential was confirmed")
	}
}

func TestManagedUsersBoundedPagingRequiresSameSnapshotViewerAndFilter(t *testing.T) {
	f := newManagedUserFixture(t)
	f.create(t)
	second := f.op("create", "create-second", 0)
	second.UserID = "user-b"
	if _, err := f.service.Change(f.ctx, second, ManagedUserChange{InboundID: "inbound-b", DisplayName: "B", IdentityRef: "central-ref-user-b", CredentialRevision: 1}); err != nil {
		t.Fatal(err)
	}
	list := f.op("list", "list-users", 0)
	list.UserID, list.TaskID = "", ""
	page, err := f.service.List(f.ctx, list, ManagedUserPageRequest{Limit: 1})
	if err != nil || len(page.Users) != 1 || page.Users[0].ID != "user-a" || page.NextAfter != "user-a" {
		t.Fatal("first bounded page failed", err)
	}
	next := ManagedUserPageRequest{AfterID: page.NextAfter, SnapshotID: page.SnapshotID, Limit: 1}
	last, err := f.service.List(f.ctx, list, next)
	if err != nil || len(last.Users) != 1 || last.Users[0].ID != "user-b" || last.NextAfter != "" {
		t.Fatal("same-snapshot second page failed", err)
	}
	page.Users[0].DisplayName = "Caller mutation"
	if got, err := f.service.Read(f.ctx, f.op("read", "read-original", 1)); err != nil || got.DisplayName != "A" {
		t.Fatal("page aliases persistent state", err)
	}
	wrongFilter := next
	wrongFilter.IncludeDeleted = true
	if _, err = f.service.List(f.ctx, list, wrongFilter); err != ErrManagedUserConflict {
		t.Fatal("cursor reused with another deletion filter", err)
	}
	originalGate := f.service.gate
	f.service.gate = func(ctx context.Context, check ManagedUserCheck) error {
		copy := check
		copy.Operation.SubjectID = "human-a" // two explicitly authenticated fixture viewers
		return originalGate(ctx, copy)
	}
	otherViewer := list
	otherViewer.SubjectID = "human-b"
	if _, err = f.service.List(f.ctx, otherViewer, next); err != ErrManagedUserConflict {
		t.Fatal("cursor reused by another viewer", err)
	}
	f.service.gate = originalGate
	if _, err = f.service.Change(f.ctx, f.op("rename", "intervening-change", 1), ManagedUserChange{DisplayName: "Renamed"}); err != nil {
		t.Fatal(err)
	}
	if _, err = f.service.List(f.ctx, list, next); err != ErrManagedUserConflict {
		t.Fatal("paging mixed metadata revisions", err)
	}
	for _, request := range []ManagedUserPageRequest{{Limit: 0}, {Limit: 101}, {Limit: 1, AfterID: "user-a"}, {Limit: 1, SnapshotID: "forged"}} {
		if _, err = f.service.List(f.ctx, list, request); err == nil {
			t.Fatal("unbounded or unbound page request accepted")
		}
	}
}
