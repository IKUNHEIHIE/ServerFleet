package server

import (
	"context"
	"errors"
	"fmt"
	"time"

	agentv1 "serverfleet.local/serverfleet/gen/go/serverfleet/agent/v1"
	"serverfleet.local/serverfleet/internal/files"
	"serverfleet.local/serverfleet/internal/storage"
	"serverfleet.local/serverfleet/internal/transport"
)

var errGatewayArtifactBindingNotFound = errors.New("gateway: artifact binding not found")

// gatewayArtifactBindingsSchema is the durable reservation used by the file
// gate to prove that an incoming file offer is exactly the gateway artifact the
// dispatcher planned. The version below is a candidate and is intentionally not
// registered in Migrations(); the controller owner assigns the real number.
const gatewayArtifactBindingsSchema = `CREATE TABLE IF NOT EXISTS gateway_artifact_bindings (
	execution_id TEXT PRIMARY KEY,
	task_id TEXT NOT NULL,
	node_id TEXT NOT NULL,
	transfer_id TEXT NOT NULL,
	plan_digest TEXT NOT NULL,
	gateway_id TEXT NOT NULL,
	plan_id TEXT NOT NULL,
	target_revision INTEGER NOT NULL,
	absolute_path TEXT NOT NULL,
	relative_path TEXT NOT NULL,
	site_path TEXT NOT NULL,
	size INTEGER NOT NULL,
	sha256 TEXT NOT NULL,
	state TEXT NOT NULL,
	result TEXT NOT NULL,
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL
)`

// GatewayArtifactMigrations is the allocated central migration cx/0010 for the
// durable gateway artifact binding table. It is registered only by the shared
// wiring candidate and preserves every historical SQL byte and checksum.
func GatewayArtifactMigrations() []storage.Migration {
	return []storage.Migration{{
		Version:    "cx/0010_gateway_artifact_bindings",
		Name:       "durable gateway artifact task/execution/transfer/plan bindings",
		Statements: []string{gatewayArtifactBindingsSchema},
	}}
}

// EnsureGatewayArtifactBindings creates the candidate table. Production wiring
// uses the migration above; tests and explicit bootstrap may call this.
func EnsureGatewayArtifactBindings(ctx context.Context, db *storage.DB) error {
	if db == nil {
		return ErrGatewayArtifactTransport
	}
	statements := []string{
		gatewayArtifactBindingsSchema,
		`CREATE INDEX IF NOT EXISTS gateway_artifact_bindings_task ON gateway_artifact_bindings(task_id)`,
	}
	for _, statement := range statements {
		if _, err := db.Exec(ctx, statement); err != nil {
			return err
		}
	}
	return nil
}

func saveGatewayArtifactBinding(ctx context.Context, db *storage.DB, binding GatewayArtifactBinding) error {
	if db == nil {
		return ErrGatewayArtifactTransport
	}
	existing, err := loadGatewayArtifactBinding(ctx, db, binding.ExecutionID)
	switch {
	case err == nil:
		if existing.TaskID != binding.TaskID || existing.NodeID != binding.NodeID ||
			existing.TransferID != binding.TransferID || existing.PlanDigest != binding.PlanDigest ||
			existing.PlanID != binding.PlanID || existing.TargetRevision != binding.TargetRevision ||
			existing.GatewayID != binding.GatewayID || existing.AbsolutePath != binding.AbsolutePath ||
			existing.RelativePath != binding.RelativePath || existing.SitePath != binding.SitePath ||
			existing.Size != binding.Size || existing.SHA256 != binding.SHA256 {
			return ErrGatewayArtifactInvalid
		}
		return nil
	case !errors.Is(err, errGatewayArtifactBindingNotFound):
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, err = db.Exec(ctx, `INSERT INTO gateway_artifact_bindings(execution_id,task_id,node_id,transfer_id,plan_digest,gateway_id,plan_id,target_revision,absolute_path,relative_path,site_path,size,sha256,state,result,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		binding.ExecutionID, binding.TaskID, binding.NodeID, binding.TransferID, binding.PlanDigest,
		binding.GatewayID, binding.PlanID, binding.TargetRevision, binding.AbsolutePath,
		binding.RelativePath, binding.SitePath, binding.Size, binding.SHA256, "reserved", "", now, now)
	return err
}

func loadGatewayArtifactBinding(ctx context.Context, db *storage.DB, executionID string) (GatewayArtifactBinding, error) {
	if db == nil || executionID == "" {
		return GatewayArtifactBinding{}, ErrGatewayArtifactTransport
	}
	var count int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM gateway_artifact_bindings WHERE execution_id=?`, executionID).Scan(&count); err != nil {
		return GatewayArtifactBinding{}, fmt.Errorf("gateway: inspect artifact binding: %w", err)
	}
	if count == 0 {
		return GatewayArtifactBinding{}, errGatewayArtifactBindingNotFound
	}
	var binding GatewayArtifactBinding
	err := db.QueryRow(ctx, `SELECT execution_id,task_id,node_id,transfer_id,plan_digest,gateway_id,plan_id,target_revision,absolute_path,relative_path,site_path,size,sha256,state,result FROM gateway_artifact_bindings WHERE execution_id=?`, executionID).
		Scan(&binding.ExecutionID, &binding.TaskID, &binding.NodeID, &binding.TransferID, &binding.PlanDigest,
			&binding.GatewayID, &binding.PlanID, &binding.TargetRevision, &binding.AbsolutePath,
			&binding.RelativePath, &binding.SitePath, &binding.Size, &binding.SHA256, &binding.State, &binding.Result)
	if err != nil {
		return GatewayArtifactBinding{}, fmt.Errorf("gateway: load artifact binding: %w", err)
	}
	return binding, nil
}

// gatewayArtifactGate is the file-frame gate for gateway artifacts. It is
// composed ahead of the CX file worker gate; Matches decides ownership by the
// durable execution binding, so an unrelated file task never reaches here.
type gatewayArtifactGate struct {
	db    *storage.DB
	stage GatewayArtifactStageChecker
}

func NewGatewayArtifactGate(db *storage.DB, stage GatewayArtifactStageChecker) (*gatewayArtifactGate, error) {
	if db == nil || stage == nil {
		return nil, files.ErrChannelUnavailable
	}
	return &gatewayArtifactGate{db: db, stage: stage}, nil
}

// Matches reports whether a file offer belongs to a persisted gateway binding.
// A missing table is an error, not a silent fallback.
func (g *gatewayArtifactGate) Matches(ctx context.Context, executionID string) (bool, error) {
	if g == nil || g.db == nil || executionID == "" {
		return false, files.ErrChannelUnavailable
	}
	var count int
	if err := g.db.QueryRow(ctx, `SELECT count(*) FROM gateway_artifact_bindings WHERE execution_id=?`, executionID).Scan(&count); err != nil {
		return false, fmt.Errorf("gateway: inspect artifact binding: %w", err)
	}
	return count == 1, nil
}

// Authorize rechecks the exact stored binding, the authenticated node identity,
// the immutable file parameters, the frame IDs and the current stage on every
// request and reply.
func (g *gatewayArtifactGate) Authorize(ctx context.Context, observation transport.LinkObservation, offer *agentv1.TaskOffer, envelope *agentv1.Envelope) error {
	if g == nil || g.db == nil || g.stage == nil || offer == nil || envelope == nil ||
		offer.ActionId != transport.FileCapability {
		return files.ErrTaskBindingRequired
	}
	params, err := transport.FileOffer(offer)
	if err != nil {
		return files.ErrTaskBindingRequired
	}
	binding, err := loadGatewayArtifactBinding(ctx, g.db, offer.ExecutionId)
	if err != nil {
		return files.ErrTaskBindingRequired
	}
	if binding.TaskID != offer.TaskId || binding.NodeID != observation.Identity.NodeID ||
		binding.PlanDigest != offer.PlanDigestSha256 ||
		params.TransferID != binding.TransferID || params.RelativePath != binding.RelativePath ||
		params.Direction != string(files.ChannelWrite) || params.Size != uint64(binding.Size) ||
		params.SHA256 != binding.SHA256 {
		return files.ErrTaskBindingRequired
	}
	if task, execution, transfer := transport.FileFrameIDs(envelope); task != "" &&
		(task != binding.TaskID || execution != binding.ExecutionID || transfer != binding.TransferID) {
		return files.ErrTaskBindingRequired
	}
	return g.stage.CheckArtifactStage(ctx, binding.TaskID, binding.NodeID, binding.PlanID, binding.TargetRevision,
		binding.GatewayID, binding.PlanDigest, binding.AbsolutePath, binding.SitePath, binding.SHA256)
}
