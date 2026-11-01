package files_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"serverfleet.local/serverfleet/internal/audit"
	"serverfleet.local/serverfleet/internal/auth"
	"serverfleet.local/serverfleet/internal/files"
	"serverfleet.local/serverfleet/internal/storage/testkit"
	"serverfleet.local/serverfleet/internal/tasks"
)

func TestRepoClaimTransferIsAtomic(t *testing.T) {
	f := newCoordinatorFixture(t, nil)
	transfer := f.createTransfer(t, files.ConflictOverwrite, []string{"/etc/blob.bin"})
	now := time.Now().UTC()
	first, won, err := f.repo.ClaimTransfer(context.Background(), transfer.TaskID, now)
	if err != nil || !won || first.Status != files.TransferRunning {
		t.Fatalf("first claim = %+v won=%v err=%v", first, won, err)
	}
	second, won, err := f.repo.ClaimTransfer(context.Background(), transfer.TaskID, now)
	if err != nil || won || second.Status != files.TransferRunning {
		t.Fatalf("second claim = %+v won=%v err=%v", second, won, err)
	}
}

func TestServiceRecoversRunningTransferConservatively(t *testing.T) {
	f := newCoordinatorFixture(t, nil)
	transfer := f.createTransfer(t, files.ConflictOverwrite, []string{"/etc/first.bin", "/etc/second.bin"})
	claimed, won, err := f.repo.ClaimTransfer(context.Background(), transfer.TaskID, time.Now().UTC())
	if err != nil || !won {
		t.Fatal(err)
	}
	total, copied := int64(10), int64(5)
	items := []files.TransferItem{
		{Sequence: 0, SourcePath: "/etc/first.bin", DestinationPath: "/srv/app/first.bin", Status: files.ItemCopied, BytesCopied: 5},
		{Sequence: 1, SourcePath: "/etc/second.bin", DestinationPath: "/srv/app/second.bin", Status: files.ItemCopying},
	}
	if err := f.repo.SaveProgress(context.Background(), claimed.ID, 0.5, &total, &copied, "copying", items, claimed.StartedAt); err != nil {
		t.Fatal(err)
	}
	recovered, err := f.service.RecoverTransfers(context.Background())
	if err != nil || recovered != 1 {
		t.Fatalf("recovered = %d, err = %v", recovered, err)
	}
	reloaded, err := f.repo.GetTransferByTask(context.Background(), transfer.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Status != files.TransferPartialFailure || !strings.Contains(reloaded.ErrorMessage, "result unknown") {
		t.Fatalf("reloaded = %+v", reloaded)
	}
	if reloaded.Items[0].Status != files.ItemCopied {
		t.Fatalf("copied item was lost: %+v", reloaded.Items[0])
	}
	if reloaded.Items[1].Status != files.ItemFailed || reloaded.Items[1].ErrorCode != "controller_restarted_unknown" {
		t.Fatalf("queued item = %+v", reloaded.Items[1])
	}
	page, err := f.audit.Query(context.Background(), audit.Query{Action: files.ActionTransferRecover})
	if err != nil || page.Total != 1 {
		t.Fatalf("recovery audit = %+v, %v", page, err)
	}
}

func TestCoordinatorPersistsItemProgress(t *testing.T) {
	f := newCoordinatorFixture(t, nil)
	f.agent.putFile("node-src", "/etc/first.bin", []byte("first payload"))
	f.agent.putFile("node-src", "/etc/second.bin", []byte("second payload"))
	f.createTransfer(t, files.ConflictOverwrite, []string{"/etc/first.bin", "/etc/second.bin"})
	var persisted bool
	outcome, err := f.coordinator.ExecuteTask(context.Background(), "task-transfer", testTransferExecution(), func(snapshot files.TransferOutcome) {
		if len(snapshot.Items) < 1 || snapshot.Items[0].Status != files.ItemCopied {
			return
		}
		stored, err := f.repo.GetTransferByTask(context.Background(), "task-transfer")
		if err != nil {
			t.Errorf("load progress: %v", err)
			return
		}
		if len(stored.Items) == 2 && stored.Items[0].Status == files.ItemCopied {
			persisted = true
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Status != files.TransferCompleted || !persisted {
		t.Fatalf("outcome = %+v persisted=%v", outcome, persisted)
	}
}

func TestCoordinatorStopsMidChunkOnRevocation(t *testing.T) {
	var mu sync.Mutex
	calls := 0
	denyAfter := 12
	f := newCoordinatorFixture(t, authorizerFunc(func(context.Context, auth.Check) error {
		mu.Lock()
		defer mu.Unlock()
		calls++
		if calls > denyAfter {
			return auth.ErrDenied
		}
		return nil
	}))
	data := randomBytes(t, 64<<10)
	f.agent.putFile("node-src", "/etc/large.bin", data)
	f.createTransfer(t, files.ConflictOverwrite, []string{"/etc/large.bin"})
	outcome, err := f.coordinator.ExecuteTask(context.Background(), "task-transfer", testTransferExecution(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Status == files.TransferCompleted {
		t.Fatal("revoked mid-chunk transfer reported success")
	}
	if _, exists := f.agent.file("node-dst", "/srv/app/large.bin"); exists {
		t.Fatal("revoked transfer committed a destination file")
	}
}

func TestCoordinatorUsesPerNodeExecutionIDsAndColonFreeTransferID(t *testing.T) {
	f := newCoordinatorFixture(t, nil)
	f.agent.putFile("node-src", "/etc/blob.bin", []byte("payload"))
	f.createTransfer(t, files.ConflictOverwrite, []string{"/etc/blob.bin"})
	execution := testTransferExecution()
	execution.SourceExecutionID = "exec-source"
	execution.DestinationExecutionID = "exec-destination"
	if _, err := f.coordinator.ExecuteTask(context.Background(), "task-transfer", execution, nil); err != nil {
		t.Fatal(err)
	}
	if len(f.agent.openCalls) != 2 {
		t.Fatalf("open calls = %+v", f.agent.openCalls)
	}
	if f.agent.openCalls[0].direction != files.ChannelRead || f.agent.openCalls[0].execution != "exec-source" {
		t.Fatalf("source open = %+v", f.agent.openCalls[0])
	}
	if f.agent.openCalls[1].direction != files.ChannelWrite || f.agent.openCalls[1].execution != "exec-destination" {
		t.Fatalf("destination open = %+v", f.agent.openCalls[1])
	}
	if strings.Contains(f.agent.openCalls[0].transfer, ":") || f.agent.openCalls[0].transfer != f.agent.openCalls[1].transfer {
		t.Fatalf("transfer id = %q / %q", f.agent.openCalls[0].transfer, f.agent.openCalls[1].transfer)
	}
}

func TestCoordinatorUnknownCommitIsNotReplayed(t *testing.T) {
	f := newCoordinatorFixture(t, nil)
	f.agent.putFile("node-src", "/etc/blob.bin", []byte("payload"))
	f.agent.unknownCommit = true
	f.createTransfer(t, files.ConflictOverwrite, []string{"/etc/blob.bin"})
	outcome, err := f.coordinator.ExecuteTask(context.Background(), "task-transfer", testTransferExecution(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Items[0].ErrorCode != "agent_unknown" || f.agent.dials != 2 {
		t.Fatalf("item = %+v dials=%d", outcome.Items[0], f.agent.dials)
	}
	if _, exists := f.agent.file("node-dst", "/srv/app/blob.bin"); exists {
		t.Fatal("unknown commit published a file")
	}
}

func TestTransferRejectsSameSourceAndDestination(t *testing.T) {
	f := newFixture(t)
	_, err := f.service.CreateTransfer(context.Background(), files.CreateTransferInput{
		SubjectID: "user-1", RequestID: "req-same",
		SourceNodeID: "n1", DestinationNodeID: "n1",
		SourcePaths: []string{"/etc/blob.bin"}, DestinationDir: "/srv/app",
		ConflictPolicy: files.ConflictOverwrite,
	})
	if !errors.Is(err, files.ErrSameNode) {
		t.Fatalf("err = %v", err)
	}
}

func TestUploadReadAndAppendRecheckCurrentAuthority(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	body := []byte("authorized bytes")
	size := int64(len(body))
	upload, err := f.service.CreateUpload(ctx, "user-1", "req-create", "n-dst", "/srv/blob", &size, "")
	if err != nil {
		t.Fatal(err)
	}
	revoked, err := files.NewService(files.Options{
		DB: f.db, Repo: f.repo, Audit: f.audit, Authorizer: authorizerFunc(func(context.Context, auth.Check) error { return auth.ErrDenied }),
		Uploads: f.upload, StagingDir: f.staging,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := revoked.AppendChunk(ctx, "user-1", "req-append", upload.ID, 0, body); !errors.Is(err, auth.ErrDenied) {
		t.Fatalf("append err = %v", err)
	}
	if _, err := revoked.GetUpload(ctx, "user-1", "req-get", upload.ID); !errors.Is(err, auth.ErrDenied) {
		t.Fatalf("get err = %v", err)
	}
	if _, err := revoked.WriteUploadContent(ctx, "user-1", "req-content", upload.ID, 0, bytes.NewReader(body)); !errors.Is(err, auth.ErrDenied) {
		t.Fatalf("content err = %v", err)
	}
	stored, err := f.service.GetUpload(ctx, "user-1", "req-get", upload.ID)
	if err != nil || stored.BytesReceived != 0 {
		t.Fatalf("stored = %+v, %v", stored, err)
	}
}

type acceptDispatcher struct {
	mu         sync.Mutex
	dispatched []tasks.Command
}

func (d *acceptDispatcher) Dispatch(_ context.Context, command tasks.Command) (tasks.DispatchResult, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.dispatched = append(d.dispatched, command)
	return tasks.DispatchResult{Accepted: true}, nil
}

func (d *acceptDispatcher) Cancel(context.Context, tasks.CancelCommand) error { return nil }

type onlineNodes struct{}

func (onlineNodes) Online(context.Context, string) (bool, error) { return true, nil }

func TestTransferReportsToRealTaskService(t *testing.T) {
	ctx := context.Background()
	db := testkit.NewDB(t, tasks.Migrations(), files.Migrations(), audit.Migrations())
	tasksRepo, err := tasks.NewRepo(db)
	if err != nil {
		t.Fatal(err)
	}
	filesRepo, err := files.NewRepo(db)
	if err != nil {
		t.Fatal(err)
	}
	limits := audit.DefaultLimits()
	limits.RequireSource = true
	actions := append(tasks.ServiceActions(), files.ServiceActions()...)
	validator, err := audit.NewValidator(actions, limits)
	if err != nil {
		t.Fatal(err)
	}
	recorder, err := audit.NewSQLRecorder(db, validator, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	dispatcher := &acceptDispatcher{}
	tasksService, err := tasks.NewService(tasks.Options{
		DB: db, Repo: tasksRepo, Audit: recorder,
		Authorizer: authorizerFunc(func(context.Context, auth.Check) error { return nil }),
		Dispatcher: dispatcher, Nodes: onlineNodes{},
		RequireApproval: func(tasks.OperationClass) bool { return false },
	})
	if err != nil {
		t.Fatal(err)
	}
	task, err := tasksService.Submit(ctx, tasks.SubmitRequest{
		SubjectID: "user-1", RequestID: "req-submit", Name: "file transfer",
		ActionID: files.ActionTransfer, OperationClass: tasks.OperationClassStandard,
		TargetType: "node_ids", TargetNodeIDs: []string{"node-src", "node-dst"},
		Params: map[string]string{"source_node": "node-src", "dest_node": "node-dst"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := tasksService.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if got := len(dispatcher.dispatched); got != 2 {
		t.Fatalf("dispatched = %d", got)
	}
	transfer := files.Transfer{
		ID: "xfer_task_binding", TaskID: task.ID, SubmitterID: "user-1",
		SourceNodeID: "node-src", DestinationNodeID: "node-dst", DestinationDir: "/srv/app",
		ConflictPolicy: files.ConflictOverwrite, Status: files.TransferQueued,
		RequestID: "req-submit", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
		Items: []files.TransferItem{{Sequence: 0, SourcePath: "/etc/blob.bin", DestinationPath: "/srv/app/blob.bin", Status: files.ItemQueued}},
	}
	if err := filesRepo.CreateTransfer(ctx, db, transfer); err != nil {
		t.Fatal(err)
	}
	agent := newFakeAgent()
	agent.putFile("node-src", "/etc/blob.bin", []byte("real task payload"))
	transport := testChannel(t, agent)
	coordinator, err := files.NewTransferCoordinator(files.CoordinatorOptions{
		Repo: filesRepo,
		Transports: files.NodeFileTransportsFunc(func(context.Context, string) (files.NodeTransport, error) {
			return files.NodeTransport{Upload: transport, Download: transport}, nil
		}),
		Authorizer: authorizerFunc(func(context.Context, auth.Check) error { return nil }),
		Admission:  allowAdmission{}, Audit: recorder, SpoolDir: privateSpool(t),
	})
	if err != nil {
		t.Fatal(err)
	}
	filesService, err := files.NewService(files.Options{
		DB: db, Repo: filesRepo, Audit: recorder,
		Authorizer: authorizerFunc(func(context.Context, auth.Check) error { return nil }),
		StagingDir: t.TempDir(), Coordinator: coordinator, Tasks: tasksService,
	})
	if err != nil {
		t.Fatal(err)
	}
	updated, err := filesService.ExecuteTransferTask(ctx, task.ID, files.TransferExecution{
		SubjectID: "user-1", RequestID: "req-submit", ExecutionID: "exec-node",
		PlanDigestSHA256: task.PlanHash,
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != files.TransferCompleted {
		t.Fatalf("transfer = %+v", updated)
	}
	final, err := tasksService.Get(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if final.Status != tasks.StatusCompleted {
		t.Fatalf("task = %+v", final)
	}
}

func TestCoordinatorSpoolCapAndPrivateDirectory(t *testing.T) {
	f := newCoordinatorFixture(t, nil)
	f.agent.putFile("node-src", "/etc/large.bin", randomBytes(t, 2000))
	f.createTransfer(t, files.ConflictOverwrite, []string{"/etc/large.bin"})
	transport := testChannel(t, f.agent)
	coordinator, err := files.NewTransferCoordinator(files.CoordinatorOptions{
		Repo: f.repo,
		Transports: files.NodeFileTransportsFunc(func(context.Context, string) (files.NodeTransport, error) {
			return files.NodeTransport{Upload: transport, Download: transport}, nil
		}),
		Authorizer: authorizerFunc(func(context.Context, auth.Check) error { return nil }),
		Admission:  allowAdmission{}, Audit: f.audit, SpoolDir: privateSpool(t), MaxFileBytes: 1024,
	})
	if err != nil {
		t.Fatal(err)
	}
	outcome, err := coordinator.ExecuteTask(context.Background(), "task-transfer", testTransferExecution(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Items[0].ErrorCode != "file_too_large" {
		t.Fatalf("item = %+v", outcome.Items[0])
	}
	if _, exists := f.agent.file("node-dst", "/srv/app/large.bin"); exists {
		t.Fatal("spool overflow committed a destination file")
	}
}
