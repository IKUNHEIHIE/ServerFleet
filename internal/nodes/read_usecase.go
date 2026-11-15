package nodes

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"unicode/utf8"

	"serverfleet.local/serverfleet/internal/auth"
)

var (
	ErrInventoryUnavailable = errors.New("nodes: inventory source unavailable")
	ErrInvalidInventory     = errors.New("nodes: invalid inventory")
)

// InventorySource is a provisional, offline function seam, not the Reader port.
// The trusted caller supplies a current snapshot, including Tags and Metrics,
// that remains read-only until ReadVisibleInventory returns. The source must
// cooperate with ctx.
type InventorySource func(context.Context) ([]Node, error)

// ReadVisibleInventory prepares a fresh nodes.read scope before reading any
// inventory, including an empty inventory. subjectID must be bound by a trusted
// authentication boundary, not a client claim. No reader, scope or result is cached.
// Only successful, authorized output may be used for downstream Filter/Classify.
// Sequential reads do not make policy, inventory and output one atomic snapshot
// or observe revocation after preparation; no production entry point is wired.
func ReadVisibleInventory(ctx context.Context, subjectID string, policy auth.PolicyReader, inventory InventorySource) (out []Node, err error) {
	if ctx != nil {
		defer func() {
			if ctxErr := ctx.Err(); ctxErr != nil {
				out, err = nil, ctxErr
			}
		}()
	}
	// Delegate nil context, subject and policy validation to the real preflight.
	scope, err := auth.PrepareNodeReadScope(ctx, subjectID, policy)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if inventory == nil {
		return nil, ErrInventoryUnavailable
	}
	snapshot, sourceErr := inventory(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if errors.Is(sourceErr, context.Canceled) {
		return nil, context.Canceled
	}
	if errors.Is(sourceErr, context.DeadlineExceeded) {
		return nil, context.DeadlineExceeded
	}
	if sourceErr != nil {
		// Do not retain source text or its unwrap chain, even alongside partial data.
		return nil, ErrInventoryUnavailable
	}
	seen := make(map[string]struct{}, len(snapshot))
	// Capacity grows from visible nodes only; do not expose hidden inventory size.
	out = make([]Node, 0)
	for _, n := range snapshot {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if _, duplicate := seen[n.ID]; duplicate {
			return nil, ErrInvalidInventory
		}
		seen[n.ID] = struct{}{}
		checkErr := scope.CheckNode(ctx, n.ID)
		if checkErr != nil {
			if classifyAuthorizationError(checkErr) == authorizationErrorPureDenial {
				continue
			}
			// Invalid IDs and every non-pure denial abort, never a partial list.
			return nil, checkErr
		}
		cloned := n
		if n.Tags != nil {
			cloned.Tags = make([]string, len(n.Tags))
			for i, tag := range n.Tags {
				if err := ctx.Err(); err != nil {
					return nil, err
				}
				cloned.Tags[i] = tag
			}
		}
		if n.Metrics != nil {
			metrics := cloneMetrics(*n.Metrics)
			cloned.Metrics = &metrics
		}
		out = append(out, cloned)
	}
	return out, nil
}

// ErrInvalidGroupNodeQuery rejects invalid filters and cursors without echoing input.
var ErrInvalidGroupNodeQuery = errors.New("nodes: invalid group node query")

// GroupNodeQuery describes a read-only view; automatic buckets are never persisted.
type GroupNodeQuery struct {
	Mode      string
	GroupID   string
	Dimension Dimension
	Value     string
	Search    string
	Status    string
	Cursor    string
	Limit     int
}

type GroupNodePage struct {
	Items      []Node
	Total      int
	NextCursor string
}

type groupNodeCursor struct {
	Version int    `json:"v"`
	Scope   string `json:"scope"`
	After   string `json:"after"`
}

func normalizeGroupNodeQuery(query GroupNodeQuery) (GroupNodeQuery, error) {
	for _, field := range []string{query.GroupID, query.Value, query.Search} {
		if len(field) > 256 || !utf8.ValidString(field) || strings.ContainsRune(field, 0) {
			return GroupNodeQuery{}, ErrInvalidGroupNodeQuery
		}
	}
	query.Value = strings.TrimSpace(query.Value)
	query.Search = strings.ToLower(strings.TrimSpace(query.Search))
	switch query.Mode {
	case "manual":
		if query.GroupID == "" || strings.TrimSpace(query.GroupID) != query.GroupID || query.Dimension != "" || query.Value != "" {
			return GroupNodeQuery{}, ErrInvalidGroupNodeQuery
		}
	case "automatic":
		if query.GroupID != "" || !query.Dimension.Valid() || query.Value == "" {
			return GroupNodeQuery{}, ErrInvalidGroupNodeQuery
		}
	default:
		return GroupNodeQuery{}, ErrInvalidGroupNodeQuery
	}
	if query.Status == "" {
		query.Status = "all"
	}
	if query.Status != "all" && query.Status != "online" && query.Status != "offline" {
		return GroupNodeQuery{}, ErrInvalidGroupNodeQuery
	}
	if query.Limit == 0 {
		query.Limit = 50
	}
	if query.Limit < 1 || query.Limit > 200 || len(query.Cursor) > 2048 {
		return GroupNodeQuery{}, ErrInvalidGroupNodeQuery
	}
	return query, nil
}

// ReadGroupNodeResults reapplies nodes.read before selecting, counting or paging.
// Mixed-visibility manual groups retain GetVisibleGroup denial semantics.
// ID keyset order is stable, but each page reads fresh inventory, not a snapshot.
// Cursors bind normalized filters and subject; they never confer node access.
func (s *Service) ReadGroupNodeResults(ctx context.Context, subjectID string, query GroupNodeQuery) (page GroupNodePage, err error) {
	if ctx != nil {
		defer func() {
			if ctxErr := ctx.Err(); ctxErr != nil {
				page, err = GroupNodePage{}, ctxErr
			}
		}()
	}
	query, err = normalizeGroupNodeQuery(query)
	if err != nil {
		return GroupNodePage{}, err
	}
	scopeBytes, _ := json.Marshal([]string{subjectID, query.Mode, query.GroupID, string(query.Dimension), query.Value, query.Search, query.Status})
	digest := sha256.Sum256(scopeBytes)
	scope := base64.RawURLEncoding.EncodeToString(digest[:])
	after := ""
	if query.Cursor != "" {
		raw, decodeErr := base64.RawURLEncoding.Strict().DecodeString(query.Cursor)
		var cursor groupNodeCursor
		if decodeErr != nil || json.Unmarshal(raw, &cursor) != nil || cursor.Version != 1 || cursor.Scope != scope || validateNodeID(cursor.After) != nil {
			return GroupNodePage{}, ErrInvalidGroupNodeQuery
		}
		// Require canonical encoding, rejecting trailing JSON and unknown fields.
		canonical, _ := json.Marshal(cursor)
		if base64.RawURLEncoding.EncodeToString(canonical) != query.Cursor {
			return GroupNodePage{}, ErrInvalidGroupNodeQuery
		}
		after = cursor.After
	}
	visible, err := s.ListVisible(ctx, subjectID)
	if err != nil {
		return GroupNodePage{}, err
	}
	members := map[string]struct{}{}
	if query.Mode == "manual" {
		group, err := s.GetVisibleGroup(ctx, subjectID, query.GroupID)
		if err != nil {
			return GroupNodePage{}, err
		}
		for _, id := range group.Members {
			members[id] = struct{}{}
		}
	}
	matched := make([]Node, 0)
	for _, node := range visible {
		if err := ctx.Err(); err != nil {
			return GroupNodePage{}, err
		}
		if query.Mode == "manual" {
			if _, ok := members[node.ID]; !ok {
				continue
			}
		} else if !matchesGroupNodeBucket(node, query.Dimension, query.Value) {
			continue
		}
		online := node.Status == StatusOnline
		if (query.Status == "online" && !online) || (query.Status == "offline" && online) {
			continue
		}
		if query.Search != "" && !strings.Contains(strings.ToLower(node.Name), query.Search) && !strings.Contains(strings.ToLower(node.Address), query.Search) {
			continue
		}
		matched = append(matched, node)
	}
	sort.Slice(matched, func(i, j int) bool { return matched[i].ID < matched[j].ID })
	start := sort.Search(len(matched), func(i int) bool { return matched[i].ID > after })
	end := start + query.Limit
	if end > len(matched) {
		end = len(matched)
	}
	page = GroupNodePage{Items: append([]Node{}, matched[start:end]...), Total: len(matched)}
	if end < len(matched) {
		raw, _ := json.Marshal(groupNodeCursor{Version: 1, Scope: scope, After: matched[end-1].ID})
		page.NextCursor = base64.RawURLEncoding.EncodeToString(raw)
	}
	return page, nil
}

func matchesGroupNodeBucket(node Node, dimension Dimension, value string) bool {
	switch dimension {
	case DimensionRegion:
		return strings.TrimSpace(node.Region) == value
	case DimensionProvider:
		return strings.TrimSpace(node.Provider) == value
	case DimensionTag:
		for _, tag := range node.Tags {
			if strings.TrimSpace(tag) == value {
				return true
			}
		}
	}
	return false
}
