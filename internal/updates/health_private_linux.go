//go:build linux

package updates

import (
	"os"
	"syscall"
)

// The readiness report is only authoritative inside a private current-UID
// directory with no group/world access and no root execution.
func healthDirectoryPrivate(dir string) bool {
	i, e := os.Lstat(dir)
	if e != nil || !i.IsDir() || i.Mode().Perm() != 0700 || i.Mode()&os.ModeSymlink != 0 {
		return false
	}
	s, ok := i.Sys().(*syscall.Stat_t)
	return ok && s.Uid == uint32(os.Geteuid()) && os.Geteuid() != 0
}
