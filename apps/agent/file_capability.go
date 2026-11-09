package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
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
	PlanSHA256                                                               string
	TaskID, ExecutionID, TransferID, NodeID, RelativePath, SHA256, Direction string
	ConflictPolicy                                                           string
	Size                                                                     int64
}
type agentFileGate func(context.Context, agentFileBinding, string) error
type agentFileCapability struct {
	writeHashes map[string]hash.Hash
	mu          sync.Mutex
	root        *os.Root
	gate        agentFileGate
	snapshots   map[string]*os.File
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
	return &agentFileCapability{root: r, gate: gate, snapshots: map[string]*os.File{}, writeHashes: map[string]hash.Hash{}}, nil
}
func (f *agentFileCapability) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, file := range f.snapshots {
		file.Close()
	}
	return f.root.Close()
}
func validAgentFileBinding(b agentFileBinding, direction string) bool {
	h, err := hex.DecodeString(b.SHA256)
	return executionIDPattern.MatchString(b.TaskID) && executionIDPattern.MatchString(b.ExecutionID) && executionIDPattern.MatchString(b.TransferID) && executionIDPattern.MatchString(b.NodeID) && b.Direction == direction && validAgentConflictPolicy(b.ConflictPolicy) && fs.ValidPath(b.RelativePath) && b.RelativePath != "." && !strings.Contains(b.RelativePath, "\\") && !strings.Contains(b.RelativePath, ":") && !strings.HasPrefix(b.RelativePath, ".sf-") && len(b.RelativePath) <= 1024 && b.Size >= 0 && b.Size <= 64<<20 && err == nil && len(h) == 32 && hex.EncodeToString(h) == b.SHA256
}

func validAgentConflictPolicy(policy string) bool {
	return policy == "skip" || policy == "overwrite" || policy == "rename"
}

type agentFileKind int

const (
	agentDestinationMissing agentFileKind = iota
	agentDestinationRegular
	agentDestinationOther
)

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

// resolveWriteDestination applies the plan's conflict policy before any bytes
// move. Skip/overwrite/rename may only touch a non-symlink regular file; a
// rename target is chosen once and reused from the private partial binding on
// resume.
func (f *agentFileCapability) resolveWriteDestination(ctx context.Context, b agentFileBinding) (agentFileBinding, bool, error) {
	if b.ConflictPolicy == "rename" {
		if prior, ok := f.priorWritePath(b); ok {
			b.RelativePath = prior
		}
	}
	if f.check(ctx, b, "stat") != nil || f.safePath(b.RelativePath, true) != nil {
		return b, false, errFileCapability
	}
	kind, err := agentFileDestination(f.root, b.RelativePath)
	if err != nil {
		return b, false, errFileCapability
	}
	switch {
	case kind == agentDestinationMissing:
		return b, false, nil
	case kind != agentDestinationRegular:
		return b, false, errFileCapability
	case b.ConflictPolicy == "skip":
		return b, true, nil
	case b.ConflictPolicy == "overwrite":
		return b, false, nil
	}
	dir, name := "", b.RelativePath
	if i := strings.LastIndex(name, "/"); i >= 0 {
		dir, name = name[:i+1], name[i+1:]
	}
	stem, ext := name, ""
	if i := strings.LastIndex(name, "."); i > 0 {
		stem, ext = name[:i], name[i:]
	}
	for index := 1; index <= 32; index++ {
		candidate := fmt.Sprintf("%s%s (%d)%s", dir, stem, index, ext)
		if len(candidate) > 1024 {
			return b, false, errFileCapability
		}
		kind, err := agentFileDestination(f.root, candidate)
		if err != nil {
			return b, false, errFileCapability
		}
		if kind == agentDestinationMissing {
			b.RelativePath = candidate
			return b, false, nil
		}
		if kind != agentDestinationRegular {
			return b, false, errFileCapability
		}
	}
	return b, false, errFileCapability
}

func (f *agentFileCapability) priorWritePath(b agentFileBinding) (string, bool) {
	raw, err := f.root.ReadFile(".sf-" + b.TransferID + "/binding.json")
	if err != nil || len(raw) > 4096 {
		return "", false
	}
	var prior agentFileBinding
	if json.Unmarshal(raw, &prior) != nil || prior.TaskID != b.TaskID || prior.ExecutionID != b.ExecutionID || prior.TransferID != b.TransferID || prior.NodeID != b.NodeID || prior.SHA256 != b.SHA256 || prior.Direction != b.Direction || prior.Size != b.Size || prior.ConflictPolicy != "rename" {
		return "", false
	}
	dir := ""
	if i := strings.LastIndex(b.RelativePath, "/"); i >= 0 {
		dir = b.RelativePath[:i+1]
	}
	if !fs.ValidPath(prior.RelativePath) || !strings.HasPrefix(prior.RelativePath, dir) || strings.HasPrefix(prior.RelativePath, ".sf-") {
		return "", false
	}
	return prior.RelativePath, true
}

func (f *agentFileCapability) Read(ctx context.Context, b agentFileBinding, offset int64, length int) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !validAgentFileBinding(b, "read") || offset < 0 || offset > b.Size || length <= 0 || length > agentFileChunkBytes || int64(length) > b.Size-offset || f.check(ctx, b, "read") != nil || f.safePath(b.RelativePath, false) != nil {
		return nil, errFileCapability
	}
	file, err := agentFileOpen(f.root, b.RelativePath, os.O_RDONLY, 0)
	if snapshot := f.snapshots[b.TransferID]; snapshot != nil {
		if file != nil {
			file.Close()
		}
		file, err = snapshot, nil
	}
	if err != nil {
		return nil, errFileCapability
	}
	if f.snapshots[b.TransferID] == nil {
		defer file.Close()
	}
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

// Write keeps the verified partial file until commit. The admitted conflict
// policy decides publication: skip never replaces, overwrite atomically swaps a
// verified regular file, and rename publishes to a free sibling chosen at open.
// Partial data survives a disconnect in a private transfer directory. Resumption
// requires the SAME binding and current gate; a different offset or plan is
// rejected. There are no bytes in task logs, execution ledger, audit metadata or
// errors.
func (f *agentFileCapability) Write(ctx context.Context, b agentFileBinding, offset int64, chunk []byte, finish bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !validAgentFileBinding(b, "write") || offset < 0 || offset > b.Size || len(chunk) > agentFileChunkBytes || int64(len(chunk)) > b.Size-offset || len(chunk) == 0 && !finish || f.check(ctx, b, "write") != nil || f.safePath(b.RelativePath, true) != nil {
		return errFileCapability
	}
	if agentFileSpace(f.root, int64(len(chunk))+4096) != nil {
		return errFileCapability
	}
	dir := ".sf-" + b.TransferID
	err := f.root.Mkdir(dir, 0700)
	if err != nil && !os.IsExist(err) {
		return errFileCapability
	}
	unlock, lockErr := agentResumeLock(f.root, dir)
	if lockErr != nil {
		return lockErr
	}
	defer unlock()
	for _, marker := range []string{"adopted.json", "commit-intent.json"} {
		if _, e := f.root.Lstat(dir + "/" + marker); !os.IsNotExist(e) {
			return errFileCapability
		}
	}
	encoded, _ := json.Marshal(b)
	if err == nil {
		file, e := agentFileOpen(f.root, dir+"/binding.json", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
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
	file, err := agentFileOpen(f.root, dir+"/partial", os.O_RDWR|os.O_CREATE, 0600)
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
	if err = f.checkpointPrefix(ctx, b, file, uint64(offset), chunk); err != nil {
		return err
	}
	if !finish {
		return nil
	}
	if offset+int64(len(chunk)) != b.Size || hashAgentFile(file, b.Size, b.SHA256) != nil || f.check(ctx, b, "commit") != nil {
		return errFileCapability
	}
	if err = writeAgentResumeRecord(f.root, dir+"/commit-intent.json", b); err != nil {
		return err
	}
	// No automatic retry after this point: a lost acknowledgement is UNKNOWN.
	publish := agentFilePublishNoReplace
	if b.ConflictPolicy == "overwrite" {
		publish = agentFilePublishReplace
	}
	if publish(f.root, dir+"/partial", b.RelativePath) != nil {
		return errFileCapability
	}
	published, err := agentFileOpen(f.root, b.RelativePath, os.O_RDONLY, 0)
	if err != nil {
		return errFileCapability
	}
	before, _ := file.Stat()
	after, statErr := published.Stat()
	verifyErr := hashAgentFile(published, b.Size, b.SHA256)
	published.Close()
	if statErr != nil || !os.SameFile(before, after) || verifyErr != nil {
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

// Snapshot freezes actual source bytes before the first chunk. Subsequent reads
// use this private descriptor, even if the source pathname is replaced/changed.
func (f *agentFileCapability) Snapshot(ctx context.Context, b agentFileBinding) (agentFileBinding, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if b.Direction != "read" || f.snapshots[b.TransferID] != nil || f.check(ctx, b, "stat") != nil || f.safePath(b.RelativePath, false) != nil {
		return b, errFileCapability
	}
	src, err := agentFileOpen(f.root, b.RelativePath, os.O_RDONLY, 0)
	if err != nil {
		return b, errFileCapability
	}
	defer src.Close()
	info, err := src.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() < 0 || info.Size() > 64<<20 {
		return b, errFileCapability
	}
	if agentFileSpace(f.root, info.Size()) != nil {
		return b, errFileCapability
	}
	name := ".sf-read-" + b.TransferID
	dst, err := agentFileOpen(f.root, name, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err != nil {
		return b, errFileCapability
	}
	accepted := false
	defer func() {
		if !accepted {
			dst.Close()
		}
	}()
	h := sha256.New()
	buffer := make([]byte, agentFileChunkBytes)
	defer clear(buffer)
	var size int64
	for {
		if f.check(ctx, b, "snapshot") != nil {
			return b, errFileCapability
		}
		n, e := src.Read(buffer)
		size += int64(n)
		if size > 64<<20 {
			return b, errFileCapability
		}
		if n > 0 {
			h.Write(buffer[:n])
			if _, e := dst.Write(buffer[:n]); e != nil {
				return b, errFileCapability
			}
		}
		if e == io.EOF {
			break
		}
		if e != nil || n == 0 {
			return b, errFileCapability
		}
	}
	digest := hex.EncodeToString(h.Sum(nil))
	if b.SHA256 != "" && (b.Size != size || b.SHA256 != digest) || dst.Sync() != nil || syncAgentFileDirectory(f.root, ".") != nil {
		return b, errFileCapability
	}
	b.Size, b.SHA256 = size, digest
	if f.check(ctx, b, "snapshot-complete") != nil {
		return b, errFileCapability
	}
	f.snapshots[b.TransferID] = dst
	accepted = true
	return b, nil
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
