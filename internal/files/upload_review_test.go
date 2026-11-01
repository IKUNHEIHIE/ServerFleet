package files_test

import (
	"context"
	"errors"
	"testing"

	"serverfleet.local/serverfleet/internal/audit"
	"serverfleet.local/serverfleet/internal/auth"
	"serverfleet.local/serverfleet/internal/files"
)

func TestWave6UnavailableUploadTransportAuditsFailureAndKeepsStagedBytes(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	body := []byte("synthetic audit payload")
	size := int64(len(body))
	upload, err := f.service.CreateUpload(ctx, "user-1", "create-upload", "n-dst", "/srv/blob", &size, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.service.AppendChunk(ctx, "user-1", "req-append", upload.ID, 0, body); err != nil {
		t.Fatal(err)
	}
	withoutTransport, err := files.NewService(files.Options{DB: f.db, Repo: f.repo, Audit: f.audit, Authorizer: f.authorizer, StagingDir: f.staging})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = withoutTransport.CompleteUpload(ctx, "user-1", "complete-upload", upload.ID); !errors.Is(err, files.ErrTransportAbsent) {
		t.Fatal(err)
	}
	page, err := f.audit.Query(ctx, audit.Query{Action: files.ActionUploadComplete})
	if err != nil || page.Total != 1 {
		t.Fatalf("missing failed completion audit: %d %v", page.Total, err)
	}
	if page.Items[0].Result != "failure" {
		t.Fatal("unavailable transport reported success")
	}
	stored, err := f.service.GetUpload(ctx, "user-1", "req-get", upload.ID)
	if err != nil || stored.Status != files.UploadOpen || stored.BytesReceived != size {
		t.Fatal("failure lost staging or reported completion")
	}
	if f.upload.calls != 0 {
		t.Fatal("missing channel fell back to another transport")
	}
}

func TestWave6UploadCompletionRechecksCurrentWritePermission(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	body := []byte("synthetic revocation payload")
	size := int64(len(body))
	upload, err := f.service.CreateUpload(ctx, "user-1", "create-upload", "n-dst", "/srv/blob", &size, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.service.AppendChunk(ctx, "user-1", "req-append", upload.ID, 0, body); err != nil {
		t.Fatal(err)
	}
	revoked, err := files.NewService(files.Options{DB: f.db, Repo: f.repo, Audit: f.audit, Authorizer: authorizerFunc(func(context.Context, auth.Check) error { return auth.ErrDenied }), Uploads: f.upload, StagingDir: f.staging})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = revoked.CompleteUpload(ctx, "user-1", "complete-upload", upload.ID); !errors.Is(err, auth.ErrDenied) {
		t.Fatal("revoked write reached completion", err)
	}
	if f.upload.calls != 0 {
		t.Fatal("revoked write reached node transport")
	}
}
