package nodes

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"serverfleet.local/serverfleet/internal/storage"
)

var (
	ErrGroupNotFound  = errors.New("nodes: group not found")
	ErrGroupNameTaken = errors.New("nodes: group name already exists")
)

// GroupRecord is the persisted ordinary maintenance group. It never schedules
// traffic and never appears in service group APIs.
type GroupRecord struct {
	ID        string
	Name      string
	Notes     string
	Revision  uint64
	Members   []string
	CreatedAt time.Time
	UpdatedAt time.Time
}

type GroupRepo struct {
	db *storage.DB
}

func NewGroupRepo(db *storage.DB) (*GroupRepo, error) {
	if db == nil {
		return nil, errors.New("nodes: database is required")
	}
	return &GroupRepo{db: db}, nil
}

func (r *GroupRepo) Create(ctx context.Context, q storage.Queryer, id, name, notes string, nodeIDs []string) (GroupRecord, error) {
	if r == nil || q == nil {
		return GroupRecord{}, errors.New("nodes: repository is not configured")
	}
	if id == "" {
		return GroupRecord{}, ErrGroupIDRequired
	}
	if !validText(id, 128) {
		return GroupRecord{}, fmt.Errorf("%w: id", ErrInvalidNode)
	}
	if name == "" {
		return GroupRecord{}, ErrGroupNameRequired
	}
	if !validText(name, 128) {
		return GroupRecord{}, fmt.Errorf("%w: group name", ErrInvalidNode)
	}
	if notes != "" && !validText(notes, 1024) {
		return GroupRecord{}, fmt.Errorf("%w: group notes", ErrInvalidNode)
	}
	members, err := memberSet(nodeIDs)
	if err != nil {
		return GroupRecord{}, err
	}
	memberIDs := sortedIDs(members)
	if err := verifyNodesExist(ctx, q, memberIDs); err != nil {
		return GroupRecord{}, err
	}
	now := time.Now().UTC()
	if _, err := q.Exec(ctx, `INSERT INTO node_groups (id, name, notes, revision, created_at, updated_at)
		VALUES (?, ?, ?, 1, ?, ?)`, id, name, notes, encodeTime(now), encodeTime(now)); err != nil {
		if storage.IsUniqueViolation(err) {
			return GroupRecord{}, ErrGroupNameTaken
		}
		return GroupRecord{}, err
	}
	for _, nodeID := range memberIDs {
		if _, err := q.Exec(ctx, "INSERT INTO node_group_members (group_id, node_id) VALUES (?, ?)", id, nodeID); err != nil {
			return GroupRecord{}, err
		}
	}
	return GroupRecord{ID: id, Name: name, Notes: notes, Revision: 1, Members: memberIDs, CreatedAt: now, UpdatedAt: now}, nil
}

func (r *GroupRepo) Get(ctx context.Context, id string) (GroupRecord, error) {
	if r == nil || r.db == nil {
		return GroupRecord{}, errors.New("nodes: repository is not configured")
	}
	record, err := scanGroup(r.db.QueryRow(ctx,
		"SELECT id, name, notes, revision, created_at, updated_at FROM node_groups WHERE id = ?", id))
	if storage.IsNotFound(err) {
		return GroupRecord{}, ErrGroupNotFound
	}
	if err != nil {
		return GroupRecord{}, err
	}
	members, err := groupMembers(ctx, r.db, id)
	if err != nil {
		return GroupRecord{}, err
	}
	record.Members = members
	return record, nil
}

func (r *GroupRepo) List(ctx context.Context) ([]GroupRecord, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("nodes: repository is not configured")
	}
	rows, err := r.db.Query(ctx, "SELECT id, name, notes, revision, created_at, updated_at FROM node_groups ORDER BY name, id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	records := make([]GroupRecord, 0)
	for rows.Next() {
		record, err := scanGroup(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for index := range records {
		members, err := groupMembers(ctx, r.db, records[index].ID)
		if err != nil {
			return nil, err
		}
		records[index].Members = members
	}
	return records, nil
}

func (r *GroupRepo) Rename(ctx context.Context, q storage.Queryer, id string, expectedRevision uint64, name, notes string) (GroupRecord, error) {
	if expectedRevision == 0 {
		return GroupRecord{}, ErrInvalidRevision
	}
	if name == "" {
		return GroupRecord{}, ErrGroupNameRequired
	}
	if !validText(name, 128) {
		return GroupRecord{}, fmt.Errorf("%w: group name", ErrInvalidNode)
	}
	if notes != "" && !validText(notes, 1024) {
		return GroupRecord{}, fmt.Errorf("%w: group notes", ErrInvalidNode)
	}
	current, err := scanGroup(q.QueryRow(ctx,
		"SELECT id, name, notes, revision, created_at, updated_at FROM node_groups WHERE id = ?", id))
	if storage.IsNotFound(err) {
		return GroupRecord{}, ErrGroupNotFound
	}
	if err != nil {
		return GroupRecord{}, err
	}
	if current.Revision != expectedRevision {
		return GroupRecord{}, ErrRevisionConflict
	}
	if current.Name == name && current.Notes == notes {
		members, err := groupMembers(ctx, q, id)
		if err != nil {
			return GroupRecord{}, err
		}
		current.Members = members
		return current, nil
	}
	now := time.Now().UTC()
	if _, err := q.Exec(ctx, "UPDATE node_groups SET name = ?, notes = ?, revision = revision + 1, updated_at = ? WHERE id = ? AND revision = ?",
		name, notes, encodeTime(now), id, expectedRevision); err != nil {
		if storage.IsUniqueViolation(err) {
			return GroupRecord{}, ErrGroupNameTaken
		}
		return GroupRecord{}, err
	}
	members, err := groupMembers(ctx, q, id)
	if err != nil {
		return GroupRecord{}, err
	}
	return GroupRecord{ID: id, Name: name, Notes: notes, Revision: expectedRevision + 1, Members: members, CreatedAt: current.CreatedAt, UpdatedAt: now}, nil
}

func (r *GroupRepo) ReplaceMembers(ctx context.Context, q storage.Queryer, id string, expectedRevision uint64, nodeIDs []string) (GroupRecord, error) {
	if expectedRevision == 0 {
		return GroupRecord{}, ErrInvalidRevision
	}
	next, err := memberSet(nodeIDs)
	if err != nil {
		return GroupRecord{}, err
	}
	nextIDs := sortedIDs(next)
	if err := verifyNodesExist(ctx, q, nextIDs); err != nil {
		return GroupRecord{}, err
	}
	current, err := scanGroup(q.QueryRow(ctx,
		"SELECT id, name, notes, revision, created_at, updated_at FROM node_groups WHERE id = ?", id))
	if storage.IsNotFound(err) {
		return GroupRecord{}, ErrGroupNotFound
	}
	if err != nil {
		return GroupRecord{}, err
	}
	if current.Revision != expectedRevision {
		return GroupRecord{}, ErrRevisionConflict
	}
	previous, err := groupMembers(ctx, q, id)
	if err != nil {
		return GroupRecord{}, err
	}
	if sameStrings(previous, nextIDs) {
		current.Members = previous
		return current, nil
	}
	if _, err := q.Exec(ctx, "DELETE FROM node_group_members WHERE group_id = ?", id); err != nil {
		return GroupRecord{}, err
	}
	for _, nodeID := range nextIDs {
		if _, err := q.Exec(ctx, "INSERT INTO node_group_members (group_id, node_id) VALUES (?, ?)", id, nodeID); err != nil {
			return GroupRecord{}, err
		}
	}
	now := time.Now().UTC()
	if _, err := q.Exec(ctx, "UPDATE node_groups SET revision = revision + 1, updated_at = ? WHERE id = ? AND revision = ?",
		encodeTime(now), id, expectedRevision); err != nil {
		return GroupRecord{}, err
	}
	return GroupRecord{ID: id, Name: current.Name, Notes: current.Notes, Revision: expectedRevision + 1, Members: nextIDs, CreatedAt: current.CreatedAt, UpdatedAt: now}, nil
}

func (r *GroupRepo) Delete(ctx context.Context, q storage.Queryer, id string, expectedRevision uint64) error {
	if expectedRevision == 0 {
		return ErrInvalidRevision
	}
	current, err := scanGroup(q.QueryRow(ctx,
		"SELECT id, name, notes, revision, created_at, updated_at FROM node_groups WHERE id = ?", id))
	if storage.IsNotFound(err) {
		return ErrGroupNotFound
	}
	if err != nil {
		return err
	}
	if current.Revision != expectedRevision {
		return ErrRevisionConflict
	}
	outcome, err := q.Exec(ctx, "DELETE FROM node_groups WHERE id = ? AND revision = ?", id, expectedRevision)
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

func groupMembers(ctx context.Context, q storage.Queryer, groupID string) ([]string, error) {
	rows, err := q.Query(ctx, "SELECT node_id FROM node_group_members WHERE group_id = ? ORDER BY node_id", groupID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	members := make([]string, 0)
	for rows.Next() {
		var nodeID string
		if err := rows.Scan(&nodeID); err != nil {
			return nil, err
		}
		members = append(members, nodeID)
	}
	return members, rows.Err()
}

func verifyNodesExist(ctx context.Context, q storage.Queryer, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	repo := &Repo{}
	count, err := repo.CountExisting(ctx, q, ids)
	if err != nil {
		return err
	}
	if count != len(ids) {
		return ErrUnknownNode
	}
	return nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanGroup(row rowScanner) (GroupRecord, error) {
	var (
		record           GroupRecord
		revision         int64
		created, updated string
	)
	if err := row.Scan(&record.ID, &record.Name, &record.Notes, &revision, &created, &updated); err != nil {
		return GroupRecord{}, err
	}
	if revision < 1 {
		return GroupRecord{}, fmt.Errorf("nodes: stored group %q has invalid revision", record.ID)
	}
	record.Revision = uint64(revision)
	var err error
	if record.CreatedAt, err = storage.DecodeTime(created); err != nil {
		return GroupRecord{}, err
	}
	if record.UpdatedAt, err = storage.DecodeTime(updated); err != nil {
		return GroupRecord{}, err
	}
	return record, nil
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	aCopy := append([]string(nil), a...)
	bCopy := append([]string(nil), b...)
	sort.Strings(aCopy)
	sort.Strings(bCopy)
	for i := range aCopy {
		if aCopy[i] != bCopy[i] {
			return false
		}
	}
	return true
}
