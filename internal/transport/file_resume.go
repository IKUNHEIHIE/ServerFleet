package transport

import (
	"encoding/json"
	"strconv"
)

const FileResumeCapability = "files.resume.v1"

func IsFileAction(action string) bool {
	return action == FileCapability || action == FileResumeCapability
}

// A resume is a NEW approved execution. It may adopt only a confirmed prefix
// of an old unpublished private file, never replay an unknown final publish.
type FileResumeSource struct {
	TaskID          string `json:"taskId"`
	ExecutionID     string `json:"executionId"`
	TransferID      string `json:"transferId"`
	PlanSHA256      string `json:"planSha256"`
	ConfirmedOffset uint64 `json:"confirmedOffset"`
	PrefixSHA256    string `json:"prefixSha256"`
}

func ParseFileResumeSource(raw string, size uint64) (FileResumeSource, error) {
	var source FileResumeSource
	if len(raw) > 1024 || json.Unmarshal([]byte(raw), &source) != nil || !FileID(source.TaskID) || !FileID(source.ExecutionID) || !FileID(source.TransferID) || !FileDigest(source.PlanSHA256) || !FileDigest(source.PrefixSHA256) || source.ConfirmedOffset == 0 || source.ConfirmedOffset >= size {
		return source, ErrLinkRejected
	}
	canonical, _ := json.Marshal(source)
	if string(canonical) != raw {
		return source, ErrLinkRejected
	}
	return source, nil
}
func ParseResumedFileParameters(raw []byte) (FileParameters, error) {
	var values map[string]string
	if len(raw) > 4096 || json.Unmarshal(raw, &values) != nil || len(values) != 8 {
		return FileParameters{}, ErrLinkRejected
	}
	resume := values["resumeFrom"]
	delete(values, "resumeFrom")
	canonical, _ := json.Marshal(values)
	p, err := ParseFileParameters(canonical)
	if err != nil || p.Direction != "write" {
		return p, ErrLinkRejected
	}
	if _, err = ParseFileResumeSource(resume, p.Size); err != nil {
		return p, err
	}
	p.ResumeFrom = resume
	return p, nil
}
func FileResumeOffset(p FileParameters) uint64 {
	if p.ResumeFrom == "" {
		return 0
	}
	source, err := ParseFileResumeSource(p.ResumeFrom, p.Size)
	if err != nil {
		return 0
	}
	return source.ConfirmedOffset
}
func FileParameterMap(p FileParameters) map[string]string {
	values := map[string]string{"transferId": p.TransferID, "relativePath": p.RelativePath, "direction": p.Direction, "size": strconv.FormatUint(p.Size, 10), "sha256": p.SHA256, "conflictPolicy": p.ConflictPolicy, "createParents": strconv.FormatBool(p.CreateParents)}
	if p.ResumeFrom != "" {
		values["resumeFrom"] = p.ResumeFrom
	}
	return values
}
