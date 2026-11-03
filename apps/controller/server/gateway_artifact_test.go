package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"serverfleet.local/serverfleet/internal/files"
)

func gatewayArtifactTestDigest(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

func newGatewayArtifactTestUploader(t *testing.T) (GatewayArtifactUploader, string) {
	t.Helper()
	root := t.TempDir()
	transport, err := files.NewLocalStagingTransport(root)
	if err != nil {
		t.Fatal(err)
	}
	uploader, err := NewGatewayArtifactUploader(transport)
	if err != nil {
		t.Fatal(err)
	}
	return uploader, root
}

func TestGatewayArtifactUploaderWritesExactBytes(t *testing.T) {
	uploader, root := newGatewayArtifactTestUploader(t)
	content := []byte("server {\n    listen 127.0.0.1:18080;\n}\n")
	destination := "/srv/gateway/artifacts/site.conf"
	if err := uploader.Upload(context.Background(), "task-1", "node-1", destination, content, gatewayArtifactTestDigest(content)); err != nil {
		t.Fatal(err)
	}
	written, err := os.ReadFile(filepath.Join(root, "node-1", "srv", "gateway", "artifacts", "site.conf"))
	if err != nil {
		t.Fatal(err)
	}
	if string(written) != string(content) {
		t.Fatalf("written bytes = %q, want %q", written, content)
	}
}

func TestGatewayArtifactUploaderRejectsDigestMismatchBeforeWrite(t *testing.T) {
	uploader, root := newGatewayArtifactTestUploader(t)
	content := []byte("server { listen 127.0.0.1:18081; }\n")
	if err := uploader.Upload(context.Background(), "task-1", "node-1", "/srv/gateway/artifacts/site.conf",
		content, gatewayArtifactTestDigest([]byte("different"))); !errors.Is(err, ErrGatewayArtifactInvalid) {
		t.Fatalf("err = %v, want ErrGatewayArtifactInvalid", err)
	}
	if _, err := os.Stat(filepath.Join(root, "node-1", "srv", "gateway", "artifacts", "site.conf")); !os.IsNotExist(err) {
		t.Fatalf("artifact was written despite digest mismatch: %v", err)
	}
}

func TestGatewayArtifactUploaderRejectsInvalidContentAndPaths(t *testing.T) {
	uploader, _ := newGatewayArtifactTestUploader(t)
	digest := gatewayArtifactTestDigest([]byte("x"))
	cases := []struct {
		name    string
		taskID  string
		nodeID  string
		path    string
		content []byte
		digest  string
	}{
		{name: "empty task", taskID: "", nodeID: "node-1", path: "/srv/gateway/artifacts/site.conf", content: []byte("x"), digest: digest},
		{name: "empty node", taskID: "task-1", nodeID: "", path: "/srv/gateway/artifacts/site.conf", content: []byte("x"), digest: digest},
		{name: "relative path", taskID: "task-1", nodeID: "node-1", path: "artifacts/site.conf", content: []byte("x"), digest: digest},
		{name: "unclean path", taskID: "task-1", nodeID: "node-1", path: "/srv/../srv/gateway/artifacts/site.conf", content: []byte("x"), digest: digest},
		{name: "empty content", taskID: "task-1", nodeID: "node-1", path: "/srv/gateway/artifacts/site.conf", content: nil, digest: digest},
		{name: "oversize", taskID: "task-1", nodeID: "node-1", path: "/srv/gateway/artifacts/site.conf", content: make([]byte, gatewayArtifactMaxBytes+1), digest: digest},
		{name: "bad digest", taskID: "task-1", nodeID: "node-1", path: "/srv/gateway/artifacts/site.conf", content: []byte("x"), digest: "ABC"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := uploader.Upload(context.Background(), tc.taskID, tc.nodeID, tc.path, tc.content, tc.digest); !errors.Is(err, ErrGatewayArtifactInvalid) {
				t.Fatalf("err = %v, want ErrGatewayArtifactInvalid", err)
			}
		})
	}
}

func TestGatewayArtifactUploaderRequiresTransport(t *testing.T) {
	if _, err := NewGatewayArtifactUploader(nil); !errors.Is(err, ErrGatewayArtifactTransport) {
		t.Fatalf("err = %v, want ErrGatewayArtifactTransport", err)
	}
}
