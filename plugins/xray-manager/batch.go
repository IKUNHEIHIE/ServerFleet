package xraymanager

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
)

// ErrBatchSpec is returned for target/policy inputs that cannot be planned. It
// never means "partially planned": a rejected plan has no tasks and no digest.
var ErrBatchSpec = errors.New("xray-manager: invalid batch specification")

// BatchMode mirrors the approved deployment policies. canary deploys a single
// wave first; batch uses fixed-size waves; normal keeps one wave per node.
type BatchMode string

const (
	BatchNormal BatchMode = "normal"
	BatchBatch  BatchMode = "batch"
	BatchCanary BatchMode = "canary"
)

// BatchPolicy is frozen into the plan digest. Zero concurrency is invalid so a
// plan can never imply unbounded parallelism.
type BatchPolicy struct {
	Mode                BatchMode
	BatchSize           int
	Concurrency         int
	MaxFailures         int
	StopOnHealthFailure bool
}

const (
	maxBatchTargets = 200
	maxBatchSize    = 50
	maxConcurrency  = 16
)

func (p BatchPolicy) validate() error {
	switch p.Mode {
	case BatchNormal, BatchBatch, BatchCanary:
	default:
		return ErrBatchSpec
	}
	if p.Concurrency < 1 || p.Concurrency > maxConcurrency {
		return ErrBatchSpec
	}
	if p.MaxFailures < 0 || p.MaxFailures > maxBatchTargets {
		return ErrBatchSpec
	}
	if p.Mode == BatchBatch && (p.BatchSize < 1 || p.BatchSize > maxBatchSize) {
		return ErrBatchSpec
	}
	if p.Mode != BatchBatch && p.BatchSize != 0 {
		// Only batch mode freezes a wave size; other modes must not carry one.
		return ErrBatchSpec
	}
	return nil
}

// NodeTarget is the read-only view the controller passes in. Eligibility is
// decided from real observations, not from browser-asserted facts.
type NodeTarget struct {
	NodeID string
	Name   string
	Online bool
	// Managed is true when this node already runs a managed Xray instance.
	Managed bool
	// Architecture is the observed target platform; empty means unknown.
	Architecture string
	// PortConflict marks a detected inbound port conflict.
	PortConflict bool
}

// TargetIssue is a machine-readable per-node reason.
type TargetIssue struct {
	NodeID  string
	Code    string
	Message string
}

// PlannedWave is one ordered group of nodes.
type PlannedWave struct {
	Index   int
	NodeIDs []string
}

// BatchPlan is the frozen, digest-bound plan. It carries no runtime handle.
type BatchPlan struct {
	Targets          []string
	Waves            []PlannedWave
	Policy           BatchPolicy
	Issues           []TargetIssue
	Eligible         bool
	Digest           string
	RequiresAdoption bool
}

// PlanBatch selects targets and builds ordered waves. Rules:
//   - requested node IDs are deduplicated and sorted for a stable digest;
//   - unknown, offline, architecture-less or port-conflicting nodes are
//     ineligible with an explicit issue (never silently dropped);
//   - adopting an existing managed instance requires explicit approval, so the
//     plan records RequiresAdoption instead of assuming consent.
func PlanBatch(requested []string, nodes []NodeTarget, policy BatchPolicy, manageExisting bool) (BatchPlan, error) {
	if err := policy.validate(); err != nil {
		return BatchPlan{}, ErrBatchSpec
	}
	if len(requested) == 0 || len(requested) > maxBatchTargets {
		return BatchPlan{}, ErrBatchSpec
	}
	known := make(map[string]NodeTarget, len(nodes))
	for _, node := range nodes {
		if node.NodeID == "" || len(node.NodeID) > 128 {
			continue
		}
		if _, exists := known[node.NodeID]; exists {
			return BatchPlan{}, ErrBatchSpec
		}
		known[node.NodeID] = node
	}
	seen := make(map[string]bool, len(requested))
	targets := make([]string, 0, len(requested))
	for _, id := range requested {
		if id == "" || len(id) > 128 {
			return BatchPlan{}, ErrBatchSpec
		}
		if seen[id] {
			// Duplicate targets would double-apply a config; reject the plan.
			return BatchPlan{}, ErrBatchSpec
		}
		seen[id] = true
		targets = append(targets, id)
	}
	sort.Strings(targets)

	plan := BatchPlan{Targets: targets, Policy: policy}
	eligibleIDs := make([]string, 0, len(targets))
	for _, id := range targets {
		node, ok := known[id]
		if !ok {
			plan.Issues = append(plan.Issues, TargetIssue{NodeID: id, Code: "unknown_node", Message: "节点不在当前清单中"})
			continue
		}
		switch {
		case !node.Online:
			plan.Issues = append(plan.Issues, TargetIssue{NodeID: id, Code: "node_offline", Message: "节点离线，无法部署"})
			continue
		case node.Architecture == "":
			plan.Issues = append(plan.Issues, TargetIssue{NodeID: id, Code: "architecture_unknown", Message: "目标平台未观测，不能声明支持"})
			continue
		case node.PortConflict:
			plan.Issues = append(plan.Issues, TargetIssue{NodeID: id, Code: "port_conflict", Message: "检测到入站端口冲突"})
			continue
		case node.Managed && !manageExisting:
			plan.Issues = append(plan.Issues, TargetIssue{NodeID: id, Code: "existing_instance", Message: "已有受管实例，需要显式接管审批"})
			plan.RequiresAdoption = true
			continue
		}
		eligibleIDs = append(eligibleIDs, id)
	}
	plan.Eligible = len(eligibleIDs) > 0 && len(plan.Issues) == 0
	plan.Waves = buildWaves(eligibleIDs, policy)
	digest, err := digestPlan(plan)
	if err != nil {
		return BatchPlan{}, err
	}
	plan.Digest = digest
	return plan, nil
}

func buildWaves(ids []string, policy BatchPolicy) []PlannedWave {
	if len(ids) == 0 {
		return nil
	}
	size := 1
	switch policy.Mode {
	case BatchCanary:
		size = 1
	case BatchBatch:
		size = policy.BatchSize
	default:
		size = len(ids)
	}
	if size < 1 {
		size = 1
	}
	waves := make([]PlannedWave, 0, (len(ids)+size-1)/size)
	for index, start := 0, 0; start < len(ids); index, start = index+1, start+size {
		end := start + size
		if end > len(ids) {
			end = len(ids)
		}
		waves = append(waves, PlannedWave{Index: index, NodeIDs: append([]string(nil), ids[start:end]...)})
	}
	return waves
}

// digestPlan hashes only stable plan fields (no timestamps, no runtime IDs) so
// the same intent always produces the same digest and a drifted intent does not.
func digestPlan(plan BatchPlan) (string, error) {
	payload := struct {
		Targets  []string      `json:"targets"`
		Waves    []PlannedWave `json:"waves"`
		Policy   BatchPolicy   `json:"policy"`
		Issues   []TargetIssue `json:"issues"`
		Eligible bool          `json:"eligible"`
		Adoption bool          `json:"requiresAdoption"`
	}{plan.Targets, plan.Waves, plan.Policy, plan.Issues, plan.Eligible, plan.RequiresAdoption}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", ErrBatchSpec
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

// BatchApproval binds an approval to the exact frozen plan.
type BatchApproval struct {
	Digest         string
	Targets        []string
	ApprovedBy     string
	ApprovalID     string
	AdoptionAgreed bool
}

// ApproveBatch verifies that the approval matches the current frozen plan:
// same digest, same sorted targets, a real approver and a real approval id, and
// explicit adoption consent when the plan touches existing instances.
func ApproveBatch(plan BatchPlan, approval BatchApproval) error {
	if plan.Digest == "" || len(plan.Targets) == 0 {
		return ErrBatchSpec
	}
	if approval.Digest != plan.Digest {
		return ErrBatchSpec
	}
	if approval.ApprovedBy == "" || len(approval.ApprovedBy) > 128 || approval.ApprovalID == "" || len(approval.ApprovalID) > 128 {
		return ErrBatchSpec
	}
	if plan.RequiresAdoption && !approval.AdoptionAgreed {
		return ErrBatchSpec
	}
	if len(approval.Targets) != len(plan.Targets) {
		return ErrBatchSpec
	}
	sorted := append([]string(nil), approval.Targets...)
	sort.Strings(sorted)
	for i := range sorted {
		if sorted[i] != plan.Targets[i] {
			return ErrBatchSpec
		}
	}
	return nil
}

// WaveOutcomes summarises execution feedback so a plan can be resumed or
// abandoned without re-deriving the waves.
type WaveOutcomes struct {
	Failed    int
	Succeeded int
}

// ShouldStop reports whether the failure budget or a health failure stops the
// remaining waves. It never reports stop for an empty/invalid plan.
func ShouldStop(plan BatchPlan, outcomes WaveOutcomes, healthFailed bool) bool {
	if plan.Digest == "" {
		return true
	}
	if healthFailed && plan.Policy.StopOnHealthFailure {
		return true
	}
	return outcomes.Failed > plan.Policy.MaxFailures
}
