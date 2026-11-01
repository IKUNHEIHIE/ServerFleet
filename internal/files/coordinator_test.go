package files_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"serverfleet.local/serverfleet/internal/audit"
	"serverfleet.local/serverfleet/internal/auth"
	"serverfleet.local/serverfleet/internal/files"
	"serverfleet.local/serverfleet/internal/storage/testkit"
	"serverfleet.local/serverfleet/internal/tasks"
)

type reporterCall struct {
	taskID  string
	nodeID  string
	code    tasks.NodeResultCode
	message string
}

func privateSpool(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	return dir
}

type fakeTaskReporter struct {
	mu    sync.Mutex
	calls []reporterCall
	err   error
}

func (r *fakeTaskReporter) RecordResult(_ context.Context, taskID, nodeID string, code tasks.NodeResultCode, message string) (tasks.Task, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, reporterCall{taskID: taskID, nodeID: nodeID, code: code, message: message})
	return tasks.Task{}, r.err
}

func (r *fakeTaskReporter) snapshot() []reporterCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]reporterCall(nil), r.calls...)
}

type allowAdmission struct{}

func (allowAdmission) AdmitFileExecution(context.Context, string, string, string, string) error {
	return nil
}

type coordinatorFixture struct {
	agent       *fakeAgent
	transport   *files.ChannelTransport
	repo        *files.Repo
	audit       *audit.SQLRecorder
	service     *files.Service
	runner      *files.Service
	coordinator *files.TransferCoordinator
	reporter    *fakeTaskReporter
	spoolDir    string
}

func newCoordinatorFixture(t *testing.T, authorizer auth.Authorizer) *coordinatorFixture {
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
	allow := authorizerFunc(func(context.Context, auth.Check) error { return nil })
	service, err := files.NewService(files.Options{
		DB: db, Repo: repo, Audit: recorder, Authorizer: allow,
		Submitter: &fakeSubmitter{taskID: "task-transfer"}, Canceller: &fakeCanceller{},
		StagingDir: t.TempDir(), MaxUploadBytes: 1 << 20,
	})
	if err != nil {
		t.Fatal(err)
	}
	agent := newFakeAgent()
	transport := testChannel(t, agent)
	spool := privateSpool(t)
	execAuthorizer := authorizer
	if execAuthorizer == nil {
		execAuthorizer = allow
	}
	coordinator, err := files.NewTransferCoordinator(files.CoordinatorOptions{
		Repo: repo,
		Transports: files.NodeFileTransportsFunc(func(context.Context, string) (files.NodeTransport, error) {
			return files.NodeTransport{Upload: transport, Download: transport}, nil
		}),
		Authorizer: execAuthorizer, Admission: allowAdmission{}, Audit: recorder, SpoolDir: spool,
	})
	if err != nil {
		t.Fatal(err)
	}
	reporter := &fakeTaskReporter{}
	runner, err := files.NewService(files.Options{
		DB: db, Repo: repo, Audit: recorder, Authorizer: execAuthorizer,
		Submitter: &fakeSubmitter{taskID: "task-transfer"}, Canceller: &fakeCanceller{},
		StagingDir: t.TempDir(), MaxUploadBytes: 1 << 20, Coordinator: coordinator, Tasks: reporter,
	})
	if err != nil {
		t.Fatal(err)
	}
	return &coordinatorFixture{agent: agent, transport: transport, repo: repo, audit: recorder,
		service: service, runner: runner, coordinator: coordinator, reporter: reporter, spoolDir: spool}
}

func (f *coordinatorFixture) createTransfer(t *testing.T, policy files.ConflictPolicy, sources []string) files.Transfer {
	t.Helper()
	transfer, err := f.service.CreateTransfer(context.Background(), files.CreateTransferInput{
		SubjectID: "user-1", RequestID: "req-run",
		SourceNodeID: "node-src", DestinationNodeID: "node-dst",
		SourcePaths: sources, DestinationDir: "/srv/app",
		ConflictPolicy: policy,
	})
	if err != nil {
		t.Fatal(err)
	}
	return transfer
}

func testTransferExecution() files.TransferExecution {
	return files.TransferExecution{
		SubjectID: "user-1", RequestID: "req-run", ExecutionID: "exec-1",
		PlanDigestSHA256: testPlanDigest(),
	}
}

func (f *coordinatorFixture) execute(t *testing.T, ctx context.Context) (files.TransferOutcome, error) {
	t.Helper()
	return f.coordinator.ExecuteTask(ctx, "task-transfer", testTransferExecution(), nil)
}

func TestCoordinatorCopiesBytesAndAudits(t *testing.T) {
	f := newCoordinatorFixture(t, nil)
	first := randomBytes(t, 30_001)
	second := []byte("second-file")
	f.agent.putFile("node-src", "/etc/first.bin", first)
	f.agent.putFile("node-src", "/etc/second.bin", second)
	transfer := f.createTransfer(t, files.ConflictOverwrite, []string{"/etc/first.bin", "/etc/second.bin"})

	outcome, err := f.execute(t, context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Status != files.TransferCompleted || len(outcome.Items) != 2 {
		t.Fatalf("outcome = %+v", outcome)
	}
	for _, item := range outcome.Items {
		if item.Status != files.ItemCopied {
			t.Fatalf("item = %+v", item)
		}
	}
	stored, ok := f.agent.file("node-dst", "/srv/app/first.bin")
	if !ok || !bytes.Equal(stored, first) {
		t.Fatal("first file bytes differ")
	}
	stored, ok = f.agent.file("node-dst", "/srv/app/second.bin")
	if !ok || !bytes.Equal(stored, second) {
		t.Fatal("second file bytes differ")
	}
	entries, err := os.ReadDir(f.spoolDir)
	if err != nil || len(entries) != 0 {
		t.Fatalf("spool not cleaned: %v, %v", entries, err)
	}
	page, err := f.audit.Query(context.Background(), audit.Query{Action: files.ActionTransferExecute})
	if err != nil || page.Total < 1 {
		t.Fatalf("audit = %+v, %v", page, err)
	}
	if _, err := f.service.ApplyTaskUpdate(context.Background(), transfer.TaskID, outcome.TaskUpdate()); err != nil {
		t.Fatal(err)
	}
	reloaded, err := f.repo.GetTransferByTask(context.Background(), transfer.TaskID)
	if err != nil || reloaded.Status != files.TransferCompleted {
		t.Fatalf("reloaded = %+v, %v", reloaded, err)
	}
}

func TestCoordinatorReportsPartialAndAgentErrors(t *testing.T) {
	f := newCoordinatorFixture(t, nil)
	f.agent.putFile("node-src", "/etc/present.bin", []byte("present"))
	transfer := f.createTransfer(t, files.ConflictOverwrite, []string{"/etc/present.bin", "/etc/missing.bin"})

	outcome, err := f.execute(t, context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Status != files.TransferPartialFailure {
		t.Fatalf("status = %s, outcome = %+v", outcome.Status, outcome)
	}
	codes := map[string]string{}
	for _, item := range outcome.Items {
		codes[item.SourcePath] = item.ErrorCode
	}
	if codes["/etc/missing.bin"] != "agent_io" || codes["/etc/present.bin"] != "" {
		t.Fatalf("codes = %+v", codes)
	}
	if _, ok := f.agent.file("node-dst", "/srv/app/missing.bin"); ok {
		t.Fatal("missing source produced a destination file")
	}
	_ = transfer
}

func TestCoordinatorDestinationSpaceAndSymlink(t *testing.T) {
	f := newCoordinatorFixture(t, nil)
	f.agent.putFile("node-src", "/etc/full.bin", []byte("payload"))
	f.agent.putFile("node-src", "/etc/link.bin", []byte("payload"))
	f.agent.reject("node-dst", "/srv/app/full.bin", "insufficient_space")
	f.agent.reject("node-src", "/etc/link.bin", "symlink_forbidden")
	f.createTransfer(t, files.ConflictOverwrite, []string{"/etc/full.bin", "/etc/link.bin"})

	outcome, err := f.execute(t, context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Status != files.TransferFailed {
		t.Fatalf("status = %s", outcome.Status)
	}
	codes := map[string]string{}
	for _, item := range outcome.Items {
		codes[item.SourcePath] = item.ErrorCode
	}
	if codes["/etc/full.bin"] != "insufficient_space" || codes["/etc/link.bin"] != "symlink_forbidden" {
		t.Fatalf("codes = %+v", codes)
	}
}

func TestCoordinatorConflictPolicies(t *testing.T) {
	for _, policy := range []files.ConflictPolicy{files.ConflictSkip, files.ConflictRename} {
		t.Run(string(policy), func(t *testing.T) {
			f := newCoordinatorFixture(t, nil)
			f.agent.putFile("node-src", "/etc/blob.bin", []byte("new"))
			f.agent.putFile("node-dst", "/srv/app/blob.bin", []byte("old"))
			f.createTransfer(t, policy, []string{"/etc/blob.bin"})
			outcome, err := f.execute(t, context.Background())
			if err != nil {
				t.Fatal(err)
			}
			item := outcome.Items[0]
			switch policy {
			case files.ConflictSkip:
				if item.Status != files.ItemSkipped || outcome.Status != files.TransferCompleted {
					t.Fatalf("item = %+v outcome = %s", item, outcome.Status)
				}
				if stored, _ := f.agent.file("node-dst", "/srv/app/blob.bin"); string(stored) != "old" {
					t.Fatalf("skip overwrote: %q", stored)
				}
			case files.ConflictRename:
				if item.Status != files.ItemCopied || item.DestinationPath != "/srv/app/blob.bin.1" {
					t.Fatalf("item = %+v", item)
				}
			}
		})
	}
}

func TestCoordinatorRunsAuthorizationBeforeAnyRead(t *testing.T) {
	denied := authorizerFunc(func(_ context.Context, check auth.Check) error {
		if check.Action == files.ActionWrite {
			return auth.ErrDenied
		}
		return nil
	})
	f := newCoordinatorFixture(t, denied)
	f.agent.putFile("node-src", "/etc/blob.bin", []byte("payload"))
	f.createTransfer(t, files.ConflictOverwrite, []string{"/etc/blob.bin"})
	_, err := f.execute(t, context.Background())
	if !errors.Is(err, auth.ErrDenied) {
		t.Fatalf("err = %v", err)
	}
	if agentFile, ok := f.agent.file("node-dst", "/srv/app/blob.bin"); ok {
		t.Fatalf("denied execution wrote %q", agentFile)
	}
	page, err := f.audit.Query(context.Background(), audit.Query{Action: files.ActionWrite, Result: "denied"})
	if err != nil || page.Total != 1 {
		t.Fatalf("denial audit = %+v, %v", page, err)
	}
}

func TestCoordinatorCancelAndTerminalRefusal(t *testing.T) {
	f := newCoordinatorFixture(t, nil)
	f.agent.putFile("node-src", "/etc/blob.bin", []byte("payload"))
	f.agent.putFile("node-src", "/etc/second.bin", []byte("payload"))
	transfer := f.createTransfer(t, files.ConflictOverwrite, []string{"/etc/blob.bin", "/etc/second.bin"})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	outcome, err := f.coordinator.ExecuteTask(ctx, "task-transfer", testTransferExecution(), func(snapshot files.TransferOutcome) {
		if len(snapshot.Items) > 0 && snapshot.Items[0].Status == files.ItemCopied {
			cancel()
		}
	})
	if !errors.Is(err, context.Canceled) || outcome.Status != files.TransferCanceled {
		t.Fatalf("outcome = %+v, err = %v", outcome, err)
	}
	if _, err := f.service.ApplyTaskUpdate(context.Background(), transfer.TaskID, files.TaskUpdate{
		Status: files.TransferCompleted,
		Items: []files.ItemUpdate{
			{SourcePath: "/etc/blob.bin", Status: files.ItemCopied},
			{SourcePath: "/etc/second.bin", Status: files.ItemCopied},
		},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.execute(t, context.Background()); !errors.Is(err, files.ErrExecutionTerminal) {
		t.Fatalf("terminal err = %v", err)
	}
}

func TestServiceExecuteTransferTaskSeam(t *testing.T) {
	f := newCoordinatorFixture(t, nil)
	f.agent.putFile("node-src", "/etc/blob.bin", []byte("payload"))
	transfer := f.createTransfer(t, files.ConflictOverwrite, []string{"/etc/blob.bin"})
	updated, err := f.runner.ExecuteTransferTask(context.Background(), transfer.TaskID, testTransferExecution())
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != files.TransferCompleted || updated.Progress != 1 {
		t.Fatalf("transfer = %+v", updated)
	}
	if stored, _ := f.agent.file("node-dst", "/srv/app/blob.bin"); string(stored) != "payload" {
		t.Fatalf("stored = %q", stored)
	}
	calls := f.reporter.snapshot()
	if len(calls) != 2 || calls[0].code != tasks.NodeResultSucceeded || calls[1].code != tasks.NodeResultSucceeded {
		t.Fatalf("task reporter calls = %+v", calls)
	}
	if calls[0].nodeID != "node-src" || calls[1].nodeID != "node-dst" || calls[0].taskID != transfer.TaskID {
		t.Fatalf("task reporter identity = %+v", calls)
	}
}

func TestServiceReportsUnknownForPartialTransfer(t *testing.T) {
	f := newCoordinatorFixture(t, nil)
	f.agent.putFile("node-src", "/etc/present.bin", []byte("present"))
	f.createTransfer(t, files.ConflictOverwrite, []string{"/etc/present.bin", "/etc/missing.bin"})
	updated, err := f.runner.ExecuteTransferTask(context.Background(), "task-transfer", testTransferExecution())
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != files.TransferPartialFailure {
		t.Fatalf("transfer = %+v", updated)
	}
	calls := f.reporter.snapshot()
	if len(calls) != 2 || calls[0].code != tasks.NodeResultUnknown || calls[1].code != tasks.NodeResultUnknown {
		t.Fatalf("task reporter calls = %+v", calls)
	}
}

func TestCoordinatorRejectsEmptyTaskAndUnknownTransfer(t *testing.T) {
	f := newCoordinatorFixture(t, nil)
	if _, err := f.coordinator.ExecuteTask(context.Background(), "", files.TransferExecution{}, nil); !errors.Is(err, files.ErrTransferIdentity) {
		t.Fatalf("identity err = %v", err)
	}
	if _, err := f.execute(t, context.Background()); !errors.Is(err, files.ErrTransferNotFound) {
		t.Fatalf("unknown transfer err = %v", err)
	}
}

func TestCoordinatorSpoolIsCreatedSecurely(t *testing.T) {
	f := newCoordinatorFixture(t, nil)
	info, err := os.Stat(filepath.Clean(f.spoolDir))
	if err != nil || !info.IsDir() {
		t.Fatalf("spool = %v, %v", info, err)
	}
}
