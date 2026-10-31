package tasks_test

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"serverfleet.local/serverfleet/internal/tasks"
)

func TestTargetSnapshotPreservesOrderAndIsolatesSlices(t *testing.T) {
	input := make([]string, 2, 4)
	input[0] = "node-b"
	input[1] = "node-a"

	snapshot, err := tasks.NewTargetSnapshot(input)
	if err != nil {
		t.Fatalf("NewTargetSnapshot returned error: %v", err)
	}
	if snapshot.Len() != 2 {
		t.Fatalf("Len = %d, want 2", snapshot.Len())
	}
	if want := []string{"node-b", "node-a"}; !slices.Equal(snapshot.IDs(), want) {
		t.Fatalf("IDs = %v, want submission order %v", snapshot.IDs(), want)
	}

	input[0] = "changed"
	input[1] = "also-changed"
	if want := []string{"node-b", "node-a"}; !slices.Equal(snapshot.IDs(), want) {
		t.Fatalf("snapshot changed with input mutation: %v", snapshot.IDs())
	}

	returned := snapshot.IDs()
	returned[0] = "mutated-copy"
	if want := []string{"node-b", "node-a"}; !slices.Equal(snapshot.IDs(), want) {
		t.Fatalf("snapshot changed with returned-copy mutation: %v", snapshot.IDs())
	}
}

func TestTargetSnapshotLaterMembersDoNotEnterOldSnapshot(t *testing.T) {
	first, err := tasks.NewTargetSnapshot([]string{"node-a"})
	if err != nil {
		t.Fatal(err)
	}
	updated, err := tasks.NewTargetSnapshot([]string{"node-a", "node-b"})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"node-a"}; !slices.Equal(first.IDs(), want) {
		t.Fatalf("old snapshot = %v, want %v", first.IDs(), want)
	}
	if want := []string{"node-a", "node-b"}; !slices.Equal(updated.IDs(), want) {
		t.Fatalf("new snapshot = %v, want %v", updated.IDs(), want)
	}
}

func TestTargetSnapshotZeroValueIsEmpty(t *testing.T) {
	var snapshot tasks.TargetSnapshot
	if snapshot.Len() != 0 {
		t.Fatalf("zero Len = %d, want 0", snapshot.Len())
	}
	if ids := snapshot.IDs(); len(ids) != 0 {
		t.Fatalf("zero IDs = %v, want none", ids)
	}
}

func TestNewTargetSnapshotRejectsEmptyTargets(t *testing.T) {
	for _, input := range [][]string{nil, {}} {
		if _, err := tasks.NewTargetSnapshot(input); !errors.Is(err, tasks.ErrEmptyTargets) {
			t.Fatalf("NewTargetSnapshot(%v) error = %v, want ErrEmptyTargets", input, err)
		}
	}
}

func TestNewTargetSnapshotRejectsBadIDs(t *testing.T) {
	for _, badID := range []string{
		"",
		" ",
		" node-a",
		"node-a ",
		"node a",
		"node\ta",
		"node\x00",
		"node\u200b",
		"node\u2028",
		string([]byte{0xff}),
		"node*",
		"node?",
		"node[1]",
		"node]",
	} {
		t.Run(badID, func(t *testing.T) {
			if _, err := tasks.NewTargetSnapshot([]string{"node-valid", badID}); !errors.Is(err, tasks.ErrInvalidTarget) {
				t.Fatalf("error = %v, want ErrInvalidTarget", err)
			}
		})
	}
}

func TestNewTargetSnapshotAcceptsExactValidIDs(t *testing.T) {
	input := []string{"node-1", "node.example", "node:443", "节点-1"}
	snapshot, err := tasks.NewTargetSnapshot(input)
	if err != nil {
		t.Fatalf("NewTargetSnapshot returned error: %v", err)
	}
	if !slices.Equal(snapshot.IDs(), input) {
		t.Fatalf("IDs = %v, want %v", snapshot.IDs(), input)
	}
}

func TestNewTargetSnapshotRejectsDuplicates(t *testing.T) {
	if _, err := tasks.NewTargetSnapshot([]string{"node-a", "node-a"}); !errors.Is(err, tasks.ErrDuplicateTarget) {
		t.Fatalf("duplicate error = %v, want ErrDuplicateTarget", err)
	}
	if _, err := tasks.NewTargetSnapshot([]string{"node-a", " node-a"}); !errors.Is(err, tasks.ErrInvalidTarget) {
		t.Fatalf("whitespace variant error = %v, want ErrInvalidTarget (no trim)", err)
	}
}

func TestTargetSnapshotErrorDoesNotEchoID(t *testing.T) {
	secret := "secret-node-id\x00"
	_, err := tasks.NewTargetSnapshot([]string{secret})
	if err == nil {
		t.Fatal("bad target was accepted")
	}
	if strings.Contains(err.Error(), "secret-node-id") {
		t.Fatalf("error leaked target value: %v", err)
	}
}
