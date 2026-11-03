package files_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"serverfleet.local/serverfleet/internal/audit"
	"serverfleet.local/serverfleet/internal/files"
	"serverfleet.local/serverfleet/internal/storage"
)

func TestReviewR2DistributionTerminalTargetCannotReopen(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	size := int64(1)
	d, err := f.service.CreateDistribution(ctx, "user-1", "review-create", []string{"n-a", "n-b"}, "/srv/test.bin", &size, "")
	if err != nil {
		t.Fatal(err)
	}
	if moved, err := f.repo.FinishDistributionTarget(ctx, f.db, d.ID, "n-a", files.TargetStateCompleted, 1, strings.Repeat("a", 64), "", ""); err != nil || !moved {
		t.Fatal(moved, err)
	}
	moved, err := f.repo.SetDistributionTargetState(ctx, d.ID, "n-a", files.TargetStateDispatching)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := f.repo.ListDistributionTargets(ctx, d.ID)
	if err != nil {
		t.Fatal(err)
	}
	if moved || rows[0].Status != files.TargetStateCompleted {
		t.Fatalf("completed target reopened: moved=%v row=%+v", moved, rows[0])
	}
}

func TestReviewR2DistributionBadChecksumClosesSession(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	body := []byte("x")
	size := int64(len(body))
	d, err := f.service.CreateDistribution(ctx, "user-1", "review-create", []string{"n-a"}, "/srv/test.bin", &size, strings.Repeat("0", 64))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.service.WriteDistributionContent(ctx, "user-1", "review-write", d.ID, 0, bytes.NewReader(body)); err != nil {
		t.Fatal(err)
	}
	if _, err = f.service.CompleteDistribution(ctx, "user-1", "review-complete", d.ID); !errors.Is(err, files.ErrChecksumMismatch) {
		t.Fatal(err)
	}
	got, err := f.repo.GetDistribution(ctx, d.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != files.DistributionFailed {
		t.Fatalf("checksum-rejected distribution retained invalid open state: %+v", got)
	}
	if got.SHA256Actual == "" || got.ErrorCode != "checksum_mismatch" || got.TaskID != "" {
		t.Fatalf("rejection lost digest/reason or submitted a task: %+v", got)
	}
	if _, err = os.Stat(filepath.Join(f.staging, got.StagingName)); !os.IsNotExist(err) {
		t.Fatalf("committed rejected staging was not removed: %v", err)
	}
	if _, err = f.service.CompleteDistribution(ctx, "user-1", "review-repeat", d.ID); !errors.Is(err, files.ErrDistributionClosed) {
		t.Fatalf("rejected distribution became reusable: %v", err)
	}
}

func TestDistributionClaimIsAtomicAndAllTerminalRowsStayClosed(t *testing.T) {
	for _, terminal := range []string{files.TargetStateCompleted, files.TargetStateFailed, files.TargetStateCancelled, files.TargetStateUnknown} {
		t.Run(terminal, func(t *testing.T) {
			f := newFixture(t)
			ctx := context.Background()
			d, err := f.service.CreateDistribution(ctx, "user-1", "claim-create", []string{"n-a", "n-b"}, "/srv/test.bin", nil, "")
			if err != nil {
				t.Fatal(err)
			}
			if err := f.service.BindDistributionTask(ctx, d, "task-claim", ""); err != nil {
				t.Fatal(err)
			}
			// Race two independent Repo instances over the same durable row.
			other, err := files.NewRepo(f.db)
			if err != nil {
				t.Fatal(err)
			}
			start := make(chan struct{})
			wins := make(chan bool, 2)
			errs := make(chan error, 2)
			var wg sync.WaitGroup
			for _, repo := range []*files.Repo{f.repo, other} {
				wg.Add(1)
				go func(repo *files.Repo) {
					defer wg.Done()
					<-start
					moved, err := repo.SetDistributionTargetState(ctx, d.ID, "n-a", files.TargetStateDispatching)
					wins <- moved
					errs <- err
				}(repo)
			}
			close(start)
			wg.Wait()
			close(wins)
			close(errs)
			count := 0
			for moved := range wins {
				if moved {
					count++
				}
			}
			for err := range errs {
				if err != nil {
					t.Fatal(err)
				}
			}
			if count != 1 {
				t.Fatalf("claims=%d, want exactly one", count)
			}
			if _, err = f.repo.FinishDistributionTarget(ctx, f.db, d.ID, "n-a", terminal, 17, strings.Repeat("a", 64), "original", "original"); err != nil {
				t.Fatal(err)
			}
			if moved, err := other.SetDistributionTargetState(ctx, d.ID, "n-a", files.TargetStateDispatching); err != nil || moved {
				t.Fatalf("terminal reopened: moved=%v err=%v", moved, err)
			}
			if moved, err := other.FinishDistributionTarget(ctx, f.db, d.ID, "n-a", files.TargetStateFailed, 0, "", "late", "late"); err != nil || moved {
				t.Fatalf("late result rewrote terminal: moved=%v err=%v", moved, err)
			}
			rows, err := f.repo.ListDistributionTargets(ctx, d.ID)
			if err != nil || rows[0].Status != terminal || rows[0].Bytes != 17 || rows[0].ErrorCode != "original" {
				t.Fatalf("terminal evidence changed: %+v err=%v", rows, err)
			}
		})
	}
}

type distributionFailureAudit struct {
	files.AuditSink
	fail bool
}

func (a *distributionFailureAudit) AppendTx(ctx context.Context, tx storage.Tx, event audit.Event) error {
	if a.fail && event.Action == files.ActionDistributionComplete {
		return fmt.Errorf("review: audit write unavailable")
	}
	return a.AuditSink.AppendTx(ctx, tx, event)
}

func TestDistributionChecksumAuditFailureRollsBackAndKeepsStaging(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	sink := &distributionFailureAudit{AuditSink: f.audit, fail: true}
	s, err := files.NewService(files.Options{DB: f.db, Repo: f.repo, Audit: sink, Authorizer: f.authorizer, Submitter: f.submitter, Canceller: f.canceller, StagingDir: f.staging})
	if err != nil {
		t.Fatal(err)
	}
	body := []byte("wrong digest")
	size := int64(len(body))
	d, err := s.CreateDistribution(ctx, "user-1", "audit-create", []string{"n-a"}, "/srv/test.bin", &size, strings.Repeat("0", 64))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.WriteDistributionContent(ctx, "user-1", "audit-write", d.ID, 0, bytes.NewReader(body)); err != nil {
		t.Fatal(err)
	}
	if _, err = s.CompleteDistribution(ctx, "user-1", "audit-complete", d.ID); err == nil || errors.Is(err, files.ErrChecksumMismatch) {
		t.Fatalf("audit failure was hidden: %v", err)
	}
	stored, err := f.repo.GetDistribution(ctx, d.ID)
	if err != nil || stored.Status != files.DistributionOpen || stored.ErrorCode != "" {
		t.Fatalf("audit rollback failed: %+v %v", stored, err)
	}
	staged, err := os.ReadFile(filepath.Join(f.staging, stored.StagingName))
	if err != nil || !bytes.Equal(staged, body) {
		t.Fatalf("staging lost before committed audit: %q %v", staged, err)
	}
	sink.fail = false
	if _, err = s.CompleteDistribution(ctx, "user-1", "audit-retry", d.ID); !errors.Is(err, files.ErrChecksumMismatch) {
		t.Fatalf("local verification retry failed: %v", err)
	}
	stored, err = f.repo.GetDistribution(ctx, d.ID)
	if err != nil || stored.Status != files.DistributionFailed {
		t.Fatalf("rejection not committed: %+v %v", stored, err)
	}
}
