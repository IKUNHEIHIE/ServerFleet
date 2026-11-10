//go:build linux

package bootstrap

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"syscall"
)

func provisionOwnerSupported() bool { return os.Geteuid() != 0 }
func openProvisionPrivateDirectory(directory string) (*os.Root, error) {
	if !filepath.IsAbs(directory) || filepath.Clean(directory) != directory {
		return nil, errProvision
	}
	info, e := os.Lstat(directory)
	if e != nil || !info.IsDir() || info.Mode().Perm() != 0700 {
		return nil, errProvision
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok || st.Uid != uint32(os.Geteuid()) {
		return nil, errProvision
	}
	root, e := os.OpenRoot(directory)
	if e != nil {
		return nil, e
	}
	after, e := root.Stat(".")
	if e != nil || !os.SameFile(info, after) {
		root.Close()
		return nil, errProvision
	}
	return root, nil
}
func newProvisionDirectory(directory string) (*os.Root, error) {
	if !filepath.IsAbs(directory) || filepath.Clean(directory) != directory || directory == "/" {
		return nil, errProvision
	}
	parent, e := openProvisionPrivateDirectory(filepath.Dir(directory))
	if e != nil {
		return nil, e
	}
	defer parent.Close()
	if e = parent.Mkdir(filepath.Base(directory), 0700); e != nil {
		return nil, e
	}
	root, e := parent.OpenRoot(filepath.Base(directory))
	if e != nil {
		return nil, e
	}
	if e = syncProvisionDirectory(parent, "."); e != nil {
		root.Close()
		return nil, e
	}
	return root, nil
}
func provisionExistingDatabase(filename string) bool {
	root, e := openProvisionPrivateDirectory(filepath.Dir(filename))
	if e != nil {
		return false
	}
	defer root.Close()
	info, e := root.Lstat(filepath.Base(filename))
	if e != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 {
		return false
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	return ok && st.Uid == uint32(os.Geteuid()) && st.Nlink == 1
}
func openProvisionProgram(filename string) (*os.File, error) {
	if !filepath.IsAbs(filename) || filepath.Clean(filename) != filename {
		return nil, errProvision
	}
	before, e := os.Lstat(filename)
	if e != nil || !before.Mode().IsRegular() || before.Mode().Perm()&0022 != 0 || before.Mode().Perm()&0100 == 0 || before.Size() < 1 || before.Size() > 256<<20 {
		return nil, errProvision
	}
	st, ok := before.Sys().(*syscall.Stat_t)
	if !ok || st.Nlink != 1 || st.Uid != 0 && st.Uid != uint32(os.Geteuid()) {
		return nil, errProvision
	}
	f, e := os.Open(filename)
	if e != nil {
		return nil, e
	}
	after, e := f.Stat()
	if e != nil || !os.SameFile(before, after) {
		f.Close()
		return nil, errProvision
	}
	return f, nil
}
func provisionWrite(root *os.Root, name string, raw []byte, mode os.FileMode) error {
	f, e := root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if e != nil {
		return e
	}
	n, e := f.Write(raw)
	se, ce := f.Sync(), f.Close()
	if e != nil || n != len(raw) || se != nil || ce != nil {
		return errProvision
	}
	return syncProvisionDirectory(root, filepath.Dir(name))
}
func copyProvisionProgram(root *os.Root, name string, source *os.File, digest string, size int64) error {
	if _, e := source.Seek(0, io.SeekStart); e != nil {
		return e
	}
	f, e := root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0700)
	if e != nil {
		return e
	}
	h := sha256.New()
	n, e := io.Copy(io.MultiWriter(f, h), io.LimitReader(source, (256<<20)+1))
	se, ce := f.Sync(), f.Close()
	if e != nil || se != nil || ce != nil || n < 1 || n > 256<<20 || digest != "" && (n != size || hex.EncodeToString(h.Sum(nil)) != digest) {
		return errProvision
	}
	return syncProvisionDirectory(root, filepath.Dir(name))
}
func syncProvisionDirectory(root *os.Root, name string) error {
	d, e := root.Open(name)
	if e != nil {
		return e
	}
	defer d.Close()
	return d.Sync()
}
