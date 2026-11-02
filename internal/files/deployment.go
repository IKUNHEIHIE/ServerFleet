package files

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
)

var ErrDeploymentConfig = errors.New("files: invalid or insecure deployment configuration")

type rootRegistration struct {
	NodeID      string `json:"nodeId"`
	LogicalRoot string `json:"logicalRoot"`
}
type rootRegistry struct {
	Version   int                `json:"version"`
	SpoolDir  string             `json:"spoolDir"`
	NodeRoots []rootRegistration `json:"nodeRoots"`
}

// LoadDeployment reads a startup-only administrator provisioned registry. It
// never grants a resource permission or adds AI/plugin authority. No reload or
// management endpoint accepts this document. Linux ownership is mandatory;
// unsupported platforms fail closed until an equivalent ACL check is provided.
func LoadDeployment(filename string) (*RootPathMapper, string, error) {
	if !filepath.IsAbs(filename) {
		return nil, "", ErrDeploymentConfig
	}
	directory := filepath.Dir(filename)
	before, err := os.Lstat(directory)
	if err != nil || !deploymentPrivate(before, true) {
		return nil, "", ErrDeploymentConfig
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return nil, "", ErrDeploymentConfig
	}
	defer root.Close()
	anchored, err := root.Stat(".")
	if err != nil || !os.SameFile(before, anchored) {
		return nil, "", ErrDeploymentConfig
	}
	name := filepath.Base(filename)
	info, err := root.Lstat(name)
	if err != nil || !deploymentPrivate(info, false) || info.Size() <= 0 || info.Size() > 128<<10 {
		return nil, "", ErrDeploymentConfig
	}
	f, err := root.Open(name)
	if err != nil {
		return nil, "", ErrDeploymentConfig
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !os.SameFile(info, opened) || !deploymentPrivate(opened, false) {
		return nil, "", ErrDeploymentConfig
	}
	raw, err := io.ReadAll(io.LimitReader(f, (128<<10)+1))
	if err != nil || len(raw) > 128<<10 {
		return nil, "", ErrDeploymentConfig
	}
	return parseDeployment(raw)
}
func parseDeployment(raw []byte) (*RootPathMapper, string, error) {
	// Duplicate fields are rejected too; encoding/json alone uses last-wins.
	decoder := json.NewDecoder(bytes.NewReader(raw))
	if uniqueJSONValue(decoder) != nil {
		return nil, "", ErrDeploymentConfig
	}
	if _, err := decoder.Token(); err != io.EOF {
		return nil, "", ErrDeploymentConfig
	}
	decoder = json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var cfg rootRegistry
	if decoder.Decode(&cfg) != nil || cfg.Version != 1 || !filepath.IsAbs(cfg.SpoolDir) || len(cfg.NodeRoots) == 0 || len(cfg.NodeRoots) > 1000 {
		return nil, "", ErrDeploymentConfig
	}
	roots := map[string]string{}
	for _, entry := range cfg.NodeRoots {
		if _, exists := roots[entry.NodeID]; exists {
			return nil, "", ErrDeploymentConfig
		}
		roots[entry.NodeID] = entry.LogicalRoot
	}
	mapper, err := NewRootPathMapper(roots)
	if err != nil || CheckExecutionSpool(cfg.SpoolDir) != nil {
		return nil, "", ErrDeploymentConfig
	}
	return mapper, cfg.SpoolDir, nil
}
func uniqueJSONValue(d *json.Decoder) error {
	token, err := d.Token()
	if err != nil {
		return err
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	if delim != '{' && delim != '[' {
		return ErrDeploymentConfig
	}
	seen := map[string]bool{}
	for d.More() {
		if delim == '{' {
			key, err := d.Token()
			if err != nil {
				return err
			}
			name, ok := key.(string)
			if !ok || seen[name] {
				return ErrDeploymentConfig
			}
			seen[name] = true
		}
		if err := uniqueJSONValue(d); err != nil {
			return err
		}
	}
	_, err = d.Token()
	return err
}
