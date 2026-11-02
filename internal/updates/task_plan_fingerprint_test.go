package updates

import (
	"encoding/json"
	"strings"
	"testing"
)

func planWithFingerprint() FrozenInstallPlan {
	attempted := ReleaseManifest{Format: 1, Component: "agent", Version: "1.0.1", Sequence: 2, OS: "linux", Architecture: "amd64", SHA256: strings.Repeat("a", 64), Bytes: 1024}
	previous := attempted
	previous.Version = "1.0.0"
	previous.Sequence = 1
	return FrozenInstallPlan{ID: "plan-fingerprint", SubjectID: "admin", NodeID: "node-fingerprint", BackupSHA256: strings.Repeat("b", 64), PolicySHA256: strings.Repeat("c", 64), SchemaFingerprint: strings.Repeat("d", 64), Previous: previous, Attempted: attempted, AllowRollback: true}
}

func TestFrozenInstallPlanRequiresReviewedSchemaFingerprintAndStaysSmall(t *testing.T) {
	plan := planWithFingerprint()
	if !plan.valid() {
		t.Fatal("complete plan rejected")
	}
	request, err := plan.TaskRequest("request-fingerprint")
	if err != nil {
		t.Fatal(err)
	}
	if len(request.Params["install_plan"]) > 1024 {
		t.Fatal("frozen plan exceeds the task parameter bound")
	}

	// A plan persisted before the fingerprint field existed must fail closed,
	// not replay under an unknown schema.
	legacy := struct {
		ID, SubjectID, NodeID, BackupSHA256, PolicySHA256 string
		SchemaFingerprint                                 string
		Previous, Attempted                               ReleaseManifest
		AllowRollback                                     bool
	}{plan.ID, plan.SubjectID, plan.NodeID, plan.BackupSHA256, plan.PolicySHA256, "", plan.Previous, plan.Attempted, true}
	raw, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	var decoded FrozenInstallPlan
	if json.Unmarshal(raw, &decoded) != nil {
		t.Fatal("legacy plan should still parse")
	}
	if decoded.valid() {
		t.Fatal("legacy plan without a schema fingerprint was accepted")
	}

	// The digest is part of the canonical bytes and the approval digest: a
	// changed fingerprint produces different plan bytes.
	legacy.SchemaFingerprint = plan.SchemaFingerprint
	raw, err = json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) == "" || json.Unmarshal(raw, &decoded) != nil || !decoded.valid() {
		t.Fatal("canonical plan with fingerprint rejected")
	}
}
