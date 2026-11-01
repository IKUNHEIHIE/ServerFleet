package files_test

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"serverfleet.local/serverfleet/internal/auth"
	"serverfleet.local/serverfleet/internal/files"
)

func TestR1UploadWriterDoesNotNeedRemoteReadAuthority(t *testing.T) {
	f := newFixture(t)
	var revoked atomic.Bool
	writer, err := files.NewService(files.Options{DB: f.db, Repo: f.repo, Audit: f.audit, StagingDir: f.staging,
		Authorizer: authorizerFunc(func(_ context.Context, check auth.Check) error {
			if !revoked.Load() && check.Action == files.ActionWrite {
				return nil
			}
			return auth.ErrDenied
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	if err := files.RegisterHTTP(mux, writer, func(*http.Request) (files.Principal, error) {
		return files.Principal{SubjectID: "user-1", RequestID: "req-writer"}, nil
	}); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(mux)
	defer server.Close()
	content := []byte("write-only upload bytes")
	id := createUploadSession(t, server.URL, "write-only.bin", int64(len(content)), sha256Hex(content))
	base := server.URL + "/api/v1/nodes/n-dst/files/uploads/" + id
	if status, body := doRaw(t, http.MethodPut, base+"/content", content); status != http.StatusOK {
		t.Fatalf("writer unexpectedly needed files.read: %d %s", status, body)
	}
	if status, body := doRaw(t, http.MethodPost, base+"/complete", nil); status != http.StatusServiceUnavailable {
		t.Fatalf("missing channel should fail only after bytes verified: %d %s", status, body)
	}
	if _, err := writer.Download(context.Background(), "user-1", "req-read", "n-dst", "/srv/write-only.bin", new(bytes.Buffer)); !errors.Is(err, auth.ErrDenied) {
		t.Fatal("writer gained remote read authority", err)
	}
	revoked.Store(true)
	if _, err := writer.GetUpload(context.Background(), "user-1", "req-status", id); !errors.Is(err, auth.ErrDenied) {
		t.Fatal("upload metadata remained available after write revocation", err)
	}
}
