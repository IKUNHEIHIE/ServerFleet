package updates

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"serverfleet.local/serverfleet/internal/storage"
)

// A schema fingerprint is an observed compatibility input for a trusted
// installer, not a claim that migrations are reversible. Only sqlite_master
// definitions and PRAGMA user_version are hashed; no application data, secrets,
// counters or rows are returned. The caller pins the reviewed value.
const schemaFingerprintMaxEntries = 1024

func SQLiteSchemaFingerprint(ctx context.Context, db *storage.DB) (string, error) {
	if ctx == nil || db == nil {
		return "", ErrArtifactRejected
	}
	var version int
	if err := db.QueryRow(ctx, `PRAGMA user_version`).Scan(&version); err != nil {
		return "", ErrArtifactRejected
	}
	rows, err := db.Query(ctx, `SELECT type,name,tbl_name,COALESCE(sql,'') FROM sqlite_master ORDER BY type,name`)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	state := struct {
		Version int
		Entries [][4]string
	}{Version: version}
	for rows.Next() {
		var item [4]string
		if rows.Scan(&item[0], &item[1], &item[2], &item[3]) != nil {
			return "", ErrArtifactRejected
		}
		if len(state.Entries) >= schemaFingerprintMaxEntries {
			return "", ErrArtifactRejected
		}
		state.Entries = append(state.Entries, item)
	}
	if rows.Err() != nil {
		return "", rows.Err()
	}
	raw, err := json.Marshal(state)
	if err != nil {
		return "", ErrArtifactRejected
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}
