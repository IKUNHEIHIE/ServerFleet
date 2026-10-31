package plugins

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

var ErrOperationRejected = errors.New("plugins: operation record rejected")

// OperationRecord is the management view of one requested plugin operation. It
// never claims a runtime outcome that the lifecycle state has not recorded.
type OperationRecord struct {
	ID              string    `json:"id"`
	PluginID        string    `json:"pluginId"`
	Action          string    `json:"action"`
	Status          string    `json:"status"`
	PreviousVersion string    `json:"previousVersion,omitempty"`
	TargetVersion   string    `json:"targetVersion,omitempty"`
	ErrorCode       string    `json:"errorCode,omitempty"`
	ErrorMessage    string    `json:"errorMessage,omitempty"`
	AuditID         string    `json:"auditId,omitempty"`
	CreatedAt       time.Time `json:"createdAt"`
}

func validOperationRecord(record OperationRecord) bool {
	if !lifecycleID(record.ID) || record.PluginID != "xray-manager" || record.CreatedAt.IsZero() {
		return false
	}
	switch record.Action {
	case "install", "activate", "deactivate", "update", "uninstall":
	default:
		return false
	}
	switch record.Status {
	case "queued", "verifying", "draining", "applying", "succeeded", "failed", "rolled_back", "deferred":
	default:
		return false
	}
	return true
}

type OperationStore interface {
	Get(context.Context, string) (OperationRecord, bool, error)
	Put(context.Context, OperationRecord) error
}

type fileOperationState struct {
	Format     int                        `json:"format"`
	Operations map[string]OperationRecord `json:"operations"`
}

type FileOperationStore struct {
	mu   sync.Mutex
	path string
}

func NewFileOperationStore(path string) (*FileOperationStore, error) {
	if !filepath.IsAbs(path) {
		return nil, ErrOperationRejected
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, ErrOperationRejected
	}
	return &FileOperationStore{path: path}, nil
}

func (s *FileOperationStore) read() (fileOperationState, error) {
	state := fileOperationState{Format: 1, Operations: map[string]OperationRecord{}}
	raw, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return state, nil
	}
	if err != nil || len(raw) == 0 || len(raw) > 512<<10 || json.Unmarshal(raw, &state) != nil || state.Format != 1 || len(state.Operations) > 1000 {
		return fileOperationState{}, ErrOperationRejected
	}
	if state.Operations == nil {
		state.Operations = map[string]OperationRecord{}
	}
	for id, record := range state.Operations {
		if id != record.ID || !validOperationRecord(record) {
			return fileOperationState{}, ErrOperationRejected
		}
	}
	return state, nil
}

func (s *FileOperationStore) Get(_ context.Context, id string) (OperationRecord, bool, error) {
	if s == nil || !lifecycleID(id) {
		return OperationRecord{}, false, ErrOperationRejected
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	state, err := s.read()
	if err != nil {
		return OperationRecord{}, false, err
	}
	record, ok := state.Operations[id]
	return record, ok, nil
}

func (s *FileOperationStore) Put(_ context.Context, record OperationRecord) error {
	if s == nil || !validOperationRecord(record) {
		return ErrOperationRejected
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	state, err := s.read()
	if err != nil {
		return err
	}
	if len(state.Operations) >= 1000 {
		ids := make([]string, 0, len(state.Operations))
		for id := range state.Operations {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		delete(state.Operations, ids[0])
	}
	state.Operations[record.ID] = record
	raw, err := json.Marshal(state)
	if err != nil || len(raw) > 512<<10 {
		return ErrOperationRejected
	}
	return atomicPrivateWrite(s.path, raw)
}
