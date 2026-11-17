package updates

import (
	"context"
	"serverfleet.local/serverfleet/internal/storage"
)

// The private DSN file is pinned in the supervisor policy. It is never copied
// into child arguments, health reports, backup bundles or error messages.
func (c ResidentSupervisorConfig) DatabaseConfig(readOnly bool) (storage.Config, error) {
	cfg := storage.Config{Path: c.Database, ReadOnly: readOnly}
	switch c.DatabaseDriver {
	case "", "sqlite":
		if c.DatabaseDSNFile != "" || c.DatabaseDSNSHA256 != "" {
			return cfg, ErrArtifactRejected
		}
	case "postgres":
		if !validPlanDigest(c.DatabaseDSNSHA256) {
			return cfg, ErrArtifactRejected
		}
		dsn, err := storage.ReadDSNFile(c.DatabaseDSNFile)
		if err != nil {
			return cfg, ErrArtifactRejected
		}
		if releaseDigest([]byte(dsn)) != c.DatabaseDSNSHA256 {
			return cfg, ErrArtifactRejected
		}
		cfg.Driver = "postgres"
		cfg.DSN = dsn
	default:
		return cfg, ErrArtifactRejected
	}
	return cfg, nil
}
func OpenResidentDatabase(ctx context.Context, c ResidentSupervisorConfig, readOnly bool) (*storage.DB, error) {
	cfg, err := c.DatabaseConfig(readOnly)
	if err != nil {
		return nil, err
	}
	return storage.Open(ctx, cfg)
}
