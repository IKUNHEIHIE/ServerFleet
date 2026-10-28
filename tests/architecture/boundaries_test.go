package architecture

import (
	"encoding/json"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

type policy struct {
	Module  string              `json:"module"`
	Allowed map[string][]string `json:"allowed"`
}

func within(name, prefix string) bool { return name == prefix || strings.HasPrefix(name, prefix+"/") }

func (p policy) owner(name string) string {
	for prefix := range p.Allowed {
		if within(name, prefix) {
			return prefix
		}
	}
	return ""
}

func (p policy) permits(from, to string) bool {
	a, b := p.owner(from), p.owner(to)
	if a == "" || b == "" {
		return false
	}
	if a == b {
		return true
	}
	for _, allowed := range p.Allowed[a] {
		if b == allowed {
			return true
		}
	}
	return false
}

func TestModuleBoundaries(t *testing.T) {
	root := filepath.Join("..", "..")
	data, err := os.ReadFile(filepath.Join(root, "docs", "development", "dependencies.json"))
	if err != nil {
		t.Fatal(err)
	}
	var p policy
	if err := json.Unmarshal(data, &p); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{"apps/controller", "apps/agent", "internal", "gen", "tests"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(name string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if d.IsDir() || !strings.HasSuffix(name, ".go") {
				return nil
			}
			rel, err := filepath.Rel(root, filepath.Dir(name))
			if err != nil {
				return err
			}
			from := filepath.ToSlash(rel)
			file, err := parser.ParseFile(token.NewFileSet(), name, nil, parser.ImportsOnly)
			if err != nil {
				return err
			}
			for _, spec := range file.Imports {
				imported, err := strconv.Unquote(spec.Path.Value)
				if err != nil {
					return err
				}
				if strings.HasPrefix(imported, p.Module+"/") {
					to := strings.TrimPrefix(imported, p.Module+"/")
					if !p.permits(from, to) {
						t.Errorf("forbidden dependency: %s -> %s", from, to)
					}
				}
				if imported == "database/sql" && !within(from, "internal/storage") {
					t.Errorf("database/sql is storage-owned: %s", name)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestBoundaryCheckerRejectsForbiddenEdges(t *testing.T) {
	p := policy{Allowed: map[string][]string{"internal/auth": {}, "internal/tasks": {"internal/auth"}, "internal/plugins": {"internal/tasks"}, "internal/transport": {}, "tests": {"internal/auth"}}}
	for _, tc := range []struct {
		from, to string
		want     bool
	}{
		{"internal/tasks", "internal/auth", true},
		{"internal/auth", "internal/tasks", false},
		{"internal/plugins", "internal/transport", false},
		{"internal/auth", "tests/mock", false},
		{"internal/auth", "plugins/xray-manager", false},
		{"internal/authz", "internal/auth", false},
	} {
		if got := p.permits(tc.from, tc.to); got != tc.want {
			t.Errorf("%s -> %s = %t, want %t", tc.from, tc.to, got, tc.want)
		}
	}
}
