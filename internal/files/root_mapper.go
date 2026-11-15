package files

import (
	"context"
	"strings"
)

// RootPathMapper maps explicitly configured logical POSIX roots to Agent roots.
// This does not inspect Controller filesystem paths or infer roots by stripping /.
//
// A logical root of "/" means the console path space IS the Agent file root:
// "/data/x" addresses "data/x" inside the Agent root. Directory browsing,
// uploads and node-to-node transfers then all speak one path space, which is
// what the console shows and what the Agent confines under its file root. Any
// other root keeps the prefix form ("/files/x" -> "x") and only addresses paths
// under it.
type RootPathMapper struct{ roots map[string]string }

func NewRootPathMapper(roots map[string]string) (*RootPathMapper, error) {
	if len(roots) == 0 || len(roots) > 1000 {
		return nil, ErrPathMappingUnavailable
	}
	m := &RootPathMapper{roots: map[string]string{}}
	for node, root := range roots {
		if !validChannelID(node) || ValidateDirectoryPath(root) != nil {
			return nil, ErrPathMappingUnavailable
		}
		if root == "/" {
			root = "" // the Agent file root itself; presence is tracked by the key
		}
		m.roots[node] = root
	}
	return m, nil
}

// root returns a node mapping. The empty string is a valid mapping (the Agent
// root, from a "/" registration), so presence must be decided by the key.
func (m *RootPathMapper) root(node string) (string, bool) {
	if m == nil {
		return "", false
	}
	value, ok := m.roots[node]
	return value, ok
}
func (m *RootPathMapper) RelativePath(ctx context.Context, node, absolute string) (string, error) {
	if m == nil || ctx.Err() != nil || ValidateRemotePath(absolute) != nil {
		return "", ErrPathMappingUnavailable
	}
	prefix, ok := m.root(node)
	if !ok {
		return "", ErrPathMappingUnavailable
	}
	relative := absolute
	if prefix != "" {
		if !strings.HasPrefix(absolute, prefix+"/") {
			return "", ErrPathMappingUnavailable
		}
		relative = strings.TrimPrefix(absolute, prefix+"/")
	} else {
		relative = strings.TrimPrefix(absolute, "/")
	}
	if relative != "" && !validRelativePath(relative) {
		return "", ErrPathMappingUnavailable
	}
	return relative, nil
}
func (m *RootPathMapper) AbsolutePath(ctx context.Context, node, relative string) (string, error) {
	if m == nil || ctx.Err() != nil || !validRelativePath(relative) {
		return "", ErrPathMappingUnavailable
	}
	prefix, ok := m.root(node)
	if !ok {
		return "", ErrPathMappingUnavailable
	}
	if prefix == "" {
		return "/" + relative, nil
	}
	return prefix + "/" + relative, nil
}
