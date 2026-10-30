package plugins

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

type historyTestTxKey struct{}

type droppedHistoryCallbacks struct{}

func (droppedHistoryCallbacks) View(context.Context, func([]byte) error) error { return nil }
func (droppedHistoryCallbacks) Update(context.Context, func(context.Context, []byte) ([]byte, error)) error {
	return nil
}

type historyTestStore struct {
	mu   sync.Mutex
	raw  []byte
	fail bool
}

func (s *historyTestStore) View(ctx context.Context, fn func([]byte) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return fn(bytes.Clone(s.raw))
}
func (s *historyTestStore) Update(ctx context.Context, fn func(context.Context, []byte) ([]byte, error)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	next, err := fn(context.WithValue(ctx, historyTestTxKey{}, true), bytes.Clone(s.raw))
	if err != nil {
		return err
	}
	if s.fail {
		return ErrHistoryRejected
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	s.raw = bytes.Clone(next)
	return nil
}
func historyFixture(t *testing.T) (*HistoryVault, *historyTestStore, HistoryOperation, []byte, map[string][]byte) {
	t.Helper()
	scope := HistoryScope{"xray-manager", "node-a", "instance-a"}
	raw, _ := json.Marshal(historyState{Format: 1, Scope: scope})
	store := &historyTestStore{raw: raw}
	keys := map[string][]byte{"key-1": bytes.Repeat([]byte{41}, 32)}
	before := func(_ context.Context, c HistoryCheck) error {
		if c.Operation.Scope != scope || c.Operation.ActorID != "admin" || c.Operation.PluginRevision != 3 || c.Operation.CurrentConfigRevision != 7 {
			return ErrHistoryRejected
		}
		return nil
	} // explicit current-policy double
	commit := func(ctx context.Context, c HistoryCheck) error {
		if ctx.Value(historyTestTxKey{}) != true || !strings.HasPrefix(c.Stage, "commit-") {
			return ErrHistoryRejected
		}
		return before(ctx, c)
	}
	v, err := NewHistoryVault(store, scope, "key-1", keys, before, commit)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(v.Close)
	content := []byte(`{"inbounds":[{"users":[{"id":"3b37d23e-e729-4f43-9d02-8a4249125551"}]}]}`)
	op := HistoryOperation{Scope: scope, ActorID: "admin", TaskID: "task-save-1", Action: "plugins.history.save", VersionID: "task-save-1", ContentSHA256: historyDigest(content), TargetVersion: "26.6.1", PluginRevision: 3, CurrentConfigRevision: 7}
	return v, store, op, content, keys
}
func historyRestoreOp(save HistoryOperation) HistoryOperation {
	op := save
	op.TaskID = "task-restore-1"
	op.Action = "plugins.history.restore-source"
	op.ExpectedHistoryRevision = 1
	return op
}

func TestHistoryCiphertextMetadataAndSingleUseRestoreSurviveReopen(t *testing.T) {
	v, store, op, raw, keys := historyFixture(t)
	ctx := context.Background()
	meta, err := v.Save(ctx, op, raw)
	if err != nil || meta.Revision != 1 {
		t.Fatal(err)
	}
	if bytes.Contains(store.raw, raw) || bytes.Contains(store.raw, []byte("3b37d23e-e729-4f43-9d02-8a4249125551")) || bytes.Contains(store.raw, keys["key-1"]) {
		t.Fatal("plaintext config/key in history")
	}
	if strings.Contains(fmt.Sprintf("%v %#v", v, v), "41 41 41") {
		t.Fatal("key ring printed")
	}
	listOp := op
	listOp.Action = "plugins.history.list"
	listOp.ExpectedHistoryRevision = 1
	listOp.VersionID = ""
	listOp.ContentSHA256 = ""
	listOp.TargetVersion = ""
	list, err := v.List(ctx, listOp)
	if err != nil || len(list) != 1 || list[0] != meta {
		t.Fatal("metadata list mismatched", err)
	}
	reopened, err := NewHistoryVault(store, v.scope, "key-1", keys, v.before, v.commit)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	var retained []byte
	calls := 0
	restore := historyRestoreOp(op)
	consume := func(_ context.Context, m HistoryVersion, b []byte) error {
		calls++
		if m != meta || !bytes.Equal(b, raw) {
			t.Error("restoration changed immutable source")
		}
		retained = b
		return nil
	}
	if err = reopened.WithRestoreSource(ctx, restore, consume); err != nil {
		t.Fatal(err)
	}
	if calls != 1 || !bytes.Equal(retained, make([]byte, len(retained))) {
		t.Fatal("consumer buffer not cleared")
	}
	if err = v.WithRestoreSource(ctx, restore, consume); err != ErrHistoryReplay || calls != 1 {
		t.Fatal("old vault repeated claimed restoration", err)
	}
	if _, err = v.Save(ctx, op, raw); err != ErrHistoryReplay {
		t.Fatal("save task replayed", err)
	}
	state, err := decodeHistory(store.raw, v.scope)
	if err != nil || state.Claims[restore.TaskID].State != "delivered" || len(state.Outbox) != 3 {
		t.Fatal("claim/audit missing", err)
	}
}

func TestHistoryConsumerFailureRevocationAndCommitFailureNeverRetry(t *testing.T) {
	for _, failure := range []string{"error", "panic", "cancel", "consume-revocation", "observe-revocation", "final-commit"} {
		t.Run(failure, func(t *testing.T) {
			v, store, save, raw, _ := historyFixture(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if _, err := v.Save(ctx, save, raw); err != nil {
				t.Fatal(err)
			}
			op := historyRestoreOp(save)
			before := v.before
			commit := v.commit
			v.before = func(ctx context.Context, c HistoryCheck) error {
				if failure == "consume-revocation" && c.Stage == "consume" || failure == "observe-revocation" && c.Stage == "observe" {
					return ErrHistoryRejected
				}
				return before(ctx, c)
			}
			v.commit = func(ctx context.Context, c HistoryCheck) error {
				if failure == "final-commit" && c.Stage == "commit-finalize" {
					return ErrHistoryRejected
				}
				return commit(ctx, c)
			}
			calls := 0
			var retained []byte
			err := v.WithRestoreSource(ctx, op, func(_ context.Context, _ HistoryVersion, b []byte) error {
				calls++
				retained = b
				switch failure {
				case "error":
					return errors.New("explicit consumer failure")
				case "panic":
					panic("consumer failed")
				case "cancel":
					cancel()
				}
				return nil
			})
			if err != ErrHistoryUnknown || failure == "consume-revocation" && calls != 0 || failure != "consume-revocation" && calls != 1 {
				t.Fatal("unknown outcome misreported", calls, err)
			}
			if retained != nil && !bytes.Equal(retained, make([]byte, len(retained))) {
				t.Fatal("failed consumer retained plaintext")
			}
			if err = v.WithRestoreSource(context.Background(), op, func(context.Context, HistoryVersion, []byte) error {
				t.Error("unknown restoration retried")
				return nil
			}); err != ErrHistoryReplay {
				t.Fatal("unknown claim disappeared", err)
			}
			state, err := decodeHistory(store.raw, v.scope)
			if err != nil {
				t.Fatal(err)
			}
			want := "unknown"
			if failure == "final-commit" {
				want = "reserved"
			}
			if state.Claims[op.TaskID].State != want {
				t.Fatal("incorrect durable claim", state.Claims[op.TaskID].State)
			}
		})
	}
}

func TestHistoryEncryptionRejectsSwappingMetadataKeysAndInstances(t *testing.T) {
	for _, failure := range []string{"ciphertext", "salt", "digest", "key-id", "missing-key", "instance", "node"} {
		t.Run(failure, func(t *testing.T) {
			v, store, save, raw, _ := historyFixture(t)
			ctx := context.Background()
			if _, err := v.Save(ctx, save, raw); err != nil {
				t.Fatal(err)
			}
			state, err := decodeHistory(store.raw, v.scope)
			if err != nil {
				t.Fatal(err)
			}
			op := historyRestoreOp(save)
			switch failure {
			case "ciphertext":
				state.Versions[0].Ciphertext[15] ^= 1
			case "salt":
				state.Versions[0].Salt[0] ^= 1
			case "digest":
				state.Versions[0].Version.ContentSHA256 = strings.Repeat("f", 64)
				op.ContentSHA256 = state.Versions[0].Version.ContentSHA256
			case "key-id":
				state.Versions[0].KeyID = "key-2"
				v.keys["key-2"] = bytes.Clone(v.keys["key-1"])
			case "missing-key":
				v.active = "key-2"
				v.keys["key-2"] = bytes.Repeat([]byte{3}, 32)
				delete(v.keys, "key-1")
			case "instance":
				state.Scope.InstanceID = "instance-b"
			case "node":
				state.Scope.NodeID = "node-b"
			}
			store.raw, _ = json.Marshal(state)
			err = v.WithRestoreSource(ctx, op, func(context.Context, HistoryVersion, []byte) error {
				t.Error("tampered/cross-instance source disclosed")
				return nil
			})
			if err != ErrHistoryUnknown && err != ErrHistoryRejected {
				t.Fatal("tampered history accepted", err)
			}
		})
	}
}

func TestHistoryCASAndAuthorizationFailBeforeAnyConsumer(t *testing.T) {
	v, store, op, raw, keys := historyFixture(t)
	ctx := context.Background()
	other, err := NewHistoryVault(store, v.scope, "key-1", keys, v.before, v.commit)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for i, vault := range []*HistoryVault{v, other} {
		wg.Add(1)
		go func(i int, vault *HistoryVault) {
			defer wg.Done()
			attempt := op
			attempt.TaskID = fmt.Sprintf("parallel-%d", i)
			attempt.VersionID = attempt.TaskID
			_, err := vault.Save(ctx, attempt, raw)
			results <- err
		}(i, vault)
	}
	wg.Wait()
	close(results)
	success, conflict := 0, 0
	for err := range results {
		switch err {
		case nil:
			success++
		case ErrHistoryConflict:
			conflict++
		default:
			t.Fatal(err)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatal("concurrent history lost CAS", success, conflict)
	}
	for _, mutate := range []func(*HistoryOperation){func(op *HistoryOperation) { op.Scope.InstanceID = "other" }, func(op *HistoryOperation) { op.ActorID = "intruder" }, func(op *HistoryOperation) { op.PluginRevision = 2 }, func(op *HistoryOperation) { op.CurrentConfigRevision = 8 }} {
		attempt := op
		mutate(&attempt)
		before := bytes.Clone(store.raw)
		if _, err = v.Save(ctx, attempt, raw); err != ErrHistoryRejected || !bytes.Equal(before, store.raw) {
			t.Fatal("unauthorized write changed history", err)
		}
	}
	before := bytes.Clone(store.raw)
	store.fail = true
	attempt := op
	attempt.TaskID = "failed-commit"
	attempt.VersionID = attempt.TaskID
	attempt.ExpectedHistoryRevision = 1
	if meta, err := v.Save(ctx, attempt, raw); err == nil || meta.ID != "" || !bytes.Equal(before, store.raw) {
		t.Fatal("failed store acknowledged history", err)
	}
}

func TestHistoryMissingDependenciesLimitsBadJSONAndObservedClockRollback(t *testing.T) {
	v, store, op, raw, keys := historyFixture(t)
	ctx := context.Background()
	if _, err := NewHistoryVault(store, v.scope, "key-1", keys, nil, v.commit); err != ErrHistoryRejected {
		t.Fatal("missing current gate accepted")
	}
	if _, err := NewHistoryVault(store, v.scope, "key-1", keys, v.before, nil); err != ErrHistoryRejected {
		t.Fatal("missing commit gate accepted")
	}
	for _, bad := range [][]byte{[]byte(`[]`), []byte(`null`), []byte(`{"bad":`), []byte(`{"key":"-----BEGIN PRIVATE KEY-----"}`), bytes.Repeat([]byte(" "), (1<<20)+1)} {
		attempt := op
		attempt.ContentSHA256 = historyDigest(bad)
		if _, err := v.Save(ctx, attempt, bad); err != ErrHistoryRejected {
			t.Fatal("invalid artifact stored", err)
		}
	}
	v.before = func(context.Context, HistoryCheck) error { panic("authority source failed") }
	if _, err := v.Save(ctx, op, raw); err != ErrHistoryRejected {
		t.Fatal("authority panic accepted")
	}
	v, store, op, raw, _ = historyFixture(t)
	for i := 0; i < 16; i++ {
		attempt := op
		attempt.TaskID = fmt.Sprintf("save-%d", i)
		attempt.VersionID = attempt.TaskID
		attempt.ExpectedHistoryRevision = uint64(i)
		if _, err := v.Save(ctx, attempt, raw); err != nil {
			t.Fatal(err)
		}
	}
	attempt := op
	attempt.TaskID = "capacity-overflow"
	attempt.VersionID = attempt.TaskID
	attempt.ExpectedHistoryRevision = 16
	if _, err := v.Save(ctx, attempt, raw); err != ErrHistoryRejected {
		t.Fatal("history silently evicted or exceeded bound", err)
	}
	state, err := decodeHistory(store.raw, v.scope)
	if err != nil {
		t.Fatal(err)
	}
	v.now = func() time.Time { return state.Clock.Add(-time.Second) }
	list := op
	list.Action = "plugins.history.list"
	list.VersionID = ""
	list.ContentSHA256 = ""
	list.TargetVersion = ""
	list.ExpectedHistoryRevision = 16
	if _, err := v.List(ctx, list); err != ErrHistoryRejected {
		t.Fatal("clock rollback served history", err)
	}
}

func TestHistoryNoOpBackendCannotReportSuccessfulSaveOrEmptyList(t *testing.T) {
	v, _, op, raw, keys := historyFixture(t)
	broken, err := NewHistoryVault(droppedHistoryCallbacks{}, v.scope, "key-1", keys, v.before, v.commit)
	if err != nil {
		t.Fatal(err)
	}
	defer broken.Close()
	if meta, err := broken.Save(context.Background(), op, raw); err != ErrHistoryRejected || meta.ID != "" {
		t.Fatal("missing transaction callback became successful snapshot", err)
	}
	op.Action = "plugins.history.list"
	op.VersionID = ""
	op.ContentSHA256 = ""
	op.TargetVersion = ""
	if list, err := broken.List(context.Background(), op); err != ErrHistoryRejected || list != nil {
		t.Fatal("missing read callback became empty success", err)
	}
}
