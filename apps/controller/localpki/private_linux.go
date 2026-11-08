//go:build linux

package localpki

import (
	"os"
	"syscall"
)

func privatePKIInfo(info os.FileInfo, directory bool) bool {
	s, ok := info.Sys().(*syscall.Stat_t)
	if !ok || s.Uid != uint32(os.Geteuid()) || info.Mode()&os.ModeSymlink != 0 {
		return false
	}
	if directory {
		return info.IsDir() && info.Mode().Perm() == 0700
	}
	return info.Mode().IsRegular() && info.Mode().Perm() == 0600 && s.Nlink == 1
}
