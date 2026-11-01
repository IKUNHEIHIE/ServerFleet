package server

import (
	"context"
	"errors"
	"path/filepath"
	"serverfleet.local/serverfleet/internal/audit"
	"serverfleet.local/serverfleet/internal/auth"
	"serverfleet.local/serverfleet/internal/gateway"
	"serverfleet.local/serverfleet/internal/storage"
	"serverfleet.local/serverfleet/internal/tasks"
	"testing"
	"time"
)

type reviewGatewayAuth bool

func (a reviewGatewayAuth) Authorize(context.Context, auth.Check) error {
	if a {
		return nil
	}
	return auth.ErrDenied
}

type reviewGatewayUploads struct{ calls int }

func (u *reviewGatewayUploads) Upload(context.Context, string, string, []byte, string) error {
	u.calls++
	return nil
}
func TestGatewayArtifactRequiresAuthorizedRunningTask(t *testing.T) {
	for _, allow := range []bool{false, true} {
		t.Run(map[bool]string{false: "denied", true: "awaiting_approval"}[allow], func(t *testing.T) {
			ctx := context.Background()
			db, err := storage.Open(ctx, storage.Config{Path: filepath.Join(t.TempDir(), "tasks.db")})
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			migrator, err := storage.NewMigrator(tasks.Migrations(), audit.Migrations())
			if err != nil {
				t.Fatal(err)
			}
			if _, err = migrator.Apply(ctx, db); err != nil {
				t.Fatal(err)
			}
			repo, err := tasks.NewRepo(db)
			if err != nil {
				t.Fatal(err)
			}
			validator, err := audit.NewValidator(AuditActions(), audit.DefaultLimits())
			if err != nil {
				t.Fatal(err)
			}
			recorder, err := audit.NewSQLRecorder(db, validator, time.Now)
			if err != nil {
				t.Fatal(err)
			}
			service, err := tasks.NewService(tasks.Options{DB: db, Repo: repo, Audit: recorder, Authorizer: reviewGatewayAuth(allow)})
			if err != nil {
				t.Fatal(err)
			}
			uploader := new(reviewGatewayUploads)
			host, err := newGatewayTaskHost(service, uploader)
			if err != nil {
				t.Fatal(err)
			}
			site, err := gateway.RenderNginxSite(gateway.NginxSiteRequest{GatewayID: "gw-one", Domain: "app.example.com", Upstream: "127.0.0.1:8080"})
			if err != nil {
				t.Fatal(err)
			}
			_, err = host.apply(ctx, site, gateway.NginxTaskInput{SubjectID: "owner", RequestID: "req-gateway", GatewayID: "gw-one", NodeID: "node-one", IdempotencyKey: "publication-one"})
			if uploader.calls != 0 {
				t.Fatalf("artifact uploaded before authorization/approval: calls=%d error=%v", uploader.calls, err)
			}
			if err == nil {
				t.Fatal("non-executed gateway stage reported success")
			}
			if !allow && !errors.Is(err, auth.ErrDenied) {
				t.Fatal("did not reach current authorization gate", err)
			}
			if allow && !errors.Is(err, tasks.ErrInvalidStatus) {
				t.Fatal("did not stop at pending approval", err)
			}
		})
	}
}
