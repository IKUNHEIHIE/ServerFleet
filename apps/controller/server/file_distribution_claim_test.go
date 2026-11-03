package server

import (
	"context"
	"testing"
	"time"

	"serverfleet.local/serverfleet/internal/files"
	"serverfleet.local/serverfleet/internal/storage"
	"serverfleet.local/serverfleet/internal/storage/testkit"
	"serverfleet.local/serverfleet/internal/tasks"
)

type distributionForbiddenReader struct{}

func (distributionForbiddenReader) Read([]byte) (int, error) {
	panic("a target without a fresh claim must not read or send any bytes")
}

func TestDistributionWorkerDoesNotExecuteWithoutFreshClaim(t *testing.T) {
	for _, state := range []string{files.TargetStateCompleted, files.TargetStateFailed, files.TargetStateCancelled, files.TargetStateUnknown, files.TargetStateDispatching} {
		t.Run(state, func(t *testing.T) {
			ctx := context.Background()
			db := testkit.NewDB(t, files.Migrations())
			repo, err := files.NewRepo(db)
			if err != nil {
				t.Fatal(err)
			}
			d := files.Distribution{ID: "dist-review", SubjectID: "owner", NodeID: "node-one", DestinationPath: "/srv/test.bin", Status: files.DistributionOpen, StagingName: "review.part", CreatedAt: time.Now(), UpdatedAt: time.Now(), Targets: []files.DistributionTarget{{NodeID: "node-one", Status: files.TargetStatePending, UpdatedAt: time.Now()}}}
			if err := db.InTx(ctx, func(ctx context.Context, tx storage.Tx) error { return repo.CreateDistribution(ctx, tx, d) }); err != nil {
				t.Fatal(err)
			}
			if moved, err := repo.BindDistributionTask(ctx, db, d.ID, "task-review", ""); err != nil || !moved {
				t.Fatal(moved, err)
			}
			if state == files.TargetStateDispatching {
				if moved, err := repo.SetDistributionTargetState(ctx, d.ID, "node-one", state); err != nil || !moved {
					t.Fatal(moved, err)
				}
			} else if moved, err := repo.FinishDistributionTarget(ctx, db, d.ID, "node-one", state, 17, "", "", "original result"); err != nil || !moved {
				t.Fatal(moved, err)
			}
			// No Server/AgentFiles is installed: any attempt to proceed beyond
			// the failed claim would panic instead of silently contacting a mock.
			w := &FileExecutionWorker{repo: repo}
			code, status, _, _, _ := w.distributeTarget(ctx, tasks.Task{ID: "task-review"}, fileTaskPlan{TransferID: d.ID}, filePlanItem{Node: "node-one"}, 0, distributionForbiddenReader{}, "")
			want := tasks.NodeResultUnknown
			switch state {
			case files.TargetStateCompleted:
				want = tasks.NodeResultSucceeded
			case files.TargetStateFailed:
				want = tasks.NodeResultFailed
			case files.TargetStateCancelled:
				want = tasks.NodeResultCanceled
			}
			if code != want || status != "" {
				t.Fatalf("claimed an existing outcome: code=%s status=%q", code, status)
			}
			rows, err := repo.ListDistributionTargets(ctx, d.ID)
			if err != nil || rows[0].Status != state {
				t.Fatalf("row changed: %+v err=%v", rows, err)
			}
		})
	}
}
