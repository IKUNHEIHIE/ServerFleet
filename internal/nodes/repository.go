package nodes

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"serverfleet.local/serverfleet/internal/storage"
)

var (
	ErrNodeNotFound  = errors.New("nodes: node not found")
	ErrNodeExists    = errors.New("nodes: node already exists")
	ErrInvalidNode   = errors.New("nodes: invalid node")
	ErrStaleUpdate   = errors.New("nodes: stale agent update")
	ErrNodeRevoked   = errors.New("nodes: node is revoked")
	ErrInvalidSample = errors.New("nodes: invalid metric sample")
	ErrUnknownNode   = errors.New("nodes: grouping references an unknown node")
)

const SampleSourceAgent = "agent"

// NodeCreateInput is trusted enrollment data from the Controller. Metrics are
// never accepted here; they must arrive as agent samples.
type NodeCreateInput struct {
	ID          string
	Name        string
	Address     string
	Region      string
	Provider    string
	CountryCode string
	Tags        []string
	Status      Status
}

// AgentUpdate is one controlled event delivered by the transport layer. Sequence
// is the per-node monotonic counter of the trusted connection; equal sequences are
// idempotent duplicates, lower sequences are stale and rejected. ObservedAt is
// the sample time reported by the agent, never the receipt time.
type AgentUpdate struct {
	NodeID       string
	Name         string
	Address      string
	Region       string
	Provider     string
	CountryCode  string
	Tags         []string
	Status       Status
	Sequence     uint64
	ObservedAt   time.Time
	Metrics      *Metrics
	SampleSource string
}

// Repo persists node inventory. It never authorizes callers; the Service layer
// enforces permissions and audit before calling it.
type Repo struct {
	db *storage.DB
}

func NewRepo(db *storage.DB) (*Repo, error) {
	if db == nil {
		return nil, errors.New("nodes: database is required")
	}
	return &Repo{db: db}, nil
}

func (r *Repo) Create(ctx context.Context, q storage.Queryer, input NodeCreateInput) (Node, error) {
	if r == nil || q == nil {
		return Node{}, errors.New("nodes: repository is not configured")
	}
	node, err := newNode(input, time.Now().UTC())
	if err != nil {
		return Node{}, err
	}
	tags, err := encodeTags(node.Tags)
	if err != nil {
		return Node{}, err
	}
	_, err = q.Exec(ctx, `INSERT INTO nodes
		(id, name, address, region, provider, country_code, tags, status, last_seen_at,
		 event_seq, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, NULL, 0, ?, ?)`,
		node.ID, node.Name, node.Address, node.Region, node.Provider, node.CountryCode, tags,
		string(node.Status), encodeTime(node.CreatedAt), encodeTime(node.UpdatedAt))
	if err != nil {
		if storage.IsUniqueViolation(err) {
			return Node{}, ErrNodeExists
		}
		return Node{}, err
	}
	return node, nil
}

func (r *Repo) Get(ctx context.Context, id string) (Node, error) {
	if r == nil || r.db == nil {
		return Node{}, errors.New("nodes: repository is not configured")
	}
	if err := validateNodeID(id); err != nil {
		return Node{}, err
	}
	row := r.db.QueryRow(ctx, nodeSelect+" WHERE id = ?", id)
	node, err := scanNode(row)
	if storage.IsNotFound(err) {
		return Node{}, ErrNodeNotFound
	}
	return node, err
}

func (r *Repo) List(ctx context.Context) ([]Node, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("nodes: repository is not configured")
	}
	rows, err := r.db.Query(ctx, nodeSelect+" ORDER BY name, id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	nodes := make([]Node, 0)
	for rows.Next() {
		node, err := scanNode(rows)
		if err != nil {
			return nil, err
		}
		nodes = append(nodes, node)
	}
	return nodes, rows.Err()
}

// ApplyAgentUpdate applies one trusted agent event inside the caller transaction.
// The bool reports whether a change was written; an equal sequence is an
// idempotent duplicate that returns applied=false without touching the row.
func (r *Repo) ApplyAgentUpdate(ctx context.Context, q storage.Queryer, update AgentUpdate) (Node, bool, error) {
	if r == nil || q == nil {
		return Node{}, false, errors.New("nodes: repository is not configured")
	}
	if err := validateAgentUpdate(update); err != nil {
		return Node{}, false, err
	}
	current, err := scanNode(q.QueryRow(ctx, nodeSelect+" WHERE id = ?", update.NodeID))
	if storage.IsNotFound(err) {
		return Node{}, false, ErrNodeNotFound
	}
	if err != nil {
		return Node{}, false, err
	}
	if current.eventSeq > 0 && update.Sequence < current.eventSeq {
		return Node{}, false, ErrStaleUpdate
	}
	if current.eventSeq > 0 && update.Sequence == current.eventSeq {
		return current, false, nil
	}
	if current.Status == StatusRevoked && update.Status != StatusRevoked {
		return Node{}, false, ErrNodeRevoked
	}

	merged := current
	if update.Name != "" {
		merged.Name = update.Name
	}
	if update.Address != "" {
		merged.Address = update.Address
	}
	if update.Region != "" {
		merged.Region = update.Region
	}
	if update.Provider != "" {
		merged.Provider = update.Provider
	}
	if update.CountryCode != "" {
		merged.CountryCode = update.CountryCode
	}
	if update.Tags != nil {
		merged.Tags = append([]string(nil), update.Tags...)
	}
	if update.Status != "" {
		merged.Status = update.Status
	}
	if current.Status == StatusSuspended && update.Status != StatusRevoked {
		// An operator suspension is not overridden by an agent heartbeat.
		merged.Status = StatusSuspended
	}
	if update.ObservedAt.After(current.LastSeenAt) {
		merged.LastSeenAt = update.ObservedAt
	}
	if update.Metrics != nil {
		sample := *update.Metrics
		sample.ObservedAt = update.ObservedAt
		merged.Metrics = &sample
	}
	if err := validateNode(merged); err != nil {
		return Node{}, false, err
	}
	tags, err := encodeTags(merged.Tags)
	if err != nil {
		return Node{}, false, err
	}
	sampleSource := ""
	var cpu, memory any
	var observed any
	if merged.Metrics != nil {
		sampleSource = SampleSourceAgent
		cpu = merged.Metrics.CPUPercent
		memory = merged.Metrics.MemoryPercent
		observed = encodeTime(merged.Metrics.ObservedAt)
	}
	outcome, err := q.Exec(ctx, `UPDATE nodes SET
		name = ?, address = ?, region = ?, provider = ?, country_code = ?, tags = ?,
		status = ?, last_seen_at = ?, event_seq = ?, metrics_cpu = ?, metrics_memory = ?,
		metrics_observed_at = ?, metrics_source = ?, updated_at = ?
		WHERE id = ? AND event_seq = ?`,
		merged.Name, merged.Address, merged.Region, merged.Provider, merged.CountryCode, tags,
		string(merged.Status), encodeTime(merged.LastSeenAt), update.Sequence, cpu, memory,
		observed, sampleSource, encodeTime(time.Now().UTC()), update.NodeID, current.eventSeq)
	if err != nil {
		return Node{}, false, err
	}
	affected, err := outcome.RowsAffected()
	if err != nil {
		return Node{}, false, err
	}
	if affected != 1 {
		return Node{}, false, ErrStaleUpdate
	}
	merged.eventSeq = update.Sequence
	return merged, true, nil
}

// UpdateStatus applies an operator-controlled lifecycle status inside the caller
// transaction. Revocation is terminal; an already-revoked node rejects any other
// transition.
func (r *Repo) UpdateStatus(ctx context.Context, q storage.Queryer, nodeID string, status Status, now time.Time) (Node, error) {
	if r == nil || q == nil {
		return Node{}, errors.New("nodes: repository is not configured")
	}
	if err := validateNodeID(nodeID); err != nil {
		return Node{}, err
	}
	if !status.Valid() {
		return Node{}, fmt.Errorf("%w: status", ErrInvalidNode)
	}
	current, err := scanNode(q.QueryRow(ctx, nodeSelect+" WHERE id = ?", nodeID))
	if storage.IsNotFound(err) {
		return Node{}, ErrNodeNotFound
	}
	if err != nil {
		return Node{}, err
	}
	if current.Status == status {
		return current, nil
	}
	if current.Status == StatusRevoked {
		return Node{}, ErrNodeRevoked
	}
	outcome, err := q.Exec(ctx, "UPDATE nodes SET status = ?, updated_at = ? WHERE id = ?",
		string(status), encodeTime(now.UTC()), nodeID)
	if err != nil {
		return Node{}, err
	}
	affected, err := outcome.RowsAffected()
	if err != nil {
		return Node{}, err
	}
	if affected != 1 {
		return Node{}, ErrNodeNotFound
	}
	current.Status = status
	current.UpdatedAt = now.UTC()
	return current, nil
}

func (r *Repo) Count(ctx context.Context) (int, error) {
	if r == nil || r.db == nil {
		return 0, errors.New("nodes: repository is not configured")
	}
	var count int
	err := r.db.QueryRow(ctx, "SELECT COUNT(*) FROM nodes").Scan(&count)
	return count, err
}

// CountExisting returns how many of the given ids exist. Duplicates are counted
// once. Callers compare it with the deduplicated length to reject unknown nodes
// before writing any membership row.
func (r *Repo) CountExisting(ctx context.Context, q storage.Queryer, ids []string) (int, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	unique := make([]string, 0, len(ids))
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		unique = append(unique, id)
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(unique)), ",")
	args := make([]any, len(unique))
	for i, id := range unique {
		args[i] = id
	}
	var count int
	err := q.QueryRow(ctx, "SELECT COUNT(*) FROM nodes WHERE id IN ("+placeholders+")", args...).Scan(&count)
	return count, err
}

const nodeSelect = `SELECT id, name, address, region, provider, country_code, tags, status,
	last_seen_at, event_seq, metrics_cpu, metrics_memory, metrics_observed_at, metrics_source,
	created_at, updated_at FROM nodes`

type scanner interface {
	Scan(dest ...any) error
}

func scanNode(row scanner) (Node, error) {
	var (
		node                            Node
		country, tags, status           string
		lastSeen, observed, source      storage.NullString
		createdAt, updatedAt            string
		cpu, memory                     *float64
		eventSeq                        int64
		storedID, name, address, region string
		provider                        string
	)
	if err := row.Scan(&storedID, &name, &address, &region, &provider, &country, &tags, &status,
		&lastSeen, &eventSeq, &cpu, &memory, &observed, &source, &createdAt, &updatedAt); err != nil {
		return Node{}, err
	}
	node.ID = storedID
	node.Name = name
	node.Address = address
	node.Region = region
	node.Provider = provider
	node.CountryCode = country
	node.Status = Status(status)
	if !node.Status.Valid() {
		return Node{}, fmt.Errorf("nodes: stored node %q has invalid status %q", storedID, status)
	}
	decodedTags, err := decodeTags(tags)
	if err != nil {
		return Node{}, err
	}
	node.Tags = decodedTags
	if lastSeen.Valid {
		parsed, err := storage.DecodeTime(lastSeen.String)
		if err != nil {
			return Node{}, err
		}
		node.LastSeenAt = parsed
	}
	if eventSeq < 0 {
		return Node{}, fmt.Errorf("nodes: stored node %q has negative sequence", storedID)
	}
	node.eventSeq = uint64(eventSeq)
	if cpu != nil || memory != nil || observed.Valid {
		if cpu == nil || memory == nil || !observed.Valid || !source.Valid || source.String == "" {
			return Node{}, fmt.Errorf("nodes: stored node %q has incomplete metrics", storedID)
		}
		observedAt, err := storage.DecodeTime(observed.String)
		if err != nil {
			return Node{}, err
		}
		node.Metrics = &Metrics{CPUPercent: *cpu, MemoryPercent: *memory, ObservedAt: observedAt}
	}
	_ = createdAt
	_ = updatedAt
	return node, nil
}

func newNode(input NodeCreateInput, now time.Time) (Node, error) {
	status := input.Status
	if status == "" {
		status = StatusUnknown
	}
	node := Node{
		ID:          input.ID,
		Name:        input.Name,
		Address:     input.Address,
		Region:      input.Region,
		Provider:    input.Provider,
		CountryCode: input.CountryCode,
		Tags:        append([]string(nil), input.Tags...),
		Status:      status,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := validateNode(node); err != nil {
		return Node{}, err
	}
	return node, nil
}

func validateAgentUpdate(update AgentUpdate) error {
	if err := validateNodeID(update.NodeID); err != nil {
		return err
	}
	if update.Sequence == 0 {
		return fmt.Errorf("%w: sequence is required", ErrInvalidNode)
	}
	if update.ObservedAt.IsZero() {
		return fmt.Errorf("%w: observed time is required", ErrInvalidNode)
	}
	if update.ObservedAt.After(time.Now().Add(5 * time.Minute)) {
		return fmt.Errorf("%w: observed time is in the future", ErrInvalidNode)
	}
	if update.Status != "" && !update.Status.Valid() {
		return fmt.Errorf("%w: status", ErrInvalidNode)
	}
	if update.Metrics != nil {
		if update.SampleSource != SampleSourceAgent {
			return fmt.Errorf("%w: sample source", ErrInvalidSample)
		}
		if !validPercent(update.Metrics.CPUPercent) || !validPercent(update.Metrics.MemoryPercent) {
			return ErrInvalidSample
		}
	}
	if update.Tags != nil {
		if _, err := normalizeTags(update.Tags); err != nil {
			return err
		}
	}
	for _, field := range []struct {
		name  string
		value string
		max   int
	}{
		{"name", update.Name, 128},
		{"address", update.Address, 255},
		{"region", update.Region, 64},
		{"provider", update.Provider, 64},
		{"countryCode", update.CountryCode, 2},
	} {
		if field.value != "" && !validText(field.value, field.max) {
			return fmt.Errorf("%w: %s", ErrInvalidNode, field.name)
		}
	}
	return nil
}

func validateNode(node Node) error {
	if err := validateNodeID(node.ID); err != nil {
		return err
	}
	if !validText(node.Name, 128) {
		return fmt.Errorf("%w: name", ErrInvalidNode)
	}
	for _, field := range []struct {
		name  string
		value string
		max   int
	}{
		{"address", node.Address, 255},
		{"region", node.Region, 64},
		{"provider", node.Provider, 64},
		{"countryCode", node.CountryCode, 2},
	} {
		if field.value != "" && !validText(field.value, field.max) {
			return fmt.Errorf("%w: %s", ErrInvalidNode, field.name)
		}
	}
	if !node.Status.Valid() {
		return fmt.Errorf("%w: status", ErrInvalidNode)
	}
	if _, err := normalizeTags(node.Tags); err != nil {
		return err
	}
	if node.Metrics != nil {
		if node.Metrics.ObservedAt.IsZero() || !validPercent(node.Metrics.CPUPercent) || !validPercent(node.Metrics.MemoryPercent) {
			return ErrInvalidSample
		}
		if node.Metrics.ObservedAt.After(time.Now().Add(5 * time.Minute)) {
			return fmt.Errorf("%w: observed time is in the future", ErrInvalidSample)
		}
	}
	return nil
}

func validateNodeID(id string) error {
	if !validText(id, 128) || strings.ContainsAny(id, "*?[]/\\") {
		return fmt.Errorf("%w: id", ErrInvalidNode)
	}
	return nil
}

// validText accepts display text with internal spaces but rejects blank values,
// surrounding whitespace and control/framing characters.
func validText(value string, max int) bool {
	if value == "" || len(value) > max || !utf8.ValidString(value) || strings.TrimSpace(value) != value {
		return false
	}
	return !strings.ContainsFunc(value, func(r rune) bool {
		return unicode.IsControl(r) || unicode.In(r, unicode.Cf, unicode.Zl, unicode.Zp)
	})
}

// validToken accepts identifiers and tags: no whitespace at all.
func validToken(value string, max int) bool {
	if !validText(value, max) {
		return false
	}
	return !strings.ContainsFunc(value, unicode.IsSpace)
}

func validPercent(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value >= 0 && value <= 100
}

func normalizeTags(tags []string) ([]string, error) {
	if len(tags) > 50 {
		return nil, fmt.Errorf("%w: too many tags", ErrInvalidNode)
	}
	out := make([]string, 0, len(tags))
	seen := make(map[string]struct{}, len(tags))
	for _, raw := range tags {
		tag := strings.TrimSpace(raw)
		if tag == "" || len(tag) > 64 || !validToken(tag, 64) {
			return nil, fmt.Errorf("%w: tag", ErrInvalidNode)
		}
		if _, ok := seen[tag]; ok {
			continue
		}
		seen[tag] = struct{}{}
		out = append(out, tag)
	}
	sort.Strings(out)
	return out, nil
}

func encodeTags(tags []string) (string, error) {
	normalized, err := normalizeTags(tags)
	if err != nil {
		return "", err
	}
	encoded, err := json.Marshal(normalized)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

func decodeTags(raw string) ([]string, error) {
	var tags []string
	if raw == "" {
		return []string{}, nil
	}
	if err := json.Unmarshal([]byte(raw), &tags); err != nil {
		return nil, fmt.Errorf("nodes: decode stored tags: %w", err)
	}
	if tags == nil {
		tags = []string{}
	}
	return tags, nil
}

func encodeTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value.UTC().Format(time.RFC3339Nano)
}
