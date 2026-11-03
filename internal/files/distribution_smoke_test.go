package files_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"serverfleet.local/serverfleet/internal/files"
)

func TestDistributionSmoke(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	body := []byte("hello distribution")
	digestBytes := sha256.Sum256(body)
	digest := hex.EncodeToString(digestBytes[:])
	size := int64(len(body))
	d, err := f.service.CreateDistribution(context.Background(), "user-1", "req-dist", []string{"n-a", "n-b"}, "/srv/app/hello.txt", &size, digest)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	written, err := f.service.WriteDistributionContent(context.Background(), "user-1", "req-append", d.ID, 0, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	if written.BytesReceived != size {
		t.Fatalf("bytes = %d", written.BytesReceived)
	}
	// simulate the submitter binding like the real assembly does
	if err := f.service.BindDistributionTask(context.Background(), d, "task-x", digest); err != nil {
		t.Fatalf("bind: %v", err)
	}
	if bound, err := f.repo.GetDistributionByTask(context.Background(), "task-x"); err != nil {
		t.Fatalf("get by task: %v", err)
	} else {
		t.Logf("bound distribution: id=%s status=%s task=%s targets=%d", bound.ID, bound.Status, bound.TaskID, len(bound.Targets))
		for _, row := range bound.Targets {
			t.Logf("  row: seq=%d node=%s status=%q write=%q", row.Sequence, row.NodeID, row.Status, row.WritePath)
		}
	}
	// simulate the worker per-target updates + aggregate
	for _, node := range []string{"n-a", "n-b"} {
		_, err := f.service.ApplyDistributionTaskUpdate(context.Background(), "task-x", files.DistributionTaskUpdate{
			TargetNode: node, Status: files.TargetStateCompleted, Bytes: size, SHA256: digest,
			ErrorCode: "", ErrorMessage: "file distribution target succeeded", RequestID: "req-dist",
		})
		if err != nil {
			t.Fatalf("apply %s: %v", node, err)
		}
		if err != nil {
			t.Fatalf("apply %s: %v", node, err)
		}
	}
	final, err := f.service.GetDistribution(context.Background(), "user-1", "req-dist", d.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if final.Status != files.DistributionCompleted {
		t.Fatalf("status = %q, want completed (targets=%+v)", final.Status, final.Targets)
	}
}

func TestDistributionTargetRowsPersisted(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	body := []byte("rows")
	digest := hex.EncodeToString(func() []byte { h := sha256.Sum256(body); return h[:] }())
	size := int64(len(body))
	d, err := f.service.CreateDistribution(context.Background(), "user-1", "req-rows", []string{"n-a", "n-b"}, "/srv/x.txt", &size, digest)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	rows, err := f.repo.ListDistributionTargets(context.Background(), d.ID)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("rows: %+v", rows)
	if len(rows) != 2 {
		t.Fatalf("rows = %d", len(rows))
	}
	moved, err := f.repo.FinishDistributionTarget(context.Background(), f.db, d.ID, "n-a", files.TargetStateCompleted, size, digest, "", "")
	if err != nil || !moved {
		t.Fatalf("finish target: moved=%v err=%v", moved, err)
	}
}
