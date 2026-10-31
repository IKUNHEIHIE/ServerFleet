package server

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"serverfleet.local/serverfleet/internal/recovery"
	"serverfleet.local/serverfleet/internal/storage"
	"serverfleet.local/serverfleet/internal/updates"
)

func (s *Server) setupUpdatesRecovery(cfg Config) error {
	backupDir := strings.TrimSpace(cfg.BackupDir)
	if backupDir == "" {
		backupDir = filepath.Join(filepath.Dir(cfg.DBPath), "backups")
	}
	if !filepath.IsAbs(backupDir) {
		absolute, err := filepath.Abs(backupDir)
		if err != nil {
			return err
		}
		backupDir = absolute
	}
	if err := os.MkdirAll(backupDir, 0o700); err != nil {
		return err
	}
	s.backupDir = backupDir

	updateHTTP, err := updates.NewHTTP(updates.Options{
		Authenticate: func(r *http.Request) (updates.Principal, error) {
			identity, requestID, err := s.authenticateIdentity(r)
			if err != nil {
				return updates.Principal{}, err
			}
			return updates.Principal{SubjectID: identity.SubjectID, RequestID: requestID, Administrator: identity.Administrator}, nil
		},
		Now: s.now,
	})
	if err != nil {
		return err
	}
	recoveryHTTP, err := recovery.NewHTTP(recovery.Options{
		Authenticate: func(r *http.Request) (recovery.Principal, error) {
			identity, requestID, err := s.authenticateIdentity(r)
			if err != nil {
				return recovery.Principal{}, err
			}
			return recovery.Principal{SubjectID: identity.SubjectID, RequestID: requestID, Administrator: identity.Administrator}, nil
		},
		VerifyBackup: s.verifyBackupFile,
		Now:          s.now,
	})
	if err != nil {
		return err
	}
	s.UpdatesHTTP = updateHTTP
	s.RecoveryHTTP = recoveryHTTP
	return nil
}

func (s *Server) verifyBackupFile(ctx context.Context, backupID string) (recovery.BackupVerification, error) {
	if backupID == "" || backupID != filepath.Base(backupID) || strings.ContainsAny(backupID, "/\\\x00") {
		return recovery.BackupVerification{}, recovery.ErrBackupNotFound
	}
	candidates := []string{filepath.Join(s.backupDir, backupID)}
	if !strings.HasSuffix(backupID, ".db") {
		candidates = append(candidates, filepath.Join(s.backupDir, backupID+".db"))
	}
	var path string
	for _, candidate := range candidates {
		info, err := os.Lstat(candidate)
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		path = candidate
		break
	}
	if path == "" {
		return recovery.BackupVerification{}, recovery.ErrBackupNotFound
	}
	if err := storage.VerifyFile(ctx, path, true); err != nil {
		return recovery.BackupVerification{Valid: false, Reason: "sqlite_integrity_check_failed"}, nil
	}
	return recovery.BackupVerification{Valid: true}, nil
}

func (s *Server) registerUpdateRecoveryRoutes() {
	if s.UpdatesHTTP != nil {
		for _, pattern := range s.UpdatesHTTP.Patterns() {
			s.mux.Handle(pattern, s.UpdatesHTTP)
		}
	}
	if s.RecoveryHTTP != nil {
		for _, pattern := range s.RecoveryHTTP.Patterns() {
			s.mux.Handle(pattern, s.RecoveryHTTP)
		}
	}
}
