//go:build linux

package updates

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"serverfleet.local/serverfleet/internal/audit"
	"serverfleet.local/serverfleet/internal/storage"
	"serverfleet.local/serverfleet/internal/storage/testkit"
	"serverfleet.local/serverfleet/internal/tasks"
)

type inertInstallAudit struct{}

func (inertInstallAudit) AppendTx(context.Context, storage.Tx, audit.Event) error { return nil }

func productTestOptions(t *testing.T, dir string) ProductDeploymentOptions {
	t.Helper()
	inbox, stage := filepath.Join(dir, "inbox"), filepath.Join(dir, "stage")
	for _, name := range []string{inbox, stage} {
		if err := os.Mkdir(name, 0700); err != nil {
			t.Fatal(err)
		}
	}
	manifest := ReleaseManifest{Format: 1, Component: "agent", Version: "1.0.0", Sequence: 1, OS: "linux", Architecture: "amd64", SHA256: strings.Repeat("a", 64), Bytes: 1024}
	return ProductDeploymentOptions{
		DB: testkit.NewDB(t), Audit: inertInstallAudit{},
		Directory: dir, Inbox: inbox, StageDir: stage, KeyID: "test-key", NodeID: "test-node",
		IdentityDir: filepath.Join(dir, "identity"), ControllerOrigin: "https://localhost:1", RootCertificate: filepath.Join(dir, "ca.pem"),
		Policy:            ReleasePolicy{Component: "agent", Architecture: "amd64", MaxBytes: 1 << 20},
		Initial:           manifest,
		SchemaVersion:     0,
		SchemaFingerprint: strings.Repeat("d", 64),
		Current:           func(context.Context, string, string) (tasks.Task, error) { return tasks.Task{}, nil },
		Administrator:     func(context.Context, string) error { return nil },
		RuntimeCurrent:    func(context.Context) error { return nil },
		NodeCurrent:       func(context.Context, string, string) error { return nil },
	}
}

// A crash or an interrupted update must never be auto-taken-over on restart.
func TestProductDeploymentRefusesLeftoverUpdateEffects(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("private non-root deployment")
	}
	for _, marker := range []string{"update-lock.json", "supervisor-reservation.json"} {
		t.Run(marker, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.Chmod(dir, 0700); err != nil {
				t.Fatal(err)
			}
			options := productTestOptions(t, dir)
			if err := os.WriteFile(filepath.Join(dir, marker), []byte("leftover"), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := NewProductDeployment(options); !errors.Is(err, ErrUpdateBusy) {
				t.Fatalf("leftover %s was accepted: %v", marker, err)
			}
		})
	}
	t.Run("clean private directories", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.Chmod(dir, 0700); err != nil {
			t.Fatal(err)
		}
		product, err := NewProductDeployment(productTestOptions(t, dir))
		if err != nil {
			t.Fatal(err)
		}
		if err := product.Close(); err != nil {
			t.Fatal(err)
		}
	})
}
