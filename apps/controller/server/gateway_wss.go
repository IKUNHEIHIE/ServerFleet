package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"time"

	agentv1 "serverfleet.local/serverfleet/gen/go/serverfleet/agent/v1"
	"serverfleet.local/serverfleet/internal/files"
	"serverfleet.local/serverfleet/internal/storage"
	"serverfleet.local/serverfleet/internal/transport"
)

func gatewayContentDigest(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

// GatewayArtifactBinding is the immutable reservation that binds one gateway
// artifact to one unified task, plan revision, execution and transfer. It is
// persisted before any byte is sent so the file gate can compare the exact
// offer/task/node/plan that the Agent receives.
type GatewayArtifactBinding struct {
	TaskID      string
	NodeID      string
	ExecutionID string
	TransferID  string
	PlanDigest  string
	GatewayID   string
	PlanID      string
	// TargetRevision is the persisted publication plan target revision.
	TargetRevision uint64
	AbsolutePath   string
	RelativePath   string
	// SitePath is the frozen nginx site path from the approved task parameters.
	SitePath string
	Size     int64
	Content  []byte
	SHA256   string
	State    string
	Result   string
}

func (b GatewayArtifactBinding) validate() error {
	if !transport.FileID(b.TaskID) || !transport.FileID(b.NodeID) ||
		!transport.FileID(b.ExecutionID) || !transport.FileID(b.TransferID) ||
		!transport.FileDigest(b.PlanDigest) || !validGatewayDigest(b.SHA256) ||
		b.GatewayID == "" || b.PlanID == "" || b.TargetRevision == 0 ||
		b.Size != int64(len(b.Content)) || b.Size > gatewayArtifactMaxBytes {
		return ErrGatewayArtifactInvalid
	}
	if !filepath.IsAbs(b.AbsolutePath) || filepath.Clean(b.AbsolutePath) != b.AbsolutePath ||
		strings.ContainsRune(b.AbsolutePath, '\x00') || strings.ContainsRune(b.AbsolutePath, '\\') {
		return ErrGatewayArtifactInvalid
	}
	if !filepath.IsAbs(b.SitePath) || filepath.Clean(b.SitePath) != b.SitePath ||
		strings.ContainsRune(b.SitePath, '\x00') || strings.ContainsRune(b.SitePath, '\\') {
		return ErrGatewayArtifactInvalid
	}
	if !transport.FilePath(b.RelativePath) {
		return ErrGatewayArtifactInvalid
	}
	if gatewayContentDigest(b.Content) != b.SHA256 {
		return ErrGatewayArtifactInvalid
	}
	return nil
}

// GatewayArtifactStageChecker is the equivalent reviewed stage gate for the
// queued upload phase. It rechecks the current unified task, approval, plan
// target revision, node authority and cancellation before every chunk. It never
// relaxes tasks.CheckExecution, which still requires running for Agent frames.
type GatewayArtifactStageChecker interface {
	CheckArtifactStage(ctx context.Context, taskID, nodeID, planID string, targetRevision uint64, gatewayID, planDigest, artifactPath, sitePath, configDigest string) error
	CheckArtifactChild(ctx context.Context, taskID, nodeID, planID string, targetRevision uint64, gatewayID, planDigest, artifactPath, sitePath, configDigest string) error
}

// GatewayBoundArtifactUploader is the real-channel seam. The dispatcher prefers
// it over the legacy Upload method; a real uploader must never accept a call
// without a persisted binding.
type GatewayBoundArtifactUploader interface {
	UploadBound(ctx context.Context, binding GatewayArtifactBinding) error
}

// gatewayFileOffer builds the exact files.transfer.v1 offer for one gateway
// artifact binding. The immutable parameters carry only the operator-mapped
// relative path; absolute paths never cross the wire.
func gatewayFileOffer(binding files.FileBinding, now time.Time) (*agentv1.TaskOffer, error) {
	params := transport.FileParameters{
		TransferID:     binding.TransferID,
		RelativePath:   binding.RelativePath,
		Direction:      string(binding.Direction),
		Size:           uint64(binding.Size),
		SHA256:         binding.SHA256,
		ConflictPolicy: string(binding.ConflictPolicy),
		CreateParents:  false,
	}
	raw, err := json.Marshal(params)
	if err != nil {
		return nil, files.ErrChannelProtocol
	}
	return &agentv1.TaskOffer{
		TaskId:                  binding.TaskID,
		ExecutionId:             binding.ExecutionID,
		PlanDigestSha256:        binding.PlanDigestSHA256,
		ActionId:                transport.FileCapability,
		CanonicalParametersJson: raw,
		Privilege:               agentv1.Privilege_PRIVILEGE_NORMAL,
		ExpiresAtUnixMs:         now.Add(10 * time.Minute).UnixMilli(),
		TimeoutSeconds:          300,
		ActionSchemaVersion:     "1",
	}, nil
}

type gatewayWSSChannel struct {
	client *transport.FileClient
	now    func() time.Time
}

func (c *gatewayWSSChannel) OpenFileChannel(ctx context.Context, binding files.FileBinding) (files.AgentFileSession, files.FileOpenResult, error) {
	return c.OpenFileChannelForTransfer(ctx, ctx, binding)
}

func (c *gatewayWSSChannel) OpenFileChannelForTransfer(transferCtx, openCtx context.Context, binding files.FileBinding) (files.AgentFileSession, files.FileOpenResult, error) {
	if c == nil || c.client == nil {
		return nil, files.FileOpenResult{}, files.ErrChannelUnavailable
	}
	now := time.Now
	if c.now != nil {
		now = c.now
	}
	offer, err := gatewayFileOffer(binding, now())
	if err != nil {
		return nil, files.FileOpenResult{}, err
	}
	if _, err := transport.FileOffer(offer); err != nil {
		return nil, files.FileOpenResult{}, files.ErrChannelProtocol
	}
	// ChannelTransport releases its frame timeout as soon as Open returns.
	// The session instead belongs to the original unified transfer context.
	session, err := c.client.NewSession(transferCtx, binding.NodeID, offer)
	if err != nil {
		return nil, files.FileOpenResult{}, err
	}
	reply, err := session.Open(openCtx)
	if err != nil {
		_ = session.Close()
		return nil, files.FileOpenResult{}, err
	}
	result := files.FileOpenResult{
		TaskID: reply.TaskId, ExecutionID: reply.ExecutionId, TransferID: reply.TransferId,
		Size: int64(reply.Size), SHA256: reply.Sha256, RelativePath: reply.RelativePath,
		ResumeOffset: int64(reply.ResumeOffset), Skipped: reply.Skipped, Complete: reply.Complete,
		ResultCode: reply.ResultCode, PlanDigestSHA256: reply.PlanDigestSha256,
	}
	return &gatewayWSSSession{session: session, binding: binding}, result, nil
}

type gatewayWSSSession struct {
	session *transport.FileSession
	binding files.FileBinding
}

func (s *gatewayWSSSession) ReadChunk(context.Context, files.FileChunkOp) (files.FileChunkResult, error) {
	return files.FileChunkResult{}, files.ErrChannelProtocol
}

func (s *gatewayWSSSession) WriteChunk(ctx context.Context, op files.FileChunkOp) (files.FileChunkResult, error) {
	if s == nil || s.session == nil {
		return files.FileChunkResult{}, files.ErrChannelDisconnected
	}
	reply, err := s.session.Chunk(ctx, &agentv1.FileChunkRequest{
		TaskId: s.binding.TaskID, ExecutionId: s.binding.ExecutionID, TransferId: s.binding.TransferID,
		Offset: op.Offset, Data: op.Data, Finish: op.Finish, Cancel: op.Cancel,
	})
	if err != nil {
		return files.FileChunkResult{}, err
	}
	return files.FileChunkResult{
		TaskID: reply.TaskId, ExecutionID: reply.ExecutionId, TransferID: reply.TransferId,
		Offset: reply.Offset, NextOffset: reply.NextOffset, Data: reply.Data,
		Complete: reply.Complete, ResultCode: reply.ResultCode,
	}, nil
}

func (s *gatewayWSSSession) Cancel(ctx context.Context, reason string) error {
	_, err := s.WriteChunk(ctx, files.FileChunkOp{Cancel: true})
	return err
}

func (s *gatewayWSSSession) Close() error {
	if s == nil || s.session == nil {
		return nil
	}
	return s.session.Close()
}

// gatewayChannelPaths maps absolute Agent paths to the explicit relative paths
// accepted by the file capability. They are operator-owned and must match the
// Agent file root; an unmapped node keeps the channel closed.
type gatewayChannelPaths struct {
	roots map[string]string
}

func NewGatewayChannelPaths(roots map[string]string) (files.PathMapper, error) {
	if len(roots) == 0 {
		return nil, files.ErrPathMappingUnavailable
	}
	clean := make(map[string]string, len(roots))
	for nodeID, root := range roots {
		if !transport.FileID(nodeID) || root == "" || !filepath.IsAbs(root) || filepath.Clean(root) != root {
			return nil, files.ErrPathMappingUnavailable
		}
		clean[nodeID] = root
	}
	return &gatewayChannelPaths{roots: clean}, nil
}

func (p *gatewayChannelPaths) RelativePath(_ context.Context, nodeID, absolutePath string) (string, error) {
	root, ok := p.roots[nodeID]
	if !ok || !filepath.IsAbs(absolutePath) || filepath.Clean(absolutePath) != absolutePath {
		return "", files.ErrPathMappingUnavailable
	}
	relative, err := filepath.Rel(root, absolutePath)
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", files.ErrPathMappingUnavailable
	}
	relative = filepath.ToSlash(relative)
	if !transport.FilePath(relative) {
		return "", files.ErrPathMappingUnavailable
	}
	return relative, nil
}

func (p *gatewayChannelPaths) AbsolutePath(_ context.Context, nodeID, relativePath string) (string, error) {
	root, ok := p.roots[nodeID]
	if !ok || !transport.FilePath(relativePath) {
		return "", files.ErrPathMappingUnavailable
	}
	return filepath.Join(root, filepath.FromSlash(relativePath)), nil
}

type gatewayChannelUploader struct {
	transport *files.ChannelTransport
	paths     files.PathMapper
	stage     GatewayArtifactStageChecker
	db        *storage.DB
}

var (
	_ GatewayArtifactUploader      = (*gatewayChannelUploader)(nil)
	_ GatewayBoundArtifactUploader = (*gatewayChannelUploader)(nil)
)

// NewGatewayChannelUploader wires the real bounded WSS file channel. Missing
// client, path map, database or stage gate keeps the capability closed; there is
// no fallback to a local staging transport.
// NewGatewayWSSChannel adapts the authenticated ControllerLinks file client to
// the bounded file capability. It never opens SSH or a second queue.
func NewGatewayWSSChannel(client *transport.FileClient) (files.AgentFileChannel, error) {
	if client == nil {
		return nil, files.ErrChannelUnavailable
	}
	return &gatewayWSSChannel{client: client, now: time.Now}, nil
}

func NewGatewayChannelUploader(channel files.AgentFileChannel, roots map[string]string, db *storage.DB, stage GatewayArtifactStageChecker) (GatewayArtifactUploader, error) {
	paths, err := NewGatewayChannelPaths(roots)
	if err != nil {
		return nil, err
	}
	return NewGatewayChannelUploaderWithMapper(channel, paths, db, stage)
}

// NewGatewayChannelUploaderWithMapper reuses the already loaded, owner-checked
// file deployment mapper. Both site and artifact paths must stay inside the
// mapped Agent root; an unmapped node keeps the channel closed.
func NewGatewayChannelUploaderWithMapper(channel files.AgentFileChannel, paths files.PathMapper, db *storage.DB, stage GatewayArtifactStageChecker) (GatewayArtifactUploader, error) {
	if channel == nil || paths == nil || db == nil || stage == nil {
		return nil, ErrGatewayArtifactTransport
	}
	channelTransport, err := files.NewChannelTransport(files.ChannelTransportConfig{Channel: channel, Paths: paths})
	if err != nil {
		return nil, err
	}
	return &gatewayChannelUploader{transport: channelTransport, paths: paths, stage: stage, db: db}, nil
}

// Upload is the legacy unbound seam. A real channel requires the persisted
// task/execution/transfer/plan binding, so this deliberately fails closed.
func (u *gatewayChannelUploader) Upload(context.Context, string, string, string, []byte, string) error {
	return files.ErrTaskBindingRequired
}

func (u *gatewayChannelUploader) UploadBound(ctx context.Context, binding GatewayArtifactBinding) error {
	if u == nil || u.transport == nil || u.paths == nil || u.stage == nil || u.db == nil || ctx == nil {
		return ErrGatewayArtifactTransport
	}
	relative, err := u.paths.RelativePath(ctx, binding.NodeID, binding.AbsolutePath)
	if err != nil {
		return err
	}
	binding.RelativePath = relative
	binding.Size = int64(len(binding.Content))
	if err := binding.validate(); err != nil {
		return err
	}
	if err := u.stage.CheckArtifactStage(ctx, binding.TaskID, binding.NodeID, binding.PlanID, binding.TargetRevision,
		binding.GatewayID, binding.PlanDigest, binding.AbsolutePath, binding.SitePath, binding.SHA256); err != nil {
		return err
	}
	if err := saveGatewayArtifactBinding(ctx, u.db, binding); err != nil {
		return err
	}
	scope := files.WithChannelScope(ctx, binding.TaskID, binding.TransferID, binding.ExecutionID, binding.PlanDigest)
	scope = files.WithChannelAuthorizer(scope, gatewayStageAuthorizer{stage: u.stage, binding: binding})
	if err := u.transport.PushFile(scope, binding.NodeID, binding.AbsolutePath, bytes.NewReader(binding.Content), int64(len(binding.Content)), binding.SHA256); err != nil {
		if errors.Is(err, files.ErrTaskBindingRequired) || errors.Is(err, files.ErrAuthorizationRequired) {
			return err
		}
		return errors.Join(ErrGatewayArtifactTransport, err)
	}
	// A final byte reply is not the child's durable terminal event. In
	// particular the resident Agent has one modifying slot: dispatching nginx
	// before the file execution finishes can race that slot and lose its result.
	return u.waitChild(ctx, binding)
}

func (u *gatewayChannelUploader) waitChild(ctx context.Context, binding GatewayArtifactBinding) error {
	bounded, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		if err := u.stage.CheckArtifactChild(bounded, binding.TaskID, binding.NodeID, binding.PlanID, binding.TargetRevision,
			binding.GatewayID, binding.PlanDigest, binding.AbsolutePath, binding.SitePath, binding.SHA256); err != nil {
			return err
		}
		child, err := loadGatewayArtifactBinding(bounded, u.db, binding.ExecutionID)
		if err != nil {
			return err
		}
		if child.State == "finished" {
			if child.Result == agentv1.ExecutionResult_EXECUTION_RESULT_SUCCEEDED.String() {
				return nil
			}
			return files.ErrAgentUnknown
		}
		if child.State == "rejected" {
			return files.ErrAgentUnknown
		}
		select {
		case <-bounded.Done():
			return errors.Join(files.ErrAgentUnknown, bounded.Err())
		case <-ticker.C:
		}
	}
}

type gatewayStageAuthorizer struct {
	stage   GatewayArtifactStageChecker
	binding GatewayArtifactBinding
}

func (a gatewayStageAuthorizer) AuthorizeChannel(ctx context.Context, direction files.ChannelDirection) error {
	if a.stage == nil || direction != files.ChannelWrite {
		return files.ErrTaskBindingRequired
	}
	return a.stage.CheckArtifactStage(ctx, a.binding.TaskID, a.binding.NodeID, a.binding.PlanID, a.binding.TargetRevision,
		a.binding.GatewayID, a.binding.PlanDigest, a.binding.AbsolutePath, a.binding.SitePath, a.binding.SHA256)
}

// gatewayArtifactExecutionID is the stable child-execution identity domain for
// one gateway artifact. The parent/Nginx agent execution id is never reused,
// because the Agent ledger claims one action per execution id.
func gatewayArtifactExecutionID(taskID, nodeID, planHash, gatewayID string) string {
	digest := sha256.Sum256([]byte(taskID + "\x00" + nodeID + "\x00" + planHash + "\x00" + gatewayID + "\x00artifact"))
	return "gwa-" + hex.EncodeToString(digest[:20])
}

// GatewayArtifactStageSlot breaks the construction cycle between the real
// uploader (needs the dispatcher as its stage gate) and the dispatcher (needs
// the uploader as its artifact sink). Until Bind is called every check fails
// closed; it is a real type used by the central composition, not a placeholder.
type GatewayArtifactStageSlot struct {
	mu    sync.RWMutex
	stage GatewayArtifactStageChecker
}

func (s *GatewayArtifactStageSlot) Bind(stage GatewayArtifactStageChecker) error {
	if stage == nil {
		return files.ErrChannelUnavailable
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stage != nil {
		return files.ErrTransferExecution
	}
	s.stage = stage
	return nil
}

func (s *GatewayArtifactStageSlot) bound() GatewayArtifactStageChecker {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.stage
}

func (s *GatewayArtifactStageSlot) CheckArtifactStage(ctx context.Context, taskID, nodeID, planID string, targetRevision uint64, gatewayID, planDigest, artifactPath, sitePath, configDigest string) error {
	stage := s.bound()
	if stage == nil {
		return files.ErrTaskBindingRequired
	}
	return stage.CheckArtifactStage(ctx, taskID, nodeID, planID, targetRevision, gatewayID, planDigest, artifactPath, sitePath, configDigest)
}

func (s *GatewayArtifactStageSlot) CheckArtifactChild(ctx context.Context, taskID, nodeID, planID string, targetRevision uint64, gatewayID, planDigest, artifactPath, sitePath, configDigest string) error {
	stage := s.bound()
	if stage == nil {
		return files.ErrTaskBindingRequired
	}
	return stage.CheckArtifactChild(ctx, taskID, nodeID, planID, targetRevision, gatewayID, planDigest, artifactPath, sitePath, configDigest)
}
