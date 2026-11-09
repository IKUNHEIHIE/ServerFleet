package files

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sync"
)

const ActionResume = "files.resume"

type ExecutionCacheManifest struct {
	Version                                                          int
	ExecutionID, TaskID, PlanSHA256, SubjectID, NodeID, SourceNodeID string
	RelativePath, Destination, ConflictPolicy, SHA256                string
	Size                                                             uint64
}

var executionCacheMu sync.Mutex

func ExecutionCacheDigest(meta ExecutionCacheManifest) string {
	raw, _ := json.Marshal(meta)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
func cacheRoot(directory string) (*os.Root, error) {
	if CheckExecutionSpool(directory) != nil {
		return nil, ErrSpoolInsecure
	}
	parent, err := os.OpenRoot(directory)
	if err != nil {
		return nil, err
	}
	defer parent.Close()
	if err = parent.Mkdir("resume-cache", 0700); err != nil && !os.IsExist(err) {
		return nil, err
	}
	path := filepath.Join(directory, "resume-cache")
	if CheckExecutionSpool(path) != nil {
		return nil, ErrSpoolInsecure
	}
	return os.OpenRoot(path)
}
func CreateExecutionCache(ctx context.Context, directory string, meta ExecutionCacheManifest, source *os.File) error {
	if meta.Version != 1 || !validChannelID(meta.ExecutionID) || !validChannelID(meta.TaskID) || !validChannelID(meta.SubjectID) || !validChannelID(meta.NodeID) || !sha256Pattern.MatchString(meta.PlanSHA256) || !sha256Pattern.MatchString(meta.SHA256) || meta.Size > DefaultChannelFileBytes || source == nil {
		return ErrTransferIdentity
	}
	executionCacheMu.Lock()
	defer executionCacheMu.Unlock()
	root, err := cacheRoot(directory)
	if err != nil {
		return err
	}
	defer root.Close()
	dir, err := root.Open(".")
	if err != nil {
		return err
	}
	entries, err := dir.ReadDir(129)
	dir.Close()
	if err != nil && err != io.EOF || len(entries) >= 128 {
		return ErrSpoolUnavailable
	}
	var used int64
	for _, entry := range entries {
		if !entry.IsDir() {
			return ErrSpoolInsecure
		}
		info, e := root.Lstat(entry.Name() + "/source")
		if e == nil {
			if !info.Mode().IsRegular() {
				return ErrSpoolInsecure
			}
			used += info.Size()
		} else if !os.IsNotExist(e) {
			return e
		}
	}
	if used+int64(meta.Size) > 512<<20 || ctx.Err() != nil {
		return ErrSpoolUnavailable
	}
	if root.Mkdir(meta.ExecutionID, 0700) != nil {
		return ErrExecutionInProgress
	}
	file, err := root.OpenFile(meta.ExecutionID+"/source", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	h := sha256.New()
	reader := io.NewSectionReader(source, 0, int64(meta.Size)+1)
	var total int64
	buffer := make([]byte, 32<<10)
	defer clear(buffer)
	for {
		if ctx.Err() != nil {
			file.Close()
			return ctx.Err()
		}
		n, e := reader.Read(buffer)
		if n > 0 {
			total += int64(n)
			if total > int64(meta.Size) {
				file.Close()
				return ErrTransferIdentity
			}
			h.Write(buffer[:n])
			if written, e := file.Write(buffer[:n]); e != nil || written != n {
				file.Close()
				return ErrSpoolUnavailable
			}
		}
		if e == io.EOF {
			break
		}
		if e != nil || n == 0 {
			file.Close()
			return ErrSpoolUnavailable
		}
	}
	syncErr, closeErr := file.Sync(), file.Close()
	if syncErr != nil || closeErr != nil || total != int64(meta.Size) || hex.EncodeToString(h.Sum(nil)) != meta.SHA256 {
		return ErrAgentChecksum
	}
	raw, _ := json.Marshal(meta)
	manifest, err := root.OpenFile(meta.ExecutionID+"/manifest.json", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	_, err = manifest.Write(raw)
	syncErr, closeErr = manifest.Sync(), manifest.Close()
	if err != nil || syncErr != nil || closeErr != nil {
		return ErrSpoolUnavailable
	}
	for _, name := range []string{meta.ExecutionID, "."} {
		dir, e := root.Open(name)
		if e != nil {
			return e
		}
		e = dir.Sync()
		dir.Close()
		if e != nil {
			return e
		}
	}
	return nil
}
func OpenExecutionCache(ctx context.Context, directory, id string) (ExecutionCacheManifest, *os.File, error) {
	var meta ExecutionCacheManifest
	if !validChannelID(id) || ctx.Err() != nil {
		return meta, nil, ErrTransferIdentity
	}
	root, err := existingCacheRoot(directory)
	if err != nil {
		return meta, nil, err
	}
	defer root.Close()
	infoManifest, e := root.Lstat(id + "/manifest.json")
	if e != nil || !infoManifest.Mode().IsRegular() || infoManifest.Size() > 8192 {
		return meta, nil, ErrSpoolInsecure
	}
	manifest, err := root.Open(id + "/manifest.json")
	if err != nil {
		return meta, nil, err
	}
	raw, err := io.ReadAll(io.LimitReader(manifest, 8193))
	manifest.Close()
	if err != nil || len(raw) > 8192 || json.Unmarshal(raw, &meta) != nil || meta.Version != 1 || meta.ExecutionID != id || meta.Size > DefaultChannelFileBytes || !sha256Pattern.MatchString(meta.SHA256) {
		return meta, nil, ErrTransferIdentity
	}
	info, err := root.Lstat(id + "/source")
	if err != nil || !info.Mode().IsRegular() || uint64(info.Size()) != meta.Size {
		return meta, nil, ErrSpoolInsecure
	}
	file, err := root.Open(id + "/source")
	if err != nil {
		return meta, nil, err
	}
	opened, e := file.Stat()
	if e != nil || !os.SameFile(info, opened) {
		file.Close()
		return meta, nil, ErrSpoolInsecure
	}
	h := sha256.New()
	buffer := make([]byte, 32<<10)
	defer clear(buffer)
	for {
		if ctx.Err() != nil {
			file.Close()
			return meta, nil, ctx.Err()
		}
		n, e := file.Read(buffer)
		if n > 0 {
			h.Write(buffer[:n])
		}
		if e == io.EOF {
			break
		}
		if e != nil || n == 0 {
			file.Close()
			return meta, nil, ErrSpoolUnavailable
		}
	}
	if hex.EncodeToString(h.Sum(nil)) != meta.SHA256 {
		file.Close()
		return meta, nil, ErrAgentChecksum
	}
	if _, err = file.Seek(0, io.SeekStart); err != nil {
		file.Close()
		return meta, nil, err
	}
	return meta, file, nil
}

func ReadExecutionCacheManifest(directory, id string) (ExecutionCacheManifest, error) {
	var meta ExecutionCacheManifest
	if !validChannelID(id) {
		return meta, ErrTransferIdentity
	}
	root, err := existingCacheRoot(directory)
	if err != nil {
		return meta, err
	}
	defer root.Close()
	info, err := root.Lstat(id + "/manifest.json")
	if err != nil || !info.Mode().IsRegular() || info.Size() > 8192 {
		return meta, ErrSpoolInsecure
	}
	raw, err := root.ReadFile(id + "/manifest.json")
	if err != nil || json.Unmarshal(raw, &meta) != nil || meta.Version != 1 || meta.ExecutionID != id || meta.Size > DefaultChannelFileBytes || !sha256Pattern.MatchString(meta.SHA256) {
		return meta, ErrTransferIdentity
	}
	source, err := root.Lstat(id + "/source")
	if err != nil || !source.Mode().IsRegular() || source.Size() != int64(meta.Size) {
		return meta, ErrSpoolInsecure
	}
	return meta, nil
}
func ReleaseExecutionCache(directory, id, task, plan string) error {
	executionCacheMu.Lock()
	defer executionCacheMu.Unlock()
	meta, err := ReadExecutionCacheManifest(directory, id)
	if err != nil || meta.TaskID != task || meta.PlanSHA256 != plan {
		return ErrTransferIdentity
	}
	root, err := existingCacheRoot(directory)
	if err != nil {
		return err
	}
	defer root.Close()
	if err = root.RemoveAll(id); err != nil {
		return err
	}
	dir, err := root.Open(".")
	if err != nil {
		return err
	}
	if e := dir.Sync(); e != nil {
		dir.Close()
		return e
	}
	dir.Close()
	// Only remove an empty container after proven success. Other active/UNKNOWN
	// caches make rmdir fail and are retained; never recursively delete the root.
	_ = os.Remove(filepath.Join(directory, "resume-cache"))
	return nil
}

// Read/cleanup paths never create a new cache directory. In particular a second
// cleanup or an unavailable resume candidate must not recreate empty residue.
func existingCacheRoot(directory string) (*os.Root, error) {
	if !filepath.IsAbs(directory) {
		return nil, ErrSpoolInsecure
	}
	path := filepath.Join(directory, "resume-cache")
	for _, p := range []string{directory, path} {
		i, e := os.Lstat(p)
		if e != nil {
			return nil, e
		}
		if !i.IsDir() || i.Mode()&os.ModeSymlink != 0 || !spoolOwned(i) || runtime.GOOS != "windows" && i.Mode().Perm()&0077 != 0 {
			return nil, ErrSpoolInsecure
		}
	}
	before, e := os.Lstat(path)
	if e != nil {
		return nil, e
	}
	r, e := os.OpenRoot(path)
	if e != nil {
		return nil, e
	}
	after, e := r.Stat(".")
	if e != nil || !os.SameFile(before, after) {
		r.Close()
		return nil, ErrSpoolInsecure
	}
	return r, nil
}
