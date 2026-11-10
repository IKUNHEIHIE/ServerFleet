package transport

import (
	"encoding/json"
	"regexp"
	"time"
)

const ResidentUpdateAction = "system.updates.agent_apply.v1"
const ControllerUpdateAction = "system.updates.controller_apply"

var releaseVersion = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)

type ResidentUpdateParameters struct {
	Version       string `json:"version"`
	KeyID         string `json:"keyId"`
	ReleaseSHA256 string `json:"releaseSha256"`
	PolicySHA256  string `json:"policySha256"`
}

func ParseResidentUpdateParameters(raw []byte) (ResidentUpdateParameters, error) {
	var values map[string]string
	var p ResidentUpdateParameters
	if len(raw) > 4096 || json.Unmarshal(raw, &values) != nil || len(values) != 4 || json.Unmarshal(raw, &p) != nil || !releaseVersion.MatchString(p.Version) || !FileID(p.KeyID) || !FileDigest(p.ReleaseSHA256) || !FileDigest(p.PolicySHA256) {
		return p, ErrLinkRejected
	}
	return p, nil
}

// Local handoff from an actual approved WSS offer, never an executable path.
type ResidentUpdateIntent struct {
	Format      int       `json:"format"`
	TaskID      string    `json:"taskId"`
	ExecutionID string    `json:"executionId"`
	PlanSHA256  string    `json:"planSha256"`
	NodeID      string    `json:"nodeId"`
	Parameters  string    `json:"parameters"`
	ExpiresAt   time.Time `json:"expiresAt"`
}

func (i ResidentUpdateIntent) Valid(now time.Time) bool {
	_, err := ParseResidentUpdateParameters([]byte(i.Parameters))
	return err == nil && i.Format == 1 && FileID(i.TaskID) && FileID(i.ExecutionID) && FileID(i.NodeID) && FileDigest(i.PlanSHA256) && i.ExpiresAt.After(now) && !i.ExpiresAt.After(now.Add(10*time.Minute))
}
