package servicegroups

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"serverfleet.local/serverfleet/internal/nodes"
	"serverfleet.local/serverfleet/internal/storage"
)

var (
	ErrNotFound         = errors.New("servicegroups: not found")
	ErrNameTaken        = errors.New("servicegroups: name already exists")
	ErrInvalid          = errors.New("servicegroups: invalid input")
	ErrRevisionConflict = errors.New("servicegroups: revision conflict")
	ErrUnknownMember    = errors.New("servicegroups: member node does not exist")
	ErrMemberNotFound   = errors.New("servicegroups: member not found")
	ErrInvalidRevision  = errors.New("servicegroups: expected revision must be positive")
)

// Member is one node in a service group with its independent scheduling pause.
type Member struct {
	NodeID string
	Paused bool
	Reason string
}

// ServiceConfig describes the workload behind the group.
type ServiceConfig struct {
	Port        int
	Protocol    string
	HealthCheck string
}

// LoadBalancing mirrors the candidate contract's loadBalancing object.
type LoadBalancing struct {
	Enabled  bool
	Strategy string
}

// Record is the persisted service group. Computed fields such as gateway ids and
// healthy members belong to the gateway/monitoring domains and are not stored here.
type Record struct {
	ID            string
	Name          string
	Description   string
	Revision      uint64
	Members       []Member
	Service       ServiceConfig
	LoadBalancing LoadBalancing
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// Snapshot is an immutable membership snapshot for task/AI target resolution.
type Snapshot struct {
	GroupID  string
	Revision uint64
	Members  []string
	TakenAt  time.Time
}

type Repo struct {
	db    *storage.DB
	nodes *nodes.Repo
}

// NewRepo requires the nodes owner port for membership validation; it never reads
// the nodes tables directly.
func NewRepo(db *storage.DB, nodeRepo *nodes.Repo) (*Repo, error) {
	if db == nil {
		return nil, errors.New("servicegroups: database is required")
	}
	if nodeRepo == nil {
		return nil, errors.New("servicegroups: nodes repository is required")
	}
	return &Repo{db: db, nodes: nodeRepo}, nil
}

const recordSelect = `SELECT id, name, description, revision, service_port, service_protocol,
	health_check, lb_enabled, lb_strategy, created_at, updated_at FROM service_groups`

func (r *Repo) Create(ctx context.Context, q storage.Queryer, input Input) (Record, error) {
	if r == nil || q == nil {
		return Record{}, errors.New("servicegroups: repository is not configured")
	}
	normalized, err := normalizeInput(input, true)
	if err != nil {
		return Record{}, err
	}
	if err := r.verifyMembers(ctx, q, normalized.MemberNodeIDs); err != nil {
		return Record{}, fmt.Errorf("servicegroups: verify members: %w", err)
	}
	now := time.Now().UTC()
	if _, err := q.Exec(ctx, `INSERT INTO service_groups
		(id, name, description, revision, service_port, service_protocol, health_check,
		 lb_enabled, lb_strategy, created_at, updated_at)
		VALUES (?, ?, ?, 1, ?, ?, ?, ?, ?, ?, ?)`,
		normalized.ID, normalized.Name, normalized.Description, normalized.Service.Port,
		normalized.Service.Protocol, normalized.Service.HealthCheck, boolInt(normalized.LoadBalancing.Enabled),
		normalized.LoadBalancing.Strategy, encodeTime(now), encodeTime(now)); err != nil {
		if storage.IsUniqueViolation(err) {
			return Record{}, ErrNameTaken
		}
		return Record{}, fmt.Errorf("servicegroups: insert group: %w", err)
	}
	for _, nodeID := range normalized.MemberNodeIDs {
		if _, err := q.Exec(ctx, "INSERT INTO service_group_members (group_id, node_id, paused, pause_reason) VALUES (?, ?, 0, '')", normalized.ID, nodeID); err != nil {
			return Record{}, fmt.Errorf("servicegroups: insert member: %w", err)
		}
	}
	return Record{
		ID:            normalized.ID,
		Name:          normalized.Name,
		Description:   normalized.Description,
		Revision:      1,
		Members:       membersFromIDs(normalized.MemberNodeIDs),
		Service:       normalized.Service,
		LoadBalancing: normalized.LoadBalancing,
		CreatedAt:     now,
		UpdatedAt:     now,
	}, nil
}

func (r *Repo) Get(ctx context.Context, id string) (Record, error) {
	if r == nil || r.db == nil {
		return Record{}, errors.New("servicegroups: repository is not configured")
	}
	record, err := scanRecord(r.db.QueryRow(ctx, recordSelect+" WHERE id = ?", id))
	if storage.IsNotFound(err) {
		return Record{}, ErrNotFound
	}
	if err != nil {
		return Record{}, err
	}
	members, err := r.members(ctx, r.db, id)
	if err != nil {
		return Record{}, err
	}
	record.Members = members
	return record, nil
}

func (r *Repo) List(ctx context.Context) ([]Record, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("servicegroups: repository is not configured")
	}
	rows, err := r.db.Query(ctx, recordSelect+" ORDER BY name, id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	records := make([]Record, 0)
	for rows.Next() {
		record, err := scanRecord(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for index := range records {
		members, err := r.members(ctx, r.db, records[index].ID)
		if err != nil {
			return nil, err
		}
		records[index].Members = members
	}
	return records, nil
}

func (r *Repo) Update(ctx context.Context, q storage.Queryer, id string, expectedRevision uint64, input Input) (Record, error) {
	if expectedRevision == 0 {
		return Record{}, ErrInvalidRevision
	}
	input.ID = id
	normalized, err := normalizeInput(input, false)
	if err != nil {
		return Record{}, err
	}
	current, err := scanRecord(q.QueryRow(ctx, recordSelect+" WHERE id = ?", id))
	if storage.IsNotFound(err) {
		return Record{}, ErrNotFound
	}
	if err != nil {
		return Record{}, err
	}
	if current.Revision != expectedRevision {
		return Record{}, ErrRevisionConflict
	}
	now := time.Now().UTC()
	_, err = q.Exec(ctx, `UPDATE service_groups SET name = ?, description = ?, revision = revision + 1,
		service_port = ?, service_protocol = ?, health_check = ?, lb_enabled = ?, lb_strategy = ?, updated_at = ?
		WHERE id = ? AND revision = ?`,
		normalized.Name, normalized.Description, normalized.Service.Port, normalized.Service.Protocol,
		normalized.Service.HealthCheck, boolInt(normalized.LoadBalancing.Enabled),
		normalized.LoadBalancing.Strategy, encodeTime(now), id, expectedRevision)
	if err != nil {
		if storage.IsUniqueViolation(err) {
			return Record{}, ErrNameTaken
		}
		return Record{}, err
	}
	members, err := r.members(ctx, q, id)
	if err != nil {
		return Record{}, err
	}
	return Record{
		ID: id, Name: normalized.Name, Description: normalized.Description,
		Revision: expectedRevision + 1, Members: members, Service: normalized.Service,
		LoadBalancing: normalized.LoadBalancing, CreatedAt: current.CreatedAt, UpdatedAt: now,
	}, nil
}

func (r *Repo) ReplaceMembers(ctx context.Context, q storage.Queryer, id string, expectedRevision uint64, memberIDs []string) (Record, error) {
	if expectedRevision == 0 {
		return Record{}, ErrInvalidRevision
	}
	unique, err := normalizeMembers(memberIDs)
	if err != nil {
		return Record{}, err
	}
	if err := r.verifyMembers(ctx, q, unique); err != nil {
		return Record{}, err
	}
	current, err := scanRecord(q.QueryRow(ctx, recordSelect+" WHERE id = ?", id))
	if storage.IsNotFound(err) {
		return Record{}, ErrNotFound
	}
	if err != nil {
		return Record{}, err
	}
	if current.Revision != expectedRevision {
		return Record{}, ErrRevisionConflict
	}
	previous, err := r.members(ctx, q, id)
	if err != nil {
		return Record{}, err
	}
	paused := make(map[string]Member, len(previous))
	for _, member := range previous {
		paused[member.NodeID] = member
	}
	if _, err := q.Exec(ctx, "DELETE FROM service_group_members WHERE group_id = ?", id); err != nil {
		return Record{}, err
	}
	for _, nodeID := range unique {
		member := paused[nodeID]
		if _, err := q.Exec(ctx, "INSERT INTO service_group_members (group_id, node_id, paused, pause_reason) VALUES (?, ?, ?, ?)",
			id, nodeID, boolInt(member.Paused), member.Reason); err != nil {
			return Record{}, err
		}
	}
	now := time.Now().UTC()
	if _, err := q.Exec(ctx, "UPDATE service_groups SET revision = revision + 1, updated_at = ? WHERE id = ? AND revision = ?",
		encodeTime(now), id, expectedRevision); err != nil {
		return Record{}, err
	}
	current.Revision = expectedRevision + 1
	current.UpdatedAt = now
	members, err := r.members(ctx, q, id)
	if err != nil {
		return Record{}, err
	}
	current.Members = members
	return current, nil
}

// SetScheduling pauses or resumes one member and advances the group revision so
// snapshots taken earlier cannot be mistaken for the new state. expectedRevision
// must match the current group revision; a concurrent change returns
// ErrRevisionConflict.
func (r *Repo) SetScheduling(ctx context.Context, q storage.Queryer, id, nodeID string, expectedRevision uint64, paused bool, reason string) (Member, uint64, error) {
	if expectedRevision == 0 {
		return Member{}, 0, ErrInvalidRevision
	}
	if reason != "" && !validText(reason, 300) {
		return Member{}, 0, fmt.Errorf("%w: pause reason", ErrInvalid)
	}
	var exists int
	if err := q.QueryRow(ctx, "SELECT COUNT(*) FROM service_group_members WHERE group_id = ? AND node_id = ?", id, nodeID).Scan(&exists); err != nil {
		return Member{}, 0, err
	}
	if exists != 1 {
		return Member{}, 0, ErrMemberNotFound
	}
	if _, err := q.Exec(ctx, "UPDATE service_group_members SET paused = ?, pause_reason = ? WHERE group_id = ? AND node_id = ?",
		boolInt(paused), reason, id, nodeID); err != nil {
		return Member{}, 0, err
	}
	outcome, err := q.Exec(ctx, "UPDATE service_groups SET revision = revision + 1, updated_at = ? WHERE id = ? AND revision = ?",
		encodeTime(time.Now().UTC()), id, expectedRevision)
	if err != nil {
		return Member{}, 0, err
	}
	affected, err := outcome.RowsAffected()
	if err != nil {
		return Member{}, 0, err
	}
	if affected != 1 {
		return Member{}, 0, ErrRevisionConflict
	}
	return Member{NodeID: nodeID, Paused: paused, Reason: reason}, expectedRevision + 1, nil
}

func (r *Repo) Delete(ctx context.Context, q storage.Queryer, id string, expectedRevision uint64) error {
	if expectedRevision == 0 {
		return ErrInvalidRevision
	}
	current, err := scanRecord(q.QueryRow(ctx, recordSelect+" WHERE id = ?", id))
	if storage.IsNotFound(err) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if current.Revision != expectedRevision {
		return ErrRevisionConflict
	}
	outcome, err := q.Exec(ctx, "DELETE FROM service_groups WHERE id = ? AND revision = ?", id, expectedRevision)
	if err != nil {
		return err
	}
	affected, err := outcome.RowsAffected()
	if err != nil {
		return err
	}
	if affected != 1 {
		return ErrRevisionConflict
	}
	return nil
}

// Snapshot returns a defensive copy of the exact member set at the current
// revision. Callers must still enforce their own permissions.
func (r *Repo) Snapshot(ctx context.Context, id string) (Snapshot, error) {
	record, err := r.Get(ctx, id)
	if err != nil {
		return Snapshot{}, err
	}
	members := make([]string, 0, len(record.Members))
	for _, member := range record.Members {
		members = append(members, member.NodeID)
	}
	sort.Strings(members)
	return Snapshot{GroupID: id, Revision: record.Revision, Members: members, TakenAt: time.Now().UTC()}, nil
}

func (r *Repo) verifyMembers(ctx context.Context, q storage.Queryer, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	count, err := r.nodes.CountExisting(ctx, q, ids)
	if err != nil {
		return err
	}
	if count != len(ids) {
		return ErrUnknownMember
	}
	return nil
}

func (r *Repo) members(ctx context.Context, q storage.Queryer, id string) ([]Member, error) {
	rows, err := q.Query(ctx, `SELECT node_id, paused, pause_reason FROM service_group_members
		WHERE group_id = ? ORDER BY node_id`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	members := make([]Member, 0)
	for rows.Next() {
		var (
			member Member
			paused int
		)
		if err := rows.Scan(&member.NodeID, &paused, &member.Reason); err != nil {
			return nil, err
		}
		member.Paused = paused != 0
		members = append(members, member)
	}
	return members, rows.Err()
}

func scanRecord(row interface{ Scan(...any) error }) (Record, error) {
	var (
		record           Record
		revision         int64
		port             int64
		lbEnabled        int
		created, updated string
	)
	if err := row.Scan(&record.ID, &record.Name, &record.Description, &revision, &port,
		&record.Service.Protocol, &record.Service.HealthCheck, &lbEnabled,
		&record.LoadBalancing.Strategy, &created, &updated); err != nil {
		return Record{}, err
	}
	if revision < 1 {
		return Record{}, fmt.Errorf("servicegroups: stored group %q has invalid revision", record.ID)
	}
	if port < 0 || port > 65535 {
		return Record{}, fmt.Errorf("servicegroups: stored group %q has invalid port", record.ID)
	}
	record.Revision = uint64(revision)
	record.Service.Port = int(port)
	record.LoadBalancing.Enabled = lbEnabled != 0
	var err error
	if record.CreatedAt, err = storage.DecodeTime(created); err != nil {
		return Record{}, err
	}
	if record.UpdatedAt, err = storage.DecodeTime(updated); err != nil {
		return Record{}, err
	}
	return record, nil
}

// Input is the full create/update payload. Update requires ID to match the path.
type Input struct {
	ID            string
	Name          string
	Description   string
	Service       ServiceConfig
	LoadBalancing LoadBalancing
	MemberNodeIDs []string
}

func normalizeInput(input Input, requireID bool) (Input, error) {
	if requireID {
		if !validToken(input.ID, 128) {
			return Input{}, fmt.Errorf("%w: id", ErrInvalid)
		}
	} else if input.ID == "" {
		return Input{}, fmt.Errorf("%w: id", ErrInvalid)
	}
	if !validText(input.Name, 128) {
		return Input{}, fmt.Errorf("%w: name", ErrInvalid)
	}
	if input.Description != "" && !validText(input.Description, 1024) {
		return Input{}, fmt.Errorf("%w: description", ErrInvalid)
	}
	if input.Service.Port < 0 || input.Service.Port > 65535 {
		return Input{}, fmt.Errorf("%w: service port", ErrInvalid)
	}
	switch input.Service.Protocol {
	case "", "tcp", "udp", "http", "https":
	default:
		return Input{}, fmt.Errorf("%w: service protocol", ErrInvalid)
	}
	if input.Service.HealthCheck != "" && !validText(input.Service.HealthCheck, 64) {
		return Input{}, fmt.Errorf("%w: health check", ErrInvalid)
	}
	if input.LoadBalancing.Strategy != "" && !validText(input.LoadBalancing.Strategy, 64) {
		return Input{}, fmt.Errorf("%w: load balancing strategy", ErrInvalid)
	}
	members, err := normalizeMembers(input.MemberNodeIDs)
	if err != nil {
		return Input{}, err
	}
	input.MemberNodeIDs = members
	return input, nil
}

func normalizeMembers(ids []string) ([]string, error) {
	if len(ids) > 500 {
		return nil, fmt.Errorf("%w: too many members", ErrInvalid)
	}
	seen := make(map[string]struct{}, len(ids))
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if !validToken(id, 128) {
			return nil, fmt.Errorf("%w: member id", ErrInvalid)
		}
		if _, duplicate := seen[id]; duplicate {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	sort.Strings(out)
	return out, nil
}

func membersFromIDs(ids []string) []Member {
	members := make([]Member, 0, len(ids))
	for _, id := range ids {
		members = append(members, Member{NodeID: id})
	}
	return members
}

func validText(value string, max int) bool {
	if value == "" || len(value) > max || !utf8.ValidString(value) || strings.TrimSpace(value) != value {
		return false
	}
	return !strings.ContainsFunc(value, func(r rune) bool {
		return unicode.IsControl(r) || unicode.In(r, unicode.Cf, unicode.Zl, unicode.Zp)
	})
}

func validToken(value string, max int) bool {
	return validText(value, max) && !strings.ContainsFunc(value, unicode.IsSpace)
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func encodeTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value.UTC().Format(time.RFC3339Nano)
}
