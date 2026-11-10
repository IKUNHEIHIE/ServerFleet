//go:build linux

package files

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExecutionCacheCleanupRetainsUnknownAndDoesNotRecreateEmptySpool(t *testing.T) {
	ctx := context.Background()
	spool := t.TempDir()
	if e := os.Chmod(spool, 0700); e != nil {
		t.Fatal(e)
	}
	source, e := os.CreateTemp(t.TempDir(), "source")
	if e != nil {
		t.Fatal(e)
	}
	defer source.Close()
	data := []byte("actual-retained-source-bytes")
	if _, e = source.Write(data); e != nil {
		t.Fatal(e)
	}
	sum := sha256.Sum256(data)
	meta := ExecutionCacheManifest{Version: 1, ExecutionID: "completed", TaskID: "task", PlanSHA256: strings.Repeat("a", 64), SubjectID: "owner", NodeID: "node", SHA256: hex.EncodeToString(sum[:]), Size: uint64(len(data))}
	if e = CreateExecutionCache(ctx, spool, meta, source); e != nil {
		t.Fatal(e)
	}
	other := meta
	other.ExecutionID = "unknown"
	if e = CreateExecutionCache(ctx, spool, other, source); e != nil {
		t.Fatal(e)
	}
	if e = ReleaseExecutionCache(spool, meta.ExecutionID, meta.TaskID, meta.PlanSHA256); e != nil {
		t.Fatal(e)
	}
	if _, f, e := OpenExecutionCache(ctx, spool, other.ExecutionID); e != nil {
		t.Fatal("unknown cache removed", e)
	} else {
		f.Close()
	}
	if e = ReleaseExecutionCache(spool, other.ExecutionID, other.TaskID, other.PlanSHA256); e != nil {
		t.Fatal(e)
	}
	if _, e = ReadExecutionCacheManifest(spool, meta.ExecutionID); e == nil {
		t.Fatal("removed cache was readable")
	}
	if _, e = os.Stat(filepath.Join(spool, "resume-cache")); !os.IsNotExist(e) {
		t.Fatal("empty cache residue recreated", e)
	}
}
