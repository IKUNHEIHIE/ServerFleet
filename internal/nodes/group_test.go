package nodes_test

import (
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"

	"serverfleet.local/serverfleet/internal/nodes"
)

func TestNewOrdinaryGroupValidatesIdentity(t *testing.T) {
	if _, err := nodes.NewOrdinaryGroup("  ", "Web Fleet"); !errors.Is(err, nodes.ErrGroupIDRequired) {
		t.Fatalf("blank id error = %v, want ErrGroupIDRequired", err)
	}
	if _, err := nodes.NewOrdinaryGroup("grp-1", "  "); !errors.Is(err, nodes.ErrGroupNameRequired) {
		t.Fatalf("blank name error = %v, want ErrGroupNameRequired", err)
	}
	group, err := nodes.NewOrdinaryGroup(" grp-1 ", " Web Fleet ")
	if err != nil {
		t.Fatalf("NewOrdinaryGroup returned error: %v", err)
	}
	if group.ID() != "grp-1" || group.Name() != "Web Fleet" {
		t.Fatalf("identity = %q/%q, want trimmed grp-1/Web Fleet", group.ID(), group.Name())
	}
	if group.Revision() != 1 || len(group.Members()) != 0 {
		t.Fatalf("new group revision/members = %d/%v, want 1/empty", group.Revision(), group.Members())
	}
}

func TestOrdinaryGroupReplaceMembersGuardsRevision(t *testing.T) {
	group, err := nodes.NewOrdinaryGroup("grp-1", "Web Fleet")
	if err != nil {
		t.Fatal(err)
	}
	if err := group.ReplaceMembers(1, []string{" node-2 ", "node-1", "node-1"}); err != nil {
		t.Fatalf("ReplaceMembers returned error: %v", err)
	}
	if group.Revision() != 2 {
		t.Fatalf("revision = %d, want 2", group.Revision())
	}
	if want := []string{"node-1", "node-2"}; !slices.Equal(group.Members(), want) {
		t.Fatalf("members = %v, want %v", group.Members(), want)
	}

	if err := group.ReplaceMembers(1, []string{"node-3"}); !errors.Is(err, nodes.ErrRevisionConflict) {
		t.Fatalf("stale revision error = %v, want ErrRevisionConflict", err)
	}
	if group.Revision() != 2 {
		t.Fatalf("revision advanced after conflict: %d", group.Revision())
	}
	if want := []string{"node-1", "node-2"}; !slices.Equal(group.Members(), want) {
		t.Fatalf("members changed after conflict: %v", group.Members())
	}

	if err := group.ReplaceMembers(0, []string{"node-3"}); !errors.Is(err, nodes.ErrInvalidRevision) {
		t.Fatalf("zero revision error = %v, want ErrInvalidRevision", err)
	}

	if err := group.ReplaceMembers(2, []string{"node-1"}); err != nil {
		t.Fatalf("ReplaceMembers returned error: %v", err)
	}
	if group.Revision() != 3 {
		t.Fatalf("revision = %d, want 3", group.Revision())
	}

	if err := group.ReplaceMembers(3, []string{"node-1"}); err != nil {
		t.Fatalf("idempotent ReplaceMembers returned error: %v", err)
	}
	if group.Revision() != 3 {
		t.Fatalf("idempotent replace advanced revision to %d", group.Revision())
	}

	if err := group.ReplaceMembers(3, []string{"  "}); !errors.Is(err, nodes.ErrInvalidMember) {
		t.Fatalf("blank member error = %v, want ErrInvalidMember", err)
	}
	if group.Revision() != 3 || !slices.Equal(group.Members(), []string{"node-1"}) {
		t.Fatalf("group changed after rejected member: rev=%d members=%v", group.Revision(), group.Members())
	}

	if err := group.ReplaceMembers(3, nil); err != nil {
		t.Fatalf("clear members error: %v", err)
	}
	if group.Revision() != 4 || len(group.Members()) != 0 {
		t.Fatalf("after clear rev=%d members=%v, want 4/empty", group.Revision(), group.Members())
	}
}

func TestOrdinaryGroupReplaceMembersIsAtomicAcrossGoroutines(t *testing.T) {
	group, err := nodes.NewOrdinaryGroup("grp-1", "Web Fleet")
	if err != nil {
		t.Fatal(err)
	}
	const workers = 16
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs <- group.ReplaceMembers(1, []string{fmt.Sprintf("node-%d", i)})
		}(i)
	}
	wg.Wait()
	close(errs)

	successes, conflicts := 0, 0
	for err := range errs {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, nodes.ErrRevisionConflict):
			conflicts++
		default:
			t.Fatalf("unexpected concurrent error: %v", err)
		}
	}
	if successes != 1 || conflicts != workers-1 {
		t.Fatalf("successes/conflicts = %d/%d, want 1/%d", successes, conflicts, workers-1)
	}
	if group.Revision() != 2 {
		t.Fatalf("revision = %d, want 2", group.Revision())
	}
	members := group.Members()
	if len(members) != 1 || !group.Contains(members[0]) {
		t.Fatalf("members = %v, want exactly the winning id", members)
	}
}

func TestOrdinaryGroupRename(t *testing.T) {
	group, err := nodes.NewOrdinaryGroup("grp-1", "Web Fleet")
	if err != nil {
		t.Fatal(err)
	}
	if err := group.Rename("Fleet A", "primary"); err != nil {
		t.Fatalf("Rename returned error: %v", err)
	}
	if group.Name() != "Fleet A" || group.Notes() != "primary" || group.Revision() != 2 {
		t.Fatalf("after rename name/notes/revision = %q/%q/%d", group.Name(), group.Notes(), group.Revision())
	}
	if err := group.Rename("Fleet A", "primary"); err != nil {
		t.Fatalf("no-op Rename returned error: %v", err)
	}
	if group.Revision() != 2 {
		t.Fatalf("no-op rename advanced revision to %d", group.Revision())
	}
	if err := group.Rename(" ", "other"); !errors.Is(err, nodes.ErrGroupNameRequired) {
		t.Fatalf("blank name error = %v, want ErrGroupNameRequired", err)
	}
	if group.Name() != "Fleet A" || group.Notes() != "primary" {
		t.Fatalf("failed rename changed group: %q/%q", group.Name(), group.Notes())
	}
}

func TestOrdinaryGroupFilterMembersDoesNotMutateMembership(t *testing.T) {
	fixture := loadFixture(t)
	group, err := nodes.NewOrdinaryGroup("grp-1", "Web Fleet")
	if err != nil {
		t.Fatal(err)
	}
	if err := group.ReplaceMembers(1, []string{"node-online-a", "node-offline-a"}); err != nil {
		t.Fatal(err)
	}
	revision := group.Revision()

	got, err := group.FilterMembers(fixture, nodes.NodeQuery{Statuses: []nodes.Status{nodes.StatusOnline}})
	if err != nil {
		t.Fatalf("FilterMembers returned error: %v", err)
	}
	if want := []string{"node-online-a"}; !slices.Equal(ids(got), want) {
		t.Fatalf("FilterMembers ids = %v, want %v", ids(got), want)
	}
	if group.Revision() != revision {
		t.Fatalf("filtering changed revision: %d", group.Revision())
	}
	if want := []string{"node-offline-a", "node-online-a"}; !slices.Equal(group.Members(), want) {
		t.Fatalf("filtering changed membership: %v", group.Members())
	}

	if _, err := group.FilterMembers(fixture, nodes.NodeQuery{Statuses: []nodes.Status{nodes.Status("bogus")}}); !errors.Is(err, nodes.ErrInvalidStatus) {
		t.Fatalf("invalid filter error = %v, want ErrInvalidStatus", err)
	}
	if want := []string{"node-offline-a", "node-online-a"}; !slices.Equal(group.Members(), want) {
		t.Fatalf("rejected filter changed membership: %v", group.Members())
	}
}

func TestOrdinaryAndServiceGroupsStaySeparate(t *testing.T) {
	ordinaryA, err := nodes.NewOrdinaryGroup("grp-1", "Web Fleet")
	if err != nil {
		t.Fatal(err)
	}
	ordinaryB, err := nodes.NewOrdinaryGroup("grp-2", "Prod Fleet")
	if err != nil {
		t.Fatal(err)
	}
	if err := ordinaryA.ReplaceMembers(1, []string{"node-online-a"}); err != nil {
		t.Fatal(err)
	}
	if err := ordinaryB.ReplaceMembers(1, []string{"node-online-a", "node-online-b"}); err != nil {
		t.Fatal(err)
	}

	serviceA, err := nodes.NewServiceGroup("svc-1", "Public Web")
	if err != nil {
		t.Fatal(err)
	}
	serviceB, err := nodes.NewServiceGroup("svc-2", "Internal API")
	if err != nil {
		t.Fatal(err)
	}
	if err := serviceA.ReplaceMembers(1, []string{"node-online-a"}); err != nil {
		t.Fatal(err)
	}
	if err := serviceB.ReplaceMembers(1, []string{"node-online-b"}); err != nil {
		t.Fatal(err)
	}

	ordinaryIDs := nodes.OrdinaryGroupsForNode([]*nodes.OrdinaryGroup{ordinaryB, ordinaryA}, "node-online-a")
	if want := []string{"grp-1", "grp-2"}; !slices.Equal(ordinaryIDs, want) {
		t.Fatalf("ordinary groups = %v, want %v", ordinaryIDs, want)
	}
	if slices.Contains(ordinaryIDs, "svc-1") {
		t.Fatal("ordinary group query leaked a service group")
	}
	if got := nodes.ServiceGroupsForNode([]*nodes.ServiceGroup{serviceB, serviceA}, "node-online-a"); !slices.Equal(got, []string{"svc-1"}) {
		t.Fatalf("service groups = %v, want [svc-1]", got)
	}
	if got := nodes.ServiceGroupsForNode([]*nodes.ServiceGroup{serviceA}, "node-online-b"); len(got) != 0 {
		t.Fatalf("service groups for unrelated node = %v, want none", got)
	}

	serviceRevision := serviceA.Revision()
	ordinaryRevision := ordinaryA.Revision()
	if err := serviceA.ReplaceMembers(serviceRevision, nil); err != nil {
		t.Fatalf("clear service members error: %v", err)
	}
	if !ordinaryA.Contains("node-online-a") || ordinaryA.Revision() != ordinaryRevision {
		t.Fatal("service membership change altered ordinary grouping")
	}
}

func TestServiceGroupReplaceMembersGuardsRevision(t *testing.T) {
	group, err := nodes.NewServiceGroup("svc-1", "Public Web")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := nodes.NewServiceGroup(" ", "Public Web"); !errors.Is(err, nodes.ErrGroupIDRequired) {
		t.Fatalf("blank id error = %v, want ErrGroupIDRequired", err)
	}
	if _, err := nodes.NewServiceGroup("svc-1", ""); !errors.Is(err, nodes.ErrGroupNameRequired) {
		t.Fatalf("blank name error = %v, want ErrGroupNameRequired", err)
	}
	if err := group.ReplaceMembers(1, []string{"node-1"}); err != nil {
		t.Fatalf("ReplaceMembers returned error: %v", err)
	}
	if group.Revision() != 2 {
		t.Fatalf("revision = %d, want 2", group.Revision())
	}
	if err := group.ReplaceMembers(1, nil); !errors.Is(err, nodes.ErrRevisionConflict) {
		t.Fatalf("stale revision error = %v, want ErrRevisionConflict", err)
	}
	if !group.Contains("node-1") {
		t.Fatal("conflicting replace changed service membership")
	}
	if err := group.ReplaceMembers(0, nil); !errors.Is(err, nodes.ErrInvalidRevision) {
		t.Fatalf("zero revision error = %v, want ErrInvalidRevision", err)
	}
	if err := group.ReplaceMembers(2, []string{""}); !errors.Is(err, nodes.ErrInvalidMember) {
		t.Fatalf("blank member error = %v, want ErrInvalidMember", err)
	}
}
