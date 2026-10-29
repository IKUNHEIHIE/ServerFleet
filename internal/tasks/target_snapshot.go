package tasks

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

var (
	ErrEmptyTargets    = errors.New("tasks: target snapshot must not be empty")
	ErrInvalidTarget   = errors.New("tasks: invalid target node id")
	ErrDuplicateTarget = errors.New("tasks: duplicate target node id")
)

// TargetSnapshot is an immutable, ordered set of exact node IDs fixed at submission time.
// The zero value is empty and must never be interpreted as "all nodes"; create snapshots with
// NewTargetSnapshot. Input and returned slices never share mutable storage.
type TargetSnapshot struct {
	ids []string
}

// NewTargetSnapshot copies and validates the caller-provided targets. Blank, whitespace
// padded, wildcard, control-character, invalid-UTF-8 and duplicate IDs are rejected rather
// than trimmed or interpreted. Submission order is preserved.
func NewTargetSnapshot(nodeIDs []string) (TargetSnapshot, error) {
	if len(nodeIDs) == 0 {
		return TargetSnapshot{}, ErrEmptyTargets
	}
	ids := make([]string, 0, len(nodeIDs))
	seen := make(map[string]struct{}, len(nodeIDs))
	for index, id := range nodeIDs {
		if !validTargetID(id) {
			return TargetSnapshot{}, fmt.Errorf("%w: index %d", ErrInvalidTarget, index)
		}
		if _, duplicate := seen[id]; duplicate {
			return TargetSnapshot{}, fmt.Errorf("%w: index %d", ErrDuplicateTarget, index)
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	return TargetSnapshot{ids: ids}, nil
}

// IDs returns a copy of the frozen targets in submission order.
func (s TargetSnapshot) IDs() []string {
	copied := make([]string, len(s.ids))
	copy(copied, s.ids)
	return copied
}

// Len returns the number of frozen targets.
func (s TargetSnapshot) Len() int {
	return len(s.ids)
}

func validTargetID(id string) bool {
	if id == "" || !utf8.ValidString(id) || id != strings.TrimSpace(id) {
		return false
	}
	if strings.ContainsAny(id, "*?[]") {
		return false
	}
	for _, r := range id {
		if unicode.IsSpace(r) || unicode.IsControl(r) || unicode.In(r, unicode.Cf, unicode.Zl, unicode.Zp) {
			return false
		}
	}
	return true
}
