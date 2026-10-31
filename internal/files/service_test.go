package files_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"serverfleet.local/serverfleet/internal/audit"
	"serverfleet.local/serverfleet/internal/auth"
	"serverfleet.local/serverfleet/internal/files"
	"serverfleet.local/serverfleet/internal/storage"
	"serverfleet.local/serverfleet/internal/storage/testkit"
)

type authorizerFunc func(context.Context, auth.Check) error

func (f authorizerFunc) Authorize(ctx context.Context, check auth.Check) error {
	return f(ctx, check)
}

type fakeSubmitter struct {
	mu       sync.Mutex
	commands []files.TransferCommand
	taskID   string
	err      error
}

func (s *fakeSubmitter) SubmitTransfer(_ context.Context, command files.TransferCommand) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.commands = append(s.commands, command)
	if s.err != nil {
		return "", s.err
	}
	if s.taskID == "" {
		s.taskID = "task-1"
	}
	return s.taskID, nil
}

type fakeCanceller struct {
	mu     sync.Mutex
	taskID string
}

func (c *fakeCanceller) CancelTask(_ context.Context, taskID, _, _ string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.taskID = taskID
	return nil
}

type fakeUpload struct {
	mu     sync.Mutex
	data   []byte
	size   int64
	digest string
	err    error
	calls  int
}

func (u *fakeUpload) PushFile(_ context.Context, _ string, _ string, content io.Reader, size int64, digest string) error {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.calls++
	if u.err != nil {
		return u.err
	}
	body, err := io.ReadAll(content)
	if err != nil {
		return err
	}
	u.data, u.size, u.digest = body, size, digest
	return nil
}

type fakeDownload struct {
	mu    sync.Mutex
	body  []byte
	calls int
}

func (d *fakeDownload) PullFile(_ context.Context, _ string, _ string, sink io.Writer) (int64, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.calls++
	return io.Copy(sink, bytes.NewReader(d.body))
}

type fixture struct {
	db           *storage.DB
	repo         *files.Repo
	audit        *audit.SQLRecorder
	authorizer   authorizerFunc
	submitter    *fakeSubmitter
	canceller    *fakeCanceller
	upload       *fakeUpload
	download     *fakeDownload
	cancellerPtr *fakeCanceller
	staging      string
	service      *files.Service
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	db := testkit.NewDB(t, files.Migrations(), audit.Migrations())
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
	submitter := &fakeSubmitter{}
	canceller := &fakeCanceller{}
	upload := &fakeUpload{}
	download := &fakeDownload{body: []byte("remote-bytes")}
	staging := t.TempDir()
	service, err := files.NewService(files.Options{
		DB: db, Repo: repo, Audit: recorder, Authorizer: authorizer,
		Submitter: submitter, Canceller: canceller, Uploads: upload,
		Downloads: download, StagingDir: staging, MaxUploadBytes: 1 << 20,
	})
	if err != nil {
		t.Fatal(err)
	}
	return &fixture{db: db, repo: repo, audit: recorder, authorizer: authorizer,
		submitter: submitter, canceller: canceller, upload: upload,
		download: download, cancellerPtr: canceller, staging: staging, service: service}
}

func validInput() files.CreateTransferInput {
	return files.CreateTransferInput{
		SubjectID: "user-1", RequestID: "req-1",
		SourceNodeID: "n-src", DestinationNodeID: "n-dst",
		SourcePaths: []string{"/etc/app/config.yml"}, DestinationDir: "/srv/app",
		ConflictPolicy: files.ConflictSkip,
	}
}

func TestPathValidation(t *testing.T) {
	t.Parallel()
	valid := []string{"/", "/etc/app/config.yml", "/var/log/a b.txt", "/data/日志.log"}
	for _, path := range valid {
		if err := files.ValidateRemotePath(path); err != nil {
			t.Fatalf("valid path rejected %q: %v", path, err)
		}
	}
	invalid := []string{"", "etc/passwd", "/etc/../passwd", "/etc//passwd", "/etc/",
		"/etc/./x", "C:\\Windows", "/etc/\x00x", "/etc/a\nb"}
	for _, path := range invalid {
		if err := files.ValidateRemotePath(path); err == nil {
			t.Fatalf("invalid path accepted %q", path)
		}
	}
}

func TestDryRunDoesNotSubmitOrPersist(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	input := validInput()
	input.DryRun = true
	transfer, err := f.service.CreateTransfer(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if len(transfer.Items) != 1 || transfer.Items[0].DestinationPath != "/srv/app/config.yml" {
		t.Fatalf("items = %+v", transfer.Items)
	}
	if len(f.submitter.commands) != 0 {
		t.Fatal("dry run submitted a task")
	}
	var count int
	if err := f.db.QueryRow(context.Background(), "SELECT COUNT(*) FROM file_transfers").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("dry run persisted %d rows", count)
	}
}

func TestTransferSubmitsUnifiedTask(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	transfer, err := f.service.CreateTransfer(context.Background(), validInput())
	if err != nil {
		t.Fatal(err)
	}
	if transfer.TaskID != "task-1" || transfer.Status != files.TransferQueued {
		t.Fatalf("transfer = %+v", transfer)
	}
	if len(f.submitter.commands) != 1 {
		t.Fatalf("commands = %+v", f.submitter.commands)
	}
	command := f.submitter.commands[0]
	if command.SourceNodeID != "n-src" || command.DestinationNodeID != "n-dst" || len(command.SourcePaths) != 1 {
		t.Fatalf("command = %+v", command)
	}
	page, err := f.audit.Query(context.Background(), audit.Query{Action: files.ActionTransferCreate})
	if err != nil || page.Total != 1 {
		t.Fatalf("audit = %+v, %v", page, err)
	}
}

func TestTransferAuthorizationIsSplitPerEndpoint(t *testing.T) {
	t.Parallel()
	for _, denyAction := range []string{files.ActionRead, files.ActionWrite} {
		f := newFixture(t)
		f.service, _ = newServiceWithAuthorizer(t, f, authorizerFunc(func(_ context.Context, check auth.Check) error {
			if check.Action == denyAction {
				return auth.ErrDenied
			}
			return nil
		}))
		if _, err := f.service.CreateTransfer(context.Background(), validInput()); !errors.Is(err, auth.ErrDenied) {
			t.Fatalf("deny %s err = %v", denyAction, err)
		}
		if len(f.submitter.commands) != 0 {
			t.Fatalf("denied transfer submitted %d tasks", len(f.submitter.commands))
		}
		page, err := f.audit.Query(context.Background(), audit.Query{Action: denyAction, Result: "denied"})
		if err != nil || page.Total != 1 {
			t.Fatalf("denial audit = %+v, %v", page, err)
		}
	}
}

func newServiceWithAuthorizer(t *testing.T, f *fixture, authorizer auth.Authorizer) (*files.Service, error) {
	t.Helper()
	return files.NewService(files.Options{
		DB: f.db, Repo: f.repo, Audit: f.audit, Authorizer: authorizer,
		Submitter: f.submitter, Canceller: f.canceller, Uploads: f.upload,
		Downloads: f.download, StagingDir: t.TempDir(), MaxUploadBytes: 1 << 20,
	})
}

func TestSymlinkFollowingRejected(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	input := validInput()
	input.FollowSymlinks = true
	if _, err := f.service.CreateTransfer(context.Background(), input); !errors.Is(err, files.ErrSymlinkForbidden) {
		t.Fatalf("err = %v", err)
	}
}

func TestSubmitFailureIsRecorded(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.submitter.err = errors.New("transport down")
	if _, err := f.service.CreateTransfer(context.Background(), validInput()); err == nil {
		t.Fatal("expected submit failure")
	}
	transfers, err := f.repo.ListTransfers(context.Background(), 10)
	if err != nil || len(transfers) != 1 {
		t.Fatalf("transfers = %+v, %v", transfers, err)
	}
	if transfers[0].Status != files.TransferFailed || transfers[0].TaskID != "" {
		t.Fatalf("transfer = %+v", transfers[0])
	}
}

func TestTaskUpdateCannotInflateSuccess(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	transfer, err := f.service.CreateTransfer(context.Background(), validInput())
	if err != nil {
		t.Fatal(err)
	}
	updated, err := f.service.ApplyTaskUpdate(context.Background(), transfer.TaskID, files.TaskUpdate{
		Status: files.TransferCompleted, Progress: 100,
		Items: []files.ItemUpdate{{SourcePath: "/etc/app/config.yml", Status: files.ItemFailed, ErrorMessage: "permission denied"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != files.TransferPartialFailure {
		t.Fatalf("status = %s", updated.Status)
	}
	completed, err := f.service.ApplyTaskUpdate(context.Background(), transfer.TaskID, files.TaskUpdate{
		Status: files.TransferCompleted, Progress: 100,
		Items: []files.ItemUpdate{{SourcePath: "/etc/app/config.yml", DestinationPath: "/srv/app/config.yml", Status: files.ItemCopied, BytesCopied: 10}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if completed.Status != files.TransferCompleted || len(completed.Items) != 1 || completed.Items[0].Status != files.ItemCopied {
		t.Fatalf("completed = %+v", completed)
	}
}

func TestUploadResumeChecksumAndTransportRetry(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	body := []byte("hello upload")
	digestBytes := sha256.Sum256(body)
	digest := hex.EncodeToString(digestBytes[:])
	size := int64(len(body))
	upload, err := f.service.CreateUpload(context.Background(), "user-1", "req-up", "n-dst", "/srv/app/hello.txt", &size, digest)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.AppendChunk(context.Background(), "user-1", upload.ID, 0, body[:5]); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.AppendChunk(context.Background(), "user-1", upload.ID, 0, body[:5]); !errors.Is(err, files.ErrOffsetConflict) {
		t.Fatalf("offset conflict err = %v", err)
	}
	if _, err := f.service.AppendChunk(context.Background(), "user-1", upload.ID, 5, body[5:]); err != nil {
		t.Fatal(err)
	}
	f.upload.err = errors.New("agent disconnected")
	if _, err := f.service.CompleteUpload(context.Background(), "user-1", "req-up", upload.ID); !errors.Is(err, files.ErrTransportFailure) {
		t.Fatalf("transport err = %v", err)
	}
	retried, err := f.service.GetUpload(context.Background(), "user-1", upload.ID)
	if err != nil || retried.Status != files.UploadOpen {
		t.Fatalf("retry state = %+v, %v", retried, err)
	}
	f.upload.err = nil
	completed, err := f.service.CompleteUpload(context.Background(), "user-1", "req-up", upload.ID)
	if err != nil || completed.Status != files.UploadCompleted {
		t.Fatalf("complete = %+v, %v", completed, err)
	}
	if !bytes.Equal(f.upload.data, body) || f.upload.digest != digest || f.upload.size != size {
		t.Fatalf("transport got %q digest=%s size=%d", f.upload.data, f.upload.digest, f.upload.size)
	}
	staging := filepath.Join(f.staging, upload.StagingName)
	if _, err := os.Stat(staging); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("staging file still present: %v", err)
	}
}

func TestUploadChecksumMismatchFails(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	body := []byte("data")
	size := int64(len(body))
	expected := strings.Repeat("0", 64)
	upload, err := f.service.CreateUpload(context.Background(), "user-1", "req-up", "n-dst", "/srv/a.txt", &size, expected)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.AppendChunk(context.Background(), "user-1", upload.ID, 0, body); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.CompleteUpload(context.Background(), "user-1", "req-up", upload.ID); !errors.Is(err, files.ErrChecksumMismatch) {
		t.Fatalf("err = %v", err)
	}
	stored, err := f.service.GetUpload(context.Background(), "user-1", upload.ID)
	if err != nil || stored.Status != files.UploadFailed {
		t.Fatalf("stored = %+v, %v", stored, err)
	}
	if f.upload.calls != 0 {
		t.Fatal("mismatched upload reached the transport")
	}
}

func TestDownloadAuthorizationAndTransport(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	var buffer bytes.Buffer
	written, err := f.service.Download(context.Background(), "user-1", "req-dl", "n-src", "/var/log/app.log", &buffer)
	if err != nil || written != int64(len("remote-bytes")) || buffer.String() != "remote-bytes" {
		t.Fatalf("download = %d %q %v", written, buffer.String(), err)
	}
	denied, err := files.NewService(files.Options{
		DB: f.db, Repo: f.repo, Audit: f.audit,
		Authorizer: authorizerFunc(func(context.Context, auth.Check) error { return auth.ErrDenied }),
		Downloads:  f.download, StagingDir: t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	before := f.download.calls
	if _, err := denied.Download(context.Background(), "user-1", "req-dl", "n-src", "/var/log/app.log", &buffer); !errors.Is(err, auth.ErrDenied) {
		t.Fatalf("denied err = %v", err)
	}
	if f.download.calls != before {
		t.Fatal("denied download reached the transport")
	}
}

func TestCancelTransferUsesUnifiedTask(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	transfer, err := f.service.CreateTransfer(context.Background(), validInput())
	if err != nil {
		t.Fatal(err)
	}
	cancelled, err := f.service.CancelTransfer(context.Background(), "user-1", "req-cancel", transfer.ID)
	if err != nil {
		t.Fatal(err)
	}
	if cancelled.Status != files.TransferCancelling {
		t.Fatalf("status = %s", cancelled.Status)
	}
	if f.cancellerPtr.taskID != "task-1" {
		t.Fatalf("canceller saw %q", f.cancellerPtr.taskID)
	}
}

func TestRecoverPendingFailsUnsubmittedTransfer(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	now := time.Now().UTC()
	err := f.repo.CreateTransfer(context.Background(), f.db, files.Transfer{
		ID: "xfer_orphan", SubmitterID: "user-1", SourceNodeID: "n-src",
		DestinationNodeID: "n-dst", DestinationDir: "/srv", ConflictPolicy: files.ConflictSkip,
		Status: files.TransferQueued, RequestID: "req-1", CreatedAt: now, UpdatedAt: now,
		Items: []files.TransferItem{{Sequence: 0, SourcePath: "/a", Status: files.ItemQueued}},
	})
	if err != nil {
		t.Fatal(err)
	}
	recovered, err := f.service.RecoverPending(context.Background())
	if err != nil || recovered != 1 {
		t.Fatalf("recovered = %d, %v", recovered, err)
	}
	transfer, err := f.repo.GetTransfer(context.Background(), "xfer_orphan")
	if err != nil || transfer.Status != files.TransferFailed {
		t.Fatalf("transfer = %+v, %v", transfer, err)
	}
}
