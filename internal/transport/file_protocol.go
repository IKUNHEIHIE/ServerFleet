package transport

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"io"
	"io/fs"
	"regexp"
	"strconv"
	"strings"

	"google.golang.org/protobuf/proto"
	agentv1 "serverfleet.local/serverfleet/gen/go/serverfleet/agent/v1"
)

const FileCapability = "files.transfer.v1"
const FileChunkBytes = 32 << 10
const FileMaxBytes = 64 << 20

var fileIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

// FileParameters are the immutable normal-privilege task parameters. Paths are
// explicit operator-mapped file-root-relative paths. No absolute path stripping.
// An empty read digest permits stat of this authorized path at open; the observed
// size/hash are frozen in the session before the first byte is released.
type FileParameters struct {
	TransferID     string `json:"transferId"`
	RelativePath   string `json:"relativePath"`
	Direction      string `json:"direction"`
	Size           uint64 `json:"size"`
	SHA256         string `json:"sha256"`
	ConflictPolicy string `json:"conflictPolicy"`
	CreateParents  bool   `json:"createParents"`
}

// The sole task service currently persists map[string]string parameters. Keep
// that exact shared port; numbers and booleans have strict canonical spellings.
func (p FileParameters) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]string{"transferId": p.TransferID, "relativePath": p.RelativePath, "direction": p.Direction, "size": strconv.FormatUint(p.Size, 10), "sha256": p.SHA256, "conflictPolicy": p.ConflictPolicy, "createParents": strconv.FormatBool(p.CreateParents)})
}

func FileID(id string) bool { return fileIDPattern.MatchString(id) }
func FileDigest(s string) bool {
	b, e := hex.DecodeString(s)
	return e == nil && len(b) == 32 && hex.EncodeToString(b) == s
}
func FilePath(s string) bool {
	return fs.ValidPath(s) && s != "." && len(s) <= 1024 && !strings.ContainsAny(s, "\\:\x00") && !strings.HasPrefix(s, ".sf-")
}
func ParseFileParameters(raw []byte) (FileParameters, error) {
	var p FileParameters
	if len(raw) == 0 || len(raw) > 4096 {
		return p, ErrLinkRejected
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	var values map[string]string
	if d.Decode(&values) != nil {
		return p, ErrLinkRejected
	}
	if _, e := d.Token(); e != io.EOF {
		return p, ErrLinkRejected
	}
	size, e := strconv.ParseUint(values["size"], 10, 64)
	if e != nil || strconv.FormatUint(size, 10) != values["size"] || (values["createParents"] != "true" && values["createParents"] != "false") {
		return p, ErrLinkRejected
	}
	p = FileParameters{TransferID: values["transferId"], RelativePath: values["relativePath"], Direction: values["direction"], Size: size, SHA256: values["sha256"], ConflictPolicy: values["conflictPolicy"], CreateParents: values["createParents"] == "true"}
	// Require every exact key once; property order may follow the task owner's
	// canonicalizer. The complete original bytes remain bound by the offer.
	keys := map[string]bool{}
	fields := json.NewDecoder(bytes.NewReader(raw))
	first, _ := fields.Token()
	if first != json.Delim('{') {
		return p, ErrLinkRejected
	}
	for fields.More() {
		keyToken, e := fields.Token()
		key, ok := keyToken.(string)
		if e != nil || !ok || keys[key] {
			return p, ErrLinkRejected
		}
		switch key {
		case "transferId", "relativePath", "direction", "size", "sha256", "conflictPolicy", "createParents":
		default:
			return p, ErrLinkRejected
		}
		keys[key] = true
		var value json.RawMessage
		if fields.Decode(&value) != nil {
			return p, ErrLinkRejected
		}
	}
	if len(keys) != 7 || !FileID(p.TransferID) || !FilePath(p.RelativePath) || p.Size > FileMaxBytes || (p.Direction != "read" && p.Direction != "write") || p.Direction == "write" && !FileDigest(p.SHA256) || p.Direction == "read" && p.SHA256 != "" && !FileDigest(p.SHA256) || (p.SHA256 == "" && p.Size != 0) || p.ConflictPolicy != "skip" || p.CreateParents {
		return p, ErrLinkRejected
	}
	return p, nil
}
func FileOffer(o *agentv1.TaskOffer) (FileParameters, error) {
	if o == nil || !FileID(o.TaskId) || !FileID(o.ExecutionId) || !FileDigest(o.PlanDigestSha256) || o.ActionId != FileCapability || o.ActionSchemaVersion != "1" || o.Privilege != agentv1.Privilege_PRIVILEGE_NORMAL || o.PluginId != "" {
		return FileParameters{}, ErrLinkRejected
	}
	return ParseFileParameters(o.CanonicalParametersJson)
}
func IsFilePayload(p proto.Message) bool {
	switch p.(type) {
	case *agentv1.FileOpenRequest, *agentv1.FileOpenReply, *agentv1.FileChunkRequest, *agentv1.FileChunkReply:
		return true
	}
	return false
}
func FileEnvelope(e *agentv1.Envelope) bool {
	return e != nil && (e.GetFileOpenRequest() != nil || e.GetFileOpenReply() != nil || e.GetFileChunkRequest() != nil || e.GetFileChunkReply() != nil)
}
func FileFrameIDs(e *agentv1.Envelope) (task, execution, transfer string) {
	switch {
	case e.GetFileOpenRequest() != nil:
		r := e.GetFileOpenRequest()
		return r.TaskId, r.ExecutionId, r.TransferId
	case e.GetFileOpenReply() != nil:
		r := e.GetFileOpenReply()
		return r.TaskId, r.ExecutionId, r.TransferId
	case e.GetFileChunkRequest() != nil:
		r := e.GetFileChunkRequest()
		return r.TaskId, r.ExecutionId, r.TransferId
	case e.GetFileChunkReply() != nil:
		r := e.GetFileChunkReply()
		return r.TaskId, r.ExecutionId, r.TransferId
	}
	return
}
func (v semanticValidator) filePayload(payload proto.Message) error {
	if !v.limits.AllowFileFrames || v.ctx.ProtocolMinor < 1 || v.limits.AllowedActions[FileCapability] != "1" {
		return invalid(ActionNotAllowed, "file.capability")
	}
	bad := func() error { return invalid(InvalidField, "file.binding_or_bounds") }
	ids := func(a, b, c string) bool { return FileID(a) && FileID(b) && FileID(c) }
	code := func(s string) bool {
		switch s {
		case "ok", "canceled", "unauthorized", "unsupported", "conflict", "checksum_mismatch", "insufficient_space", "io_error", "unknown":
			return true
		}
		return false
	}
	switch m := payload.(type) {
	case *agentv1.FileOpenRequest:
		if !ids(m.TaskId, m.ExecutionId, m.TransferId) || !FilePath(m.RelativePath) || !FileDigest(m.PlanDigestSha256) || m.Size > FileMaxBytes || m.ResumeOffset > m.Size || (m.Direction != "read" && m.Direction != "write") || m.ConflictPolicy != "skip" || m.CreateParents || m.Direction == "write" && !FileDigest(m.Sha256) || m.Direction == "read" && m.Sha256 != "" && !FileDigest(m.Sha256) {
			return bad()
		}
	case *agentv1.FileOpenReply:
		if !ids(m.TaskId, m.ExecutionId, m.TransferId) || !code(m.ResultCode) || !FileDigest(m.PlanDigestSha256) || m.Size > FileMaxBytes || m.ResumeOffset > m.Size || m.ResultCode == "ok" && (!FilePath(m.RelativePath) || !FileDigest(m.Sha256)) {
			return bad()
		}
	case *agentv1.FileChunkRequest:
		if !ids(m.TaskId, m.ExecutionId, m.TransferId) || m.Offset > FileMaxBytes || m.ReadBytes > FileChunkBytes || len(m.Data) > FileChunkBytes || m.Offset+uint64(len(m.Data)) > FileMaxBytes || m.Cancel && (len(m.Data) > 0 || m.ReadBytes > 0 || m.Finish) || m.ReadBytes > 0 && (len(m.Data) > 0 || m.Finish) || !m.Cancel && m.ReadBytes == 0 && len(m.Data) == 0 && !m.Finish {
			return bad()
		}
	case *agentv1.FileChunkReply:
		if !ids(m.TaskId, m.ExecutionId, m.TransferId) || !code(m.ResultCode) || m.Offset > m.NextOffset || m.NextOffset > FileMaxBytes || len(m.Data) > FileChunkBytes {
			return bad()
		}
	default:
		return bad()
	}
	return nil
}
