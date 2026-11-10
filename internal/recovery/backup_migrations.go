package recovery

import "serverfleet.local/serverfleet/internal/storage"

func Migrations() []storage.Migration {
	return []storage.Migration{{Version: "cx/0011_encrypted_backups", Name: "encrypted consistent bundles and recovery-key confirmation", Statements: []string{
		`CREATE TABLE cx_recovery_backups(id TEXT PRIMARY KEY,task_id TEXT NOT NULL UNIQUE,kind TEXT NOT NULL,status TEXT NOT NULL,cipher_name TEXT NOT NULL,sha256 TEXT NOT NULL DEFAULT '',bytes INTEGER NOT NULL DEFAULT 0,created_at TEXT NOT NULL,updated_at TEXT NOT NULL,subject_id TEXT NOT NULL,recipient_fingerprint TEXT NOT NULL,authority_id TEXT NOT NULL,root_spki TEXT NOT NULL,trust_generation INTEGER NOT NULL,manifest_sha256 TEXT NOT NULL DEFAULT '',remote_status TEXT NOT NULL DEFAULT 'not_requested',error_code TEXT NOT NULL DEFAULT '')`,
		`CREATE INDEX cx_recovery_backups_created ON cx_recovery_backups(created_at,id)`,
		`CREATE TABLE cx_recovery_recipient_checks(recipient_fingerprint TEXT PRIMARY KEY,challenge_id TEXT NOT NULL,nonce_sha256 TEXT NOT NULL,confirmed INTEGER NOT NULL DEFAULT 0,confirmed_by TEXT NOT NULL DEFAULT '',created_at TEXT NOT NULL,confirmed_at TEXT NOT NULL DEFAULT '')`,
	}}}
}
