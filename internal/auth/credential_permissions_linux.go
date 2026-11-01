package auth

import (
	"os"
	"syscall"
)

func credentialPrivateOwner(info os.FileInfo, directory bool, _ string) bool {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok || st.Uid != uint32(os.Geteuid()) || info.Mode()&os.ModeSymlink != 0 {
		return false
	}
	if directory {
		return info.IsDir() && info.Mode().Perm() == 0700
	}
	return info.Mode().IsRegular() && info.Mode().Perm() == 0600
}

func makeCredentialDirectory(dir string) error { return os.Mkdir(dir, 0700) }

func publishCredentialFile(root *os.Root, name string, replace bool) error {
	var err error
	if replace {
		err = root.Rename(name, "credential-keys.json")
	} else {
		err = root.Link(name, "credential-keys.json")
	}
	if err != nil {
		return err
	}
	dir, err := root.Open(".")
	if err != nil {
		return ErrIdentityUnavailable
	}
	defer dir.Close()
	if dir.Sync() != nil {
		return ErrIdentityUnavailable
	}
	return nil
}
