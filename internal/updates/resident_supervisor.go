package updates

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"serverfleet.local/serverfleet/internal/transport"
	"time"
)

// Operator-provisioned config. Management requests cannot replace any path,
// key, argv, health endpoint or component. The supervisor owns one child only.
type ResidentSupervisorConfig struct {
	FenceConfiguration       string          `json:"fenceConfiguration,omitempty"`
	FenceConfigurationSHA256 string          `json:"fenceConfigurationSha256,omitempty"`
	RecoveryRuntime          string          `json:"recoveryRuntime,omitempty"`
	Format                   int             `json:"format"`
	Component                string          `json:"component"`
	NodeID                   string          `json:"nodeId"`
	Directory                string          `json:"directory"`
	Mailbox                  string          `json:"mailbox"`
	Inbox                    string          `json:"inbox"`
	StageDir                 string          `json:"stageDir"`
	HealthFile               string          `json:"healthFile"`
	Arguments                []string        `json:"arguments"`
	Environment              []string        `json:"environment"`
	Initial                  ReleaseManifest `json:"initial"`
	Policy                   ReleasePolicy   `json:"policy"`
	// Controller uses its live owner repositories even while the child is down.
	Database     string `json:"database,omitempty"`
	SchemaSHA256 string `json:"schemaSha256,omitempty"`
	// Remote supervisors reuse the node's already enrolled private identity.
	IdentityDir           string           `json:"identityDir,omitempty"`
	ControllerOrigin      string           `json:"controllerOrigin,omitempty"`
	ControllerAddresses   []string         `json:"controllerAddresses,omitempty"`
	RootCertificate       string           `json:"rootCertificate,omitempty"`
	RootPin               string           `json:"rootPin,omitempty"`
	FeedRootCertificate   string           `json:"feedRootCertificate,omitempty"`
	FeedRootSHA256        string           `json:"feedRootSha256,omitempty"`
	RootCertificateSHA256 string           `json:"rootCertificateSha256"`
	Feed                  *HTTPReleaseFeed `json:"feed,omitempty"`
}
type ResidentSupervisorHooks struct {
	ServeFence   func(context.Context, ResidentFenceControl) (func() error, error)
	Current      func(context.Context, transport.ResidentUpdateIntent, string) error
	Ready        func(context.Context, ReleaseManifest, OwnedProcessObservation) error
	BeforeStop   func(context.Context, transport.ResidentUpdateIntent) error
	Stopped      func(context.Context, transport.ResidentUpdateIntent) error
	RollbackSafe func(context.Context) error
	Commit       func(context.Context, transport.ResidentUpdateIntent, InstallReceipt) (string, error)
}

func ResidentPolicySHA(c ResidentSupervisorConfig) string {
	// Node identity is bound independently in both the task target and local
	// mailbox. Identical provisioned host policies may share a fleet digest.
	c.NodeID = ""
	b, _ := json.Marshal(c)
	return releaseDigest(b)
}
func LoadResidentSupervisor(filename string) (ResidentSupervisorConfig, error) {
	var c ResidentSupervisorConfig
	if !filepath.IsAbs(filename) || !healthDirectoryPrivate(filepath.Dir(filename)) {
		return c, ErrArtifactRejected
	}
	raw, e := readSizedRegularFile(filename, 32<<10)
	if e != nil {
		return c, e
	}
	d := json.NewDecoder(bytesReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&c) != nil || d.Decode(&struct{}{}) != io.EOF {
		return c, ErrArtifactRejected
	}
	if !validPlanDigest(c.RootCertificateSHA256) {
		return c, ErrArtifactRejected
	}
	root, e := readSizedRegularFile(c.RootCertificate, 128<<10)
	if e != nil || releaseDigest(root) != c.RootCertificateSHA256 {
		return c, ErrArtifactRejected
	}
	if c.FenceConfiguration != "" {
		b, e := readSizedRegularFile(c.FenceConfiguration, 32<<10)
		if e != nil || !validPlanDigest(c.FenceConfigurationSHA256) || releaseDigest(b) != c.FenceConfigurationSHA256 {
			return c, ErrArtifactRejected
		}
	}
	if c.Feed != nil {
		root, e := readSizedRegularFile(c.FeedRootCertificate, 128<<10)
		if e != nil || !validPlanDigest(c.FeedRootSHA256) || releaseDigest(root) != c.FeedRootSHA256 {
			return c, ErrArtifactRejected
		}
		c.Feed.RootPEM = root
	}
	return c, nil
}

// A separate adapter avoids retaining a private request buffer in errors/logs.
type byteReader struct{ b []byte }

func (r *byteReader) Read(p []byte) (int, error) {
	if len(r.b) == 0 {
		return 0, io.EOF
	}
	n := copy(p, r.b)
	r.b = r.b[n:]
	return n, nil
}
func bytesReader(b []byte) io.Reader { return &byteReader{b} }
func WriteResidentIntent(directory string, i transport.ResidentUpdateIntent) error {
	if !healthDirectoryPrivate(directory) || !i.Valid(time.Now()) {
		return ErrArtifactRejected
	}
	r, e := os.OpenRoot(directory)
	if e != nil {
		return e
	}
	defer r.Close()
	b, _ := json.Marshal(i)
	f, e := r.OpenFile(i.ExecutionID+".request.json", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if e != nil {
		return ErrUpdateBusy
	}
	n, e := f.Write(b)
	se := f.Sync()
	ce := f.Close()
	if e != nil || n != len(b) || se != nil || ce != nil || syncStagingDirectory(r, ".") != nil {
		return ErrApplyUnknown
	}
	return nil
}

func writeResidentRecord(root *os.Root, name string, value any) error {
	b, e := json.Marshal(value)
	if e != nil || len(b) > 32<<10 {
		return ErrArtifactRejected
	}
	f, e := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if e != nil {
		return e
	}
	n, we := f.Write(b)
	sy := f.Sync()
	ce := f.Close()
	if we != nil || n != len(b) || sy != nil || ce != nil || syncStagingDirectory(root, ".") != nil {
		return ErrApplyUnknown
	}
	return nil
}
