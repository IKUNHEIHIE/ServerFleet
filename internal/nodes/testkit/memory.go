// Package testkit contains explicit test-only adapters. Nothing here may be wired into a
// production entry point.
package testkit

import (
	"context"
	"errors"
	"strings"

	"serverfleet.local/serverfleet/internal/nodes"
)

var (
	ErrRecordNotFound = errors.New("nodes testkit: record not found")
	ErrDuplicateID    = errors.New("nodes testkit: duplicate record id")
	ErrEmptyID        = errors.New("nodes testkit: empty record id")
)

// MemoryReader is an in-memory nodes.Reader for isolated tests. It is not storage and must
// not replace a real reader in a production composition root.
type MemoryReader struct {
	records map[string]nodes.Record
}

// NewMemoryReader copies records into memory. Ids are trimmed and must be unique.
func NewMemoryReader(records ...nodes.Record) (*MemoryReader, error) {
	m := &MemoryReader{records: make(map[string]nodes.Record, len(records))}
	for _, record := range records {
		record.ID = strings.TrimSpace(record.ID)
		if record.ID == "" {
			return nil, ErrEmptyID
		}
		if _, ok := m.records[record.ID]; ok {
			return nil, ErrDuplicateID
		}
		m.records[record.ID] = record
	}
	return m, nil
}

// Get returns the stored record but never fabricates one for an unknown id.
func (m *MemoryReader) Get(ctx context.Context, id string) (nodes.Record, error) {
	if err := ctx.Err(); err != nil {
		return nodes.Record{}, err
	}
	record, ok := m.records[strings.TrimSpace(id)]
	if !ok {
		return nodes.Record{}, ErrRecordNotFound
	}
	return record, nil
}

var _ nodes.Reader = (*MemoryReader)(nil)
