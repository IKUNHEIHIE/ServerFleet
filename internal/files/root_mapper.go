package files

import (
	"context"
	"strings"
)

// RootPathMapper maps explicitly configured logical POSIX roots to Agent roots.
// This does not inspect Controller filesystem paths or infer roots by stripping /.
type RootPathMapper struct{ roots map[string]string }

func NewRootPathMapper(roots map[string]string) (*RootPathMapper, error) {
	if len(roots) == 0 || len(roots) > 1000 {
		return nil, ErrPathMappingUnavailable
	}
	m := &RootPathMapper{roots: map[string]string{}}
	for node, root := range roots {
		if !validChannelID(node) || ValidateDirectoryPath(root) != nil || root == "/" {
			return nil, ErrPathMappingUnavailable
		}
		m.roots[node] = root
	}
	return m, nil
}
func (m *RootPathMapper) RelativePath(ctx context.Context, node, absolute string) (string, error) {
	if m == nil || ctx.Err() != nil || ValidateRemotePath(absolute) != nil {
		return "", ErrPathMappingUnavailable
	}
	root := m.roots[node]
	if root == "" || !strings.HasPrefix(absolute, root+"/") {
		return "", ErrPathMappingUnavailable
	}
	relative := strings.TrimPrefix(absolute, root+"/")
	if !validRelativePath(relative) {
		return "", ErrPathMappingUnavailable
	}
	return relative, nil
}
func (m *RootPathMapper) AbsolutePath(ctx context.Context, node, relative string) (string, error) {
	if m == nil || ctx.Err() != nil || m.roots[node] == "" || !validRelativePath(relative) {
		return "", ErrPathMappingUnavailable
	}
	return m.roots[node] + "/" + relative, nil
}
