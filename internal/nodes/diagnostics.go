package nodes

import (
	"context"
	"encoding/json"
	"serverfleet.local/serverfleet/internal/storage"
	"sort"
	"time"
)

type ConnectionObservation struct {
	AgentVersion         string    `json:"agentVersion"`
	Capabilities         []string  `json:"capabilities"`
	ConnectionID         string    `json:"connectionId"`
	ObservedAt           time.Time `json:"observedAt,omitzero"`
	CertificateExpiresAt time.Time `json:"certificateExpiresAt,omitzero"`
}
type ConnectionEvent struct {
	At   time.Time `json:"at"`
	Code string    `json:"code"`
}
type ConnectionDiagnostics struct {
	Recorded bool `json:"recorded"`
	ConnectionObservation
	History []ConnectionEvent `json:"history"`
}

func DiagnosticMigrations() []storage.Migration {
	return []storage.Migration{{Version: "ds/0050_node_diagnostics", Name: "bounded authenticated Agent connection diagnostics", Statements: []string{
		`CREATE TABLE node_connection_diagnostics (node_id TEXT PRIMARY KEY REFERENCES nodes(id) ON DELETE CASCADE, payload BLOB NOT NULL)`,
		`CREATE TABLE node_connection_history (id INTEGER PRIMARY KEY AUTOINCREMENT,node_id TEXT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,at TEXT NOT NULL,code TEXT NOT NULL)`,
		`CREATE INDEX node_connection_history_node ON node_connection_history(node_id,id DESC)`,
	}}}
}

func (r *Repo) RecordConnection(ctx context.Context, node string, o ConnectionObservation) error {
	if validateNodeID(node) != nil || !validDiagnosticText(o.AgentVersion) || !validDiagnosticText(o.ConnectionID) || o.ObservedAt.IsZero() || len(o.Capabilities) > 32 {
		return ErrInvalidNode
	}
	o.Capabilities = append([]string{}, o.Capabilities...)
	sort.Strings(o.Capabilities)
	for _, id := range o.Capabilities {
		if !validDiagnosticText(id) {
			return ErrInvalidNode
		}
	}
	payload, e := json.Marshal(o)
	if e != nil {
		return e
	}
	return r.db.InTx(ctx, func(ctx context.Context, tx storage.Tx) error {
		if _, e := tx.Exec(ctx, `INSERT INTO node_connection_diagnostics(node_id,payload) VALUES(?,?) ON CONFLICT(node_id) DO UPDATE SET payload=excluded.payload`, node, payload); e != nil {
			return e
		}
		return appendConnectionEvent(ctx, tx, node, o.ObservedAt, "authenticated")
	})
}
func validDiagnosticText(s string) bool {
	if len(s) == 0 || len(s) > 128 {
		return false
	}
	for _, c := range s {
		if c < 0x21 || c > 0x7e {
			return false
		}
	}
	return true
}
func appendConnectionEvent(ctx context.Context, tx storage.Tx, node string, at time.Time, code string) error {
	if _, e := tx.Exec(ctx, `INSERT INTO node_connection_history(node_id,at,code) VALUES(?,?,?)`, node, encodeTime(at), code); e != nil {
		return e
	}
	_, e := tx.Exec(ctx, `DELETE FROM node_connection_history WHERE node_id=? AND id NOT IN (SELECT id FROM node_connection_history WHERE node_id=? ORDER BY id DESC LIMIT 100)`, node, node)
	return e
}
func (r *Repo) RecordDisconnect(ctx context.Context, node string, at time.Time) error {
	return r.db.InTx(ctx, func(ctx context.Context, tx storage.Tx) error {
		return appendConnectionEvent(ctx, tx, node, at, "disconnected")
	})
}
func (r *Repo) ConnectionDiagnostics(ctx context.Context, node string) (ConnectionDiagnostics, error) {
	out := ConnectionDiagnostics{History: []ConnectionEvent{}, ConnectionObservation: ConnectionObservation{Capabilities: []string{}}}
	var payload []byte
	e := r.db.QueryRow(ctx, `SELECT payload FROM node_connection_diagnostics WHERE node_id=?`, node).Scan(&payload)
	if storage.IsNotFound(e) {
		return out, nil
	}
	if e != nil {
		return out, e
	}
	if len(payload) > 16<<10 || json.Unmarshal(payload, &out.ConnectionObservation) != nil {
		return out, ErrInventoryUnavailable
	}
	out.Recorded = true
	rows, e := r.db.Query(ctx, `SELECT at,code FROM node_connection_history WHERE node_id=? ORDER BY id DESC LIMIT 100`, node)
	if e != nil {
		return out, e
	}
	defer rows.Close()
	for rows.Next() {
		var at, code string
		if e = rows.Scan(&at, &code); e != nil {
			return out, e
		}
		when, e := time.Parse(time.RFC3339Nano, at)
		if e != nil {
			return out, e
		}
		out.History = append(out.History, ConnectionEvent{when, code})
	}
	return out, rows.Err()
}
func (s *Service) ConnectionDiagnostics(ctx context.Context, subject, node string) (ConnectionDiagnostics, error) {
	if _, e := s.GetVisible(ctx, subject, node); e != nil {
		return ConnectionDiagnostics{}, e
	}
	return s.nodes.ConnectionDiagnostics(ctx, node)
}
