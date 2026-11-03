package files_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"
	"time"

	"serverfleet.local/serverfleet/internal/audit"
	"serverfleet.local/serverfleet/internal/auth"
	"serverfleet.local/serverfleet/internal/files"
	"serverfleet.local/serverfleet/internal/storage"
	"serverfleet.local/serverfleet/internal/storage/testkit"
	"serverfleet.local/serverfleet/internal/tasks"
)

// cloudReviewUploadSubmitter wraps the fixture submitter with the unified
// upload task port, mirroring the ChatGPT R1 review probe composition.
type cloudReviewUploadSubmitter struct {
	fake       *fakeSubmitter
	command    files.UploadCommand
	outcome    *tasks.Task
	outcomeErr error
}

func (s *cloudReviewUploadSubmitter) SubmitTransfer(ctx context.Context, command files.TransferCommand) (string, error) {
	return s.fake.SubmitTransfer(ctx, command)
}

func (s *cloudReviewUploadSubmitter) SubmitUpload(_ context.Context, command files.UploadCommand) (string, error) {
	s.command = command
	return "tsk_cloud_review", nil
}

func (s *cloudReviewUploadSubmitter) ReadTaskOutcome(_ context.Context, taskID string) (tasks.Task, error) {
	if s.outcomeErr != nil {
		return tasks.Task{}, s.outcomeErr
	}
	if s.outcome != nil {
		return *s.outcome, nil
	}
	t := tasks.Task{ID: taskID, Status: tasks.StatusRunning}
	return t, nil
}

// newUploadReviewFixture mirrors newFixture but also applies the execution
// binding migration so cancellation evidence queries work.
func newUploadReviewFixture(t *testing.T) *fixture {
	t.Helper()
	db := testkit.NewDB(t, files.Migrations(), []storage.Migration{files.ExecutionBindingsMigration()}, audit.Migrations())
	repo, err := files.NewRepo(db)
	if err != nil {
		t.Fatal(err)
	}
	limits := audit.DefaultLimits()
	limits.RequireSource = true
	validator, err := audit.NewValidator(files.ServiceActions(), limits)
	if err != nil {
		t.Fatal(err)
	}
	recorder, err := audit.NewSQLRecorder(db, validator, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	authorizer := authorizerFunc(func(context.Context, auth.Check) error { return nil })
	staging := t.TempDir()
	submitter := &fakeSubmitter{}
	canceller := &fakeCanceller{}
	f := &fixture{db: db, repo: repo, audit: recorder, authorizer: authorizer,
		submitter: submitter, canceller: canceller, staging: staging}
	return f
}

// TestCloudReviewUploadWithoutClientDigestKeepsActualSHA reproduces review
// probe 1 (AM-R4): without a client sha256 the completed upload must still
// persist the computed and verified digest.
func TestCloudReviewUploadWithoutClientDigestKeepsActualSHA(t *testing.T) {
	f := newUploadReviewFixture(t)
	want := sha256.Sum256([]byte("payload-without-client-digest"))
	queue := &cloudReviewUploadSubmitter{fake: f.submitter}
	s, err := files.NewService(files.Options{DB: f.db, Repo: f.repo, Audit: f.audit,
		Authorizer: f.authorizer, Submitter: queue, Canceller: f.canceller, StagingDir: f.staging})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	body := []byte("payload-without-client-digest")
	size := int64(len(body))
	u, err := s.CreateUpload(ctx, "user-1", "review-create", "n-dst", "/srv/blob", &size, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.AppendChunk(ctx, "user-1", "review-append", u.ID, 0, body); err != nil {
		t.Fatal(err)
	}
	u, err = s.CompleteUpload(ctx, "user-1", "review-complete", u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if u.Status != files.UploadDispatching || u.TaskID == "" {
		t.Fatalf("dispatch = %q task %q", u.Status, u.TaskID)
	}
	if u.SHA256Actual != hex.EncodeToString(want[:]) {
		t.Fatalf("actual digest %q, want %q", u.SHA256Actual, hex.EncodeToString(want[:]))
	}
	if queue.command.SHA256Expected != hex.EncodeToString(want[:]) {
		t.Fatalf("task digest %q, want %q", queue.command.SHA256Expected, hex.EncodeToString(want[:]))
	}
	stored, err := f.repo.GetUpload(ctx, u.ID)
	if err != nil || stored.SHA256Actual != hex.EncodeToString(want[:]) {
		t.Fatalf("stored digest = %q err=%v, want %q", stored.SHA256Actual, err, hex.EncodeToString(want[:]))
	}
	// The trusted terminal completion keeps the persisted verified digest.
	stored, err = s.ApplyUploadTaskUpdate(ctx, u.TaskID, files.UploadTaskUpdate{Status: files.UploadCompleted})
	if err != nil || stored.SHA256Actual != hex.EncodeToString(want[:]) {
		t.Fatalf("completed digest = %q err=%v, want %q", stored.SHA256Actual, err, hex.EncodeToString(want[:]))
	}
}

// TestCloudReviewQueuedUploadCancelTerminates reproduces review probe 2
// (AM-R1): cancelling an upload that never started converges to a persisted
// terminal state instead of staying dispatching forever.
func TestCloudReviewQueuedUploadCancelTerminates(t *testing.T) {
	f := newUploadReviewFixture(t)
	queue := &cloudReviewUploadSubmitter{fake: f.submitter}
	s, err := files.NewService(files.Options{DB: f.db, Repo: f.repo, Audit: f.audit,
		Authorizer: f.authorizer, Submitter: queue, Canceller: f.canceller, StagingDir: f.staging})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	body := []byte("queued-upload-never-started")
	size := int64(len(body))
	u, err := s.CreateUpload(ctx, "user-1", "review-create", "n-dst", "/srv/blob", &size, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.AppendChunk(ctx, "user-1", "review-append", u.ID, 0, body); err != nil {
		t.Fatal(err)
	}
	u, err = s.CompleteUpload(ctx, "user-1", "review-complete", u.ID)
	if err != nil {
		t.Fatal(err)
	}
	u, err = s.CancelUpload(ctx, "user-1", "review-cancel", u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if f.canceller.taskID != u.TaskID {
		t.Fatal("cancel did not reach admitted queue")
	}
	if u.Status != files.UploadCancelled {
		t.Fatalf("unstarted upload stayed %q after acknowledged cancellation", u.Status)
	}
}

// TestCancelUploadKeepsInflightStreamUnknown verifies the worker competition
// guard (AM-R1): once a binding reached the opened state a cancel must NOT
// converge the upload to a known cancelled state over possibly written bytes.
func TestCancelUploadKeepsInflightStreamUnknown(t *testing.T) {
	f := newUploadReviewFixture(t)
	queue := &cloudReviewUploadSubmitter{fake: f.submitter}
	s, err := files.NewService(files.Options{DB: f.db, Repo: f.repo, Audit: f.audit,
		Authorizer: f.authorizer, Submitter: queue, Canceller: f.canceller, StagingDir: f.staging})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	body := []byte("inflight-upload-stream")
	size := int64(len(body))
	u, err := s.CreateUpload(ctx, "user-1", "review-create", "n-dst", "/srv/blob", &size, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.AppendChunk(ctx, "user-1", "review-append", u.ID, 0, body); err != nil {
		t.Fatal(err)
	}
	u, err = s.CompleteUpload(ctx, "user-1", "review-complete", u.ID)
	if err != nil {
		t.Fatal(err)
	}
	planHash := sha256Hex([]byte("review-plan"))
	binding := files.ExecutionBinding{
		ExecutionID: "exec-inflight", TaskID: u.TaskID, TransferID: u.ID,
		SubjectID: "user-1", NodeID: "n-dst", Direction: "write",
		ParentPlan: planHash, Observation: `{"conn":1}`, Offer: []byte("{}"), Size: 22,
		Sequence: 0, State: "reserved",
	}
	if err := f.repo.ReserveExecution(ctx, binding, f.audit); err != nil {
		t.Fatal(err)
	}
	// Advance the binding to the opened state: the agent accepted the
	// destination open, so write frames may have been issued.
	if err := f.repo.ObserveExecution(ctx, binding.ExecutionID, "frame", f.audit, func(b *files.ExecutionBinding) error {
		b.State = "opened"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	u, err = s.CancelUpload(ctx, "user-1", "review-cancel", u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if u.Status != files.UploadDispatching {
		t.Fatalf("cancel converged an in-flight stream to %q", u.Status)
	}
}

func TestUploadTaskReconciliationConvergesTerminalTasks(t *testing.T) {
	f := newUploadReviewFixture(t)
	queue := &cloudReviewUploadSubmitter{fake: f.submitter}
	s, err := files.NewService(files.Options{DB: f.db, Repo: f.repo, Audit: f.audit,
		Authorizer: f.authorizer, Submitter: queue, Canceller: f.canceller, StagingDir: f.staging})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	body := []byte("reconcile-me")
	size := int64(len(body))
	u, err := s.CreateUpload(ctx, "user-1", "review-create", "n-dst", "/srv/blob", &size, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.AppendChunk(ctx, "user-1", "review-append", u.ID, 0, body); err != nil {
		t.Fatal(err)
	}
	if u, err = s.CompleteUpload(ctx, "user-1", "review-complete", u.ID); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		status   tasks.TaskStatus
		want     string
		wantCode string
	}{
		{tasks.StatusCanceled, files.UploadCancelled, "cancelled_by_task"},
		{tasks.StatusExpired, files.UploadFailed, "task_expired"},
		{tasks.StatusFailed, files.UploadFailed, "task_failed"},
		{tasks.StatusUnknown, files.UploadFailed, files.UploadErrorAgentUnknown},
	} {
		fresh := newUploadReviewFixture(t)
		freshQueue := &cloudReviewUploadSubmitter{fake: fresh.submitter}
		freshService, err := files.NewService(files.Options{DB: fresh.db, Repo: fresh.repo, Audit: fresh.audit,
			Authorizer: fresh.authorizer, Submitter: freshQueue, Canceller: fresh.canceller, StagingDir: fresh.staging})
		if err != nil {
			t.Fatal(err)
		}
		freshBody := []byte("reconcile-mapping")
		freshSize := int64(len(freshBody))
		fu, err := freshService.CreateUpload(ctx, "user-1", "map-create", "n-dst", "/srv/blob", &freshSize, "")
		if err != nil {
			t.Fatal(err)
		}
		if _, err = freshService.AppendChunk(ctx, "user-1", "map-append", fu.ID, 0, freshBody); err != nil {
			t.Fatal(err)
		}
		if _, err = freshService.CompleteUpload(ctx, "user-1", "map-complete", fu.ID); err != nil {
			t.Fatal(err)
		}
		freshQueue.outcome = &tasks.Task{ID: fu.TaskID, Status: tc.status}
		converged, err := freshService.ReconcileUploadTasks(ctx)
		if err != nil || converged != 1 {
			t.Fatalf("reconcile %s: converged=%d err=%v", tc.status, converged, err)
		}
		got, err := fresh.repo.GetUpload(ctx, fu.ID)
		if err != nil || got.Status != tc.want || got.ErrorCode != tc.wantCode {
			t.Fatalf("task %s produced upload %s/%s, want %s/%s", tc.status, got.Status, got.ErrorCode, tc.want, tc.wantCode)
		}
	}
	// Running tasks provide no terminal evidence: the upload stays dispatching.
	queue.outcome = &tasks.Task{ID: u.TaskID, Status: tasks.StatusRunning}
	if _, err := s.ReconcileUploadTasks(ctx); err != nil {
		t.Fatal(err)
	}
	got, err := f.repo.GetUpload(ctx, u.ID)
	if err != nil || got.Status != files.UploadDispatching {
		t.Fatalf("running task converged upload to %q", got.Status)
	}
}

// TestUploadTaskReconciliationRequiresPositiveEvidence verifies AM-R3 for the
// reconciliation path: a task read failure leaves the upload untouched.
func TestUploadTaskReconciliationRequiresPositiveEvidence(t *testing.T) {
	f := newUploadReviewFixture(t)
	queue := &cloudReviewUploadSubmitter{fake: f.submitter}
	s, err := files.NewService(files.Options{DB: f.db, Repo: f.repo, Audit: f.audit,
		Authorizer: f.authorizer, Submitter: queue, Canceller: f.canceller, StagingDir: f.staging})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	body := []byte("no-evidence-reconcile")
	size := int64(len(body))
	u, err := s.CreateUpload(ctx, "user-1", "review-create", "n-dst", "/srv/blob", &size, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.AppendChunk(ctx, "user-1", "review-append", u.ID, 0, body); err != nil {
		t.Fatal(err)
	}
	if _, err = s.CompleteUpload(ctx, "user-1", "review-complete", u.ID); err != nil {
		t.Fatal(err)
	}
	queue.outcomeErr = errors.New("task read infrastructure failure")
	if _, err := s.ReconcileUploadTasks(ctx); err != nil {
		t.Fatal(err)
	}
	got, err := f.repo.GetUpload(ctx, u.ID)
	if err != nil || got.Status != files.UploadDispatching {
		t.Fatalf("evidence-free reconciliation converged upload to %q", got.Status)
	}
	// Lazy GetUpload reconciliation keeps the same guard.
	queue.outcomeErr = nil
	queue.outcome = &tasks.Task{ID: u.TaskID, Status: tasks.StatusUnknown}
	lazy, err := s.GetUpload(ctx, "user-1", "review-lazy", u.ID)
	if err != nil || lazy.Status != files.UploadFailed || lazy.ErrorCode != files.UploadErrorAgentUnknown {
		t.Fatalf("lazy reconciliation = %s/%s err=%v", lazy.Status, lazy.ErrorCode, err)
	}
}
