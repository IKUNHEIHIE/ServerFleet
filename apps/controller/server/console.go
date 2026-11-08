package server

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var ErrConsoleAssets = errors.New("controller: invalid or insecure built Web assets")

const consolePolicy = "default-src 'none'; script-src 'self'; style-src 'self' 'unsafe-inline'; connect-src 'self'; img-src 'self' data:; font-src 'self'; base-uri 'none'; object-src 'none'; frame-ancestors 'none'; form-action 'self'"

type consoleAsset struct {
	bytes             []byte
	contentType, etag string
}
type consoleHandler struct{ assets map[string]consoleAsset }

// LoadConsole accepts only an operator-owned Vite build, never a source tree,
// arbitrary filesystem mount or test fixture host. Snapshot bounded bytes once:
// later path swaps cannot publish private files, symlinks or new JavaScript.
func LoadConsole(directory string) (http.Handler, error) {
	if !filepath.IsAbs(directory) || filepath.Clean(directory) != directory {
		return nil, ErrConsoleAssets
	}
	before, err := os.Lstat(directory)
	if err != nil || !gatewayDeploymentPrivate(before, true) {
		return nil, ErrConsoleAssets
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return nil, ErrConsoleAssets
	}
	defer root.Close()
	after, err := root.Stat(".")
	if err != nil || !os.SameFile(before, after) {
		return nil, ErrConsoleAssets
	}
	h := &consoleHandler{assets: map[string]consoleAsset{}}
	var total int64
	var readDirectory func(string) error
	readDirectory = func(dir string) error {
		f, err := root.Open(dir)
		if err != nil {
			return ErrConsoleAssets
		}
		entries, err := f.ReadDir(-1)
		f.Close()
		if err != nil || len(entries) > 1024 {
			return ErrConsoleAssets
		}
		for _, entry := range entries {
			name := entry.Name()
			if strings.HasPrefix(name, ".") || strings.ContainsAny(name, "\\\x00") {
				return ErrConsoleAssets
			}
			relative := name
			if dir != "." {
				relative = dir + "/" + name
			}
			info, err := root.Lstat(relative)
			if err != nil {
				return ErrConsoleAssets
			}
			if info.IsDir() {
				if relative != "assets" || !gatewayDeploymentPrivate(info, true) {
					return ErrConsoleAssets
				}
				if err := readDirectory(relative); err != nil {
					return err
				}
				continue
			}
			if !gatewayDeploymentPrivate(info, false) || info.Size() <= 0 || info.Size() > 8<<20 || len(h.assets) >= 1024 {
				return ErrConsoleAssets
			}
			kind := ""
			switch path.Ext(relative) {
			case ".html":
				if relative == "index.html" {
					kind = "text/html; charset=utf-8"
				}
			case ".js":
				kind = "text/javascript; charset=utf-8"
			case ".css":
				kind = "text/css; charset=utf-8"
			case ".svg":
				kind = "image/svg+xml"
			case ".png":
				kind = "image/png"
			case ".jpg", ".jpeg":
				kind = "image/jpeg"
			case ".webp":
				kind = "image/webp"
			case ".ico":
				kind = "image/x-icon"
			case ".woff":
				kind = "font/woff"
			case ".woff2":
				kind = "font/woff2"
			}
			if kind == "" || relative != "index.html" && relative != "favicon.ico" && !strings.HasPrefix(relative, "assets/") {
				return ErrConsoleAssets
			}
			file, err := root.Open(relative)
			if err != nil {
				return ErrConsoleAssets
			}
			opened, err := file.Stat()
			if err != nil || !os.SameFile(info, opened) || !gatewayDeploymentPrivate(opened, false) {
				file.Close()
				return ErrConsoleAssets
			}
			data, err := io.ReadAll(io.LimitReader(file, (8<<20)+1))
			file.Close()
			total += int64(len(data))
			if err != nil || int64(len(data)) != info.Size() || total > 32<<20 {
				return ErrConsoleAssets
			}
			digest := sha256.Sum256(data)
			h.assets["/"+relative] = consoleAsset{data, kind, `"` + hex.EncodeToString(digest[:]) + `"`}
		}
		return nil
	}
	if readDirectory(".") != nil {
		return nil, ErrConsoleAssets
	}
	index, ok := h.assets["/index.html"]
	if !ok || !bytes.Contains(index.bytes, []byte(`id="root"`)) || !bytes.Contains(index.bytes, []byte(`/assets/`)) || bytes.Contains(index.bytes, []byte(`/src/`)) {
		return nil, ErrConsoleAssets
	}
	hasScript := false
	for _, match := range regexp.MustCompile(`(?:src|href)="(/assets/[a-zA-Z0-9_.-]+)"`).FindAllSubmatch(index.bytes, -1) {
		name := string(match[1])
		if _, ok := h.assets[name]; !ok {
			return nil, ErrConsoleAssets
		}
		if strings.HasSuffix(name, ".js") {
			hasScript = true
		}
	}
	if !hasScript {
		return nil, ErrConsoleAssets
	}
	h.assets["/"] = index
	return h, nil
}

func (h *consoleHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	asset, ok := h.assets[r.URL.Path]
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", asset.contentType)
	w.Header().Set("Content-Security-Policy", consolePolicy)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("ETag", asset.etag)
	if r.TLS != nil {
		w.Header().Set("Strict-Transport-Security", "max-age=31536000")
	}
	// Files are immutable process-owned snapshots. Management API and Agent WSS
	// use their existing more-specific mux routes and current authorization gates.
	http.ServeContent(w, r, path.Base(r.URL.Path), time.Time{}, bytes.NewReader(asset.bytes))
}
