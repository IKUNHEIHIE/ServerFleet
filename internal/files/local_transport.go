package files

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// LocalStagingTransport mirrors the controlled staging directory as a node file
// channel: remote POSIX paths land under <rootDir>/<nodeID>/... . It is meant for
// local/offline composition and tests, never as a production Agent replacement.
type LocalStagingTransport struct {
	rootDir string
}

var (
	_ UploadTransport   = (*LocalStagingTransport)(nil)
	_ DownloadTransport = (*LocalStagingTransport)(nil)
)

// NewLocalStagingTransport validates and prepares rootDir. The resolved real
// path is stored so later containment checks are not confused by symlinked
// ancestors of the configured root.
func NewLocalStagingTransport(rootDir string) (*LocalStagingTransport, error) {
	if rootDir == "" || strings.TrimSpace(rootDir) != rootDir {
		return nil, fmt.Errorf("%w: staging root", ErrInvalidPath)
	}
	absolute, err := filepath.Abs(rootDir)
	if err != nil {
		return nil, fmt.Errorf("files: resolve staging root: %w", err)
	}
	if err := os.MkdirAll(absolute, 0o700); err != nil {
		return nil, fmt.Errorf("files: create staging root: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return nil, fmt.Errorf("files: resolve staging root: %w", err)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return nil, fmt.Errorf("files: inspect staging root: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%w: staging root is not a directory", ErrInvalidPath)
	}
	return &LocalStagingTransport{rootDir: resolved}, nil
}

// PushFile atomically writes one staged file after validating the node
// identifier, the remote path, the byte count and the SHA-256 digest. Symlinked
// directories or targets are rejected; the temporary file never escapes the
// validated directory.
func (t *LocalStagingTransport) PushFile(ctx context.Context, nodeID, destinationPath string, content io.Reader, size int64, sha256Hex string) error {
	if content == nil || size < 0 {
		return fmt.Errorf("%w: content", ErrInvalidTransfer)
	}
	if !sha256Pattern.MatchString(sha256Hex) {
		return fmt.Errorf("%w: sha256", ErrInvalidTransfer)
	}
	parts, err := t.nodePathParts(nodeID, destinationPath)
	if err != nil {
		return err
	}
	directory, err := t.ensureDir(ctx, parts[:len(parts)-1])
	if err != nil {
		return err
	}
	target := filepath.Join(directory, parts[len(parts)-1])
	if err := rejectSymlinkTarget(target); err != nil {
		return err
	}
	temp, err := os.CreateTemp(directory, ".sfstage-*")
	if err != nil {
		return fmt.Errorf("files: create staged file: %w", err)
	}
	tempName := temp.Name()
	discard := func() {
		_ = temp.Close()
		_ = os.Remove(tempName)
	}
	hasher := sha256.New()
	written, err := io.Copy(io.MultiWriter(temp, hasher), io.LimitReader(&stagedReader{ctx: ctx, reader: content}, size+1))
	if err != nil {
		discard()
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("files: write staged file: %w", err)
	}
	if written != size {
		discard()
		return fmt.Errorf("%w: staged %d of %d bytes", ErrInvalidTransfer, written, size)
	}
	digest := hex.EncodeToString(hasher.Sum(nil))
	if digest != sha256Hex {
		discard()
		return fmt.Errorf("%w: staged digest", ErrChecksumMismatch)
	}
	if err := temp.Sync(); err != nil {
		discard()
		return fmt.Errorf("files: sync staged file: %w", err)
	}
	if err := temp.Close(); err != nil {
		_ = os.Remove(tempName)
		return fmt.Errorf("files: close staged file: %w", err)
	}
	if err := os.Rename(tempName, target); err != nil {
		_ = os.Remove(tempName)
		return fmt.Errorf("files: publish staged file: %w", err)
	}
	return nil
}

// PullFile streams one staged file into sink after validating every path
// component. Symbolic links and non-regular files are rejected.
func (t *LocalStagingTransport) PullFile(ctx context.Context, nodeID, remotePath string, sink io.Writer) (int64, error) {
	if sink == nil {
		return 0, fmt.Errorf("%w: sink", ErrInvalidTransfer)
	}
	parts, err := t.nodePathParts(nodeID, remotePath)
	if err != nil {
		return 0, err
	}
	current := t.rootDir
	for index, part := range parts {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		next := filepath.Join(current, part)
		info, err := os.Lstat(next)
		if err != nil {
			return 0, fmt.Errorf("files: staged file is unavailable: %w", err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return 0, fmt.Errorf("%w: staging path is a symlink", ErrSymlinkForbidden)
		}
		if index == len(parts)-1 {
			if !info.Mode().IsRegular() {
				return 0, fmt.Errorf("%w: staged file is not a regular file", ErrInvalidPath)
			}
		} else if !info.IsDir() {
			return 0, fmt.Errorf("%w: staging path is not a directory", ErrInvalidPath)
		}
		current = next
	}
	file, err := os.Open(current)
	if err != nil {
		return 0, fmt.Errorf("files: open staged file: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return 0, fmt.Errorf("files: inspect staged file: %w", err)
	}
	if !info.Mode().IsRegular() {
		return 0, fmt.Errorf("%w: staged file is not a regular file", ErrInvalidPath)
	}
	written, err := io.Copy(sink, &stagedReader{ctx: ctx, reader: file})
	if err != nil {
		if ctx.Err() != nil {
			return written, ctx.Err()
		}
		return written, fmt.Errorf("files: read staged file: %w", err)
	}
	return written, nil
}

func (t *LocalStagingTransport) nodePathParts(nodeID, remotePath string) ([]string, error) {
	if !validToken(nodeID, 128) || nodeID == "." || nodeID == ".." || strings.Contains(nodeID, ":") ||
		(runtime.GOOS == "windows" && windowsReservedName(nodeID)) {
		return nil, fmt.Errorf("%w: node", ErrInvalidTransfer)
	}
	if err := ValidateRemotePath(remotePath); err != nil {
		return nil, err
	}
	if remotePath == "/" {
		return nil, fmt.Errorf("%w: remote path must be a file", ErrInvalidTransfer)
	}
	segments := strings.Split(strings.TrimPrefix(remotePath, "/"), "/")
	parts := make([]string, 0, len(segments)+1)
	parts = append(parts, nodeID)
	for _, segment := range segments {
		if segment == "" || segment == "." || segment == ".." || strings.ContainsAny(segment, `/\:`) {
			return nil, ErrInvalidPath
		}
		if runtime.GOOS == "windows" && windowsReservedName(segment) {
			return nil, fmt.Errorf("%w: reserved file name", ErrInvalidPath)
		}
		parts = append(parts, segment)
	}
	return parts, nil
}

// ensureDir creates missing components one at a time and refuses to traverse an
// existing symbolic link, unlike os.MkdirAll which would follow it.
func (t *LocalStagingTransport) ensureDir(ctx context.Context, segments []string) (string, error) {
	current := t.rootDir
	for _, segment := range segments {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		next := filepath.Join(current, segment)
		info, err := os.Lstat(next)
		if errors.Is(err, fs.ErrNotExist) {
			if mkErr := os.Mkdir(next, 0o700); mkErr != nil && !errors.Is(mkErr, fs.ErrExist) {
				return "", fmt.Errorf("files: create staging directory: %w", mkErr)
			}
			info, err = os.Lstat(next)
		}
		if err != nil {
			return "", fmt.Errorf("files: inspect staging directory: %w", err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("%w: staging directory is a symlink", ErrSymlinkForbidden)
		}
		if !info.IsDir() {
			return "", fmt.Errorf("%w: staging path is not a directory", ErrInvalidPath)
		}
		current = next
	}
	return current, nil
}

func rejectSymlinkTarget(target string) error {
	info, err := os.Lstat(target)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("files: inspect staging target: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%w: staging target is a symlink", ErrSymlinkForbidden)
	}
	if info.IsDir() {
		return fmt.Errorf("%w: staging target is a directory", ErrInvalidPath)
	}
	return nil
}

func windowsReservedName(name string) bool {
	base := name
	if index := strings.IndexByte(base, '.'); index >= 0 {
		base = base[:index]
	}
	base = strings.ToUpper(strings.TrimRight(base, " "))
	switch base {
	case "CON", "PRN", "AUX", "NUL":
		return true
	}
	if len(base) == 4 && (strings.HasPrefix(base, "COM") || strings.HasPrefix(base, "LPT")) {
		return base[3] >= '1' && base[3] <= '9'
	}
	return false
}

type stagedReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r *stagedReader) Read(buffer []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(buffer)
}
