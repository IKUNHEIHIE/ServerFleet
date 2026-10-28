package nodes

import (
	"errors"
	"sort"
	"strings"
	"sync"
)

var (
	ErrGroupIDRequired   = errors.New("nodes: group id is required")
	ErrGroupNameRequired = errors.New("nodes: group name is required")
	ErrInvalidRevision   = errors.New("nodes: expected revision must be positive")
	ErrRevisionConflict  = errors.New("nodes: group revision conflict")
	ErrInvalidMember     = errors.New("nodes: member id is required")
)

// OrdinaryGroup is a manual inventory grouping. It organizes nodes only: it never schedules
// traffic, issues internal certificates or authorizes AI sessions. Discarding a group removes
// the grouping relationship, never the nodes themselves. Methods are safe for concurrent use.
type OrdinaryGroup struct {
	mu       sync.Mutex
	id       string
	name     string
	notes    string
	revision uint64
	members  map[string]struct{}
}

// NewOrdinaryGroup creates an empty ordinary group at revision 1.
func NewOrdinaryGroup(id, name string) (*OrdinaryGroup, error) {
	id = strings.TrimSpace(id)
	name = strings.TrimSpace(name)
	if id == "" {
		return nil, ErrGroupIDRequired
	}
	if name == "" {
		return nil, ErrGroupNameRequired
	}
	return &OrdinaryGroup{id: id, name: name, revision: 1, members: map[string]struct{}{}}, nil
}

func (g *OrdinaryGroup) ID() string { return g.id }

func (g *OrdinaryGroup) Name() string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.name
}

func (g *OrdinaryGroup) Notes() string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.notes
}

func (g *OrdinaryGroup) Revision() uint64 {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.revision
}

// Members returns a sorted snapshot of member node ids.
func (g *OrdinaryGroup) Members() []string { return sortedIDs(g.memberSnapshot()) }

// Contains reports whether nodeID is currently a member.
func (g *OrdinaryGroup) Contains(nodeID string) bool {
	nodeID = strings.TrimSpace(nodeID)
	g.mu.Lock()
	defer g.mu.Unlock()
	_, ok := g.members[nodeID]
	return ok
}

// ReplaceMembers atomically replaces the full member set when expectedRevision matches the
// current revision. Duplicate ids are collapsed, blank ids are rejected, and an unchanged set
// is a no-op that does not advance the revision.
func (g *OrdinaryGroup) ReplaceMembers(expectedRevision uint64, nodeIDs []string) error {
	if expectedRevision == 0 {
		return ErrInvalidRevision
	}
	next, err := memberSet(nodeIDs)
	if err != nil {
		return err
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if expectedRevision != g.revision {
		return ErrRevisionConflict
	}
	if sameSet(g.members, next) {
		return nil
	}
	g.members = next
	g.revision++
	return nil
}

// Rename atomically updates the editable name and notes when expectedRevision matches the
// current revision; stale writes are rejected without changing state. An unchanged name and
// notes at the current revision is a no-op that does not advance the revision.
func (g *OrdinaryGroup) Rename(expectedRevision uint64, name, notes string) error {
	if expectedRevision == 0 {
		return ErrInvalidRevision
	}
	name = strings.TrimSpace(name)
	notes = strings.TrimSpace(notes)
	g.mu.Lock()
	defer g.mu.Unlock()
	if expectedRevision != g.revision {
		return ErrRevisionConflict
	}
	if name == "" {
		return ErrGroupNameRequired
	}
	if name == g.name && notes == g.notes {
		return nil
	}
	g.name = name
	g.notes = notes
	g.revision++
	return nil
}

// FilterMembers returns matching members for display. Filtering never changes membership or
// the revision; callers must use ReplaceMembers to persist a new member set.
func (g *OrdinaryGroup) FilterMembers(nodes []Node, q NodeQuery) ([]Node, error) {
	memberIDs := g.memberSnapshot()
	members := make([]Node, 0, len(nodes))
	for _, n := range nodes {
		if _, ok := memberIDs[strings.TrimSpace(n.ID)]; ok {
			members = append(members, n)
		}
	}
	return Filter(members, q)
}

func (g *OrdinaryGroup) memberSnapshot() map[string]struct{} {
	g.mu.Lock()
	defer g.mu.Unlock()
	snapshot := make(map[string]struct{}, len(g.members))
	for id := range g.members {
		snapshot[id] = struct{}{}
	}
	return snapshot
}

// ServiceGroup is a separate business resource, not an ordinary grouping. Service membership
// is deliberately a distinct type so ordinary group APIs cannot alter service membership or
// scheduling. Gateway, load-balancing and certificate behavior are out of this domain task.
type ServiceGroup struct {
	mu       sync.Mutex
	id       string
	name     string
	revision uint64
	members  map[string]struct{}
}

// NewServiceGroup creates an empty service group at revision 1.
func NewServiceGroup(id, name string) (*ServiceGroup, error) {
	id = strings.TrimSpace(id)
	name = strings.TrimSpace(name)
	if id == "" {
		return nil, ErrGroupIDRequired
	}
	if name == "" {
		return nil, ErrGroupNameRequired
	}
	return &ServiceGroup{id: id, name: name, revision: 1, members: map[string]struct{}{}}, nil
}

func (g *ServiceGroup) ID() string { return g.id }

func (g *ServiceGroup) Name() string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.name
}

func (g *ServiceGroup) Revision() uint64 {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.revision
}

// Members returns a frozen sorted snapshot, suitable for later target resolution.
func (g *ServiceGroup) Members() []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return sortedIDs(g.members)
}

func (g *ServiceGroup) Contains(nodeID string) bool {
	nodeID = strings.TrimSpace(nodeID)
	g.mu.Lock()
	defer g.mu.Unlock()
	_, ok := g.members[nodeID]
	return ok
}

// ReplaceMembers atomically replaces the service member set when expectedRevision matches.
func (g *ServiceGroup) ReplaceMembers(expectedRevision uint64, nodeIDs []string) error {
	if expectedRevision == 0 {
		return ErrInvalidRevision
	}
	next, err := memberSet(nodeIDs)
	if err != nil {
		return err
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if expectedRevision != g.revision {
		return ErrRevisionConflict
	}
	if sameSet(g.members, next) {
		return nil
	}
	g.members = next
	g.revision++
	return nil
}

// OrdinaryGroupsForNode lists ordinary group ids containing nodeID, sorted.
func OrdinaryGroupsForNode(groups []*OrdinaryGroup, nodeID string) []string {
	ids := make([]string, 0, len(groups))
	for _, g := range groups {
		if g != nil && g.Contains(nodeID) {
			ids = append(ids, g.id)
		}
	}
	sort.Strings(ids)
	return ids
}

// ServiceGroupsForNode lists service group ids containing nodeID, sorted. Service membership
// is reported separately so ordinary grouping cannot be mistaken for business scheduling scope.
func ServiceGroupsForNode(groups []*ServiceGroup, nodeID string) []string {
	ids := make([]string, 0, len(groups))
	for _, g := range groups {
		if g != nil && g.Contains(nodeID) {
			ids = append(ids, g.id)
		}
	}
	sort.Strings(ids)
	return ids
}

func memberSet(nodeIDs []string) (map[string]struct{}, error) {
	set := make(map[string]struct{}, len(nodeIDs))
	for _, raw := range nodeIDs {
		id := strings.TrimSpace(raw)
		if id == "" {
			return nil, ErrInvalidMember
		}
		set[id] = struct{}{}
	}
	return set, nil
}

func sameSet(a, b map[string]struct{}) bool {
	if len(a) != len(b) {
		return false
	}
	for id := range a {
		if _, ok := b[id]; !ok {
			return false
		}
	}
	return true
}

func sortedIDs(set map[string]struct{}) []string {
	ids := make([]string, 0, len(set))
	for id := range set {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}
