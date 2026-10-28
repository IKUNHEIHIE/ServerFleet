package nodes_test

import (
	"encoding/json"
	"errors"
	"slices"
	"testing"

	"serverfleet.local/serverfleet/internal/nodes"
)

func TestFilterMatchesNameAddressAndTags(t *testing.T) {
	fixture := loadFixture(t)
	for _, tc := range []struct {
		name  string
		query nodes.NodeQuery
		want  []string
	}{
		{
			name:  "name is case-insensitive and trimmed",
			query: nodes.NodeQuery{Text: "  ALPHA  "},
			want:  []string{"node-online-a"},
		},
		{
			name:  "address substring",
			query: nodes.NodeQuery{Text: "10.0.0.2"},
			want:  []string{"node-offline-a", "node-offline-b"},
		},
		{
			name:  "tag",
			query: nodes.NodeQuery{Text: "prod"},
			want:  []string{"node-online-a", "node-online-b", "node-offline-b"},
		},
		{
			name:  "status only",
			query: nodes.NodeQuery{Statuses: []nodes.Status{nodes.StatusOffline}},
			want:  []string{"node-offline-a", "node-offline-b"},
		},
		{
			name: "combined text and status",
			query: nodes.NodeQuery{
				Text:     "prod",
				Statuses: []nodes.Status{nodes.StatusOffline},
			},
			want: []string{"node-offline-b"},
		},
		{
			name:  "empty query keeps all in input order",
			query: nodes.NodeQuery{},
			want:  []string{"node-online-a", "node-online-b", "node-offline-a", "node-offline-b", "node-suspended-a"},
		},
		{
			name:  "multiple statuses",
			query: nodes.NodeQuery{Statuses: []nodes.Status{nodes.StatusOnline, nodes.StatusSuspended}},
			want:  []string{"node-online-a", "node-online-b", "node-suspended-a"},
		},
		{
			name:  "no match",
			query: nodes.NodeQuery{Text: "does-not-exist"},
			want:  []string{},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := mustJSON(t, fixture)
			got, err := nodes.Filter(fixture, tc.query)
			if err != nil {
				t.Fatalf("Filter returned error: %v", err)
			}
			if !slices.Equal(ids(got), tc.want) {
				t.Fatalf("Filter ids = %v, want %v", ids(got), tc.want)
			}
			if after := mustJSON(t, fixture); string(before) != string(after) {
				t.Fatal("Filter mutated its input")
			}
		})
	}
}

func TestFilterRejectsUnknownStatus(t *testing.T) {
	fixture := loadFixture(t)
	got, err := nodes.Filter(fixture, nodes.NodeQuery{Statuses: []nodes.Status{nodes.Status("bogus")}})
	if !errors.Is(err, nodes.ErrInvalidStatus) {
		t.Fatalf("error = %v, want ErrInvalidStatus", err)
	}
	if got != nil {
		t.Fatalf("result = %v, want nil on rejected filter", got)
	}
}

func TestFilterEmptyInput(t *testing.T) {
	got, err := nodes.Filter(nil, nodes.NodeQuery{Text: "anything"})
	if err != nil {
		t.Fatalf("Filter returned error: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("got %d nodes, want none", len(got))
	}
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal snapshot: %v", err)
	}
	return data
}
