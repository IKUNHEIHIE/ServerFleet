package main

import (
	"os"
	"strings"

	"golang.org/x/sys/unix"
)

// Every component is opened against an anchored directory fd, with NOFOLLOW.
// A path swap after validation cannot redirect I/O to a sibling via a symlink.
func agentFileParent(root *os.Root, path string) (*os.File, string, error) {
	parts := strings.Split(path, "/")
	parent, err := root.Open(".")
	if err != nil {
		return nil, "", err
	}
	for _, part := range parts[:len(parts)-1] {
		fd, e := unix.Openat(int(parent.Fd()), part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		parent.Close()
		if e != nil {
			return nil, "", e
		}
		parent = os.NewFile(uintptr(fd), "agent-private-directory")
	}
	return parent, parts[len(parts)-1], nil
}
func agentFileOpen(root *os.Root, path string, flags int, mode os.FileMode) (*os.File, error) {
	parent, name, err := agentFileParent(root, path)
	if err != nil {
		return nil, err
	}
	defer parent.Close()
	fd, err := unix.Openat(int(parent.Fd()), name, flags|unix.O_NOFOLLOW|unix.O_CLOEXEC|unix.O_NONBLOCK, uint32(mode.Perm()))
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), "agent-private-file"), nil
}
func agentFilePublish(root *os.Root, source, destination string) error {
	src, sname, err := agentFileParent(root, source)
	if err != nil {
		return err
	}
	defer src.Close()
	dst, dname, err := agentFileParent(root, destination)
	if err != nil {
		return err
	}
	defer dst.Close()
	if err = unix.Linkat(int(src.Fd()), sname, int(dst.Fd()), dname, 0); err != nil {
		return err
	}
	return dst.Sync()
}
