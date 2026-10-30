package plugins

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"sync"
	"time"
	"unicode/utf8"
)

var ErrHistoryRejected = errors.New("plugins: configuration history unavailable or rejected")
var ErrHistoryConflict = errors.New("plugins: configuration history revision conflict")
var ErrHistoryReplay = errors.New("plugins: history restoration already claimed; reconcile before another task")
var ErrHistoryUnknown = errors.New("plugins: history consumer outcome unknown; no automatic retry")

var historyID = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,127}$`)

type HistoryScope struct{ PluginID, NodeID, InstanceID string }
type HistoryOperation struct {
	Scope                                             HistoryScope
	ActorID, TaskID, Action, VersionID, ContentSHA256 string
	TargetVersion                                     string
	ExpectedHistoryRevision, CurrentConfigRevision    uint64
	PluginRevision                                    uint64
}
type HistoryVersion struct {
	ID, ContentSHA256, TargetVersion string
	Revision                         uint64
	Bytes                            int
	CreatedAt                        time.Time
}
type HistoryAuditEvent struct {
	Scope                           HistoryScope
	ActorID, TaskID, Action, Result string
	VersionID, ContentSHA256        string
	At                              time.Time
}
type HistoryStore interface {
	View(context.Context, func([]byte) error) error
	Update(context.Context, func(context.Context, []byte) ([]byte, error)) error
}

// Gate must verify the exact current actor, unified task/approval, plugin
// enablement/signature/revision, node/instance grants and config revision.
// The commit gate receives the trusted transaction context; never nest writers.
type HistoryCheck struct {
	Operation HistoryOperation
	// Server-derived stage. Observation/finalization verifies the same task's
	// outcome; it must not reapply a stale pre-execution config revision.
	Stage string
}
type HistoryGate func(context.Context, HistoryCheck) error
type sealedHistory struct {
	Version          HistoryVersion
	KeyID            string
	Salt, Ciphertext []byte
}
type historyClaim struct{ Binding, State string }
type historyState struct {
	Format   int
	Scope    HistoryScope
	Revision uint64
	Clock    time.Time
	Versions []sealedHistory
	Claims   map[string]historyClaim
	Outbox   []HistoryAuditEvent
}
type HistoryVault struct {
	mu             sync.Mutex
	store          HistoryStore
	scope          HistoryScope
	active         string
	keys           map[string][]byte
	before, commit HistoryGate
	now            func() time.Time
}

func validHistoryScope(scope HistoryScope) bool {
	return scope.PluginID == "xray-manager" && historyID.MatchString(scope.NodeID) && historyID.MatchString(scope.InstanceID)
}
func historyDigest(raw []byte) string { h := sha256.Sum256(raw); return hex.EncodeToString(h[:]) }
func validHistoryDigest(s string) bool {
	raw, err := hex.DecodeString(s)
	return err == nil && len(raw) == 32 && hex.EncodeToString(raw) == s
}
func (v *HistoryVault) String() string   { return "plugins.HistoryVault{encryption-keys:redacted}" }
func (v *HistoryVault) GoString() string { return v.String() }

func NewHistoryVault(store HistoryStore, scope HistoryScope, activeKey string, keys map[string][]byte, before, commit HistoryGate) (*HistoryVault, error) {
	if store == nil || !validHistoryScope(scope) || !historyID.MatchString(activeKey) || len(keys) == 0 || len(keys) > 16 || len(keys[activeKey]) != 32 || before == nil || commit == nil {
		return nil, ErrHistoryRejected
	}
	v := &HistoryVault{store: store, scope: scope, active: activeKey, keys: map[string][]byte{}, before: before, commit: commit, now: time.Now}
	for id, key := range keys {
		if !historyID.MatchString(id) || len(key) != 32 {
			v.clearKeys()
			return nil, ErrHistoryRejected
		}
		v.keys[id] = bytes.Clone(key)
	}
	return v, nil
}
func (v *HistoryVault) clearKeys() {
	for _, key := range v.keys {
		clear(key)
	}
	v.keys = nil
}
func (v *HistoryVault) Close() {
	if v != nil {
		v.mu.Lock()
		defer v.mu.Unlock()
		v.clearKeys()
	}
}

func decodeHistory(raw []byte, scope HistoryScope) (*historyState, error) {
	var st historyState
	if len(raw) == 0 || len(raw) > 24<<20 || json.Unmarshal(raw, &st) != nil || st.Format != 1 || st.Scope != scope || len(st.Versions) > 16 || len(st.Claims) > 10000 || len(st.Outbox) > 10000 {
		return nil, ErrHistoryRejected
	}
	if st.Claims == nil {
		st.Claims = map[string]historyClaim{}
	}
	seen := map[string]bool{}
	var prior uint64
	for _, entry := range st.Versions {
		m := entry.Version
		if !historyID.MatchString(m.ID) || seen[m.ID] || !validHistoryDigest(m.ContentSHA256) || !pluginVersion.MatchString(m.TargetVersion) || m.Revision == 0 || m.Revision <= prior || m.Revision > st.Revision || m.Bytes < 1 || m.Bytes > 1<<20 || m.CreatedAt.IsZero() || m.CreatedAt.After(st.Clock) || !historyID.MatchString(entry.KeyID) || len(entry.Salt) != 32 || len(entry.Ciphertext) != m.Bytes+28 {
			return nil, ErrHistoryRejected
		}
		seen[m.ID] = true
		prior = m.Revision
	}
	if st.Revision != uint64(len(st.Versions)) {
		return nil, ErrHistoryRejected
	}
	for id, claim := range st.Claims {
		if !historyID.MatchString(id) || !validHistoryDigest(claim.Binding) || (claim.State != "reserved" && claim.State != "delivered" && claim.State != "unknown") {
			return nil, ErrHistoryRejected
		}
	}
	return &st, nil
}
func (v *HistoryVault) gate(ctx context.Context, gate HistoryGate, op HistoryOperation, stage string) (err error) {
	defer func() {
		if recover() != nil {
			err = ErrHistoryRejected
		}
	}()
	if ctx == nil || ctx.Err() != nil || gate == nil || len(v.keys[v.active]) != 32 || op.Scope != v.scope || !historyID.MatchString(op.ActorID) || !historyID.MatchString(op.TaskID) || op.PluginRevision == 0 || op.CurrentConfigRevision == 0 {
		return ErrHistoryRejected
	}
	bounded, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if gate(bounded, HistoryCheck{Operation: op, Stage: stage}) != nil || bounded.Err() != nil {
		return ErrHistoryRejected
	}
	return nil
}
func (v *HistoryVault) change(ctx context.Context, op HistoryOperation, phase string, fn func(*historyState, time.Time) error) error {
	called := false
	err := v.store.Update(ctx, func(txCtx context.Context, raw []byte) ([]byte, error) {
		if called {
			return nil, ErrHistoryRejected
		}
		called = true
		st, err := decodeHistory(raw, v.scope)
		if err != nil || txCtx == nil || txCtx.Err() != nil || ctx.Err() != nil {
			return nil, ErrHistoryRejected
		}
		now := v.now().UTC()
		if now.IsZero() || now.Before(st.Clock) {
			return nil, ErrHistoryRejected
		}
		st.Clock = now
		if err = fn(st, now); err != nil {
			return nil, err
		}
		if len(st.Outbox) > 10000 || v.gate(txCtx, v.commit, op, "commit-"+phase) != nil {
			return nil, ErrHistoryRejected
		}
		next, err := json.Marshal(st)
		if err != nil || len(next) > 24<<20 || ctx.Err() != nil || txCtx.Err() != nil {
			return nil, ErrHistoryRejected
		}
		return next, nil
	})
	if !called {
		return ErrHistoryRejected
	}
	return err
}
func historyAAD(scope HistoryScope, entry sealedHistory) ([]byte, error) {
	return json.Marshal(struct {
		Domain  string
		Scope   HistoryScope
		Version HistoryVersion
		KeyID   string
		Salt    []byte
	}{"serverfleet.plugin.config-history.v1", scope, entry.Version, entry.KeyID, entry.Salt})
}
func (v *HistoryVault) aead(entry sealedHistory) (cipher.AEAD, error) {
	master := v.keys[entry.KeyID]
	if len(master) != 32 || len(entry.Salt) != 32 {
		return nil, ErrHistoryRejected
	}
	// Fresh 256-bit salt per envelope derives a separate AES key. This avoids
	// reusing one random-nonce GCM key across an unbounded multi-instance log.
	key, err := hkdf.Key(sha256.New, master, entry.Salt, "serverfleet.plugin.config-history.aes256.v1", 32)
	if err != nil {
		return nil, ErrHistoryRejected
	}
	defer clear(key)
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, ErrHistoryRejected
	}
	return cipher.NewGCMWithRandomNonce(block)
}
func (v *HistoryVault) seal(meta HistoryVersion, plaintext []byte) (sealedHistory, error) {
	entry := sealedHistory{Version: meta, KeyID: v.active, Salt: make([]byte, 32)}
	if _, err := rand.Read(entry.Salt); err != nil {
		return sealedHistory{}, ErrHistoryRejected
	}
	aead, err := v.aead(entry)
	if err != nil {
		return sealedHistory{}, err
	}
	aad, err := historyAAD(v.scope, entry)
	if err != nil {
		return sealedHistory{}, ErrHistoryRejected
	}
	entry.Ciphertext = aead.Seal(nil, nil, plaintext, aad)
	return entry, nil
}
func (v *HistoryVault) open(entry sealedHistory) ([]byte, error) {
	aead, err := v.aead(entry)
	if err != nil {
		return nil, err
	}
	aad, err := historyAAD(v.scope, entry)
	if err != nil {
		return nil, ErrHistoryRejected
	}
	raw, err := aead.Open(nil, nil, entry.Ciphertext, aad)
	if err != nil || len(raw) != entry.Version.Bytes || historyDigest(raw) != entry.Version.ContentSHA256 {
		clear(raw)
		return nil, ErrHistoryRejected
	}
	return raw, nil
}
func historyAudit(st *historyState, op HistoryOperation, result string, now time.Time) {
	st.Outbox = append(st.Outbox, HistoryAuditEvent{op.Scope, op.ActorID, op.TaskID, op.Action, result, op.VersionID, op.ContentSHA256, now})
}

// Save records a candidate/previous config, NOT deployment success. Raw bytes
// enter only from a program-registered controlled task; never a generic secret
// write API. VersionID must be the unique source task ID. No eviction on full.
func (v *HistoryVault) Save(ctx context.Context, op HistoryOperation, plaintext []byte) (HistoryVersion, error) {
	var zero HistoryVersion
	if v == nil {
		return zero, ErrHistoryRejected
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	if len(plaintext) == 0 || len(plaintext) > 1<<20 {
		return zero, ErrHistoryRejected
	}
	plaintext = bytes.Clone(plaintext)
	defer clear(plaintext)
	if op.Action != "plugins.history.save" || op.VersionID != op.TaskID || !pluginVersion.MatchString(op.TargetVersion) || len(plaintext) == 0 || len(plaintext) > 1<<20 || !utf8.Valid(plaintext) || !json.Valid(plaintext) || !bytes.HasPrefix(bytes.TrimSpace(plaintext), []byte("{")) || bytes.Contains(plaintext, []byte("PRIVATE KEY-----")) || !validHistoryDigest(op.ContentSHA256) || historyDigest(plaintext) != op.ContentSHA256 || v.gate(ctx, v.before, op, "prepare") != nil {
		return zero, ErrHistoryRejected
	}
	var saved HistoryVersion
	err := v.change(ctx, op, "save", func(st *historyState, now time.Time) error {
		for _, entry := range st.Versions {
			if entry.Version.ID == op.VersionID {
				return ErrHistoryReplay
			}
		}
		if op.ExpectedHistoryRevision != st.Revision {
			return ErrHistoryConflict
		}
		if len(st.Versions) >= 16 {
			return ErrHistoryRejected
		}
		meta := HistoryVersion{ID: op.VersionID, ContentSHA256: op.ContentSHA256, TargetVersion: op.TargetVersion, Revision: st.Revision + 1, Bytes: len(plaintext), CreatedAt: now}
		entry, err := v.seal(meta, plaintext)
		if err != nil {
			return err
		}
		st.Revision++
		st.Versions = append(st.Versions, entry)
		historyAudit(st, op, "success", now)
		saved = meta
		return nil
	})
	if err != nil {
		return zero, err
	}
	if v.gate(ctx, v.before, op, "deliver") != nil {
		return zero, ErrHistoryRejected
	}
	return saved, nil
}
func (v *HistoryVault) List(ctx context.Context, op HistoryOperation) ([]HistoryVersion, error) {
	if v == nil {
		return nil, ErrHistoryRejected
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	if op.Action != "plugins.history.list" || op.VersionID != "" || op.ContentSHA256 != "" || op.TargetVersion != "" || v.gate(ctx, v.before, op, "read") != nil {
		return nil, ErrHistoryRejected
	}
	out := make([]HistoryVersion, 0)
	called := false
	err := v.store.View(ctx, func(raw []byte) error {
		if called {
			return ErrHistoryRejected
		}
		called = true
		st, err := decodeHistory(raw, v.scope)
		if err != nil {
			return err
		}
		if op.ExpectedHistoryRevision != st.Revision {
			return ErrHistoryConflict
		}
		if now := v.now().UTC(); now.IsZero() || now.Before(st.Clock) {
			return ErrHistoryRejected
		}
		for i := len(st.Versions) - 1; i >= 0; i-- {
			out = append(out, st.Versions[i].Version)
		}
		return nil
	})
	if !called {
		return nil, ErrHistoryRejected
	}
	if err != nil {
		return nil, err
	}
	if v.gate(ctx, v.before, op, "deliver") != nil {
		return nil, ErrHistoryRejected
	}
	return out, nil
}

// WithRestoreSource claims an immutable source exactly once before decrypting or
// invoking the trusted task consumer. Consumer must not retain bytes or reenter
// the vault. It still needs fresh preflight, target -test, unified approval and
// health/audit; history bytes never authorize an Agent/OS call by themselves.
// A delivered receipt proves source consumption only, NEVER service deployment.
func (v *HistoryVault) WithRestoreSource(ctx context.Context, op HistoryOperation, consume func(context.Context, HistoryVersion, []byte) error) (result error) {
	if v == nil {
		return ErrHistoryRejected
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	if consume == nil || op.Action != "plugins.history.restore-source" || !historyID.MatchString(op.VersionID) || !validHistoryDigest(op.ContentSHA256) || !pluginVersion.MatchString(op.TargetVersion) || v.gate(ctx, v.before, op, "prepare") != nil {
		return ErrHistoryRejected
	}
	bindingRaw, _ := json.Marshal(op)
	binding := historyDigest(bindingRaw)
	var entry sealedHistory
	err := v.change(ctx, op, "claim", func(st *historyState, now time.Time) error {
		if _, exists := st.Claims[op.TaskID]; exists {
			return ErrHistoryReplay
		}
		if len(st.Claims) >= 10000 {
			return ErrHistoryRejected
		}
		if st.Revision != op.ExpectedHistoryRevision {
			return ErrHistoryConflict
		}
		found := false
		for _, candidate := range st.Versions {
			if candidate.Version.ID == op.VersionID && candidate.Version.ContentSHA256 == op.ContentSHA256 && candidate.Version.TargetVersion == op.TargetVersion {
				entry = candidate
				found = true
				break
			}
		}
		if !found {
			return ErrHistoryRejected
		}
		st.Claims[op.TaskID] = historyClaim{binding, "reserved"}
		historyAudit(st, op, "reserved", now)
		return nil
	})
	if err != nil {
		return err
	}
	// Any uncertain post-claim step stays permanently reserved/unknown. A
	// callback panic is captured and never presented as successful restoration.
	known := false
	defer func() {
		defer func() {
			if recover() != nil {
				result = ErrHistoryUnknown
			}
		}()
		if recover() != nil {
			known = false
		}
		finishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		finishErr := v.change(finishCtx, op, "finalize", func(st *historyState, now time.Time) error {
			claim, ok := st.Claims[op.TaskID]
			if !ok || claim.Binding != binding || claim.State != "reserved" {
				return ErrHistoryRejected
			}
			claim.State = "unknown"
			if known {
				claim.State = "delivered"
			}
			st.Claims[op.TaskID] = claim
			historyAudit(st, op, claim.State, now)
			return nil
		})
		if !known || finishErr != nil {
			result = ErrHistoryUnknown
		}
	}()
	raw, err := v.open(entry)
	if err != nil {
		return ErrHistoryUnknown
	}
	defer clear(raw)
	if v.gate(ctx, v.before, op, "consume") != nil || consume(ctx, entry.Version, raw) != nil || v.gate(ctx, v.before, op, "observe") != nil {
		return ErrHistoryUnknown
	}
	known = true
	return nil
}
