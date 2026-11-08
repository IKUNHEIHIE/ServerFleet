package files

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
)

// openStagingAppend is called only while an open upload/distribution is locked
// and currently authorized. The database offset is the sole acknowledged byte
// boundary. Bytes beyond it can be an interrupted, never-acknowledged staging
// write; they are not an Agent execution and must not strand a resumed client.
func (s *Service) openStagingAppend(name string, offset int64) (*os.File, error) {
	if _, err := s.stagingPath(name); err != nil {
		return nil, err
	}
	dir, err := os.Lstat(s.stagingDir)
	if err != nil || !dir.IsDir() || dir.Mode()&os.ModeSymlink != 0 {
		return nil, ErrSpoolInsecure
	}
	root, err := os.OpenRoot(s.stagingDir)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	anchored, err := root.Stat(".")
	if err != nil || !os.SameFile(dir, anchored) {
		return nil, ErrSpoolInsecure
	}
	before, err := root.Lstat(filepath.Base(name))
	if err != nil || !before.Mode().IsRegular() {
		return nil, ErrSpoolInsecure
	}
	file, err := root.OpenFile(name, os.O_RDWR, 0)
	if err != nil {
		return nil, err
	}
	fail := func(err error) (*os.File, error) { _ = file.Close(); return nil, err }
	info, err := file.Stat()
	if err != nil {
		return fail(err)
	}
	if !os.SameFile(before, info) || !info.Mode().IsRegular() ||
		(runtime.GOOS == "linux" && !deploymentPrivate(info, false)) {
		return fail(ErrSpoolInsecure)
	}
	if offset < 0 || info.Size() < offset {
		return fail(fmt.Errorf("files: confirmed staging bytes are missing"))
	}
	if info.Size() > offset {
		// Automatic tail recovery requires the Linux ownership/link/private
		// checks above. Other platforms keep the prior fail-closed behavior.
		if runtime.GOOS != "linux" || !deploymentPrivate(anchored, true) {
			return fail(ErrSpoolInsecure)
		}
		if err := file.Truncate(offset); err != nil {
			return fail(err)
		}
		if err := file.Sync(); err != nil {
			return fail(err)
		}
	}
	if _, err := file.Seek(offset, io.SeekStart); err != nil {
		return fail(err)
	}
	return file, nil
}

// Hash the very same checked file descriptor after repairing an uncommitted
// tail; never accept fewer bytes than were acknowledged in the database.
func (s *Service) stagingDigest(name string, confirmed int64) (string, error) {
	file, err := s.openStagingAppend(name, confirmed)
	if err != nil {
		return "", err
	}
	defer file.Close()
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return "", err
	}
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}
