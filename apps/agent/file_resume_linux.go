package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"hash"
	"io"
	"os"
	"strconv"
	"strings"
	"syscall"

	"serverfleet.local/serverfleet/internal/transport"
)

type agentPrefixCheckpoint struct {
	Version      int              `json:"version"`
	Binding      agentFileBinding `json:"binding"`
	Offset       uint64           `json:"offset"`
	PrefixSHA256 string           `json:"prefixSha256"`
}

func agentResumeLock(root *os.Root, dir string) (func(), error) {
	file, err := agentFileOpen(root, dir+"/io.lock", os.O_RDWR|os.O_CREATE, 0600)
	if err != nil {
		return nil, err
	}
	if syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) != nil {
		file.Close()
		return nil, errFileCapability
	}
	return func() { _ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN); file.Close() }, nil
}
func writeAgentResumeRecord(root *os.Root, name string, value any) error {
	raw, err := json.Marshal(value)
	if err != nil || len(raw) > 8192 {
		return errFileCapability
	}
	file, err := agentFileOpen(root, name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	_, err = file.Write(raw)
	syncErr := file.Sync()
	closeErr := file.Close()
	if err != nil || syncErr != nil || closeErr != nil {
		return errFileCapability
	}
	return syncAgentFileDirectory(root, name[:strings.LastIndex(name, "/")])
}
func (f *agentFileCapability) checkpointPrefix(ctx context.Context, b agentFileBinding, file *os.File, offset uint64, chunk []byte) error {
	if !transport.FileDigest(b.PlanSHA256) {
		return nil
	} // legacy isolated byte-port has no resumable authority
	if f.writeHashes == nil {
		f.writeHashes = map[string]hash.Hash{}
	}
	h := f.writeHashes[b.TransferID]
	if h == nil {
		h = sha256.New()
		if offset > 0 {
			if _, err := io.Copy(h, io.NewSectionReader(file, 0, int64(offset))); err != nil {
				return err
			}
		}
		f.writeHashes[b.TransferID] = h
	}
	h.Write(chunk)
	checkpoint := agentPrefixCheckpoint{Version: 1, Binding: b, Offset: offset + uint64(len(chunk)), PrefixSHA256: hex.EncodeToString(h.Sum(nil))}
	return writeAgentResumeRecord(f.root, ".sf-"+b.TransferID+"/prefix-"+strconv.FormatUint(checkpoint.Offset, 10)+".json", checkpoint)
}
func (f *agentFileCapability) adoptResume(ctx context.Context, b agentFileBinding, raw string) (agentFileBinding, error) {
	source, err := transport.ParseFileResumeSource(raw, uint64(b.Size))
	if err != nil || f.check(ctx, b, "resume-admit") != nil {
		return b, errFileCapability
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	oldDir := ".sf-" + source.TransferID
	unlock, err := agentResumeLock(f.root, oldDir)
	if err != nil {
		return b, err
	}
	defer unlock()
	for _, marker := range []string{"commit-intent.json", "complete", "adopted.json"} {
		if _, e := f.root.Lstat(oldDir + "/" + marker); !os.IsNotExist(e) {
			return b, errFileCapability
		}
	}
	record, err := agentFileOpen(f.root, oldDir+"/prefix-"+strconv.FormatUint(source.ConfirmedOffset, 10)+".json", os.O_RDONLY, 0)
	if err != nil {
		return b, err
	}
	defer record.Close()
	bytes, err := io.ReadAll(io.LimitReader(record, 8193))
	if err != nil || len(bytes) > 8192 {
		return b, errFileCapability
	}
	var checkpoint agentPrefixCheckpoint
	if json.Unmarshal(bytes, &checkpoint) != nil || checkpoint.Version != 1 || checkpoint.Offset != source.ConfirmedOffset || checkpoint.PrefixSHA256 != source.PrefixSHA256 {
		return b, errFileCapability
	}
	old := checkpoint.Binding
	if old.TaskID != source.TaskID || old.ExecutionID != source.ExecutionID || old.TransferID != source.TransferID || old.PlanSHA256 != source.PlanSHA256 || old.NodeID != b.NodeID || old.SHA256 != b.SHA256 || old.Size != b.Size || old.Direction != "write" || old.ConflictPolicy != b.ConflictPolicy || old.TaskID == b.TaskID || old.ExecutionID == b.ExecutionID {
		return b, errFileCapability
	}
	if old.RelativePath != b.RelativePath {
		if b.ConflictPolicy != "rename" || !transport.FilePath(old.RelativePath) || agentFileDirectory(old.RelativePath) != agentFileDirectory(b.RelativePath) {
			return b, errFileCapability
		}
	}
	oldFile, err := agentFileOpen(f.root, oldDir+"/partial", os.O_RDONLY, 0)
	if err != nil {
		return b, err
	}
	defer oldFile.Close()
	info, err := oldFile.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() < int64(source.ConfirmedOffset) || info.Size() > b.Size {
		return b, errFileCapability
	}
	hash := sha256.New()
	if n, e := io.Copy(hash, io.NewSectionReader(oldFile, 0, int64(source.ConfirmedOffset))); e != nil || uint64(n) != source.ConfirmedOffset || hex.EncodeToString(hash.Sum(nil)) != source.PrefixSHA256 {
		return b, errFileCapability
	}
	b.RelativePath = old.RelativePath
	if f.safePath(b.RelativePath, true) != nil {
		return b, errFileCapability
	}
	kind, e := agentFileDestination(f.root, b.RelativePath)
	if e != nil || kind == agentDestinationOther || b.ConflictPolicy == "rename" && kind != agentDestinationMissing {
		return b, errFileCapability
	}
	if err = writeAgentResumeRecord(f.root, oldDir+"/adopted.json", b); err != nil {
		return b, err
	}
	newDir := ".sf-" + b.TransferID
	if f.root.Mkdir(newDir, 0700) != nil {
		return b, errFileCapability
	}
	if err = writeAgentResumeRecord(f.root, newDir+"/binding.json", b); err != nil {
		return b, err
	}
	destination, err := agentFileOpen(f.root, newDir+"/partial", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return b, err
	}
	n, e := io.Copy(destination, io.NewSectionReader(oldFile, 0, int64(source.ConfirmedOffset)))
	syncErr, closeErr := destination.Sync(), destination.Close()
	if e != nil || uint64(n) != source.ConfirmedOffset || syncErr != nil || closeErr != nil || f.check(ctx, b, "resume-prefix-copied") != nil {
		return b, errFileCapability
	}
	if err = writeAgentResumeRecord(f.root, newDir+"/prefix-"+strconv.FormatUint(source.ConfirmedOffset, 10)+".json", agentPrefixCheckpoint{1, b, source.ConfirmedOffset, source.PrefixSHA256}); err != nil {
		return b, err
	}
	f.writeHashes[b.TransferID] = hash
	return b, nil
}
func agentFileDirectory(path string) string {
	if i := strings.LastIndex(path, "/"); i >= 0 {
		return path[:i]
	}
	return ""
}
