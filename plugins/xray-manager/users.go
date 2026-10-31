package xraymanager

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

var ErrManagedUser = errors.New("xray-manager: user state rejected or unavailable")
var ErrManagedUserConflict = errors.New("xray-manager: user revision or operation conflict")

type ManagedUserScope struct{ NodeID, InstanceID string }

// This is business metadata, never a second credential store. Enabled/Deleted
// describe desired configuration; Application records confirmed task evidence.
type ManagedUser struct {
	ID, InboundID, DisplayName, StatsEmail, IdentityRef string
	CredentialRevision, Revision                        uint64
	Enabled, Deleted                                    bool
	Application, PendingTaskID, PendingPlanDigest       string
	PendingAction                                       string
	AppliedRevision, AppliedConfigRevision              uint64
	CreatedAt, UpdatedAt                                time.Time
}
type ManagedUserOperation struct {
	Scope                                          ManagedUserScope
	SubjectID, OperationID, TaskID, Source, Action string
	UserID, PlanDigest                             string
	ExpectedRevision                               uint64
	ExpiresAt                                      time.Time
}
type ManagedUserChange struct {
	InboundID, DisplayName, IdentityRef string
	CredentialRevision                  uint64
	Enabled                             bool
}
type ManagedUserApplication struct {
	Result         string // applied or unknown; never infers success from queued
	ConfigRevision uint64
}
type ManagedUserPageRequest struct {
	AfterID, SnapshotID string
	Limit               int
	IncludeDeleted      bool
}
type ManagedUserPage struct {
	Users                 []ManagedUser
	SnapshotID, NextAfter string
}
type ManagedUserAudit struct {
	Operation   ManagedUserOperation
	Change      ManagedUserChange
	Application ManagedUserApplication
	Revision    uint64
	Result      string
	At          time.Time
}

// A trusted platform Host adapter must persist BOTH returned state and complete
// audit events in ONE transaction, invoke fn exactly once, and never retry it.
// txCtx carries that transaction's current read-only authorization reader.
// No SQL, Agent channel, scheduler or secret access is exposed to this package.
type ManagedUserStore interface {
	View(context.Context, func(context.Context, []byte) error) error
	Update(context.Context, func(context.Context, []byte) ([]byte, []ManagedUserAudit, error)) error
}
type ManagedUserCheck struct {
	Operation   ManagedUserOperation
	Change      ManagedUserChange
	Application ManagedUserApplication
	Page        ManagedUserPageRequest
	Stage       string
}
type ManagedUserGate func(context.Context, ManagedUserCheck) error
type managedUserInput struct {
	Change      ManagedUserChange
	Application ManagedUserApplication
	Page        ManagedUserPageRequest
}

// Called inside the SAME transaction for creation and export. Must resolve the
// CENTRAL reference to the exact scope/user/inbound/revision and ready state.
type ManagedUserMaterialCheck func(context.Context, ManagedUserScope, ManagedUser) error

type ManagedUserService struct {
	scope    ManagedUserScope
	store    ManagedUserStore
	gate     ManagedUserGate
	material ManagedUserMaterialCheck
}
type managedUserReceipt struct {
	Digest, UserID string
	Revision       uint64
}
type managedUserState struct {
	Format     int
	Scope      ManagedUserScope
	ClockFloor time.Time
	Users      map[string]ManagedUser
	Receipts   map[string]managedUserReceipt
}

func validManagedScope(s ManagedUserScope) bool {
	return safeConfigID(s.NodeID) && safeConfigID(s.InstanceID)
}
func managedDigest(v any) string {
	raw, _ := json.Marshal(v)
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:])
}
func managedStatsEmail(id string) string {
	// Match the existing versioned template. CounterKey.InstanceID provides the
	// process/instance boundary; display names never enter the counter identity.
	return "sf-user-" + id
}
func validManagedName(name string) bool {
	if name == "" || len(name) > 128 || !utf8.ValidString(name) || strings.TrimSpace(name) != name {
		return false
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

// Only trusted new-instance provisioning may insert this seed. Missing/corrupt
// state is never interpreted as an empty instance or automatically reseeded.
func ManagedUserSeed(scope ManagedUserScope) ([]byte, error) {
	if !validManagedScope(scope) {
		return nil, ErrManagedUser
	}
	return json.Marshal(managedUserState{Format: 1, Scope: scope, Users: map[string]ManagedUser{}, Receipts: map[string]managedUserReceipt{}})
}
func NewManagedUserService(scope ManagedUserScope, store ManagedUserStore, gate ManagedUserGate, material ManagedUserMaterialCheck) (*ManagedUserService, error) {
	if !validManagedScope(scope) || store == nil || gate == nil || material == nil {
		return nil, ErrManagedUser
	}
	return &ManagedUserService{scope, store, gate, material}, nil
}
func decodeManagedUsers(raw []byte, scope ManagedUserScope) (*managedUserState, error) {
	var st managedUserState
	if len(raw) == 0 || len(raw) > 8<<20 || json.Unmarshal(raw, &st) != nil || st.Format != 1 || st.Scope != scope || st.Users == nil || st.Receipts == nil || len(st.Users) > 5000 || len(st.Receipts) > 10000 {
		return nil, ErrManagedUser
	}
	for id, u := range st.Users {
		if id != u.ID || !safeConfigID(id) || !safeConfigID(u.InboundID) || !safeConfigID(u.IdentityRef) || !validManagedName(u.DisplayName) || u.StatsEmail != managedStatsEmail(id) || u.CredentialRevision == 0 || u.Revision == 0 || u.CreatedAt.IsZero() || u.UpdatedAt.Before(u.CreatedAt) || u.UpdatedAt.After(st.ClockFloor) || u.AppliedRevision > u.Revision || u.Deleted && u.Enabled {
			return nil, ErrManagedUser
		}
		switch u.Application {
		case "pending", "unknown":
			if !safeConfigID(u.PendingTaskID) || !validManagedDigest(u.PendingPlanDigest) || u.AppliedRevision >= u.Revision {
				return nil, ErrManagedUser
			}
			if u.PendingAction != "create" && u.PendingAction != "rename" && u.PendingAction != "set-enabled" && u.PendingAction != "remove" {
				return nil, ErrManagedUser
			}
		case "applied":
			if u.PendingTaskID != "" || u.PendingPlanDigest != "" || u.PendingAction != "" || u.AppliedRevision != u.Revision || u.AppliedConfigRevision == 0 {
				return nil, ErrManagedUser
			}
		default:
			return nil, ErrManagedUser
		}
	}
	for id, receipt := range st.Receipts {
		if !safeConfigID(id) || !validManagedDigest(receipt.Digest) || receipt.Revision == 0 || receipt.Revision > st.Users[receipt.UserID].Revision {
			return nil, ErrManagedUser
		}
	}
	return &st, nil
}
func validManagedDigest(digest string) bool {
	raw, err := hex.DecodeString(digest)
	return err == nil && len(raw) == 32 && hex.EncodeToString(raw) == digest
}
func (s *ManagedUserService) check(ctx context.Context, op ManagedUserOperation, input managedUserInput, stage string) (err error) {
	defer func() {
		if recover() != nil {
			err = ErrManagedUser
		}
	}()
	now := time.Now()
	if s == nil || ctx == nil || ctx.Err() != nil || op.Scope != s.scope || !safeConfigID(op.SubjectID) || !safeConfigID(op.OperationID) || op.Action != "list" && !safeConfigID(op.UserID) || !validManagedDigest(op.PlanDigest) || !now.Before(op.ExpiresAt) || op.ExpiresAt.After(now.Add(15*time.Minute)) {
		return ErrManagedUser
	}
	if op.Source != "web" && op.Source != "plugin" && op.Source != "workflow" && op.Source != "ai" {
		return ErrManagedUser
	}
	switch op.Action {
	case "create":
		if op.ExpectedRevision != 0 || !safeConfigID(op.TaskID) {
			return ErrManagedUser
		}
	case "rename", "set-enabled", "remove", "application":
		if op.ExpectedRevision == 0 || !safeConfigID(op.TaskID) {
			return ErrManagedUser
		}
	case "read", "export":
		if op.ExpectedRevision == 0 || op.TaskID != "" || op.Action == "export" && op.Source != "web" {
			return ErrManagedUser
		}
	case "list":
		if op.UserID != "" || op.TaskID != "" || op.ExpectedRevision != 0 {
			return ErrManagedUser
		}
	default:
		return ErrManagedUser
	}
	bounded, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err = s.gate(bounded, ManagedUserCheck{Operation: op, Change: input.Change, Application: input.Application, Page: input.Page, Stage: stage}); err != nil || bounded.Err() != nil {
		return ErrManagedUser
	}
	return nil
}
func (s *ManagedUserService) update(ctx context.Context, op ManagedUserOperation, payload managedUserInput, mutate func(context.Context, *managedUserState, time.Time) (ManagedUser, error)) (ManagedUser, error) {
	var result ManagedUser
	if err := s.check(ctx, op, payload, "prepare"); err != nil {
		return result, err
	}
	digest := managedDigest(struct {
		Operation ManagedUserOperation
		Payload   managedUserInput
	}{op, payload})
	called := false
	err := s.store.Update(ctx, func(txCtx context.Context, raw []byte) ([]byte, []ManagedUserAudit, error) {
		if called || txCtx == nil || txCtx.Err() != nil || ctx.Err() != nil {
			return nil, nil, ErrManagedUser
		}
		called = true
		st, err := decodeManagedUsers(raw, s.scope)
		now := time.Now().UTC()
		if err != nil || now.Before(st.ClockFloor) || s.check(txCtx, op, payload, "commit-prepare") != nil {
			return nil, nil, ErrManagedUser
		}
		if receipt, exists := st.Receipts[op.OperationID]; exists {
			if receipt.Digest != digest || st.Users[receipt.UserID].Revision != receipt.Revision {
				return nil, nil, ErrManagedUserConflict
			}
			result = st.Users[receipt.UserID]
			return bytes.Clone(raw), nil, s.check(txCtx, op, payload, "commit")
		}
		if len(st.Receipts) >= 10000 {
			return nil, nil, ErrManagedUser
		}
		result, err = mutate(txCtx, st, now)
		if err != nil {
			return nil, nil, err
		}
		st.ClockFloor = now
		st.Users[result.ID] = result
		st.Receipts[op.OperationID] = managedUserReceipt{digest, result.ID, result.Revision}
		next, err := json.Marshal(st)
		if err != nil || len(next) > 8<<20 || s.check(txCtx, op, payload, "commit") != nil {
			return nil, nil, ErrManagedUser
		}
		return next, []ManagedUserAudit{{Operation: op, Change: payload.Change, Application: payload.Application, Revision: result.Revision, Result: result.Application, At: now}}, nil
	})
	if err != nil {
		return ManagedUser{}, err
	}
	if !called || s.check(ctx, op, payload, "deliver") != nil {
		return ManagedUser{}, ErrManagedUser
	}
	return result, nil
}

func (s *ManagedUserService) Change(ctx context.Context, op ManagedUserOperation, change ManagedUserChange) (ManagedUser, error) {
	switch op.Action {
	case "create":
		if !safeConfigID(change.InboundID) || !safeConfigID(change.IdentityRef) || change.CredentialRevision == 0 || !validManagedName(change.DisplayName) {
			return ManagedUser{}, ErrManagedUser
		}
	case "rename":
		if !validManagedName(change.DisplayName) || change.InboundID != "" || change.IdentityRef != "" || change.CredentialRevision != 0 || change.Enabled {
			return ManagedUser{}, ErrManagedUser
		}
	case "set-enabled":
		if change.InboundID != "" || change.IdentityRef != "" || change.CredentialRevision != 0 || change.DisplayName != "" {
			return ManagedUser{}, ErrManagedUser
		}
	case "remove":
		if change != (ManagedUserChange{}) {
			return ManagedUser{}, ErrManagedUser
		}
	default:
		return ManagedUser{}, ErrManagedUser
	}
	return s.update(ctx, op, managedUserInput{Change: change}, func(txCtx context.Context, st *managedUserState, now time.Time) (ManagedUser, error) {
		u, exists := st.Users[op.UserID]
		if op.Action == "create" {
			if exists || len(st.Users) >= 5000 {
				return ManagedUser{}, ErrManagedUserConflict
			}
			u = ManagedUser{ID: op.UserID, InboundID: change.InboundID, DisplayName: change.DisplayName, StatsEmail: managedStatsEmail(op.UserID), IdentityRef: change.IdentityRef, CredentialRevision: change.CredentialRevision, Enabled: change.Enabled, CreatedAt: now}
		} else {
			if !exists || u.Deleted || u.Revision != op.ExpectedRevision || u.Revision == ^uint64(0) {
				return ManagedUser{}, ErrManagedUserConflict
			}
			switch op.Action {
			case "rename":
				u.DisplayName = change.DisplayName
			case "set-enabled":
				u.Enabled = change.Enabled
			case "remove":
				u.Deleted, u.Enabled = true, false
			}
		}
		// Revoked/unavailable secrets must not prevent disabling or removing an
		// existing user. Only creation/re-enable introduces an active reference.
		if op.Action == "create" || op.Action == "set-enabled" && u.Enabled {
			if err := s.checkMaterial(txCtx, u); err != nil {
				return ManagedUser{}, ErrManagedUser
			}
		}
		u.Revision++
		u.UpdatedAt = now
		u.Application, u.PendingTaskID, u.PendingPlanDigest = "pending", op.TaskID, op.PlanDigest
		u.PendingAction = op.Action
		return u, nil
	})
}

// Only the sole task's confirmed persisted-config/health result may mark applied.
// This records evidence; it neither calls HandlerService nor executes a job.
func (s *ManagedUserService) RecordApplication(ctx context.Context, op ManagedUserOperation, evidence ManagedUserApplication) (ManagedUser, error) {
	if op.Action != "application" || evidence.Result != "applied" && evidence.Result != "unknown" || evidence.Result == "applied" && evidence.ConfigRevision == 0 || evidence.Result == "unknown" && evidence.ConfigRevision != 0 {
		return ManagedUser{}, ErrManagedUser
	}
	return s.update(ctx, op, managedUserInput{Application: evidence}, func(txCtx context.Context, st *managedUserState, now time.Time) (ManagedUser, error) {
		u, ok := st.Users[op.UserID]
		if !ok || u.Revision != op.ExpectedRevision || u.Application != "pending" || u.PendingTaskID != op.TaskID || u.PendingPlanDigest != op.PlanDigest || evidence.Result == "applied" && evidence.ConfigRevision < u.AppliedConfigRevision {
			return ManagedUser{}, ErrManagedUserConflict
		}
		if evidence.Result == "applied" {
			if u.PendingAction != "rename" && evidence.ConfigRevision <= u.AppliedConfigRevision {
				return ManagedUser{}, ErrManagedUserConflict
			}
			if u.Enabled && !u.Deleted && s.checkMaterial(txCtx, u) != nil {
				return ManagedUser{}, ErrManagedUser
			}
		}
		u.Application, u.UpdatedAt = evidence.Result, now
		if evidence.Result == "applied" {
			u.AppliedRevision, u.AppliedConfigRevision = u.Revision, evidence.ConfigRevision
			u.PendingTaskID, u.PendingPlanDigest = "", ""
			u.PendingAction = ""
		}
		return u, nil
	})
}

func (s *ManagedUserService) Read(ctx context.Context, op ManagedUserOperation) (ManagedUser, error) {
	var result ManagedUser
	if op.Action != "read" && op.Action != "export" || s.check(ctx, op, managedUserInput{}, "read") != nil {
		return result, ErrManagedUser
	}
	called := false
	err := s.store.View(ctx, func(readCtx context.Context, raw []byte) error {
		if called || readCtx == nil || readCtx.Err() != nil || ctx.Err() != nil {
			return ErrManagedUser
		}
		called = true
		st, err := decodeManagedUsers(raw, s.scope)
		if err != nil || time.Now().Before(st.ClockFloor) || s.check(readCtx, op, managedUserInput{}, "read-current") != nil {
			return ErrManagedUser
		}
		u, ok := st.Users[op.UserID]
		if !ok || u.Revision != op.ExpectedRevision {
			return ErrManagedUserConflict
		}
		if op.Action == "export" && (u.Deleted || !u.Enabled || u.Application != "applied" || u.AppliedRevision != u.Revision || s.checkMaterial(readCtx, u) != nil) {
			return ErrManagedUser
		}
		result = u
		return nil
	})
	if err != nil {
		return ManagedUser{}, err
	}
	if !called || s.check(ctx, op, managedUserInput{}, "deliver") != nil {
		return ManagedUser{}, ErrManagedUser
	}
	return result, nil
}

func (s *ManagedUserService) checkMaterial(ctx context.Context, u ManagedUser) (err error) {
	defer func() {
		if recover() != nil {
			err = ErrManagedUser
		}
	}()
	bounded, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err = s.material(bounded, s.scope, u); err != nil || bounded.Err() != nil {
		return ErrManagedUser
	}
	return nil
}

// Pages are bound to the exact persisted snapshot, viewer, instance and tombstone
// filter. Any intervening metadata/application change requires restarting paging.
func (s *ManagedUserService) List(ctx context.Context, op ManagedUserOperation, request ManagedUserPageRequest) (ManagedUserPage, error) {
	var page ManagedUserPage
	input := managedUserInput{Page: request}
	if op.Action != "list" || request.Limit < 1 || request.Limit > 100 || request.AfterID != "" && (!safeConfigID(request.AfterID) || !validManagedDigest(request.SnapshotID)) || request.SnapshotID != "" && !validManagedDigest(request.SnapshotID) || s.check(ctx, op, input, "read") != nil {
		return page, ErrManagedUser
	}
	called := false
	err := s.store.View(ctx, func(readCtx context.Context, raw []byte) error {
		if called || readCtx == nil || readCtx.Err() != nil || ctx.Err() != nil {
			return ErrManagedUser
		}
		called = true
		st, err := decodeManagedUsers(raw, s.scope)
		if err != nil || time.Now().Before(st.ClockFloor) || s.check(readCtx, op, input, "read-current") != nil {
			return ErrManagedUser
		}
		page.SnapshotID = managedDigest(struct {
			Domain, SubjectID, State string
			Scope                    ManagedUserScope
			IncludeDeleted           bool
		}{"serverfleet.xray.user-page.v1", op.SubjectID, managedDigest(raw), s.scope, request.IncludeDeleted})
		if request.SnapshotID != "" && request.SnapshotID != page.SnapshotID {
			return ErrManagedUserConflict
		}
		ids := make([]string, 0, len(st.Users))
		for id, user := range st.Users {
			if request.IncludeDeleted || !user.Deleted {
				ids = append(ids, id)
			}
		}
		sort.Strings(ids)
		start := 0
		if request.AfterID != "" {
			index := sort.SearchStrings(ids, request.AfterID)
			if index == len(ids) || ids[index] != request.AfterID {
				return ErrManagedUserConflict
			}
			start = index + 1
		}
		end := min(start+request.Limit, len(ids))
		page.Users = make([]ManagedUser, 0, end-start)
		for _, id := range ids[start:end] {
			page.Users = append(page.Users, st.Users[id])
		}
		if end < len(ids) {
			page.NextAfter = ids[end-1]
		}
		return nil
	})
	if err != nil {
		return ManagedUserPage{}, err
	}
	if !called || s.check(ctx, op, input, "deliver") != nil {
		return ManagedUserPage{}, ErrManagedUser
	}
	return page, nil
}
