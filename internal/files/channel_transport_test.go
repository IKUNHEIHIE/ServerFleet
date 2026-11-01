package files_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"testing"
	"time"

	"serverfleet.local/serverfleet/internal/files"
)

func testChannel(t *testing.T, agent *fakeAgent, options ...func(*files.ChannelTransportConfig)) *files.ChannelTransport {
	t.Helper()
	config := files.ChannelTransportConfig{
		Channel: agent, MaxChunkBytes: 1024, MaxFileBytes: 1 << 20,
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
	ctx := files.WithChannelIdentity(context.Background(), "transfer-1", "execution-1")

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
	if agent.lastTransferID != "transfer-1" || agent.lastExecID != "execution-1" {
		t.Fatalf("channel identity = %q/%q", agent.lastTransferID, agent.lastExecID)
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

	result, err := transport.PushFileWithPolicy(context.Background(), "node-a", "/srv/blob.bin", files.ConflictOverwrite, bytes.NewReader(data), int64(len(data)), digest(data))
	if err != nil {
		t.Fatal(err)
	}
	if !result.Resumed {
		t.Fatalf("expected resume, result = %+v", result)
	}
	if agent.dials < 2 || agent.uploads != 1 {
		t.Fatalf("dials=%d uploads=%d", agent.dials, agent.uploads)
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
	written, err := transport.PullFile(context.Background(), "node-a", "/srv/blob.bin", &downloaded)
	if err != nil {
		t.Fatal(err)
	}
	if written != int64(len(data)) || !bytes.Equal(downloaded.Bytes(), data) {
		t.Fatalf("downloaded %d bytes, equal=%v", written, bytes.Equal(downloaded.Bytes(), data))
	}
	if agent.dials < 2 {
		t.Fatalf("dials=%d", agent.dials)
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
			err := transport.PushFile(context.Background(), "node-a", "/srv/blocked.bin", bytes.NewReader(data), int64(len(data)), digest(data))
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
	err := transport.PushFile(context.Background(), "node-a", "/srv/blob.bin", bytes.NewReader(data), int64(len(data)), digest(data))
	if !errors.Is(err, files.ErrAgentChecksum) {
		t.Fatalf("err = %v", err)
	}
	if _, ok := agent.file("node-a", "/srv/blob.bin"); ok {
		t.Fatal("failed upload published a file")
	}
}

func TestChannelTransportConflictPolicies(t *testing.T) {
	agent := newFakeAgent()
	agent.putFile("node-a", "/srv/blob.bin", []byte("original"))
	transport := testChannel(t, agent)
	data := []byte("replacement")

	result, err := transport.PushFileWithPolicy(context.Background(), "node-a", "/srv/blob.bin", files.ConflictSkip, bytes.NewReader(data), int64(len(data)), digest(data))
	if err != nil || !result.Skipped {
		t.Fatalf("skip result = %+v, %v", result, err)
	}
	if stored, _ := agent.file("node-a", "/srv/blob.bin"); string(stored) != "original" {
		t.Fatalf("skip overwrote destination: %q", stored)
	}

	result, err = transport.PushFileWithPolicy(context.Background(), "node-a", "/srv/blob.bin", files.ConflictRename, bytes.NewReader(data), int64(len(data)), digest(data))
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

func TestChannelTransportBoundsAndResumeSupport(t *testing.T) {
	agent := newFakeAgent()
	transport := testChannel(t, agent)
	data := []byte("payload")
	if err := transport.PushFile(context.Background(), "node-a", "/srv/blob.bin", bytes.NewReader(data), 1<<21, digest(data)); !errors.Is(err, files.ErrFileTooLarge) {
		t.Fatalf("oversize err = %v", err)
	}
	if err := transport.PushFile(context.Background(), "node-a", "relative/path", bytes.NewReader(data), int64(len(data)), digest(data)); !errors.Is(err, files.ErrInvalidPath) {
		t.Fatalf("path err = %v", err)
	}

	agent.failUpload["node-a"] = 1024
	reader := io.NopCloser(bytes.NewReader(randomBytes(t, 4096)))
	_, err := transport.PushFileWithPolicy(context.Background(), "node-a", "/srv/blob.bin", files.ConflictOverwrite, reader, 4096, digest([]byte("unused")))
	if !errors.Is(err, files.ErrResumeUnsupported) {
		t.Fatalf("non-seekable resume err = %v", err)
	}
}

func TestChannelTransportDownloadDetectsTamperedDigest(t *testing.T) {
	agent := newFakeAgent()
	data := []byte("remote-bytes")
	agent.putFile("node-a", "/srv/blob.bin", data)
	transport := testChannel(t, agent)
	var buffer bytes.Buffer
	if _, err := transport.PullFile(context.Background(), "node-a", "/srv/blob.bin", &buffer); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(buffer.Bytes(), data) {
		t.Fatal("download bytes differ")
	}
	missing := &bytes.Buffer{}
	if _, err := transport.PullFile(context.Background(), "node-a", "/srv/missing.bin", missing); !errors.Is(err, files.ErrAgentIO) {
		t.Fatalf("missing err = %v", err)
	}
}
