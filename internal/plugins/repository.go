package plugins

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"
)

var ErrRepositoryRejected = errors.New("plugins: repository configuration rejected")

// RepositoryConfig is declarative source metadata. It never downloads or installs
// a package and carries no credential material.
type RepositoryConfig struct {
	Mode             string `json:"mode"`
	URL              string `json:"url,omitempty"`
	Ref              string `json:"ref"`
	Path             string `json:"path"`
	ResolvedCommit   string `json:"resolvedCommit,omitempty"`
	OfficialVerified bool   `json:"officialVerified,omitempty"`
	Configured       bool   `json:"configured"`
}

func safeRepositoryToken(value string, max int) bool {
	if value == "" || len(value) > max || !utf8.ValidString(value) {
		return false
	}
	for _, r := range value {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.' || r == '/' {
			continue
		}
		return false
	}
	return true
}

func validRepositoryURL(value string) bool {
	parsed, err := url.Parse(value)
	return err == nil && parsed.Scheme == "https" && parsed.Host != "" && parsed.User == nil && parsed.RawQuery == "" && parsed.Fragment == ""
}

func normalizeRepository(input RepositoryConfig) (RepositoryConfig, error) {
	config := RepositoryConfig{
		Mode:             strings.TrimSpace(input.Mode),
		URL:              strings.TrimSpace(input.URL),
		Ref:              strings.TrimSpace(input.Ref),
		Path:             strings.TrimSpace(input.Path),
		ResolvedCommit:   strings.TrimSpace(input.ResolvedCommit),
		OfficialVerified: false,
		Configured:       true,
	}
	if config.Path == "" {
		config.Path = "Plugin"
	}
	if config.Mode != "official" && config.Mode != "custom" || config.Path != "Plugin" || !safeRepositoryToken(config.Ref, 128) {
		return RepositoryConfig{}, ErrRepositoryRejected
	}
	if config.URL != "" && !validRepositoryURL(config.URL) {
		return RepositoryConfig{}, ErrRepositoryRejected
	}
	if config.Mode == "custom" && config.URL == "" {
		return RepositoryConfig{}, ErrRepositoryRejected
	}
	if config.Mode == "official" && config.URL != "" {
		return RepositoryConfig{}, ErrRepositoryRejected
	}
	if config.ResolvedCommit != "" && !safeRepositoryToken(config.ResolvedCommit, 128) {
		return RepositoryConfig{}, ErrRepositoryRejected
	}
	for _, r := range config.Ref {
		if unicode.IsControl(r) || unicode.IsSpace(r) {
			return RepositoryConfig{}, ErrRepositoryRejected
		}
	}
	return config, nil
}

type RepositoryStore interface {
	Get(context.Context) (RepositoryConfig, error)
	Put(context.Context, RepositoryConfig) error
}

type FileRepositoryStore struct {
	mu   sync.Mutex
	path string
}

func NewFileRepositoryStore(path string) (*FileRepositoryStore, error) {
	if !filepath.IsAbs(path) {
		return nil, ErrRepositoryRejected
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, ErrRepositoryRejected
	}
	return &FileRepositoryStore{path: path}, nil
}

func (s *FileRepositoryStore) Get(_ context.Context) (RepositoryConfig, error) {
	if s == nil {
		return RepositoryConfig{}, ErrRepositoryRejected
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	raw, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return RepositoryConfig{Mode: "official", Path: "Plugin", Configured: false}, nil
	}
	if err != nil || len(raw) == 0 || len(raw) > 16<<10 {
		return RepositoryConfig{}, ErrRepositoryRejected
	}
	var stored RepositoryConfig
	if json.Unmarshal(raw, &stored) != nil {
		return RepositoryConfig{}, ErrRepositoryRejected
	}
	normalized, err := normalizeRepository(stored)
	if err != nil {
		return RepositoryConfig{}, err
	}
	return normalized, nil
}

func (s *FileRepositoryStore) Put(_ context.Context, input RepositoryConfig) error {
	if s == nil {
		return ErrRepositoryRejected
	}
	config, err := normalizeRepository(input)
	if err != nil {
		return err
	}
	raw, err := json.Marshal(config)
	if err != nil || len(raw) > 16<<10 {
		return ErrRepositoryRejected
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return atomicPrivateWrite(s.path, raw)
}

func atomicPrivateWrite(path string, raw []byte) error {
	temp := path + ".tmp"
	file, err := os.OpenFile(temp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return ErrRepositoryRejected
	}
	_, writeErr := file.Write(raw)
	syncErr := file.Sync()
	closeErr := file.Close()
	if writeErr != nil || syncErr != nil || closeErr != nil {
		_ = os.Remove(temp)
		return ErrRepositoryRejected
	}
	if err := os.Rename(temp, path); err != nil {
		_ = os.Remove(temp)
		return ErrRepositoryRejected
	}
	return nil
}
