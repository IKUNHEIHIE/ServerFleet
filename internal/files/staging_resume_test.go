package files_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"serverfleet.local/serverfleet/internal/audit"
	"serverfleet.local/serverfleet/internal/auth"
	"serverfleet.local/serverfleet/internal/files"
	"serverfleet.local/serverfleet/internal/storage/testkit"
)

type stagingResumeSubmitter struct{ *fakeSubmitter }

func (s stagingResumeSubmitter) SubmitDistribution(context.Context, files.DistributionCommand) (string, error) {
	return "resume-task", nil
}

// Reopen the actual SQLite database and all service state; no in-memory offset
// from the pre-crash service is available to the resumed writer.
func reopenStagingService(t *testing.T, f *fixture, denied bool) *files.Service {
	t.Helper()
	path := f.db.Path()
	if err := f.db.Close(); err != nil {
		t.Fatal(err)
	}
	db := testkit.NewDBAt(t, path, files.Migrations(), audit.Migrations())
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
	authorizer := f.authorizer
	if denied {
		authorizer = authorizerFunc(func(context.Context, auth.Check) error { return auth.ErrDenied })
	}
	service, err := files.NewService(files.Options{DB: db, Repo: repo, Audit: recorder, Authorizer: authorizer,
		Submitter: stagingResumeSubmitter{f.submitter}, Canceller: f.canceller, Uploads: f.upload, StagingDir: f.staging})
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func TestStagingResumeAfterUnacknowledgedWriteAndDatabaseReopen(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("automatic tail recovery requires Linux ownership and link checks")
	}
	for _, kind := range []string{"upload", "chunk", "distribution"} {
		for _, scenario := range []string{"recover", "missing-confirmed", "denied", "wrong-offset", "symlink", "hardlink", "public-file", "public-directory", "dispatched", "bad-checksum"} {
			t.Run(kind+"/"+scenario, func(t *testing.T) {
				f := newFixture(t)
				if err := os.Chmod(f.staging, 0700); err != nil {
					t.Fatal(err)
				}
				ctx := context.Background()
				payload := []byte("confirmed-prefix-and-tail")
				sum := sha256.Sum256(payload)
				digest := hex.EncodeToString(sum[:])
				size := int64(len(payload))
				var id, name string
				var upload files.Upload
				var distribution files.Distribution
				var err error
				if kind == "distribution" {
					distribution, err = f.service.CreateDistribution(ctx, "user-1", "resume-create", []string{"n-a", "n-b"}, "/srv/blob", &size, digest)
					id, name = distribution.ID, distribution.StagingName
				} else {
					upload, err = f.service.CreateUpload(ctx, "user-1", "resume-create", "n-dst", "/srv/blob", &size, digest)
					id, name = upload.ID, upload.StagingName
				}
				if err != nil {
					t.Fatal(err)
				}
				write := func(s *files.Service, offset int64, body []byte) error {
					switch kind {
					case "distribution":
						_, e := s.WriteDistributionContent(ctx, "user-1", "resume-content", id, offset, bytes.NewReader(body))
						return e
					case "chunk":
						_, e := s.AppendChunk(ctx, "user-1", "resume-content", id, offset, body)
						return e
					default:
						_, e := s.WriteUploadContent(ctx, "user-1", "resume-content", id, offset, bytes.NewReader(body))
						return e
					}
				}
				const offset = 9
				if err = write(f.service, 0, payload[:offset]); err != nil {
					t.Fatal(err)
				}
				path := filepath.Join(f.staging, name)
				// Reproduce a write+Sync followed by process loss before progress commit.
				// This is deterministic fault-state construction, not a claimed process kill.
				orphan, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0)
				if err != nil {
					t.Fatal(err)
				}
				if _, err = orphan.Write([]byte("uncommitted-tail-longer-than-payload")); err != nil {
					t.Fatal(err)
				}
				if err = orphan.Sync(); err != nil {
					t.Fatal(err)
				}
				if err = orphan.Close(); err != nil {
					t.Fatal(err)
				}
				requestedOffset := int64(offset)
				switch scenario {
				case "missing-confirmed":
					err = os.Truncate(path, offset-1)
				case "wrong-offset":
					requestedOffset++
				case "public-file":
					err = os.Chmod(path, 0644)
				case "public-directory":
					err = os.Chmod(f.staging, 0755)
				case "symlink":
					original := path + ".original"
					if err = os.Rename(path, original); err == nil {
						err = os.Symlink(original, path)
					}
				case "hardlink":
					err = os.Link(path, path+".alias")
				case "bad-checksum":
					corrupt := bytes.Repeat([]byte("!"), offset)
					var file *os.File
					file, err = os.OpenFile(path, os.O_WRONLY, 0)
					if err == nil {
						_, err = file.WriteAt(corrupt, 0)
						_ = file.Close()
					}
				case "dispatched":
					if kind == "distribution" {
						err = f.service.BindDistributionTask(ctx, distribution, "existing-task", digest)
					} else {
						err = f.service.SubmitUploadTask(ctx, upload, "existing-task", digest)
					}
				}
				if err != nil {
					t.Fatal(err)
				}
				before, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				service := reopenStagingService(t, f, scenario == "denied")
				err = write(service, requestedOffset, payload[offset:])
				if scenario == "denied" && kind == "distribution" {
					// Staging is owner-bound, not a node execution. Preserve the frozen
					// multi-target plan; completion must still reject current revocation.
					if err != nil {
						t.Fatal(err)
					}
					if _, e := service.CompleteDistribution(ctx, "user-1", "denied-complete", id); !errors.Is(e, auth.ErrDenied) {
						t.Fatalf("revoked distribution submitted: %v", e)
					}
					got, e := service.GetDistribution(ctx, "user-1", "denied-read", id)
					if e != nil || got.Status != files.DistributionOpen || got.TaskID != "" {
						t.Fatalf("revoked distribution dispatched: %+v %v", got, e)
					}
					return
				}
				if scenario != "recover" && scenario != "bad-checksum" {
					if err == nil {
						t.Fatal("unsafe staging resume accepted")
					}
					after, e := os.ReadFile(path)
					if e != nil || !bytes.Equal(before, after) {
						t.Fatalf("rejected resume modified evidence: %v", e)
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				written, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				if scenario == "recover" && !bytes.Equal(written, payload) {
					t.Fatalf("resumed bytes=%q", written)
				}
				if kind == "distribution" {
					got, e := service.CompleteDistribution(ctx, "user-1", "resume-complete", id)
					if scenario == "bad-checksum" {
						if !errors.Is(e, files.ErrChecksumMismatch) {
							t.Fatalf("corrupt prefix accepted: %v", e)
						}
						return
					}
					if e != nil || got.Status != files.DistributionDispatching || got.SHA256Actual != digest {
						t.Fatalf("distribution digest/task evidence: %+v %v", got, e)
					}
				} else {
					got, e := service.CompleteUpload(ctx, "user-1", "resume-complete", id)
					if scenario == "bad-checksum" {
						if !errors.Is(e, files.ErrChecksumMismatch) {
							t.Fatalf("corrupt prefix accepted: %v", e)
						}
						return
					}
					if e != nil || got.Status != files.UploadCompleted || got.SHA256Actual != digest {
						t.Fatalf("upload digest evidence: %+v %v", got, e)
					}
				}
			})
		}
	}
}

func TestStagingCompletionAfterCrashChecksAcknowledgedBytes(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("tail recovery requires Linux ownership checks")
	}
	for _, kind := range []string{"upload", "distribution"} {
		for _, missing := range []bool{false, true} {
			name := kind + "/uncommitted-tail"
			if missing {
				name = kind + "/missing-confirmed"
			}
			t.Run(name, func(t *testing.T) {
				f := newFixture(t)
				ctx := context.Background()
				if err := os.Chmod(f.staging, 0700); err != nil {
					t.Fatal(err)
				}
				payload := []byte("complete-acknowledged-bytes")
				size := int64(len(payload))
				sum := sha256.Sum256(payload)
				digest := hex.EncodeToString(sum[:])
				var id, stage string
				if kind == "upload" {
					u, e := f.service.CreateUpload(ctx, "user-1", "complete-create", "n-dst", "/srv/blob", &size, "")
					if e != nil {
						t.Fatal(e)
					}
					id, stage = u.ID, u.StagingName
					if _, e = f.service.AppendChunk(ctx, "user-1", "complete-stage", id, 0, payload); e != nil {
						t.Fatal(e)
					}
				} else {
					d, e := f.service.CreateDistribution(ctx, "user-1", "complete-create", []string{"n-a", "n-b"}, "/srv/blob", &size, "")
					if e != nil {
						t.Fatal(e)
					}
					id, stage = d.ID, d.StagingName
					if _, e = f.service.WriteDistributionContent(ctx, "user-1", "complete-stage", id, 0, bytes.NewReader(payload)); e != nil {
						t.Fatal(e)
					}
				}
				path := filepath.Join(f.staging, stage)
				if missing {
					if e := os.Truncate(path, size-1); e != nil {
						t.Fatal(e)
					}
				} else {
					file, e := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0)
					if e != nil {
						t.Fatal(e)
					}
					if _, e = file.Write([]byte("never-acknowledged")); e != nil {
						t.Fatal(e)
					}
					if e = file.Sync(); e != nil {
						t.Fatal(e)
					}
					_ = file.Close()
				}
				service := reopenStagingService(t, f, false)
				var actual string
				var err error
				if kind == "upload" {
					got, e := service.CompleteUpload(ctx, "user-1", "complete-resume", id)
					actual, err = got.SHA256Actual, e
				} else {
					got, e := service.CompleteDistribution(ctx, "user-1", "complete-resume", id)
					actual, err = got.SHA256Actual, e
				}
				if missing {
					if err == nil {
						t.Fatal("missing acknowledged bytes were submitted without a checksum")
					}
					return
				}
				if err != nil || actual != digest {
					t.Fatalf("completion evidence: sha=%s err=%v", actual, err)
				}
			})
		}
	}
}
