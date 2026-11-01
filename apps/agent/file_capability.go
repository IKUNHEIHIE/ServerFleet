package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

var errFileCapability = errors.New("agent: file capability rejected or outcome unknown")

const agentFileChunkBytes = 32 << 10

// Candidate byte-channel port, NOT task output. Controller freezes this complete
// binding in its sole task plan; source-read and destination-write have distinct
// gates. A browser cannot register a gate or choose the local root directory.
type agentFileBinding struct {
	TaskID, ExecutionID, TransferID, NodeID, RelativePath, SHA256, Direction string
	Size                                                                     int64
}
type agentFileGate func(context.Context, agentFileBinding, string) error
type agentFileCapability struct {
	mu   sync.Mutex
	root *os.Root
	gate agentFileGate
}

func openAgentFileCapability(directory string, gate agentFileGate) (*agentFileCapability, error) {
	if runtime.GOOS != "linux" || !filepath.IsAbs(directory) || gate == nil {
		return nil, errFileCapability
	}
	info, err := os.Lstat(directory)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || runtime.GOOS == "linux" && (os.Geteuid() == 0 || info.Mode().Perm()&0077 != 0) {
		return nil, errFileCapability
	}
	r, err := os.OpenRoot(directory)
	if err != nil {
		return nil, errFileCapability
	}
	return &agentFileCapability{root: r, gate: gate}, nil
}
func (f *agentFileCapability) Close() error { return f.root.Close() }
func validAgentFileBinding(b agentFileBinding, direction string) bool {
	h, err := hex.DecodeString(b.SHA256)
	return executionIDPattern.MatchString(b.TaskID) && executionIDPattern.MatchString(b.ExecutionID) && executionIDPattern.MatchString(b.TransferID) && executionIDPattern.MatchString(b.NodeID) && b.Direction == direction && fs.ValidPath(b.RelativePath) && b.RelativePath != "." && !strings.Contains(b.RelativePath, "\\") && !strings.Contains(b.RelativePath, ":") && !strings.HasPrefix(b.RelativePath, ".sf-") && len(b.RelativePath) <= 1024 && b.Size >= 0 && b.Size <= 64<<20 && err == nil && len(h) == 32 && hex.EncodeToString(h) == b.SHA256
}
func (f *agentFileCapability) check(ctx context.Context, b agentFileBinding, phase string) (err error) {
	defer func() {
		if recover() != nil {
			err = errFileCapability
		}
	}()
	if ctx == nil || ctx.Err() != nil || f.gate(ctx, b, phase) != nil || ctx.Err() != nil {
		return errFileCapability
	}
	return nil
}
func (f *agentFileCapability) safePath(name string, missingFinal bool) error {
	parts := strings.Split(name, "/")
	for i := range parts {
		info, err := f.root.Lstat(strings.Join(parts[:i+1], "/"))
		if missingFinal && i == len(parts)-1 && os.IsNotExist(err) {
			return nil
		}
		if err != nil || info.Mode()&os.ModeSymlink != 0 || i < len(parts)-1 && !info.IsDir() {
			return errFileCapability
		}
	}
	return nil
}
func hashAgentFile(file *os.File, size int64, digest string) error {
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() != size {
		return errFileCapability
	}
	h := sha256.New()
	n, err := io.Copy(h, io.LimitReader(io.NewSectionReader(file, 0, size+1), size+1))
	if err != nil || n != size || hex.EncodeToString(h.Sum(nil)) != digest {
		return errFileCapability
	}
	return nil
}
func (f *agentFileCapability) Read(ctx context.Context, b agentFileBinding, offset int64, length int) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !validAgentFileBinding(b, "read") || offset < 0 || offset > b.Size || length <= 0 || length > agentFileChunkBytes || int64(length) > b.Size-offset || f.check(ctx, b, "read") != nil || f.safePath(b.RelativePath, false) != nil {
		return nil, errFileCapability
	}
	file, err := f.root.Open(b.RelativePath)
	if err != nil {
		return nil, errFileCapability
	}
	defer file.Close()
	if hashAgentFile(file, b.Size, b.SHA256) != nil {
		return nil, errFileCapability
	}
	chunk := make([]byte, length)
	n, err := file.ReadAt(chunk, offset)
	if err != nil || n != length || f.check(ctx, b, "read-complete") != nil {
		clear(chunk)
		return nil, errFileCapability
	}
	return chunk, nil
}

// Write never overwrites an existing destination. Partial data survives a
// disconnect in a private transfer directory. Resumption requires the SAME
// binding and current gate; a different offset or plan is rejected. There are
// no bytes in task logs, execution ledger, audit metadata or errors.
func (f *agentFileCapability) Write(ctx context.Context, b agentFileBinding, offset int64, chunk []byte, finish bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !validAgentFileBinding(b, "write") || offset < 0 || offset > b.Size || len(chunk) > agentFileChunkBytes || int64(len(chunk)) > b.Size-offset || len(chunk) == 0 && !finish || f.check(ctx, b, "write") != nil || f.safePath(b.RelativePath, true) != nil {
		return errFileCapability
	}
	dir := ".sf-" + b.TransferID
	err := f.root.Mkdir(dir, 0700)
	if err != nil && !os.IsExist(err) {
		return errFileCapability
	}
	encoded, _ := json.Marshal(b)
	if err == nil {
		file, e := f.root.OpenFile(dir+"/binding.json", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if e != nil {
			return errFileCapability
		}
		_, e = file.Write(encoded)
		if e == nil {
			e = file.Sync()
		}
		closed := file.Close()
		if e != nil || closed != nil {
			return errFileCapability
		}
		if syncAgentFileDirectory(f.root, dir) != nil || syncAgentFileDirectory(f.root, ".") != nil {
			return errFileCapability
		}
	} else {
		prior, e := f.root.ReadFile(dir + "/binding.json")
		if e != nil || string(prior) != string(encoded) {
			return errFileCapability
		}
	}
	if _, e := f.root.Lstat(dir + "/complete"); e == nil || !os.IsNotExist(e) {
		return errFileCapability
	}
	file, err := f.root.OpenFile(dir+"/partial", os.O_RDWR|os.O_CREATE, 0600)
	if err != nil {
		return errFileCapability
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() != offset {
		return errFileCapability
	}
	if _, err = file.WriteAt(chunk, offset); err != nil || file.Sync() != nil {
		return errFileCapability
	}
	if f.check(ctx, b, "write-complete") != nil {
		return errFileCapability
	}
	if !finish {
		return nil
	}
	if offset+int64(len(chunk)) != b.Size || hashAgentFile(file, b.Size, b.SHA256) != nil || f.check(ctx, b, "commit") != nil {
		return errFileCapability
	}
	// No automatic retry after this point: a lost acknowledgement is UNKNOWN.
	if f.root.Link(dir+"/partial", b.RelativePath) != nil {
		return errFileCapability
	}
	marker, err := f.root.OpenFile(dir+"/complete", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return errFileCapability
	}
	err = marker.Sync()
	closed := marker.Close()
	if err != nil || closed != nil {
		return errFileCapability
	}
	d, err := f.root.Open(filepath.ToSlash(filepath.Dir(b.RelativePath)))
	if err != nil {
		return errFileCapability
	}
	defer d.Close()
	if d.Sync() != nil || syncAgentFileDirectory(f.root, dir) != nil || f.check(ctx, b, "committed") != nil {
		return errFileCapability
	}
	return nil
}

func syncAgentFileDirectory(root *os.Root, name string) error {
	d, err := root.Open(name)
	if err != nil {
		return errFileCapability
	}
	defer d.Close()
	if d.Sync() != nil {
		return errFileCapability
	}
	return nil
}
