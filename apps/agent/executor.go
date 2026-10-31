package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"time"

	agentv1 "serverfleet.local/serverfleet/gen/go/serverfleet/agent/v1"
)

var errExecutionRejected = errors.New("agent: execution rejected")
var executionIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

type executionClaim struct{ TaskID, ExecutionID, OfferDigest string }
type executionResult struct {
	Result    agentv1.ExecutionResult
	ExitCode  int32
	Output    []byte
	Truncated bool
}
type agentAction struct {
	// Validate and Run are fixed program-owned capabilities, never client code.
	Validate func([]byte) error
	Run      func(context.Context, []byte) executionResult
	// Output is withheld when this program-owned policy is absent or fails.
	// The callback must remove secrets BEFORE output reaches disk or transport.
	FilterOutput func(context.Context, []byte) ([]byte, error)
}
type agentExecutor struct {
	ledger  *os.Root
	actions map[string]agentAction
	now     func() time.Time
}

// The installer must create a private local ledger directory (0700 on Linux).
// Each execution directory is an immutable durable reservation. A reservation
// without a terminal record means UNKNOWN after restart, never an auto-retry.
func newAgentExecutor(ledgerPath string, actions map[string]agentAction) (*agentExecutor, error) {
	if !filepath.IsAbs(ledgerPath) || len(actions) == 0 {
		return nil, errExecutionRejected
	}
	root, err := os.OpenRoot(ledgerPath)
	if err != nil {
		return nil, errExecutionRejected
	}
	info, err := root.Stat(".")
	if err != nil || !info.IsDir() || runtime.GOOS == "linux" && info.Mode().Perm()&0077 != 0 {
		_ = root.Close()
		return nil, errExecutionRejected
	}
	copyActions := make(map[string]agentAction, len(actions))
	for id, a := range actions {
		if id == "" || a.Validate == nil || a.Run == nil {
			_ = root.Close()
			return nil, errExecutionRejected
		}
		copyActions[id] = a
	}
	return &agentExecutor{root, copyActions, time.Now}, nil
}
func (e *agentExecutor) Close() error {
	if e == nil || e.ledger == nil {
		return nil
	}
	return e.ledger.Close()
}

func offerDigest(offer *agentv1.TaskOffer) string {
	// Exact field binding, including expiry, privilege, scope, schema and plugin.
	// JSON of the generated struct is local ledger format, not the plan digest.
	b, _ := json.Marshal(offer)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func (e *agentExecutor) execute(ctx context.Context, offer *agentv1.TaskOffer) (executionResult, bool, error) {
	return e.executeObserved(ctx, offer, nil)
}

// observe is a program-owned event adapter, not an authorization replacement.
// accepted follows durable reservation; started follows its own durable marker.
// A failed notification prevents NEW effects but never removes the reservation.
func (e *agentExecutor) executeObserved(ctx context.Context, offer *agentv1.TaskOffer, observe func(stage string, previouslySeen bool) error) (executionResult, bool, error) {
	unknown := executionResult{Result: agentv1.ExecutionResult_EXECUTION_RESULT_UNKNOWN}
	if ctx == nil || ctx.Err() != nil || e == nil || e.ledger == nil || e.now == nil || offer == nil {
		return unknown, false, errExecutionRejected
	}
	// Root/Shell capabilities are deliberately absent until the PRIV admission
	// contract and sandbox evidence exist. A privilege claim is not approval.
	// ScopeRevision identifies an AI session scope; web and workflow tasks are
	// unscoped and legitimately carry 0. It is recorded in the offer digest but is
	// never an Agent-side authorization substitute for the Controller.
	if !executionIDPattern.MatchString(offer.ExecutionId) || !executionIDPattern.MatchString(offer.TaskId) || offer.Privilege != agentv1.Privilege_PRIVILEGE_NORMAL || offer.PluginId != "" || offer.ActionSchemaVersion == "" || offer.TimeoutSeconds == 0 || offer.TimeoutSeconds > 3600 || len(offer.CanonicalParametersJson) > 64<<10 {
		return unknown, false, errExecutionRejected
	}
	if digest, err := hex.DecodeString(offer.PlanDigestSha256); err != nil || len(digest) != 32 {
		return unknown, false, errExecutionRejected
	}
	now := e.now()
	if offer.ExpiresAtUnixMs <= now.UnixMilli() || offer.ExpiresAtUnixMs > now.Add(15*time.Minute).UnixMilli() {
		return unknown, false, errExecutionRejected
	}
	action, ok := e.actions[offer.ActionId]
	if !ok || action.Validate(offer.CanonicalParametersJson) != nil {
		return unknown, false, errExecutionRejected
	}
	// Claim the ID before invoking any action. os.Root prevents traversal and
	// symlink escape; the directory O_EXCL-equivalent also serializes processes.
	name := offer.ExecutionId
	err := e.ledger.Mkdir(name, 0700)
	claim := executionClaim{offer.TaskId, offer.ExecutionId, offerDigest(offer)}
	if errors.Is(err, os.ErrExist) {
		data, readErr := e.readRecord(name+"/claim.json", 1024)
		var prior executionClaim
		if readErr != nil || json.Unmarshal(data, &prior) != nil || prior != claim {
			return unknown, true, errExecutionRejected
		}
		if observe != nil && observe("accepted", true) != nil {
			return unknown, true, errExecutionRejected
		}
		data, readErr = e.readRecord(name+"/result.json", 128<<10)
		if readErr != nil {
			return unknown, true, nil
		}
		var result executionResult
		if json.Unmarshal(data, &result) != nil || !validExecutionResult(result) {
			return unknown, true, errExecutionRejected
		}
		return result, true, nil
	}
	if err != nil {
		return unknown, false, errExecutionRejected
	}
	if err = e.writeRecord(name+"/claim.json", claim); err != nil {
		return unknown, false, errExecutionRejected
	}
	if err = e.syncDirectory(name); err != nil {
		return unknown, false, errExecutionRejected
	}
	if err = e.syncDirectory("."); err != nil {
		return unknown, false, errExecutionRejected
	}
	if observe != nil && observe("accepted", false) != nil {
		return unknown, false, errExecutionRejected
	}
	if ctx.Err() != nil {
		return unknown, false, ctx.Err()
	}
	if e.now().UnixMilli() >= offer.ExpiresAtUnixMs {
		return unknown, false, errExecutionRejected
	}
	if err = e.writeRecord(name+"/started.json", executionStarted{At: e.now().UTC()}); err != nil || e.syncDirectory(name) != nil {
		return unknown, false, errExecutionRejected
	}
	if observe != nil && observe("started", false) != nil {
		return unknown, false, errExecutionRejected
	}
	if ctx.Err() != nil || e.now().UnixMilli() >= offer.ExpiresAtUnixMs {
		return unknown, false, errExecutionRejected
	}
	runCtx, cancel := context.WithTimeout(ctx, time.Duration(offer.TimeoutSeconds)*time.Second)
	defer cancel()
	result := runRegisteredAction(runCtx, action, append([]byte(nil), offer.CanonicalParametersJson...))
	if result.Result != agentv1.ExecutionResult_EXECUTION_RESULT_SUCCEEDED && result.Result != agentv1.ExecutionResult_EXECUTION_RESULT_FAILED && result.Result != agentv1.ExecutionResult_EXECUTION_RESULT_CANCELED && result.Result != agentv1.ExecutionResult_EXECUTION_RESULT_TIMED_OUT && result.Result != agentv1.ExecutionResult_EXECUTION_RESULT_UNKNOWN {
		result = unknown
	}
	// Run must cooperate with context; a canceled action can have side effects.
	// Preserve UNKNOWN rather than treating missing/late evidence as a success.
	if runCtx.Err() != nil {
		result.Result = agentv1.ExecutionResult_EXECUTION_RESULT_UNKNOWN
	}
	if len(result.Output) > 64<<10 {
		result.Output = append([]byte(nil), result.Output[:64<<10]...)
		result.Truncated = true
	}
	result = filterExecutionOutput(runCtx, action, result)
	if err = e.writeRecord(name+"/result.json", result); err != nil {
		return unknown, false, errExecutionRejected
	}
	if err = e.syncDirectory(name); err != nil {
		return unknown, false, errExecutionRejected
	}
	return result, false, nil
}

func (e *agentExecutor) readRecord(name string, limit int64) ([]byte, error) {
	f, err := e.ledger.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > limit {
		return nil, errExecutionRejected
	}
	b, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil || int64(len(b)) > limit {
		return nil, errExecutionRejected
	}
	return b, nil
}

func (e *agentExecutor) writeRecord(name string, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	f, err := e.ledger.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	_, writeErr := f.Write(data)
	syncErr := f.Sync()
	closeErr := f.Close()
	return errors.Join(writeErr, syncErr, closeErr)
}
func (e *agentExecutor) syncDirectory(name string) error {
	// Windows tests verify logical reservation/restart, not power-loss fsync.
	// Actual Agent admission is Linux-only and requires directory fsync there.
	if runtime.GOOS == "windows" {
		return nil
	}
	dir, err := e.ledger.Open(name)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
