package files_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"serverfleet.local/serverfleet/internal/files"
)

type slashPathMapper struct{}

func (slashPathMapper) RelativePath(_ context.Context, _, absolute string) (string, error) {
	if !strings.HasPrefix(absolute, "/") || absolute == "/" {
		return "", files.ErrPathMappingUnavailable
	}
	return strings.TrimPrefix(absolute, "/"), nil
}

func (slashPathMapper) AbsolutePath(_ context.Context, _, relative string) (string, error) {
	return "/" + relative, nil
}

type allowChannelAuthorizer struct{}

func (allowChannelAuthorizer) AuthorizeChannel(context.Context, files.ChannelDirection) error {
	return nil
}

func testPlanDigest() string { return strings.Repeat("a", 64) }

func testChannelContext() context.Context {
	ctx := files.WithChannelScope(context.Background(), "task_1", "transfer_1", "exec_1", testPlanDigest())
	return files.WithChannelAuthorizer(ctx, allowChannelAuthorizer{})
}

func testChannel(t *testing.T, agent *fakeAgent, options ...func(*files.ChannelTransportConfig)) *files.ChannelTransport {
	t.Helper()
	config := files.ChannelTransportConfig{
		Channel: agent, Paths: slashPathMapper{}, MaxChunkBytes: 1024, MaxFileBytes: 1 << 20,
		MaxAttempts: 3, FrameTimeout: 2 * time.Second,
	}
	for _, option := range options {
		option(&config)
	}
	transport, err := files.NewChannelTransport(config)
	if err != nil {
		t.Fatal(err)
	}
	return transport
}

func digest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func randomBytes(t *testing.T, size int) []byte {
	t.Helper()
	data := make([]byte, size)
	if _, err := rand.Read(data); err != nil {
		t.Fatal(err)
	}
	return data
}

func TestChannelTransportUploadsAndDownloadsRealBytes(t *testing.T) {
	agent := newFakeAgent()
	transport := testChannel(t, agent)
	data := randomBytes(t, 70_003)
	ctx := testChannelContext()
	result, err := transport.PushFileWithPolicy(ctx, "node-a", "/srv/data/blob.bin", files.ConflictOverwrite, bytes.NewReader(data), int64(len(data)), digest(data))
	if err != nil {
		t.Fatal(err)
	}
	if result.Skipped || result.Bytes != int64(len(data)) || result.SHA256 != digest(data) {
		t.Fatalf("result = %+v", result)
	}
	stored, ok := agent.file("node-a", "/srv/data/blob.bin")
	if !ok || !bytes.Equal(stored, data) {
		t.Fatalf("stored bytes do not match (%d bytes, found=%v)", len(stored), ok)
	}
	if agent.lastTaskID != "task_1" || agent.lastTransferID != "transfer_1" || agent.lastExecID != "exec_1" || agent.lastPlanDigest != testPlanDigest() {
		t.Fatalf("binding = %q/%q/%q/%q", agent.lastTaskID, agent.lastTransferID, agent.lastExecID, agent.lastPlanDigest)
	}

	var downloaded bytes.Buffer
	written, err := transport.PullFile(ctx, "node-a", "/srv/data/blob.bin", &downloaded)
	if err != nil {
		t.Fatal(err)
	}
	if written != int64(len(data)) || !bytes.Equal(downloaded.Bytes(), data) {
		t.Fatalf("downloaded %d bytes, equal=%v", written, bytes.Equal(downloaded.Bytes(), data))
	}
}

func TestChannelTransportUploadResumesAfterDisconnect(t *testing.T) {
	agent := newFakeAgent()
	agent.failUpload["node-a"] = 5_000
	transport := testChannel(t, agent)
	data := randomBytes(t, 20_001)
	result, err := transport.PushFileWithPolicy(testChannelContext(), "node-a", "/srv/blob.bin", files.ConflictOverwrite, bytes.NewReader(data), int64(len(data)), digest(data))
	if err != nil {
		t.Fatal(err)
	}
	if !result.Resumed || agent.dials < 2 || agent.uploads != 1 {
		t.Fatalf("result=%+v dials=%d uploads=%d", result, agent.dials, agent.uploads)
	}
	stored, _ := agent.file("node-a", "/srv/blob.bin")
	if !bytes.Equal(stored, data) {
		t.Fatal("resumed upload bytes differ")
	}
}

func TestChannelTransportDownloadResumesAfterDisconnect(t *testing.T) {
	agent := newFakeAgent()
	data := randomBytes(t, 20_001)
	agent.putFile("node-a", "/srv/blob.bin", data)
	agent.failDownload["node-a"] = 5_000
	transport := testChannel(t, agent)
	var downloaded bytes.Buffer
	written, err := transport.PullFile(testChannelContext(), "node-a", "/srv/blob.bin", &downloaded)
	if err != nil {
		t.Fatal(err)
	}
	if written != int64(len(data)) || !bytes.Equal(downloaded.Bytes(), data) || agent.dials < 2 {
		t.Fatalf("downloaded %d equal=%v dials=%d", written, bytes.Equal(downloaded.Bytes(), data), agent.dials)
	}
}

func TestChannelTransportRejectsAgentErrors(t *testing.T) {
	cases := []struct {
		code string
		want error
	}{
		{"path_rejected", files.ErrAgentPathRejected},
		{"symlink_forbidden", files.ErrAgentSymlink},
		{"insufficient_space", files.ErrInsufficientSpace},
		{"io_error", files.ErrAgentIO},
		{"unauthorized", files.ErrAgentUnauthorized},
	}
	for _, test := range cases {
		t.Run(test.code, func(t *testing.T) {
			agent := newFakeAgent()
			agent.reject("node-a", "/srv/blocked.bin", test.code)
			transport := testChannel(t, agent)
			data := []byte("payload")
			_, err := transport.PushFileWithPolicy(testChannelContext(), "node-a", "/srv/blocked.bin", files.ConflictOverwrite, bytes.NewReader(data), int64(len(data)), digest(data))
			if !errors.Is(err, test.want) {
				t.Fatalf("err = %v, want %v", err, test.want)
			}
		})
	}
}

func TestChannelTransportChecksumMismatch(t *testing.T) {
	agent := newFakeAgent()
	agent.corruptDigest = true
	transport := testChannel(t, agent)
	data := []byte("payload")
	_, err := transport.PushFileWithPolicy(testChannelContext(), "node-a", "/srv/blob.bin", files.ConflictOverwrite, bytes.NewReader(data), int64(len(data)), digest(data))
	if !errors.Is(err, files.ErrAgentChecksum) {
		t.Fatalf("err = %v", err)
	}
	if _, ok := agent.file("node-a", "/srv/blob.bin"); ok {
		t.Fatal("failed upload published a file")
	}
}

func TestChannelTransportUnknownCommitIsNotRetried(t *testing.T) {
	agent := newFakeAgent()
	agent.unknownCommit = true
	transport := testChannel(t, agent)
	data := []byte("payload")
	_, err := transport.PushFileWithPolicy(testChannelContext(), "node-a", "/srv/blob.bin", files.ConflictOverwrite, bytes.NewReader(data), int64(len(data)), digest(data))
	if !errors.Is(err, files.ErrAgentUnknown) {
		t.Fatalf("err = %v", err)
	}
	if agent.dials != 1 {
		t.Fatalf("unknown commit was retried: dials=%d", agent.dials)
	}
}

func TestChannelTransportConflictPolicies(t *testing.T) {
	agent := newFakeAgent()
	agent.putFile("node-a", "/srv/blob.bin", []byte("original"))
	transport := testChannel(t, agent)
	data := []byte("replacement")
	result, err := transport.PushFileWithPolicy(testChannelContext(), "node-a", "/srv/blob.bin", files.ConflictSkip, bytes.NewReader(data), int64(len(data)), digest(data))
	if err != nil || !result.Skipped {
		t.Fatalf("skip result = %+v, %v", result, err)
	}
	if stored, _ := agent.file("node-a", "/srv/blob.bin"); string(stored) != "original" {
		t.Fatalf("skip overwrote destination: %q", stored)
	}
	result, err = transport.PushFileWithPolicy(testChannelContext(), "node-a", "/srv/blob.bin", files.ConflictRename, bytes.NewReader(data), int64(len(data)), digest(data))
	if err != nil {
		t.Fatal(err)
	}
	if result.DestinationPath != "/srv/blob.bin.1" {
		t.Fatalf("rename destination = %q", result.DestinationPath)
	}
	if stored, _ := agent.file("node-a", "/srv/blob.bin.1"); !bytes.Equal(stored, data) {
		t.Fatalf("renamed bytes = %q", stored)
	}
}

func TestChannelTransportBoundsBindingAndPathMapping(t *testing.T) {
	agent := newFakeAgent()
	transport := testChannel(t, agent)
	data := []byte("payload")
	if _, err := transport.PushFileWithPolicy(testChannelContext(), "node-a", "/srv/blob.bin", files.ConflictOverwrite, bytes.NewReader(data), 1<<21, digest(data)); !errors.Is(err, files.ErrFileTooLarge) {
		t.Fatalf("oversize err = %v", err)
	}
	if _, err := transport.PushFileWithPolicy(testChannelContext(), "node-a", "relative/path", files.ConflictOverwrite, bytes.NewReader(data), int64(len(data)), digest(data)); !errors.Is(err, files.ErrInvalidTransfer) {
		t.Fatalf("path err = %v", err)
	}
	if _, err := transport.PushFileWithPolicy(context.Background(), "node-a", "/srv/blob.bin", files.ConflictOverwrite, bytes.NewReader(data), int64(len(data)), digest(data)); !errors.Is(err, files.ErrTaskBindingRequired) {
		t.Fatalf("missing scope err = %v", err)
	}
	noAuth := files.WithChannelScope(context.Background(), "task_1", "transfer_1", "exec_1", testPlanDigest())
	if _, err := transport.PushFileWithPolicy(noAuth, "node-a", "/srv/blob.bin", files.ConflictOverwrite, bytes.NewReader(data), int64(len(data)), digest(data)); !errors.Is(err, files.ErrAuthorizationRequired) {
		t.Fatalf("missing authorizer err = %v", err)
	}
	colon := files.WithChannelAuthorizer(files.WithChannelScope(context.Background(), "task_1", "xfer:1", "exec_1", testPlanDigest()), allowChannelAuthorizer{})
	if _, err := transport.PushFileWithPolicy(colon, "node-a", "/srv/blob.bin", files.ConflictOverwrite, bytes.NewReader(data), int64(len(data)), digest(data)); !errors.Is(err, files.ErrTaskBindingRequired) {
		t.Fatalf("colon transfer id err = %v", err)
	}
	if _, err := files.NewChannelTransport(files.ChannelTransportConfig{Channel: agent}); !errors.Is(err, files.ErrPathMappingUnavailable) {
		t.Fatalf("missing mapper err = %v", err)
	}

	agent.failUpload["node-a"] = 1024
	reader := io.NopCloser(bytes.NewReader(randomBytes(t, 4096)))
	_, err := transport.PushFileWithPolicy(testChannelContext(), "node-a", "/srv/blob.bin", files.ConflictOverwrite, reader, 4096, digest([]byte("unused")))
	if !errors.Is(err, files.ErrResumeUnsupported) {
		t.Fatalf("non-seekable resume err = %v", err)
	}
}

func TestChannelTransportEmptyFileAndDownloadErrors(t *testing.T) {
	agent := newFakeAgent()
	transport := testChannel(t, agent)
	ctx := testChannelContext()
	empty := []byte{}
	if _, err := transport.PushFileWithPolicy(ctx, "node-a", "/srv/empty.bin", files.ConflictOverwrite, bytes.NewReader(empty), 0, digest(empty)); err != nil {
		t.Fatal(err)
	}
	var buffer bytes.Buffer
	if written, err := transport.PullFile(ctx, "node-a", "/srv/empty.bin", &buffer); err != nil || written != 0 {
		t.Fatalf("empty download = %d, %v", written, err)
	}
	if _, err := transport.PullFile(ctx, "node-a", "/srv/missing.bin", &buffer); !errors.Is(err, files.ErrAgentIO) {
		t.Fatalf("missing err = %v", err)
	}
}

func TestChannelTransportOverwriteRejectedByAgentIsExplicit(t *testing.T) {
	agent := newFakeAgent()
	agent.rejectOverwrite = true
	agent.putFile("node-a", "/srv/blob.bin", []byte("original"))
	transport := testChannel(t, agent)
	data := []byte("replacement")
	_, err := transport.PushFileWithPolicy(testChannelContext(), "node-a", "/srv/blob.bin", files.ConflictOverwrite, bytes.NewReader(data), int64(len(data)), digest(data))
	if !errors.Is(err, files.ErrAgentConflict) {
		t.Fatalf("err = %v", err)
	}
}
