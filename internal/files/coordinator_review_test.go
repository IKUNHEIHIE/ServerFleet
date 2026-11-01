package files_test

import (
	"context"
	"sync/atomic"
	"testing"

	"serverfleet.local/serverfleet/internal/auth"
	"serverfleet.local/serverfleet/internal/files"
)

func TestWave6CoordinatorStopsAfterRevocationBetweenItems(t *testing.T) {
	var revoked atomic.Bool
	f := newCoordinatorFixture(t, authorizerFunc(func(context.Context, auth.Check) error {
		if revoked.Load() {
			return auth.ErrDenied
		}
		return nil
	}))
	f.agent.putFile("node-src", "/etc/first.bin", []byte("first payload"))
	f.agent.putFile("node-src", "/etc/second.bin", []byte("second payload"))
	f.createTransfer(t, files.ConflictOverwrite, []string{"/etc/first.bin", "/etc/second.bin"})
	outcome, err := f.coordinator.ExecuteTask(context.Background(), "task-transfer", testTransferExecution(), func(snapshot files.TransferOutcome) {
		if len(snapshot.Items) > 0 && snapshot.Items[0].Status == files.ItemCopied {
			revoked.Store(true)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := f.agent.file("node-dst", "/srv/app/second.bin"); exists {
		t.Fatal("second file was written after current permission was revoked")
	}
	if outcome.Status == files.TransferCompleted {
		t.Fatal("revoked transfer reported success")
	}
}
