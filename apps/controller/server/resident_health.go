package server

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"serverfleet.local/serverfleet/internal/updates"
	"time"
)

// Private report is requested only by the owning process parent. It is not a
// health HTTP override. The parent independently verifies executable/PID/nonce
// and a real TLS HTTP request before accepting this report.
func (s *Server) RunResidentHealth(ctx context.Context, version string) error {
	filename := os.Getenv("SF_CONTROLLER_HEALTH_FILE")
	if filename == "" {
		return nil
	}
	nonce := os.Getenv("SF_SUPERVISOR_NONCE")
	if !filepath.IsAbs(filename) || len(nonce) != 32 || s.residentSupervisor == nil {
		return updates.ErrArtifactRejected
	}
	info, e := os.Lstat(filepath.Dir(filename))
	if e != nil || !info.IsDir() || info.Mode().Perm() != 0700 {
		return updates.ErrArtifactRejected
	}
	root, e := os.OpenRoot(filepath.Dir(filename))
	if e != nil {
		return e
	}
	defer root.Close()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		if s.runtimeGate(ctx) != nil {
			return updates.ErrApplyUnknown
		}
		schema, e := updates.SQLiteSchemaFingerprint(ctx, s.DB)
		if e != nil {
			return e
		}
		report, _ := json.Marshal(struct {
			Version             string
			PID                 int
			Nonce, SchemaSHA256 string
			ObservedAt          time.Time
		}{version, os.Getpid(), nonce, schema, time.Now().UTC()})
		name := filepath.Base(filename)
		if _, e := root.Lstat(name + ".next"); e == nil {
			return updates.ErrUpdateBusy
		}
		file, e := root.OpenFile(name+".next", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if e != nil {
			return e
		}
		n, we := file.Write(report)
		sy := file.Sync()
		ce := file.Close()
		if we != nil || n != len(report) || sy != nil || ce != nil || root.Rename(name+".next", name) != nil {
			return updates.ErrApplyUnknown
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}
