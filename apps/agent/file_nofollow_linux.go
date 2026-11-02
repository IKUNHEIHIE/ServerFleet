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
func agentFilePublishNoReplace(root *os.Root, source, destination string) error {
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

// agentFilePublishReplace atomically swaps the verified partial file over an
// existing destination. Renameat never follows a symlink at either endpoint, so
// a symlink planted at the destination would be replaced as an entry, never
// followed. The caller re-opens the result with O_NOFOLLOW and verifies identity.
func agentFilePublishReplace(root *os.Root, source, destination string) error {
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
	if err = unix.Renameat(int(src.Fd()), sname, int(dst.Fd()), dname); err != nil {
		return err
	}
	return dst.Sync()
}

// agentFileDestination classifies one existing entry without following links.
func agentFileDestination(root *os.Root, path string) (agentFileKind, error) {
	parent, name, err := agentFileParent(root, path)
	if err != nil {
		return agentDestinationOther, err
	}
	defer parent.Close()
	var st unix.Stat_t
	if err := unix.Fstatat(int(parent.Fd()), name, &st, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		if err == unix.ENOENT {
			return agentDestinationMissing, nil
		}
		return agentDestinationOther, err
	}
	if st.Mode&unix.S_IFMT == unix.S_IFREG {
		return agentDestinationRegular, nil
	}
	return agentDestinationOther, nil
}

func agentFileSpace(root *os.Root, bytes int64) error {
	if bytes < 0 || bytes > 64<<20 {
		return errFileCapability
	}
	dir, err := root.Open(".")
	if err != nil {
		return errFileCapability
	}
	defer dir.Close()
	var stat unix.Statfs_t
	if unix.Fstatfs(int(dir.Fd()), &stat) != nil || stat.Bsize <= 0 {
		return errFileCapability
	}
	needed := (uint64(bytes) + (1 << 20) + uint64(stat.Bsize) - 1) / uint64(stat.Bsize)
	if uint64(stat.Bavail) < needed || (stat.Files > 0 && stat.Ffree == 0) {
		return errFileCapability
	}
	return nil
}
