package testkit_test

import (
	"context"
	"errors"
	"testing"

	"serverfleet.local/serverfleet/internal/nodes"
	"serverfleet.local/serverfleet/internal/nodes/testkit"
)

func TestMemoryReaderGet(t *testing.T) {
	reader, err := testkit.NewMemoryReader(
		nodes.Record{ID: "node-1", Name: "Alpha"},
		nodes.Record{ID: "node-2", Name: "Beta"},
	)
	if err != nil {
		t.Fatalf("NewMemoryReader returned error: %v", err)
	}
	var port nodes.Reader = reader
	got, err := port.Get(context.Background(), "node-1")
	if err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	if want := (nodes.Record{ID: "node-1", Name: "Alpha"}); got != want {
		t.Fatalf("Get = %+v, want %+v", got, want)
	}
}

func TestMemoryReaderRejectsBlankAndDuplicateIDs(t *testing.T) {
	if _, err := testkit.NewMemoryReader(nodes.Record{ID: " "}); !errors.Is(err, testkit.ErrEmptyID) {
		t.Fatalf("blank id error = %v, want ErrEmptyID", err)
	}
	if _, err := testkit.NewMemoryReader(
		nodes.Record{ID: "node-1"},
		nodes.Record{ID: "node-1"},
	); !errors.Is(err, testkit.ErrDuplicateID) {
		t.Fatalf("duplicate id error = %v, want ErrDuplicateID", err)
	}
}

func TestMemoryReaderNotFound(t *testing.T) {
	reader, err := testkit.NewMemoryReader(nodes.Record{ID: "node-1"})
	if err != nil {
		t.Fatal(err)
	}
	if got, err := reader.Get(context.Background(), " node-1 "); err != nil || got.ID != "node-1" {
		t.Fatalf("trimmed id lookup = %+v, %v; want node-1", got, err)
	}
	if _, err := reader.Get(context.Background(), "node-2"); !errors.Is(err, testkit.ErrRecordNotFound) {
		t.Fatalf("unknown id error = %v, want ErrRecordNotFound", err)
	}
	if _, err := reader.Get(context.Background(), " "); !errors.Is(err, testkit.ErrRecordNotFound) {
		t.Fatalf("blank id error = %v, want ErrRecordNotFound", err)
	}
}

func TestMemoryReaderHonorsCanceledContext(t *testing.T) {
	reader, err := testkit.NewMemoryReader(nodes.Record{ID: "node-1"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := reader.Get(ctx, "node-1"); !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
}
