package bootstrap

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"serverfleet.local/serverfleet/internal/recovery"
	"serverfleet.local/serverfleet/internal/updates"
)

// Freeze identity before stop, then include legitimate ledger finalization in
// the stopped snapshot. Never rewind candidate state or old UNKNOWN results.
type supervisorAgentCheckpoint struct{ directory, identity, state string }

func (s *supervisorAgentCheckpoint) BeforeStop(ctx context.Context) error {
	if ctx == nil || ctx.Err() != nil {
		return updates.ErrApplyUnknown
	}
	if s.state != "" {
		return s.RollbackSafe(ctx)
	}
	d, e := supervisorIdentityDigest(s.directory)
	if e != nil {
		return e
	}
	if s.identity != "" && s.identity != d {
		return updates.ErrApplyUnknown
	}
	s.identity = d
	return nil
}
func (s *supervisorAgentCheckpoint) Stopped(ctx context.Context) error {
	if ctx == nil || ctx.Err() != nil || s.identity == "" {
		return updates.ErrApplyUnknown
	}
	d, e := supervisorIdentityDigest(s.directory)
	if e != nil || d != s.identity {
		return updates.ErrApplyUnknown
	}
	state, e := supervisorStateDigest(s.directory)
	if e != nil {
		return e
	}
	if s.state != "" && state != s.state {
		return updates.ErrApplyUnknown
	}
	s.state = state
	return nil
}
func (s *supervisorAgentCheckpoint) RollbackSafe(ctx context.Context) error {
	if ctx == nil || ctx.Err() != nil || s.state == "" {
		return updates.ErrApplyUnknown
	}
	d, e := supervisorStateDigest(s.directory)
	if e != nil || d != s.state {
		return updates.ErrApplyUnknown
	}
	return nil
}
func supervisorIdentityDigest(directory string) (string, error) {
	h := sha256.New()
	for _, name := range []string{"enrollment.json", "identity-key.pem", "issued.json"} {
		raw, e := recovery.ReadPrivateFile(filepath.Join(directory, name), 128<<10)
		if e != nil {
			return "", e
		}
		sum := sha256.Sum256(raw)
		clear(raw)
		h.Write([]byte(name))
		h.Write([]byte{0})
		h.Write(sum[:])
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
