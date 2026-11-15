package nodes

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"serverfleet.local/serverfleet/internal/storage"
	"strings"
	"time"
)

var ErrMetadataConflict = errors.New("nodes: metadata conflict")

func ManagementMigrations() []storage.Migration {
	return []storage.Migration{{Version: "ds/0089_node_archives", Name: "node archival and operator metadata concurrency", Statements: []string{
		`CREATE TABLE node_archives (node_id TEXT PRIMARY KEY REFERENCES nodes(id), archived_at TEXT NOT NULL)`,
		`ALTER TABLE nodes ADD COLUMN metadata_updated_at TEXT NOT NULL DEFAULT ''`,
	}}}
}

type NodeMetadataInput struct {
	Name              string   `json:"name"`
	Address           string   `json:"address"`
	Region            string   `json:"region"`
	Provider          string   `json:"provider"`
	CountryCode       string   `json:"countryCode"`
	Tags              []string `json:"tags"`
	ExpectedUpdatedAt string   `json:"expectedUpdatedAt"`
}
type managedNodeDTO struct {
	NodeID       string   `json:"nodeId"`
	DisplayName  string   `json:"displayName"`
	Status       Status   `json:"status"`
	LastSeenAt   string   `json:"lastSeenAt,omitempty"`
	AgentVersion string   `json:"agentVersion,omitempty"`
	Address      string   `json:"address"`
	Region       string   `json:"region"`
	Provider     string   `json:"provider"`
	CountryCode  string   `json:"countryCode"`
	Tags         []string `json:"tags"`
	UpdatedAt    string   `json:"updatedAt"`
}

func RegisterManagementHTTP(mux *http.ServeMux, service *Service, authenticate Authenticator) error {
	if mux == nil || service == nil || authenticate == nil {
		return errors.New("nodes: management dependencies required")
	}
	api := &httpAPI{service: service, authenticator: authenticate, now: service.now}
	mux.HandleFunc("GET /api/v1/nodes/{nodeId}", api.getManagedNode)
	mux.HandleFunc("PATCH /api/v1/nodes/{nodeId}", api.updateManagedNode)
	mux.HandleFunc("DELETE /api/v1/nodes/{nodeId}", api.archiveManagedNode)
	return nil
}
func metadataStamp(ctx context.Context, q storage.Queryer, id string) (string, error) {
	var stamp, created string
	e := q.QueryRow(ctx, `SELECT metadata_updated_at,created_at FROM nodes WHERE id=?`, id).Scan(&stamp, &created)
	if e != nil {
		return "", e
	}
	if stamp == "" {
		stamp = created
	}
	return stamp, nil
}
func (s *Service) managedNode(ctx context.Context, subject, id string) (managedNodeDTO, error) {
	if _, e := s.GetVisible(ctx, subject, id); e != nil {
		return managedNodeDTO{}, e
	}
	// Read metadata and its CAS version from the same SQL snapshot.
	n := managedNodeDTO{NodeID: id}
	var tags, status string
	var lastSeen storage.NullString
	e := s.db.QueryRow(ctx, `SELECT name,address,region,provider,country_code,tags,status,last_seen_at,
		COALESCE(NULLIF(metadata_updated_at,''),created_at)
		FROM nodes WHERE id=? AND NOT EXISTS (SELECT 1 FROM node_archives WHERE node_id=nodes.id)`, id).
		Scan(&n.DisplayName, &n.Address, &n.Region, &n.Provider, &n.CountryCode, &tags, &status, &lastSeen, &n.UpdatedAt)
	if storage.IsNotFound(e) {
		return managedNodeDTO{}, ErrNodeNotFound
	}
	if e != nil {
		return managedNodeDTO{}, e
	}
	n.Status = Status(status)
	if !n.Status.Valid() {
		return managedNodeDTO{}, fmt.Errorf("nodes: stored node %q has invalid status %q", id, status)
	}
	if n.Tags, e = decodeTags(tags); e != nil {
		return managedNodeDTO{}, e
	}
	if lastSeen.Valid {
		parsed, e := storage.DecodeTime(lastSeen.String)
		if e != nil {
			return managedNodeDTO{}, e
		}
		n.LastSeenAt = formatNodeTime(parsed)
	}
	diag, e := s.nodes.ConnectionDiagnostics(ctx, id)
	if e != nil {
		return managedNodeDTO{}, e
	}
	n.AgentVersion = diag.AgentVersion
	return n, nil
}
func (api *httpAPI) getManagedNode(w http.ResponseWriter, r *http.Request) {
	p, ok := api.principal(w, r)
	if !ok {
		return
	}
	n, e := api.service.managedNode(r.Context(), p.SubjectID, r.PathValue("nodeId"))
	if e != nil {
		writeError(w, e)
		return
	}
	writeJSON(w, 200, n)
}

func (s *Service) UpdateMetadata(ctx context.Context, subject, request, id string, in NodeMetadataInput) error {
	if e := s.requireRequest(request); e != nil {
		return e
	}
	if e := s.authorize(ctx, subject, ActionNodesWrite, request, []string{id}, "node_metadata"); e != nil {
		return e
	}
	if _, e := s.GetVisible(ctx, subject, id); e != nil {
		return e
	}
	in.Name = strings.TrimSpace(in.Name)
	in.Address = strings.TrimSpace(in.Address)
	in.Region = strings.TrimSpace(in.Region)
	in.Provider = strings.TrimSpace(in.Provider)
	in.CountryCode = strings.ToUpper(strings.TrimSpace(in.CountryCode))
	if _, e := time.Parse(time.RFC3339Nano, in.ExpectedUpdatedAt); e != nil {
		return ErrInvalidNode
	}
	probe := Node{ID: id, Name: in.Name, Address: in.Address, Region: in.Region, Provider: in.Provider, CountryCode: in.CountryCode, Tags: in.Tags, Status: StatusOffline}
	if e := validateNode(probe); e != nil {
		return e
	}
	tags, e := encodeTags(in.Tags)
	if e != nil {
		return e
	}
	return s.db.InTx(ctx, func(ctx context.Context, tx storage.Tx) error {
		var archived int
		if e := tx.QueryRow(ctx, `SELECT count(*) FROM node_archives WHERE node_id=?`, id).Scan(&archived); e != nil {
			return e
		}
		if archived != 0 {
			return ErrNodeRevoked
		}
		stamp, e := metadataStamp(ctx, tx, id)
		if e != nil {
			return e
		}
		if stamp != in.ExpectedUpdatedAt {
			return ErrMetadataConflict
		}
		now := encodeTime(s.now().UTC())
		if now == stamp {
			t, _ := time.Parse(time.RFC3339Nano, stamp)
			now = encodeTime(t.Add(time.Nanosecond))
		}
		result, e := tx.Exec(ctx, `UPDATE nodes SET name=?,address=?,region=?,provider=?,country_code=?,tags=?,metadata_updated_at=?,updated_at=? WHERE id=? AND (metadata_updated_at=? OR (metadata_updated_at='' AND created_at=?))`, in.Name, in.Address, in.Region, in.Provider, in.CountryCode, tags, now, now, id, stamp, stamp)
		if e != nil {
			return e
		}
		count, e := result.RowsAffected()
		if e != nil {
			return e
		}
		if count != 1 {
			return ErrMetadataConflict
		}
		return s.audit.AppendTx(ctx, tx, s.event(subject, ActionNodesWrite, "success", request, []string{id}, "node_metadata"))
	})
}
func (api *httpAPI) updateManagedNode(w http.ResponseWriter, r *http.Request) {
	p, ok := api.principal(w, r)
	if !ok {
		return
	}
	var in NodeMetadataInput
	if !decodeJSON(w, r, &in) {
		return
	}
	e := api.service.UpdateMetadata(r.Context(), p.SubjectID, p.RequestID, r.PathValue("nodeId"), in)
	if errors.Is(e, ErrMetadataConflict) {
		writeFixed(w, 409, "node_metadata_conflict", "node metadata changed; refresh before editing")
		return
	}
	if e != nil {
		writeError(w, e)
		return
	}
	n, e := api.service.managedNode(r.Context(), p.SubjectID, r.PathValue("nodeId"))
	if e != nil {
		writeError(w, e)
		return
	}
	writeJSON(w, 200, n)
}
func (s *Service) Archive(ctx context.Context, subject, request, id, reason string) error {
	if e := s.requireRequest(request); e != nil {
		return e
	}
	if !validText(strings.TrimSpace(reason), 300) {
		return ErrInvalidNode
	}
	if e := s.authorize(ctx, subject, ActionNodesRevoke, request, []string{id}, "node_archive"); e != nil {
		return e
	}
	if _, e := s.GetVisible(ctx, subject, id); e != nil {
		return e
	}
	return s.db.InTx(ctx, func(ctx context.Context, tx storage.Tx) error {
		var exists int
		if e := tx.QueryRow(ctx, `SELECT count(*) FROM node_archives WHERE node_id=?`, id).Scan(&exists); e != nil {
			return e
		}
		if exists > 0 {
			return nil
		}
		if _, e := s.nodes.UpdateStatus(ctx, tx, id, StatusRevoked, s.now()); e != nil {
			return e
		}
		if _, e := tx.Exec(ctx, `INSERT INTO node_archives(node_id,archived_at) VALUES(?,?)`, id, encodeTime(s.now())); e != nil {
			return e
		}
		return s.audit.AppendTx(ctx, tx, s.event(subject, ActionNodesRevoke, "success", request, []string{id}, "node_archive"))
	})
}
func (api *httpAPI) archiveManagedNode(w http.ResponseWriter, r *http.Request) {
	p, ok := api.principal(w, r)
	if !ok {
		return
	}
	var in struct {
		Reason string `json:"reason"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if e := api.service.Archive(r.Context(), p.SubjectID, p.RequestID, r.PathValue("nodeId"), in.Reason); e != nil {
		writeError(w, e)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
