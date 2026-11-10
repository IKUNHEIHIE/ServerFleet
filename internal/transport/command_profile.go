package transport

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"unicode"
)

const CommandCapability = "tasks.exec.v1"
const RootCommandCapability = "host.root"

// A profile ID references trusted Agent startup policy, never an executable
// path, environment, cwd, sudo password or an unreviewed server-side shell.
type CommandParameters struct {
	ProfileID      string
	Arguments      []string
	TimeoutSeconds int
	PolicySHA256   string
}

func ParseCommandParameters(raw []byte) (CommandParameters, error) {
	var p CommandParameters
	var fields map[string]string
	if len(raw) == 0 || len(raw) > 16<<10 || json.Unmarshal(raw, &fields) != nil || len(fields) != 4 || !FileID(fields["profileId"]) || !FileDigest(fields["policySha256"]) {
		return p, ErrLinkRejected
	}
	var seconds int
	if json.Unmarshal([]byte(fields["timeoutSeconds"]), &seconds) != nil || seconds < 1 || seconds > 300 {
		return p, ErrLinkRejected
	}
	decoder := json.NewDecoder(strings.NewReader(fields["arguments"]))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&p.Arguments) != nil || p.Arguments == nil || len(p.Arguments) > 32 || decoder.Decode(&struct{}{}) != io.EOF {
		return p, ErrLinkRejected
	}
	total := 0
	for _, arg := range p.Arguments {
		total += len(arg)
		if len(arg) > 512 || strings.ContainsFunc(arg, unicode.IsControl) {
			return p, ErrLinkRejected
		}
	}
	if total > 768 {
		return p, ErrLinkRejected
	}
	canonicalArgs, _ := json.Marshal(p.Arguments)
	if !bytes.Equal(canonicalArgs, []byte(fields["arguments"])) {
		return p, ErrLinkRejected
	}
	p.ProfileID, p.PolicySHA256, p.TimeoutSeconds = fields["profileId"], fields["policySha256"], seconds
	return p, nil
}
