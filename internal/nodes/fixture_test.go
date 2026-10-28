package nodes_test

import (
	"encoding/json"
	"os"
	"testing"

	"serverfleet.local/serverfleet/internal/nodes"
)

func loadFixture(t *testing.T) []nodes.Node {
	t.Helper()
	data, err := os.ReadFile("testdata/nodes.json")
	if err != nil {
		t.Fatalf("read synthetic fixture: %v", err)
	}
	var fixture []nodes.Node
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatalf("decode synthetic fixture: %v", err)
	}
	if len(fixture) == 0 {
		t.Fatal("synthetic fixture is empty")
	}
	return fixture
}

func ids(ns []nodes.Node) []string {
	result := make([]string, 0, len(ns))
	for _, n := range ns {
		result = append(result, n.ID)
	}
	return result
}

func findNode(t *testing.T, ns []nodes.Node, id string) nodes.Node {
	t.Helper()
	for _, n := range ns {
		if n.ID == id {
			return n
		}
	}
	t.Fatalf("node %q not found in fixture", id)
	return nodes.Node{}
}
