package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func fileBinding(data []byte, direction string) agentFileBinding {
	h := sha256.Sum256(data)
	return agentFileBinding{TaskID: "file-task", ExecutionID: "file-execution", TransferID: "file-transfer", NodeID: "file-node", RelativePath: "received.bin", SHA256: hex.EncodeToString(h[:]), Direction: direction, ConflictPolicy: "skip", Size: int64(len(data))}
}
func TestAgentFileBytesResumeHashAndIndependentReadGate(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux non-root byte capability")
	}
	dir := t.TempDir()
	os.Chmod(dir, 0700)
	allowed := true
	f, err := openAgentFileCapability(dir, func(context.Context, agentFileBinding, string) error {
		if !allowed {
			return errFileCapability
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	data := []byte(strings.Repeat("synthetic-file-bytes", 2000))
	b := fileBinding(data, "write")
	if err = f.Write(context.Background(), b, 0, data[:agentFileChunkBytes], false); err != nil {
		t.Fatal(err)
	}
	f.Close()
	f, err = openAgentFileCapability(dir, func(context.Context, agentFileBinding, string) error {
		if !allowed {
			return errFileCapability
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err = f.Write(context.Background(), b, agentFileChunkBytes, data[agentFileChunkBytes:], true); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(filepath.Join(dir, "received.bin"))
	if string(got) != string(data) {
		t.Fatal("byte mismatch")
	}
	if f.Write(context.Background(), b, 0, data[:1], false) == nil {
		t.Fatal("completed transfer replay")
	}
	if _, err = f.Read(context.Background(), b, 0, 1); err == nil {
		t.Fatal("write binding authorized read")
	}
	b.Direction = "read"
	chunk, err := f.Read(context.Background(), b, 0, agentFileChunkBytes)
	if err != nil || string(chunk) != string(data[:agentFileChunkBytes]) {
		t.Fatal(err)
	}
	allowed = false
	if chunk, err = f.Read(context.Background(), b, 0, 1); err == nil || chunk != nil {
		t.Fatal("revoked read disclosed data")
	}
}
func TestAgentFileRejectsPlanChangeEscapeHashOverwriteAndCancel(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux non-root byte capability")
	}
	dir := t.TempDir()
	os.Chmod(dir, 0700)
	f, err := openAgentFileCapability(dir, func(context.Context, agentFileBinding, string) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	ctx := context.Background()
	data := []byte("synthetic")
	b := fileBinding(data, "write")
	for _, path := range []string{"../escape", "/absolute", "a\\b", "C:drive", ".sf-file-transfer/partial"} {
		bad := b
		bad.RelativePath = path
		if f.Write(ctx, bad, 0, data, true) == nil {
			t.Fatal("path accepted", path)
		}
	}
	if f.Write(ctx, b, 0, data[:2], false) != nil {
		t.Fatal("initial write")
	}
	changed := b
	changed.RelativePath = "other.bin"
	if f.Write(ctx, changed, 2, data[2:], true) == nil {
		t.Fatal("mutable binding")
	}
	if f.Write(ctx, b, 0, data, true) == nil {
		t.Fatal("repeated offset")
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if f.Write(canceled, b, 2, data[2:], true) == nil {
		t.Fatal("cancel ignored")
	}
	if f.Write(ctx, b, 2, []byte("WRONG!!"), true) == nil {
		t.Fatal("hash mismatch")
	}
	if _, err = os.Stat(filepath.Join(dir, b.RelativePath)); !os.IsNotExist(err) {
		t.Fatal("bad hash published")
	}
	b.TransferID = "second-transfer"
	os.WriteFile(filepath.Join(dir, b.RelativePath), []byte("original"), 0600)
	if f.Write(ctx, b, 0, data, true) == nil {
		t.Fatal("overwrote destination")
	}
	got, _ := os.ReadFile(filepath.Join(dir, b.RelativePath))
	if string(got) != "original" {
		t.Fatal("existing file changed")
	}
	replacement := []byte("explicit replacement")
	overwrite := fileBinding(replacement, "write")
	overwrite.TransferID = "overwrite-transfer"
	overwrite.ConflictPolicy = "overwrite"
	if err = f.Write(ctx, overwrite, 0, replacement, true); err != nil {
		t.Fatal("explicit overwrite rejected", err)
	}
	if got, _ = os.ReadFile(filepath.Join(dir, overwrite.RelativePath)); string(got) != string(replacement) {
		t.Fatal("explicit overwrite did not replace the regular file")
	}
}
