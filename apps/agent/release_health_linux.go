package main

import (
	"os"
	"syscall"
)

func openAgentHealthDirectory(directory string) (*os.Root, error) {
	i, e := os.Lstat(directory)
	if e != nil || !i.IsDir() || i.Mode().Perm() != 0700 || i.Mode()&os.ModeSymlink != 0 {
		return nil, errAgentConnect
	}
	s, ok := i.Sys().(*syscall.Stat_t)
	if !ok || s.Uid != uint32(os.Geteuid()) || os.Geteuid() == 0 {
		return nil, errAgentConnect
	}
	r, e := os.OpenRoot(directory)
	if e != nil {
		return nil, e
	}
	opened, e := r.Stat(".")
	if e != nil || !os.SameFile(i, opened) {
		r.Close()
		return nil, errAgentConnect
	}
	return r, nil
}
