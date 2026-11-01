package files

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
)

var ErrSpoolInsecure = errors.New("files: transfer spool directory is not private")

// secureSpoolDirectory creates the spool root if needed and rejects a symlink or
// a directory readable by other users. The spool holds actual file bytes, so it
// must not be shared.
func secureSpoolDirectory(dir string) error {
	if dir == "" || !filepath.IsAbs(dir) {
		return ErrSpoolUnavailable
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return ErrSpoolUnavailable
	}
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return ErrSpoolInsecure
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
		return ErrSpoolInsecure
	}
	return nil
}

// cappedWriter fails as soon as the source stream exceeds the configured file
// ceiling instead of filling the controller disk.
type cappedWriter struct {
	writer    io.Writer
	remaining int64
}

func (w *cappedWriter) Write(data []byte) (int, error) {
	if w == nil || w.writer == nil {
		return 0, ErrSpoolUnavailable
	}
	if int64(len(data)) > w.remaining {
		if w.remaining > 0 {
			if _, err := w.writer.Write(data[:w.remaining]); err != nil {
				w.remaining = 0
				return 0, err
			}
		}
		w.remaining = 0
		return 0, ErrFileTooLarge
	}
	written, err := w.writer.Write(data)
	w.remaining -= int64(written)
	return written, err
}
