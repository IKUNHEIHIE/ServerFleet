package files_test

import (
	"context"
	"serverfleet.local/serverfleet/internal/auth"
	"serverfleet.local/serverfleet/internal/files"
	"testing"
)

type cancelR2Func func(context.Context, string, string, string) error

func TestQueuedCancelTerminatesAndPreventsWorkerClaim(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	tr, err := f.service.CreateTransfer(ctx, validInput())
	if err != nil {
		t.Fatal(err)
	}
	got, err := f.service.CancelTransfer(ctx, "user-1", "review-queued-cancel", tr.ID)
	if err != nil || got.Status != files.TransferCanceled || got.CompletedAt.IsZero() {
		t.Fatalf("queued cancel did not terminate: %+v error=%v", got, err)
	}
	_, claimed, err := f.repo.ClaimTransfer(ctx, tr.TaskID, got.CompletedAt)
	if err != nil || claimed {
		t.Fatalf("canceled transfer became runnable: claimed=%v error=%v", claimed, err)
	}
}

func (f cancelR2Func) CancelTask(ctx context.Context, task, subject, request string) error {
	return f(ctx, task, subject, request)
}

func TestCancelReadSourceAndConcurrentTerminalPreserved(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	tr, err := f.service.CreateTransfer(ctx, validInput())
	if err != nil {
		t.Fatal(err)
	}
	authorizer := authorizerFunc(func(_ context.Context, c auth.Check) error {
		if c.Action == files.ActionWrite && len(c.NodeIDs) == 1 && c.NodeIDs[0] == "n-src" {
			return auth.ErrDenied
		}
		return nil
	})
	service, err := files.NewService(files.Options{DB: f.db, Repo: f.repo, Audit: f.audit, Authorizer: authorizer, Submitter: f.submitter, StagingDir: f.staging, Canceller: cancelR2Func(func(ctx context.Context, _, _, _ string) error {
		_, e := f.service.ApplyTaskUpdate(ctx, tr.TaskID, files.TaskUpdate{Status: files.TransferFailed, Message: "uncertain write evidence", Items: []files.ItemUpdate{{SourcePath: "/etc/app/config.yml", Status: files.ItemFailed, ErrorCode: "agent_unknown"}}})
		return e
	})})
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.CancelTransfer(ctx, "user-1", "r2-cancel", tr.ID)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != files.TransferFailed || result.ErrorMessage != "uncertain write evidence" || result.Items[0].ErrorCode != "agent_unknown" || result.CompletedAt.IsZero() {
		t.Fatalf("cancel overwrote terminal evidence: %+v", result)
	}
}

func TestRestartFencesCancellingTransferWithoutReplay(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	tr, err := f.service.CreateTransfer(ctx, validInput())
	if err != nil {
		t.Fatal(err)
	}
	if _, claimed, err := f.repo.ClaimTransfer(ctx, tr.TaskID, tr.CreatedAt); err != nil || !claimed {
		t.Fatal("running fixture not claimed", claimed, err)
	}
	if _, err = f.service.CancelTransfer(ctx, "user-1", "r2-cancel", tr.ID); err != nil {
		t.Fatal(err)
	}
	n, err := f.service.RecoverTransfers(ctx)
	if err != nil || n != 1 {
		t.Fatal(n, err)
	}
	got, err := f.repo.GetTransfer(ctx, tr.ID)
	if err != nil || !got.Status.Terminal() || got.Items[0].ErrorCode != "controller_restarted_unknown" {
		t.Fatal(got, err)
	}
	n, err = f.service.RecoverTransfers(ctx)
	if err != nil || n != 0 || len(f.submitter.commands) != 1 {
		t.Fatal("recovery replayed", n, err)
	}
}
