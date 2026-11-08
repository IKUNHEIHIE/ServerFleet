package files_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"serverfleet.local/serverfleet/internal/audit"
	"serverfleet.local/serverfleet/internal/files"
	"serverfleet.local/serverfleet/internal/storage"
	"serverfleet.local/serverfleet/internal/tasks"
)

func TestUploadPoliciesFreezeAndPersistHonestTerminalDestination(t *testing.T) {
	for _, policy := range []files.ConflictPolicy{files.ConflictSkip, files.ConflictOverwrite, files.ConflictRename} {
		t.Run(string(policy), func(t *testing.T) {
			f := newUploadReviewFixture(t)
			ctx := context.Background()
			queue := &cloudReviewUploadSubmitter{fake: f.submitter}
			service, err := files.NewService(files.Options{DB: f.db, Repo: f.repo, Audit: f.audit, Authorizer: f.authorizer, Submitter: queue, StagingDir: f.staging})
			if err != nil {
				t.Fatal(err)
			}
			body := []byte("new candidate content")
			size := int64(len(body))
			u, err := service.CreateUpload(ctx, "user-1", "create-policy", "n-dst", "/srv/blob", &size, sha256Hex(body), policy)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = service.AppendChunk(ctx, "user-1", "append-policy", u.ID, 0, body); err != nil {
				t.Fatal(err)
			}
			u, err = service.CompleteUpload(ctx, "user-1", "complete-policy", u.ID)
			if err != nil || queue.command.ConflictPolicy != policy {
				t.Fatalf("frozen plan: %+v %v", queue.command, err)
			}
			state, actual := files.UploadCompleted, "/srv/blob"
			if policy == files.ConflictSkip {
				state = files.UploadSkipped
			}
			if policy == files.ConflictRename {
				actual = "/srv/blob-1"
			}
			if _, err = service.ApplyUploadTaskUpdate(ctx, u.TaskID, files.UploadTaskUpdate{Status: state, ActualDestination: "/elsewhere/blob"}); !errors.Is(err, files.ErrTransferIdentity) {
				t.Fatal("escaped receipt", err)
			}
			result, err := service.ApplyUploadTaskUpdate(ctx, u.TaskID, files.UploadTaskUpdate{Status: state, ActualDestination: actual})
			if err != nil || result.Status != state || result.ActualDestination != actual {
				t.Fatalf("terminal: %+v %v", result, err)
			}
			if _, err = os.Stat(filepath.Join(f.staging, u.StagingName)); !os.IsNotExist(err) {
				t.Fatal("terminal staging not removed", err)
			}
			stored, err := f.repo.GetUpload(ctx, u.ID)
			if err != nil || stored.ActualDestination != actual || stored.SHA256Actual != sha256Hex(body) {
				t.Fatalf("durable receipt: %+v %v", stored, err)
			}
			duplicate, err := service.CompleteUpload(ctx, "user-1", "repeat-complete", u.ID)
			if err != nil || duplicate.Status != state || duplicate.ActualDestination != actual {
				t.Fatalf("repeat changed receipt: %+v %v", duplicate, err)
			}
		})
	}
}

func TestUploadNonOverwriteBareTaskSuccessIsNotAnInventedReceipt(t *testing.T) {
	for _, policy := range []files.ConflictPolicy{files.ConflictSkip, files.ConflictRename} {
		t.Run(string(policy), func(t *testing.T) {
			f := newUploadReviewFixture(t)
			ctx := context.Background()
			queue := &cloudReviewUploadSubmitter{fake: f.submitter}
			s, err := files.NewService(files.Options{DB: f.db, Repo: f.repo, Audit: f.audit, Authorizer: f.authorizer, Submitter: queue, StagingDir: f.staging})
			if err != nil {
				t.Fatal(err)
			}
			size := int64(1)
			u, err := s.CreateUpload(ctx, "user-1", "create", "n-dst", "/srv/blob", &size, "", policy)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = s.AppendChunk(ctx, "user-1", "append", u.ID, 0, []byte("a")); err != nil {
				t.Fatal(err)
			}
			u, err = s.CompleteUpload(ctx, "user-1", "complete", u.ID)
			if err != nil {
				t.Fatal(err)
			}
			queue.outcome = &tasks.Task{ID: u.TaskID, Status: tasks.StatusCompleted}
			got, err := s.GetUpload(ctx, "user-1", "read", u.ID)
			if err != nil || got.Status != files.UploadFailed || got.ErrorCode != files.UploadErrorAgentUnknown || got.ActualDestination != "" {
				t.Fatalf("invented success: %+v %v", got, err)
			}
			if _, err = os.Stat(filepath.Join(f.staging, u.StagingName)); err != nil {
				t.Fatal("unknown outcome lost staging", err)
			}
		})
	}
}

type uploadFailureAudit struct {
	files.AuditSink
	fail bool
}

func (a *uploadFailureAudit) AppendTx(ctx context.Context, tx storage.Tx, e audit.Event) error {
	if a.fail && e.Action == files.ActionUploadComplete {
		return fmt.Errorf("test: audit unavailable")
	}
	return a.AuditSink.AppendTx(ctx, tx, e)
}
func TestUploadChecksumAuditFailurePreservesStateAndBytes(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	sink := &uploadFailureAudit{AuditSink: f.audit, fail: true}
	s, err := files.NewService(files.Options{DB: f.db, Repo: f.repo, Audit: sink, Authorizer: f.authorizer, StagingDir: f.staging})
	if err != nil {
		t.Fatal(err)
	}
	body := []byte("actual bytes")
	size := int64(len(body))
	u, err := s.CreateUpload(ctx, "user-1", "create", "n-dst", "/srv/blob", &size, strings.Repeat("0", 64))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.AppendChunk(ctx, "user-1", "append", u.ID, 0, body); err != nil {
		t.Fatal(err)
	}
	if _, err = s.CompleteUpload(ctx, "user-1", "complete", u.ID); err == nil || errors.Is(err, files.ErrChecksumMismatch) {
		t.Fatal("audit failure hidden", err)
	}
	stored, err := f.repo.GetUpload(ctx, u.ID)
	if err != nil || stored.Status != files.UploadOpen || stored.ErrorCode != "" {
		t.Fatalf("state committed without audit: %+v %v", stored, err)
	}
	raw, err := os.ReadFile(filepath.Join(f.staging, u.StagingName))
	if err != nil || !bytes.Equal(raw, body) {
		t.Fatal("bytes removed before transaction", err)
	}
	sink.fail = false
	if _, err = s.CompleteUpload(ctx, "user-1", "reject", u.ID); !errors.Is(err, files.ErrChecksumMismatch) {
		t.Fatal(err)
	}
	stored, err = f.repo.GetUpload(ctx, u.ID)
	if err != nil || stored.Status != files.UploadFailed || stored.ErrorCode != "checksum_mismatch" {
		t.Fatalf("missing rejection: %+v %v", stored, err)
	}
	if _, err = os.Stat(filepath.Join(f.staging, u.StagingName)); !os.IsNotExist(err) {
		t.Fatal("rejected bytes retained", err)
	}
}
