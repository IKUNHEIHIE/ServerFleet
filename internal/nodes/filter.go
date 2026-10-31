package nodes

import (
	"errors"
	"strings"
)

// ErrInvalidStatus rejects unknown status values instead of silently ignoring them.
var ErrInvalidStatus = errors.New("nodes: invalid status filter")

// NodeQuery is an immutable view filter. An empty Text and Statuses match every node.
type NodeQuery struct {
	Text     string
	Statuses []Status
}

// Filter returns matching nodes in input order without changing the input slice.
// Text matches node name, address or any tag, case-insensitively.
func Filter(nodes []Node, q NodeQuery) ([]Node, error) {
	statuses, err := statusSet(q.Statuses)
	if err != nil {
		return nil, err
	}
	text := strings.ToLower(strings.TrimSpace(q.Text))
	out := make([]Node, 0, len(nodes))
	for _, n := range nodes {
		if len(statuses) > 0 {
			if _, ok := statuses[n.Status]; !ok {
				continue
			}
		}
		if text != "" && !matchesText(n, text) {
			continue
		}
		out = append(out, n)
	}
	return out, nil
}

func statusSet(statuses []Status) (map[Status]struct{}, error) {
	set := make(map[Status]struct{}, len(statuses))
	for _, status := range statuses {
		if !status.Valid() {
			return nil, ErrInvalidStatus
		}
		set[status] = struct{}{}
	}
	return set, nil
}

func matchesText(n Node, lowerQuery string) bool {
	if strings.Contains(strings.ToLower(n.Name), lowerQuery) {
		return true
	}
	if strings.Contains(strings.ToLower(n.Address), lowerQuery) {
		return true
	}
	for _, tag := range n.Tags {
		if strings.Contains(strings.ToLower(tag), lowerQuery) {
			return true
		}
	}
	return false
}
