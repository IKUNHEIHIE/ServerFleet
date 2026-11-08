package server

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"serverfleet.local/serverfleet/internal/recovery"
)

func consoleDirectory(t *testing.T) string {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("private Linux deployment assets")
	}
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "assets"), 0700); err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string]string{"index.html": `<!doctype html><div id="root"></div><script type="module" src="/assets/main-abcd.js"></script>`, "assets/main-abcd.js": `document.title="Server Fleet";`} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}
func TestConsoleServesOnlyFrozenBuiltAssetsAndHead(t *testing.T) {
	dir := consoleDirectory(t)
	handler, err := LoadConsole(dir)
	if err != nil {
		t.Fatal(err)
	}
	// A later same-owner edit is not dynamically exposed as a replacement bundle.
	if err := os.WriteFile(filepath.Join(dir, "assets/main-abcd.js"), []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, route := range []string{"/", "/index.html", "/assets/main-abcd.js"} {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest("GET", route, nil))
		if w.Code != 200 || w.Header().Get("Content-Security-Policy") == "" || w.Header().Get("X-Content-Type-Options") != "nosniff" || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("asset %s: %d %v", route, w.Code, w.Header())
		}
		if route == "/assets/main-abcd.js" && strings.Contains(w.Body.String(), "changed") {
			t.Fatal("served unfrozen replacement")
		}
		head := httptest.NewRecorder()
		handler.ServeHTTP(head, httptest.NewRequest("HEAD", route, nil))
		if head.Code != 200 || head.Body.Len() != 0 {
			t.Fatalf("HEAD %s %d", route, head.Code)
		}
	}
	for _, route := range []string{"/api/v1/nodes", "/agent/ws", "/source.ts", "/assets/../index.html", "/assets/.secret", "/missing"} {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest("GET", route, nil))
		if w.Code != 404 {
			t.Fatalf("unexpected SPA/file fallback %s: %d", route, w.Code)
		}
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest("POST", "/", nil))
	if w.Code != 405 {
		t.Fatal(w.Code)
	}
}
func TestConsoleRejectsSourceTreesSecretsLinksAndMissingBundle(t *testing.T) {
	for _, mode := range []string{"source", "secret", "symlink", "hardlink", "directory", "missing", "public-file", "public-root"} {
		t.Run(mode, func(t *testing.T) {
			dir := consoleDirectory(t)
			bundle := filepath.Join(dir, "assets/main-abcd.js")
			switch mode {
			case "source":
				os.WriteFile(filepath.Join(dir, "index.html"), []byte(`<div id="root"></div><script src="/src/main.tsx"></script>`), 0600)
			case "secret":
				os.WriteFile(filepath.Join(dir, "token.txt"), []byte("must not publish"), 0600)
			case "symlink":
				os.Symlink(bundle, filepath.Join(dir, "assets/linked.js"))
			case "hardlink":
				if err := os.Link(bundle, filepath.Join(dir, "assets/linked.js")); err != nil {
					t.Fatal(err)
				}
			case "directory":
				os.Mkdir(filepath.Join(dir, "state"), 0700)
			case "missing":
				os.Remove(bundle)
			case "public-file":
				os.Chmod(bundle, 0644)
			case "public-root":
				os.Chmod(dir, 0755)
			}
			if _, err := LoadConsole(dir); !errors.Is(err, ErrConsoleAssets) {
				t.Fatal("unsafe asset source accepted", err)
			}
		})
	}
}
func TestFormalConsolePreservesAPIAndLiveQuarantineGate(t *testing.T) {
	dir := consoleDirectory(t)
	s, err := New(context.Background(), Config{DBPath: filepath.Join(t.TempDir(), "controller.db"), Origin: "https://localhost", WebDirectory: dir})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	root := httptest.NewRecorder()
	s.ServeHTTP(root, httptest.NewRequest("GET", "/", nil))
	if root.Code != 200 || !bytes.Contains(root.Body.Bytes(), []byte(`id="root"`)) {
		t.Fatal("formal console missing", root.Code)
	}
	api := httptest.NewRecorder()
	s.ServeHTTP(api, httptest.NewRequest("GET", "/api/v1/nodes", nil))
	if api.Code != 401 || strings.Contains(api.Body.String(), `id="root"`) {
		t.Fatalf("console intercepted protected API: %d %s", api.Code, api.Body.String())
	}
	missing := httptest.NewRecorder()
	s.ServeHTTP(missing, httptest.NewRequest("GET", "/api/v1/unknown", nil))
	if missing.Code != 404 {
		t.Fatal("unknown API became SPA", missing.Code)
	}
	s.runtimeGate = func(context.Context) error { return recovery.ErrQuarantined }
	denied := httptest.NewRecorder()
	s.ServeHTTP(denied, httptest.NewRequest("GET", "/", nil))
	if denied.Code != 503 || !strings.Contains(denied.Body.String(), "CONTROLLER_QUARANTINED") {
		t.Fatal("console bypassed restored runtime gate", denied.Code)
	}
}
