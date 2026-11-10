//go:build linux

package runtime

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

func readEnrollmentTokenFile(filename string) (string, error) {
	if !filepath.IsAbs(filename) || filepath.Clean(filename) != filename {
		return "", errRuntime
	}
	info, e := os.Lstat(filepath.Dir(filename))
	if e != nil || !info.IsDir() || info.Mode().Perm() != 0700 {
		return "", errRuntime
	}
	owner, ok := info.Sys().(*syscall.Stat_t)
	if !ok || owner.Uid != uint32(os.Geteuid()) {
		return "", errRuntime
	}
	root, e := os.OpenRoot(filepath.Dir(filename))
	if e != nil {
		return "", errRuntime
	}
	defer root.Close()
	opened, e := root.Stat(".")
	if e != nil || !os.SameFile(info, opened) {
		return "", errRuntime
	}
	info, e = root.Lstat(filepath.Base(filename))
	if e != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || info.Size() < 1 || info.Size() > 4096 {
		return "", errRuntime
	}
	owner, ok = info.Sys().(*syscall.Stat_t)
	if !ok || owner.Uid != uint32(os.Geteuid()) || owner.Nlink != 1 {
		return "", errRuntime
	}
	f, e := root.Open(filepath.Base(filename))
	if e != nil {
		return "", errRuntime
	}
	defer f.Close()
	opened, e = f.Stat()
	if e != nil || !os.SameFile(info, opened) {
		return "", errRuntime
	}
	raw, e := io.ReadAll(io.LimitReader(f, 4097))
	defer clear(raw)
	if e != nil || int64(len(raw)) != info.Size() {
		return "", errRuntime
	}
	token := strings.TrimSpace(string(raw))
	if len(token) < 16 || len(token) > 1024 {
		return "", errRuntime
	}
	for _, c := range token {
		if c < 0x21 || c > 0x7e {
			return "", errRuntime
		}
	}
	return token, nil
}
