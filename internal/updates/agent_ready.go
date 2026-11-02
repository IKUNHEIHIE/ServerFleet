package updates

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"time"
)

const agentHealthMaxBytes = 4096

// AgentProductReady checks actual resident product readiness. The supervisor
// separately verifies /proc/PID/exe SHA; nonce/PID/freshness bind this report to
// that exact owned process. Build version is compiled into the Agent, not argv.
// NodeCurrent must check the actual Controller session/identity on every probe.
func AgentProductReady(filename, node string, nodeCurrent func(context.Context, string, string) error) func(context.Context, ReleaseManifest, OwnedProcessObservation) error {
	return func(ctx context.Context, m ReleaseManifest, p OwnedProcessObservation) error {
		if !filepath.IsAbs(filename) || !installID(node) || nodeCurrent == nil || m.Component != "agent" || !healthDirectoryPrivate(filepath.Dir(filename)) {
			return ErrArtifactRejected
		}
		ticker := time.NewTicker(40 * time.Millisecond)
		defer ticker.Stop()
		for {
			var v struct {
				Version                     string
				PID                         int
				Nonce, NodeID, ConnectionID string
				StateFormat                 int
				TrustGeneration             uint64
				ObservedAt                  time.Time
			}
			raw, e := readSizedRegularFile(filename, agentHealthMaxBytes)
			if e == nil && json.Unmarshal(raw, &v) == nil && v.PID == p.PID && v.Nonce == p.Nonce && !v.ObservedAt.Before(p.StartedAt) && time.Since(v.ObservedAt) >= 0 && time.Since(v.ObservedAt) < 3*time.Second {
				if v.Version != m.Version || v.NodeID != node || v.StateFormat != 1 || v.TrustGeneration == 0 {
					return ErrHealthUnhealthy
				}
				if nodeCurrent(ctx, node, v.ConnectionID) != nil {
					return ErrApplyUnknown
				}
				return nil
			}
			select {
			case <-ctx.Done():
				return ErrApplyUnknown
			case <-ticker.C:
			}
		}
	}
}

// readSizedRegularFile opens the entry once, proves the opened handle is the
// same non-symlink regular file that was measured and reads at most max bytes
// from that handle, so a swap or growth between Lstat and Read cannot slip
// unbounded data (or a replaced file) into the readiness decision.
func readSizedRegularFile(name string, max int64) ([]byte, error) {
	before, err := os.Lstat(name)
	if err != nil || !before.Mode().IsRegular() || before.Size() <= 0 || before.Size() > max {
		return nil, ErrArtifactRejected
	}
	f, err := os.Open(name)
	if err != nil {
		return nil, ErrArtifactRejected
	}
	defer f.Close()
	after, err := f.Stat()
	if err != nil || !os.SameFile(before, after) || !after.Mode().IsRegular() || after.Size() != before.Size() || after.Size() > max {
		return nil, ErrArtifactRejected
	}
	raw, err := io.ReadAll(io.LimitReader(f, max+1))
	if err != nil || int64(len(raw)) != after.Size() {
		return nil, ErrArtifactRejected
	}
	return raw, nil
}
